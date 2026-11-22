package compiler

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type Diag struct {
	Pos     Pos
	Msg     string
	Warning bool
	Notes   []string
}

// Diagnostics 收集编译期错误。
type Diagnostics struct {
	List []*Diag
	seen map[string]bool
}

// ErrLimit 是停止报告前的最大错误数。
const ErrLimit = 25

type tooManyErrors struct{}

func (d *Diagnostics) add(pos Pos, warn bool, msg string) *Diag {
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	key := fmt.Sprintf("%p:%d:%d:%s", pos.File, pos.Line, pos.Col, msg)
	if d.seen[key] {
		return &Diag{}
	}
	d.seen[key] = true
	dg := &Diag{Pos: pos, Msg: msg, Warning: warn}
	d.List = append(d.List, dg)
	if !warn && d.ErrorCount() > ErrLimit {
		panic(tooManyErrors{})
	}
	return dg
}

func (d *Diagnostics) Errorf(pos Pos, format string, args ...any) *Diag {
	return d.add(pos, false, fmt.Sprintf(format, args...))
}

func (d *Diagnostics) Warnf(pos Pos, format string, args ...any) *Diag {
	return d.add(pos, true, fmt.Sprintf(format, args...))
}

func (d *Diagnostics) ErrorCount() int {
	n := 0
	for _, x := range d.List {
		if !x.Warning {
			n++
		}
	}
	return n
}

func (d *Diagnostics) HasErrors() bool { return d.ErrorCount() > 0 }

// Format 把诊断信息格式化为带源码行与插入符的文本。
func (d *Diagnostics) Format(color bool) string {
	list := append([]*Diag(nil), d.List...)
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i].Pos, list[j].Pos
		an, bn := "", ""
		if a.File != nil {
			an = a.File.Name
		}
		if b.File != nil {
			bn = b.File.Name
		}
		if an != bn {
			return false
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	var sb strings.Builder
	for _, dg := range list {
		kind := "错误"
		if dg.Warning {
			kind = "警告"
		}
		if color {
			if dg.Warning {
				kind = "\x1b[1;33m" + kind + "\x1b[0m"
			} else {
				kind = "\x1b[1;31m" + kind + "\x1b[0m"
			}
		}
		fmt.Fprintf(&sb, "%s: %s: %s\n", dg.Pos, kind, dg.Msg)
		if dg.Pos.File != nil && dg.Pos.Line > 0 {
			line := dg.Pos.File.Line(dg.Pos.Line)
			if strings.TrimSpace(line) != "" {
				shown := strings.ReplaceAll(line, "\t", "    ")
				fmt.Fprintf(&sb, "    %s\n", shown)
				// 计算插入符位置（考虑全角字符显示宽度）
				width := 0
				col := 1
				for _, r := range line {
					if col >= dg.Pos.Col {
						break
					}
					if r == '\t' {
						width += 4
					} else {
						width += runeWidth(r)
					}
					col++
				}
				fmt.Fprintf(&sb, "    %s^\n", strings.Repeat(" ", width))
			}
		}
		for _, n := range dg.Notes {
			fmt.Fprintf(&sb, "    提示: %s\n", n)
		}
	}
	return sb.String()
}

func runeWidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	// 粗略判断东亚全角字符
	if (r >= 0x1100 && r <= 0x115F) || (r >= 0x2E80 && r <= 0xA4CF) ||
		(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE30 && r <= 0xFE4F) || (r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) || (r >= 0x1F300 && r <= 0x1FAFF) ||
		(r >= 0x20000 && r <= 0x3FFFD) {
		return 2
	}
	_ = utf8.RuneLen
	return 1
}
