package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"aether/assets"
	"aether/compiler"
)

// cmdLSP 运行语言服务器（Language Server Protocol），通过标准输入输出与编辑器通信：
// 实时诊断、悬停显示类型、跳转到定义、补全、大纲。
func cmdLSP(args []string) int {
	return runLSP(os.Stdin, os.Stdout)
}

func runLSP(r io.Reader, w io.Writer) int {
	s := &lspServer{docs: map[string]*lspDoc{}, out: bufio.NewWriter(w)}
	in := bufio.NewReader(r)
	for {
		msg, err := readLSPMessage(in)
		if err != nil {
			if err == io.EOF {
				return 0
			}
			fmt.Fprintln(os.Stderr, "aether lsp:", err)
			return 1
		}
		if s.handle(msg) {
			return 0
		}
	}
}

type lspMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func readLSPMessage(r *bufio.Reader) (*lspMessage, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("无效的 Content-Length：%q", v)
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("缺少 Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	var m lspMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type lspDoc struct {
	uri      string
	path     string
	text     string
	version  int
	analysis *compiler.Analysis // 最近一次成功完成类型检查的结果（用于悬停、跳转、补全）
	timer    *time.Timer
}

type lspServer struct {
	mu       sync.Mutex
	docs     map[string]*lspDoc
	outMu    sync.Mutex
	out      *bufio.Writer
	shutdown bool
}

func (s *lspServer) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(data))
	s.out.Write(data)
	s.out.Flush()
}

func (s *lspServer) reply(id json.RawMessage, result any) {
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *lspServer) replyError(id json.RawMessage, code int, msg string) {
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func (s *lspServer) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// handle 处理一条消息；返回 true 表示应当退出。
func (s *lspServer) handle(m *lspMessage) (exit bool) {
	isRequest := len(m.ID) > 0 && string(m.ID) != "null"
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "aether lsp: 处理 %s 时出错：%v\n", m.Method, r)
			if isRequest {
				s.replyError(m.ID, -32603, fmt.Sprint(r))
			}
		}
	}()
	switch m.Method {
	case "initialize":
		s.reply(m.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":       map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}},
				"hoverProvider":          true,
				"definitionProvider":     true,
				"documentSymbolProvider": true,
				"completionProvider":     map[string]any{"triggerCharacters": []string{"."}},
			},
			"serverInfo": map[string]any{"name": "aether", "version": compiler.Version},
		})
	case "initialized", "$/cancelRequest", "$/setTrace", "workspace/didChangeConfiguration", "workspace/didChangeWatchedFiles":
	case "shutdown":
		s.shutdown = true
		s.reply(m.ID, nil)
	case "exit":
		return true
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Text    string `json:"text"`
				Version int    `json:"version"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		doc := &lspDoc{uri: p.TextDocument.URI, path: uriToPath(p.TextDocument.URI), text: p.TextDocument.Text, version: p.TextDocument.Version}
		s.mu.Lock()
		s.docs[doc.uri] = doc
		s.mu.Unlock()
		s.analyze(doc.uri)
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int    `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		json.Unmarshal(m.Params, &p)
		s.mu.Lock()
		doc := s.docs[p.TextDocument.URI]
		if doc != nil && len(p.ContentChanges) > 0 {
			doc.text = p.ContentChanges[len(p.ContentChanges)-1].Text
			doc.version = p.TextDocument.Version
			// 输入停顿一会儿再检查
			if doc.timer != nil {
				doc.timer.Stop()
			}
			uri := doc.uri
			doc.timer = time.AfterFunc(250*time.Millisecond, func() { s.analyze(uri) })
		}
		s.mu.Unlock()
	case "textDocument/didSave":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		// 保存后重新检查所有打开的文档（它们可能导入了刚保存的模块）
		s.mu.Lock()
		var uris []string
		for u := range s.docs {
			uris = append(uris, u)
		}
		s.mu.Unlock()
		for _, u := range uris {
			s.analyze(u)
		}
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		s.mu.Lock()
		if doc := s.docs[p.TextDocument.URI]; doc != nil && doc.timer != nil {
			doc.timer.Stop()
		}
		delete(s.docs, p.TextDocument.URI)
		s.mu.Unlock()
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}})
	case "textDocument/hover":
		doc, line, col := s.position(m.Params)
		if doc == nil || doc.analysis == nil {
			s.reply(m.ID, nil)
			return
		}
		text := doc.analysis.Hover(line, col)
		if text == "" {
			s.reply(m.ID, nil)
			return
		}
		s.reply(m.ID, map[string]any{"contents": map[string]any{"kind": "markdown", "value": text}})
	case "textDocument/definition":
		doc, line, col := s.position(m.Params)
		if doc == nil || doc.analysis == nil {
			s.reply(m.ID, nil)
			return
		}
		path, dl, dc, ok := doc.analysis.Definition(line, col)
		if !ok {
			s.reply(m.ID, nil)
			return
		}
		uri := pathToURI(path)
		text := ""
		if path == doc.path {
			uri, text = doc.uri, doc.text
		} else if data, err := os.ReadFile(path); err == nil {
			text = string(data)
		}
		ln := lineOf(text, dl)
		start := runeColToUTF16(ln, dc)
		s.reply(m.ID, map[string]any{"uri": uri, "range": lspRange(dl-1, start, dl-1, start+wordLen(ln, dc))})
	case "textDocument/documentSymbol":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		s.mu.Lock()
		doc := s.docs[p.TextDocument.URI]
		s.mu.Unlock()
		if doc == nil || doc.analysis == nil {
			s.reply(m.ID, []any{})
			return
		}
		s.reply(m.ID, docSymbols(doc.text, doc.analysis.Outline()))
	case "textDocument/completion":
		doc, line, col := s.position(m.Params)
		if doc == nil {
			s.reply(m.ID, []any{})
			return
		}
		s.reply(m.ID, s.complete(doc, line, col))
	default:
		if isRequest {
			s.replyError(m.ID, -32601, "不支持的方法 "+m.Method)
		}
	}
	return false
}

