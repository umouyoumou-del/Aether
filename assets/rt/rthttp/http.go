package rthttp

import (
	"aether/assets/rt"
	"aether/assets/rt/rtjson"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// HTTPRequest 是服务器收到的请求。
type HTTPRequest struct {
	r    *http.Request
	body string
	read bool
}

func (q *HTTPRequest) Method() string { return q.r.Method }
func (q *HTTPRequest) Path() string   { return q.r.URL.Path }
func (q *HTTPRequest) URL() string    { return q.r.URL.String() }

func (q *HTTPRequest) Query(name string) rt.Opt[string] {
	vals := q.r.URL.Query()
	if !vals.Has(name) {
		return rt.Opt[string]{}
	}
	return rt.Opt[string]{V: vals.Get(name), Ok: true}
}

func (q *HTTPRequest) Param(name string) rt.Opt[string] {
	v := q.r.PathValue(name)
	if v == "" {
		return rt.Opt[string]{}
	}
	return rt.Opt[string]{V: v, Ok: true}
}

func (q *HTTPRequest) Header(name string) rt.Opt[string] {
	v := q.r.Header.Get(name)
	if v == "" {
		return rt.Opt[string]{}
	}
	return rt.Opt[string]{V: v, Ok: true}
}

func (q *HTTPRequest) Body() string {
	if !q.read {
		b, _ := io.ReadAll(io.LimitReader(q.r.Body, 32<<20))
		q.body = string(b)
		q.read = true
	}
	return q.body
}

func (q *HTTPRequest) JSON() any { return rtjson.JSONLoads(q.Body()) }

func (q *HTTPRequest) Form(name string) rt.Opt[string] {
	q.r.ParseForm()
	if !q.r.Form.Has(name) {
		return rt.Opt[string]{}
	}
	return rt.Opt[string]{V: q.r.Form.Get(name), Ok: true}
}

func (q *HTTPRequest) String() string { return "<请求 " + q.r.Method + " " + q.r.URL.Path + ">" }

// HTTPResponse 是响应（服务器返回的或客户端收到的）。
type HTTPResponse struct {
	status  int
	body    string
	headers http.Header
}

func newResp(status int, body, ctype string) *HTTPResponse {
	h := http.Header{}
	if ctype != "" {
		h.Set("Content-Type", ctype)
	}
	return &HTTPResponse{status: status, body: body, headers: h}
}

func (p *HTTPResponse) Status() int    { return p.status }
func (p *HTTPResponse) Text() string   { return p.body }
func (p *HTTPResponse) JSON() any      { return rtjson.JSONLoads(p.body) }
func (p *HTTPResponse) Ok() bool       { return p.status >= 200 && p.status < 300 }
func (p *HTTPResponse) String() string { return fmt.Sprintf("<响应 %d，%d 字节>", p.status, len(p.body)) }

func (p *HTTPResponse) Header(name string) rt.Opt[string] {
	v := p.headers.Get(name)
	if v == "" {
		return rt.Opt[string]{}
	}
	return rt.Opt[string]{V: v, Ok: true}
}

func (p *HTTPResponse) SetHeader(name, value string) *HTTPResponse {
	p.headers.Set(name, value)
	return p
}

func HTTPText(body string, status int) *HTTPResponse {
	return newResp(status, body, "text/plain; charset=utf-8")
}

func HTTPHTML(body string, status int) *HTTPResponse {
	return newResp(status, body, "text/html; charset=utf-8")
}

func HTTPJSON(v any, status int) *HTTPResponse {
	return newResp(status, rtjson.JSONDumps(v, 0, false), "application/json; charset=utf-8")
}

func HTTPStatus(code int, msg string) *HTTPResponse {
	if msg == "" {
		msg = http.StatusText(code)
	}
	return newResp(code, msg, "text/plain; charset=utf-8")
}

func HTTPRedirect(url string) *HTTPResponse {
	r := newResp(http.StatusFound, "", "")
	r.headers.Set("Location", url)
	return r
}

// HTTPServer 是一个简单的路由服务器。
type HTTPServer struct {
	mux    *http.ServeMux
	srv    *http.Server
	mu     sync.Mutex
	Quiet  bool
}

func NewHTTPServer() *HTTPServer {
	return &HTTPServer{mux: http.NewServeMux()}
}

func (s *HTTPServer) Handle(method, pattern string, h func(*HTTPRequest) *HTTPResponse) {
	key := pattern
	if method != "" && method != "*" {
		key = strings.ToUpper(method) + " " + pattern
	}
	s.mux.HandleFunc(key, func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		resp := s.call(h, r)
		for k, vs := range resp.headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.status)
		io.WriteString(w, resp.body)
		if !s.Quiet {
			fmt.Fprintf(os.Stderr, "[http] %s %s → %d（%s）\n", r.Method, r.URL.Path, resp.status, rt.FmtDur(time.Since(start)))
		}
	})
}

