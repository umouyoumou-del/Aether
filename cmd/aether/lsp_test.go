package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lspClient 通过管道驱动语言服务器。
type lspClient struct {
	t      *testing.T
	w      io.Writer
	r      *bufio.Reader
	nextID int
	notes  []map[string]any // 收到的通知
}

func (c *lspClient) send(v map[string]any) {
	v["jsonrpc"] = "2.0"
	data, _ := json.Marshal(v)
	fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

// request 发送请求并等待响应（期间收到的通知保存在 notes 中）。
func (c *lspClient) request(method string, params any) any {
	c.nextID++
	id := c.nextID
	c.send(map[string]any{"id": id, "method": method, "params": params})
	for {
		raw := c.readRaw()
		var m map[string]any
		json.Unmarshal(raw, &m)
		if m["id"] == nil {
			c.notes = append(c.notes, m)
			continue
		}
		if int(m["id"].(float64)) != id {
			continue
		}
		if e, ok := m["error"]; ok {
			c.t.Fatalf("%s 返回错误：%v", method, e)
		}
		return m["result"]
	}
}

func (c *lspClient) readRaw() []byte {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("读取失败：%v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		fmt.Sscanf(line, "Content-Length: %d", &length)
	}
	buf := make([]byte, length)
	io.ReadFull(c.r, buf)
	return buf
}

// waitDiagnostics 等待指定文档的诊断通知。
func (c *lspClient) waitDiagnostics(uri string) []any {
	for i := 0; i < 50; i++ {
		for k := len(c.notes) - 1; k >= 0; k-- {
			n := c.notes[k]
			if n["method"] == "textDocument/publishDiagnostics" {
				p := n["params"].(map[string]any)
				if p["uri"] == uri {
					c.notes = append(c.notes[:k], c.notes[k+1:]...)
					return p["diagnostics"].([]any)
				}
			}
		}
		raw := c.readRaw()
		var m map[string]any
		json.Unmarshal(raw, &m)
		c.notes = append(c.notes, m)
	}
	c.t.Fatal("没有收到诊断")
	return nil
}

func TestLSP(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "helper.ae"), []byte("def twice(x: int) -> int:\n    return x * 2\n"), 0o644)
	path := filepath.Join(dir, "main.ae")
	uri := pathToURI(path)
	src := "import helper\n\nclass 用户:\n    名字: str\n    年龄: int = 0\n    def 问候(self) -> str:\n        return f\"你好，{self.名字}\"\n\nu = 用户(\"小明\", 3)\nprint(u.问候(), helper.twice(u.年龄))\n"

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go runLSP(inR, outW)
	c := &lspClient{t: t, w: inW, r: bufio.NewReader(outR)}

	res := c.request("initialize", map[string]any{"capabilities": map[string]any{}})
	caps := res.(map[string]any)["capabilities"].(map[string]any)
	if caps["hoverProvider"] != true {
		t.Fatalf("capabilities：%v", caps)
	}
	c.send(map[string]any{"method": "initialized", "params": map[string]any{}})
	c.send(map[string]any{"method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "aether", "version": 1, "text": src},
	}})
	if d := c.waitDiagnostics(uri); len(d) != 0 {
		t.Fatalf("意外的诊断：%v", d)
	}
	at := func(line, ch int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": ch}}
	}
	// 悬停：第 10 行（从 0 开始为 9）的 u.问候
	hover := c.request("textDocument/hover", at(9, 9))
	if h := fmt.Sprint(hover); !strings.Contains(h, "def 用户.问候() -> str") {
		t.Errorf("悬停：%s", h)
	}
	// 跳转到另一个文件中的定义：helper.twice
	def := c.request("textDocument/definition", at(9, 24))
	if d := fmt.Sprint(def); !strings.Contains(d, "helper.ae") || !strings.Contains(d, "character:4") {
		t.Errorf("定义：%s", d)
	}
	// 大纲
	syms := c.request("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": uri}})
	if s := fmt.Sprint(syms); !strings.Contains(s, "用户") || !strings.Contains(s, "问候") {
		t.Errorf("大纲：%s", s)
	}
	// 修改：在末尾输入 “u.” 后请求补全，同时应该报告语法错误
	text2 := src + "u.\n"
	c.send(map[string]any{"method": "textDocument/didChange", "params": map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": text2}},
	}})
	comp := c.request("textDocument/completion", at(10, 2))
	if s := fmt.Sprint(comp); !strings.Contains(s, "名字") || !strings.Contains(s, "问候") {
		t.Errorf("补全：%s", s)
	}
	// 嵌套在调用中的不完整输入：print(u.名字.
	text3 := src + "print(u.名字.\n"
	c.send(map[string]any{"method": "textDocument/didChange", "params": map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": text3}},
	}})
	comp = c.request("textDocument/completion", at(10, 12))
	if s := fmt.Sprint(comp); !strings.Contains(s, "upper") || !strings.Contains(s, "split") {
		t.Errorf("字符串的补全：%s", s)
	}
	time.Sleep(400 * time.Millisecond) // 等待防抖后的检查
	if d := c.waitDiagnostics(uri); len(d) == 0 {
		t.Errorf("应该报告语法错误")
	}
	c.request("shutdown", nil)
	c.send(map[string]any{"method": "exit"})
	inW.Close()
}

func TestURIPath(t *testing.T) {
	if p := uriToPath("file:///e%3A/aether/x.ae"); p != filepath.FromSlash("e:/aether/x.ae") {
		t.Errorf("uriToPath：%q", p)
	}
	if u := pathToURI(`E:\aether\中文 目录\x.ae`); u != "file:///e%3A/aether/%E4%B8%AD%E6%96%87%20%E7%9B%AE%E5%BD%95/x.ae" {
		t.Errorf("pathToURI：%q", u)
	}
	if c := utf16ToRuneCol("a😀b", 3); c != 3 {
		t.Errorf("utf16ToRuneCol：%d", c)
	}
	if c := runeColToUTF16("a😀b", 3); c != 3 {
		t.Errorf("runeColToUTF16：%d", c)
	}
}