// position 解析 textDocument/position 参数，返回文档与 Aether 的行、列（从 1 开始，列按字符计）。
func (s *lspServer) position(raw json.RawMessage) (*lspDoc, int, int) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}
	json.Unmarshal(raw, &p)
	s.mu.Lock()
	doc := s.docs[p.TextDocument.URI]
	s.mu.Unlock()
	if doc == nil {
		return nil, 0, 0
	}
	ln := lineOf(doc.text, p.Position.Line+1)
	return doc, p.Position.Line + 1, utf16ToRuneCol(ln, p.Position.Character)
}

// analyze 检查文档并发布诊断。
func (s *lspServer) analyze(uri string) {
	s.mu.Lock()
	doc := s.docs[uri]
	if doc == nil {
		s.mu.Unlock()
		return
	}
	text, path, version := doc.text, doc.path, doc.version
	s.mu.Unlock()
	a := compiler.Analyze(path, []byte(text), assets.Lib())
	s.mu.Lock()
	if doc.version != version {
		// 检查期间文档又被修改了：等下一次
		s.mu.Unlock()
		return
	}
	if a.Checked() {
		doc.analysis = a
	}
	s.mu.Unlock()
	diags := []any{}
	for _, d := range a.Diags {
		ln := lineOf(text, d.Pos.Line)
		line := d.Pos.Line - 1
		if line < 0 {
			line = 0
		}
		start := runeColToUTF16(ln, d.Pos.Col)
		msg := d.Msg
		for _, n := range d.Notes {
			msg += "\n提示：" + n
		}
		sev := 1
		if d.Warning {
			sev = 2
		}
		diags = append(diags, map[string]any{
			"range":    lspRange(line, start, line, start+wordLen(ln, d.Pos.Col)),
			"severity": sev,
			"source":   "aether",
			"message":  msg,
		})
	}
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": diags, "version": version})
}