func (s *HTTPServer) call(h func(*HTTPRequest) *HTTPResponse, r *http.Request) (resp *HTTPResponse) {
	defer func() {
		if rec := recover(); rec != nil {
			pcs := rt.PCsOf(rec)
			if pcs == nil {
				pcs = rt.Callers(3)
			}
			rt.Flush()
			os.Stderr.WriteString("[http] 处理请求时出现异常：\n" + rt.FormatTraceback(rec, pcs))
			resp = HTTPStatus(500, "服务器内部错误："+rt.ExcMessage(rt.Catch(rec)))
		}
	}()
	resp = h(&HTTPRequest{r: r})
	if resp == nil {
		resp = HTTPStatus(204, "")
	}
	return resp
}

func (s *HTTPServer) Get(pattern string, h func(*HTTPRequest) *HTTPResponse)  { s.Handle("GET", pattern, h) }
func (s *HTTPServer) Post(pattern string, h func(*HTTPRequest) *HTTPResponse) { s.Handle("POST", pattern, h) }

func (s *HTTPServer) Static(prefix, dir string) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	s.mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.Dir(dir))))
}

func normAddr(addr string) string {
	if addr == "" {
		return ":8080"
	}
	if !strings.Contains(addr, ":") {
		return ":" + addr
	}
	return addr
}

// Listen 启动服务器并阻塞。
func (s *HTTPServer) Listen(addr string) {
	a := normAddr(addr)
	s.mu.Lock()
	s.srv = &http.Server{Addr: a, Handler: s.mux}
	srv := s.srv
	s.mu.Unlock()
	rt.Flush()
	fmt.Fprintf(os.Stderr, "[http] 服务器运行在 http://localhost%s\n", a)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		rt.Raise("IOError", "服务器启动失败：%v", err)
	}
}

// Start 在后台启动服务器，返回其地址（addr 为 ":0" 时自动选择空闲端口）。
func (s *HTTPServer) Start(addr string) string {
	l, err := net.Listen("tcp", normAddr(addr))
	if err != nil {
		rt.Raise("IOError", "服务器启动失败：%v", err)
	}
	s.mu.Lock()
	s.srv = &http.Server{Handler: s.mux}
	srv := s.srv
	s.mu.Unlock()
	go srv.Serve(l)
	port := l.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (s *HTTPServer) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}
}

func (s *HTTPServer) SetQuiet(q bool) { s.Quiet = q }

func httpDo(method, url, body, ctype string, timeout float64) *HTTPResponse {
	client := &http.Client{Timeout: time.Duration(timeout * float64(time.Second))}
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		rt.Raise("ValueError", "无效的请求：%v", err)
	}
	if ctype != "" && body != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "Aether/0.1")
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "Timeout") || strings.Contains(err.Error(), "deadline") {
			rt.Raise("TimeoutError", "请求超时：%s", url)
		}
		rt.Raise("IOError", "请求失败：%v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		rt.Raise("IOError", "读取响应失败：%v", err)
	}
	return &HTTPResponse{status: resp.StatusCode, body: string(b), headers: resp.Header}
}

func HTTPGet(url string, timeout float64) *HTTPResponse {
	return httpDo("GET", url, "", "", timeout)
}

func HTTPPost(url, body, ctype string, timeout float64) *HTTPResponse {
	return httpDo("POST", url, body, ctype, timeout)
}
