// Package rtcsv 实现 Aether 的 csv 模块（只在用到时链接）。
package rtcsv

import (
	"encoding/csv"
	"errors"
	"strings"

	"aether/assets/rt"
)

func reader(text string, delim rt.Char) *csv.Reader {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	r.Comma = rune(delim)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	return r
}

func parseRows(text string, delim rt.Char) [][]string {
	rows, err := reader(text, delim).ReadAll()
	if err != nil {
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			rt.Raise("ValueError", "CSV 格式错误（第 %d 行）：%v", pe.Line, pe.Err)
		}
		rt.Raise("ValueError", "CSV 格式错误：%v", err)
	}
	return rows
}

func toList(rows [][]string) *rt.List[*rt.List[string]] {
	out := rt.NewList[*rt.List[string]](len(rows))
	for _, r := range rows {
		out.Append(&rt.List[string]{S: r})
	}
	return out
}

// Parse 把 CSV 文本解析为行的列表，每行是字段的列表。
func Parse(text string, delim rt.Char) *rt.List[*rt.List[string]] {
	return toList(parseRows(text, delim))
}

func Read(path string, delim rt.Char) *rt.List[*rt.List[string]] {
	return Parse(rt.ReadFile(path), delim)
}

// ParseDicts 把第一行作为列名，其余每行解析为 {列名: 值}（相当于 csv.DictReader）。
func ParseDicts(text string, delim rt.Char) *rt.List[*rt.Dict[string, string]] {
	rows := parseRows(text, delim)
	out := rt.NewList[*rt.Dict[string, string]](0)
	if len(rows) == 0 {
		return out
	}
	head := rows[0]
	for _, r := range rows[1:] {
		d := rt.NewDict[string, string](len(head))
		for i, name := range head {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			d.Set(name, v)
		}
		out.Append(d)
	}
	return out
}

func ReadDicts(path string, delim rt.Char) *rt.List[*rt.Dict[string, string]] {
	return ParseDicts(rt.ReadFile(path), delim)
}

func writeRows(rows [][]string, delim rt.Char) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	w.Comma = rune(delim)
	if err := w.WriteAll(rows); err != nil {
		rt.Raise("ValueError", "CSV 写入失败：%v", err)
	}
	return sb.String()
}

// Format 把行（每行是值的列表，值用 str() 转换）格式化为 CSV 文本。
func Format[T any](rows *rt.List[*rt.List[T]], delim rt.Char) string {
	var out [][]string
	for _, r := range rows.S {
		var fields []string
		for _, v := range r.S {
			fields = append(fields, rt.Str(v))
		}
		out = append(out, fields)
	}
	return writeRows(out, delim)
}

func Write[T any](path string, rows *rt.List[*rt.List[T]], delim rt.Char) {
	rt.WriteFile(path, Format(rows, delim))
}

// FormatDicts 把字典的列表格式化为带表头的 CSV；fieldnames 为空时使用第一个字典的键。
func FormatDicts[V any](rows *rt.List[*rt.Dict[string, V]], fieldnames rt.Opt[*rt.List[string]], delim rt.Char) string {
	var head []string
	if fieldnames.Ok {
		head = fieldnames.V.S
	} else if len(rows.S) > 0 {
		head = rows.S[0].Keys().S
	}
	out := [][]string{head}
	for _, d := range rows.S {
		fields := make([]string, len(head))
		for i, name := range head {
			if v, ok := d.Lookup(name); ok {
				fields[i] = rt.Str(v)
			}
		}
		out = append(out, fields)
	}
	return writeRows(out, delim)
}

func WriteDicts[V any](path string, rows *rt.List[*rt.Dict[string, V]], fieldnames rt.Opt[*rt.List[string]], delim rt.Char) {
	rt.WriteFile(path, FormatDicts(rows, fieldnames, delim))
}