// complete 计算补全项：“.” 之后是成员，否则是当前位置可用的名字。
func (s *lspServer) complete(doc *lspDoc, line, col int) []any {
	s.mu.Lock()
	text, path, prev := doc.text, doc.path, doc.analysis
	s.mu.Unlock()
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return []any{}
	}
	rs := []rune(strings.TrimRight(lines[line-1], "\r"))
	cur := col - 1
	if cur > len(rs) {
		cur = len(rs)
	}
	j := cur
	for j > 0 && isIdentRune(rs[j-1]) {
		j--
	}
	var cs []compiler.Completion
	if j > 0 && rs[j-1] == '.' {
		cs = memberCompletions(path, lines, line, rs, j, cur)
		if cs == nil && prev != nil {
			cs = prev.MembersBefore(line, j)
		}
	} else if prev != nil {
		cs = prev.NamesAt(line, col)
	}
	out := []any{}
	for _, c := range cs {
		out = append(out, map[string]any{"label": c.Label, "kind": completionKind(c.Kind), "detail": c.Detail})
	}
	return out
}

// memberCompletions 求第 line 行中 “.”（位于第 dot 列，rs 是这一行）之前的表达式的成员。
// 正在输入的行往往不完整，依次尝试几种修补，直到能够通过语法分析：
// 去掉 “.前缀”；只保留 “.” 之前的部分并补齐括号；整行只保留 “.” 之前的表达式。
func memberCompletions(path string, lines []string, line int, rs []rune, dot, cur int) []compiler.Completion {
	before := rs[:dot-1]
	indent := 0
	for indent < len(rs) && (rs[indent] == ' ' || rs[indent] == '\t') {
		indent++
	}
	recv := receiverStart(before)
	nextIndented := false
	for k := line; k < len(lines); k++ {
		l := strings.TrimRight(lines[k], "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		nextIndented = len(l)-len(strings.TrimLeft(l, " \t")) > indent
		break
	}
	prefix := string(rs[:indent])
	type try struct {
		text   string
		dotCol int // 修补后的行中 “.” 所在的列
	}
	tries := []try{
		{string(before) + string(rs[cur:]), dot},
		{string(before) + closers(before), dot},
	}
	if recv >= indent && recv < len(before) {
		expr := before[recv:]
		if nextIndented {
			// 下一行是代码块：保持语句头的形式
			tries = append(tries, try{prefix + "while " + string(expr) + ":", indent + len("while ") + len(expr) + 1})
		} else {
			tries = append(tries, try{prefix + string(expr), indent + len(expr) + 1})
		}
	}
	for _, t := range tries {
		patched := append([]string(nil), lines...)
		patched[line-1] = t.text
		a := compiler.Analyze(path, []byte(strings.Join(patched, "\n")), assets.Lib())
		if !a.Checked() {
			continue
		}
		if cs := a.MembersBefore(line, t.dotCol); cs != nil {
			return cs
		}
	}
	return nil
}

// receiverStart 返回 “.” 之前的表达式（名字、成员访问、调用、下标组成的链）在 rs 中开始的位置。
func receiverStart(rs []rune) int {
	i := len(rs)
	for i > 0 {
		c := rs[i-1]
		switch {
		case isIdentRune(c) || c == '.':
			i--
		case c == ')' || c == ']':
			depth := 0
			k := i - 1
			for ; k >= 0; k-- {
				if rs[k] == ')' || rs[k] == ']' {
					depth++
				} else if rs[k] == '(' || rs[k] == '[' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if k < 0 {
				return i
			}
			i = k
		case c == '"':
			k := i - 2
			for k >= 0 && rs[k] != '"' {
				k--
			}
			if k < 0 {
				return i
			}
			i = k
		default:
			return i
		}
	}
	return i
}

// closers 返回补齐 rs 中未闭合的括号所需的字符。
func closers(rs []rune) string {
	var stack []rune
	inStr := false
	for _, c := range rs {
		if inStr {
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			stack = append(stack, ')')
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ')', ']', '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	var b strings.Builder
	for k := len(stack) - 1; k >= 0; k-- {
		b.WriteRune(stack[k])
	}
	return b.String()
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func completionKind(k string) int {
	switch k {
	case "method":
		return 2
	case "function":
		return 3
	case "field":
		return 5
	case "variable":
		return 6
	case "class":
		return 7
	case "interface":
		return 8
	case "module":
		return 9
	case "property":
		return 10
	case "enum":
		return 13
	case "keyword":
		return 14
	case "enumMember":
		return 20
	case "constant":
		return 21
	case "struct":
		return 22
	case "typeParameter":
		return 25
	}
	return 1
}

func symbolKind(k string) int {
	switch k {
	case "module":
		return 2
	case "class":
		return 5
	case "method":
		return 6
	case "property":
		return 7
	case "field":
		return 8
	case "enum":
		return 10
	case "interface":
		return 11
	case "function":
		return 12
	case "variable":
		return 13
	case "constant":
		return 14
	case "enumMember":
		return 22
	case "struct":
		return 23
	}
	return 13
}

func docSymbols(text string, syms []compiler.DocSymbol) []any {
	out := []any{}
	for _, d := range syms {
		ln := lineOf(text, d.Line)
		start := runeColToUTF16(ln, d.Col)
		sel := lspRange(d.Line-1, start, d.Line-1, start+wordLen(ln, d.Col))
		end := d.EndLine
		if end < d.Line {
			end = d.Line
		}
		endLn := lineOf(text, end)
		item := map[string]any{
			"name":           d.Name,
			"kind":           symbolKind(d.Kind),
			"range":          lspRange(d.Line-1, 0, end-1, utf16Len(endLn)),
			"selectionRange": sel,
		}
		if d.Detail != "" {
			item["detail"] = d.Detail
		}
		if len(d.Children) > 0 {
			item["children"] = docSymbols(text, d.Children)
		}
		out = append(out, item)
	}
	return out
}

// ===== 位置与路径转换 =====

func lspRange(l1, c1, l2, c2 int) map[string]any {
	return map[string]any{
		"start": map[string]int{"line": l1, "character": c1},
		"end":   map[string]int{"line": l2, "character": c2},
	}
}

func lineOf(text string, n int) string {
	if n < 1 {
		return ""
	}
	for i := 1; i < n; i++ {
		k := strings.IndexByte(text, '\n')
		if k < 0 {
			return ""
		}
		text = text[k+1:]
	}
	if k := strings.IndexByte(text, '\n'); k >= 0 {
		text = text[:k]
	}
	return strings.TrimRight(text, "\r")
}

// utf16ToRuneCol 把 LSP 的列（UTF-16 码元，从 0 开始）转换为 Aether 的列（字符，从 1 开始）。
func utf16ToRuneCol(line string, ch int) int {
	units, col := 0, 1
	for _, r := range line {
		if units >= ch {
			break
		}
		units += len(utf16.Encode([]rune{r}))
		col++
	}
	return col
}

// runeColToUTF16 是 utf16ToRuneCol 的逆转换。
func runeColToUTF16(line string, col int) int {
	units, i := 0, 1
	for _, r := range line {
		if i >= col {
			break
		}
		units += len(utf16.Encode([]rune{r}))
		i++
	}
	return units
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// wordLen 返回从第 col 个字符开始的名字的长度（UTF-16 码元），没有名字时为 1。
func wordLen(line string, col int) int {
	rs := []rune(line)
	i := col - 1
	if i < 0 || i >= len(rs) {
		return 1
	}
	n := 0
	for j := i; j < len(rs) && isIdentRune(rs[j]); j++ {
		n += len(utf16.Encode([]rune{rs[j]}))
	}
	if n == 0 {
		_, size := utf8.DecodeRuneInString(string(rs[i:]))
		_ = size
		return 1
	}
	return n
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	// Windows：/e:/aether/x.ae
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if len(p) >= 2 && p[1] == ':' {
		// 与 VS Code 相同：盘符小写，冒号编码为 %3A
		p = strings.ToLower(p[:1]) + "%3A" + p[2:]
	}
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		if strings.HasSuffix(seg, "%3A") && len(seg) == 4 {
			parts = append(parts, seg)
			continue
		}
		parts = append(parts, url.PathEscape(seg))
	}
	return "file:///" + strings.TrimPrefix(strings.Join(parts, "/"), "/")
}
