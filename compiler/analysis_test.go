package compiler

import (
	"strings"
	"testing"

	"aether/assets"
)

const analysisSrc = `import math

class Point:
    x: float
    y: float = 0.0
    def dist(self, o: Point) -> float:
        return math.sqrt((self.x - o.x) ** 2 + (self.y - o.y) ** 2)

type Pts = list[Point]

def far(ps: Pts, limit: float = 1.0) -> list[Point]:
    origin = Point(0.0)
    name: str? = null
    if name != null:
        print(name)
    return [p for p in ps if p.dist(origin) > limit]

enum Color:
    Red
    Green

pts = [Point(3.0, 4.0)]
print(far(pts), Color.Red, pts[0].x)
`

// pos 返回 needle 在源码中第 nth 次出现的位置（行、列从 1 开始，列按字符计）。
func pos(t *testing.T, src, needle string, nth int) (int, int) {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		rs := []rune(line)
		for j := range rs {
			if strings.HasPrefix(string(rs[j:]), needle) {
				nth--
				if nth == 0 {
					return i + 1, j + 1
				}
			}
		}
	}
	t.Fatalf("找不到 %q", needle)
	return 0, 0
}

func TestAnalysisHoverDefinition(t *testing.T) {
	a := Analyze("analysis_sample.ae", []byte(analysisSrc), assets.Lib())
	if len(a.Diags) > 0 {
		t.Fatalf("意外的诊断：%v", a.Diags[0].Msg)
	}
	cases := []struct {
		needle string
		nth    int
		want   string
	}{
		{"dist(origin)", 1, "def Point.dist(o: Point) -> float"},
		{"origin)", 1, "origin: Point"},
		{"name)", 1, "name: str?"},
		{"ps if", 1, "(参数) ps: list[Point]"},
		{"Pts,", 1, "type Pts = list[Point]"},
		{"Point(0.0)", 1, "class Point"},
		{"sqrt", 1, "def sqrt(x: float) -> float"},
		{"far(pts)", 1, "def far(ps: list[Point], limit: float = …) -> list[Point]"},
		{"Red,", 1, "Color.Red"},
		{"x)", 2, "(字段) Point.x: float"},
		{"math.sqrt", 1, "import math"},
	}
	for _, c := range cases {
		l, col := pos(t, analysisSrc, c.needle, c.nth)
		h := a.Hover(l, col)
		if !strings.Contains(h, c.want) {
			t.Errorf("悬停 %q（%d:%d）：期望包含 %q，得到 %q", c.needle, l, col, c.want, h)
		}
	}
	// 窄化后的类型
	l, col := pos(t, analysisSrc, "name)", 1)
	if h := a.Hover(l, col); !strings.Contains(h, "窄化为 str") {
		t.Errorf("窄化：%q", h)
	}
	// 跳转到定义
	l, col = pos(t, analysisSrc, "dist(origin)", 1)
	if _, dl, _, ok := a.Definition(l, col); !ok || dl != 6 {
		t.Errorf("dist 的定义应在第 6 行，得到 %d %v", dl, ok)
	}
	l, col = pos(t, analysisSrc, "Pts,", 1)
	if _, dl, _, ok := a.Definition(l, col); !ok || dl != 9 {
		t.Errorf("Pts 的定义应在第 9 行，得到 %d %v", dl, ok)
	}
}

func TestAnalysisCompletionOutline(t *testing.T) {
	a := Analyze("analysis_sample.ae", []byte(analysisSrc), assets.Lib())
	labels := func(cs []Completion) string {
		var s []string
		for _, c := range cs {
			s = append(s, c.Label)
		}
		return " " + strings.Join(s, " ") + " "
	}
	// pts[0].x 之前的 pts[0] —— 用 “pts” 之后的位置测试列表的成员
	l, col := pos(t, analysisSrc, "pts[0]", 1)
	ms := labels(a.MembersAt(l, col+3))
	for _, want := range []string{" append ", " sort ", " map ", " length "} {
		if !strings.Contains(ms, want) {
			t.Errorf("列表成员中缺少 %q：%s", want, ms)
		}
	}
	l, col = pos(t, analysisSrc, "origin)", 1)
	ms = labels(a.MembersAt(l, col+6))
	for _, want := range []string{" x ", " y ", " dist "} {
		if !strings.Contains(ms, want) {
			t.Errorf("Point 的成员中缺少 %q：%s", want, ms)
		}
	}
	l, col = pos(t, analysisSrc, "math.sqrt", 1)
	if ms := labels(a.MembersAt(l, col+4)); !strings.Contains(ms, " sqrt ") || !strings.Contains(ms, " pi ") {
		t.Errorf("math 的成员：%s", ms)
	}
	// “.” 之前是下标、调用的结果
	l, col = pos(t, analysisSrc, "].x", 1)
	if ms := labels(a.MembersBefore(l, col+1)); !strings.Contains(ms, " dist ") {
		t.Errorf("pts[0]. 的成员：%s", ms)
	}
	l, col = pos(t, analysisSrc, "far(pts)", 1)
	if ms := labels(a.MembersBefore(l, col+len("far(pts)"))); !strings.Contains(ms, " append ") {
		t.Errorf("far(pts). 的成员：%s", ms)
	}
	l, _ = pos(t, analysisSrc, "return [p", 1)
	names := labels(a.NamesAt(l, 5))
	for _, want := range []string{" ps ", " limit ", " origin ", " far ", " Point ", " print ", " return "} {
		if !strings.Contains(names, want) {
			t.Errorf("可用名字中缺少 %q", want)
		}
	}
	out := a.Outline()
	var kinds []string
	for _, s := range out {
		kinds = append(kinds, s.Kind+":"+s.Name)
	}
	got := strings.Join(kinds, ",")
	if got != "class:Point,class:Pts,function:far,enum:Color" {
		t.Errorf("大纲：%s", got)
	}
	if len(out[0].Children) != 3 {
		t.Errorf("Point 的大纲应有 3 个成员，得到 %d", len(out[0].Children))
	}
}

func TestAnalysisDiagnostics(t *testing.T) {
	src := "x: int = \"a\"\ndef f() -> int:\n    pass\n"
	a := Analyze("analysis_bad.ae", []byte(src), assets.Lib())
	if len(a.Diags) != 2 {
		for _, d := range a.Diags {
			t.Log(d.Pos.Line, d.Msg)
		}
		t.Fatalf("期望 2 个诊断，得到 %d", len(a.Diags))
	}
	// 语法错误：仍然返回诊断，不崩溃
	a = Analyze("analysis_bad.ae", []byte("def f(:\n  x = \n"), assets.Lib())
	if len(a.Diags) == 0 {
		t.Fatal("语法错误应产生诊断")
	}
}
