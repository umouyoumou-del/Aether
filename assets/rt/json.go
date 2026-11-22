package rt

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type fielder interface{ Fieldsꞏ() ([]string, []any) }
type itemser interface{ Itemsꞏ() []any }
type pairser interface{ Pairsꞏ() ([]any, []any) }

func (l *List[T]) Itemsꞏ() []any {
	out := make([]any, len(l.S))
	for i, x := range l.S {
		out[i] = x
	}
	return out
}

func (s *Set[T]) Itemsꞏ() []any {
	var out []any
	for x := range s.All() {
		out = append(out, x)
	}
	return out
}

func (d *Dict[K, V]) Pairsꞏ() ([]any, []any) {
	var ks, vs []any
	for k, v := range d.All() {
		ks = append(ks, k)
		vs = append(vs, v)
	}
	return ks, vs
}

type jsonEnc struct {
	sb     strings.Builder
	indent int
	sortKeys bool
	seen   map[any]bool
}

// JSONDumps 实现 json.dumps(value, indent)。
func JSONDumps(v any, indent int, sortKeys bool) string {
	e := &jsonEnc{indent: indent, sortKeys: sortKeys, seen: map[any]bool{}}
	e.value(v, 0)
	return e.sb.String()
}

func (e *jsonEnc) newline(depth int) {
	if e.indent > 0 {
		e.sb.WriteByte('\n')
		e.sb.WriteString(strings.Repeat(" ", e.indent*depth))
	}
}

func jsonQuote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func (e *jsonEnc) object(keys []string, vals []any, depth int) {
	if len(keys) == 0 {
		e.sb.WriteString("{}")
		return
	}
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	if e.sortKeys {
		sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	}
	e.sb.WriteByte('{')
	for n, i := range idx {
		if n > 0 {
			e.sb.WriteByte(',')
			if e.indent == 0 {
				e.sb.WriteByte(' ')
			}
		}
		e.newline(depth + 1)
		e.sb.WriteString(jsonQuote(keys[i]))
		e.sb.WriteString(": ")
		e.value(vals[i], depth+1)
	}
	e.newline(depth)
	e.sb.WriteByte('}')
}

func (e *jsonEnc) array(items []any, depth int) {
	if len(items) == 0 {
		e.sb.WriteString("[]")
		return
	}
	e.sb.WriteByte('[')
	for i, x := range items {
		if i > 0 {
			e.sb.WriteByte(',')
			if e.indent == 0 {
				e.sb.WriteByte(' ')
			}
		}
		e.newline(depth + 1)
		e.value(x, depth+1)
	}
	e.newline(depth)
	e.sb.WriteByte(']')
}

func (e *jsonEnc) value(v any, depth int) {
	if depth > 200 {
		Raise("JSONError", "JSON 嵌套太深（可能存在循环引用）")
	}
	switch x := v.(type) {
	case nil:
		e.sb.WriteString("null")
		return
	case bool:
		e.sb.WriteString(BoolStr(x))
		return
	case string:
		e.sb.WriteString(jsonQuote(x))
		return
	case Char:
		e.sb.WriteString(jsonQuote(string(rune(x))))
		return
	case float64:
		e.float(x)
		return
	case float32:
		e.float(float64(x))
		return
	case interface{ Any() any }:
		e.value(x.Any(), depth)
		return
	case pairser:
		ks, vs := x.Pairsꞏ()
		keys := make([]string, len(ks))
		for i, k := range ks {
			keys[i] = Str(k)
		}
		e.object(keys, vs, depth)
		return
	case itemser:
		e.array(x.Itemsꞏ(), depth)
		return
	case fielder:
		names, vals := x.Fieldsꞏ()
		e.object(names, vals, depth)
		return
	}
	if iv, ok, _ := toInt(v); ok {
		if _, isEnum := v.(fmt.Stringer); isEnum {
			e.sb.WriteString(jsonQuote(Str(v)))
			return
		}
		e.sb.WriteString(strconv.FormatInt(iv, 10))
		return
	}
	if u, ok := v.(uint64); ok {
		e.sb.WriteString(strconv.FormatUint(u, 10))
		return
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Struct && strings.Contains(rv.Type().String(), "rt.T") {
		var items []any
		for i := 0; i < rv.NumField(); i++ {
			items = append(items, rv.Field(i).Interface())
		}
		e.array(items, depth)
		return
	}
	e.sb.WriteString(jsonQuote(Str(v)))
}

func (e *jsonEnc) float(f float64) {
	switch {
	case math.IsNaN(f):
		e.sb.WriteString("NaN")
	case math.IsInf(f, 1):
		e.sb.WriteString("Infinity")
	case math.IsInf(f, -1):
		e.sb.WriteString("-Infinity")
	default:
		e.sb.WriteString(FloatStr(f))
	}
}

// JSONLoads 实现 json.loads(text)：对象变为 dict[str, any]，数组变为 list[any]。
func JSONLoads(s string) any {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	v, err := jsonParse(dec)
	if err != nil {
		Raise("JSONError", "JSON 解析失败：%v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		Raise("JSONError", "JSON 解析失败：末尾有多余的内容")
	}
	return v
}

func jsonParse(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return jsonFromToken(dec, tok)
}

func jsonFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			d := NewDict[string, any](0)
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("对象的键必须是字符串")
				}
				v, err := jsonParse(dec)
				if err != nil {
					return nil, err
				}
				d.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return d, nil
		case '[':
			l := NewList[any](0)
			for dec.More() {
				v, err := jsonParse(dec)
				if err != nil {
					return nil, err
				}
				l.Append(v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return l, nil
		}
		return nil, fmt.Errorf("意外的分隔符 %v", t)
	case json.Number:
		s := string(t)
		if !strings.ContainsAny(s, ".eE") {
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return int(i), nil
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("无法识别的 JSON 值")
}

func JSONLoadsDict(s string) *Dict[string, any] {
	v := JSONLoads(s)
	d, ok := v.(*Dict[string, any])
	if !ok {
		Raise("JSONError", "JSON 的顶层不是对象，而是 %s", TypeName(v))
	}
	return d
}

func JSONLoadsList(s string) *List[any] {
	v := JSONLoads(s)
	l, ok := v.(*List[any])
	if !ok {
		Raise("JSONError", "JSON 的顶层不是数组，而是 %s", TypeName(v))
	}
	return l
}

// JSONValid 判断字符串是否为有效的 JSON。
func JSONValid(s string) bool { return json.Valid([]byte(s)) }
