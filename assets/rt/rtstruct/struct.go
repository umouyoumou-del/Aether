// Package rtstruct 实现 Aether 的 struct 模块（Python struct 的常用子集）。
//
// 支持的格式串：可选的前缀 <（小端，默认）或 >（大端），然后是重复的格式码，
// 每个格式码前面可以带一个十进制重复次数，例如 "<iiiqq"、"<3i"、">8q"。
//
// 格式码：b B h H i I q Q f d x（x 为填充字节，不需要对应值）
package rtstruct

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"

	"aether/assets/rt"
)

type field struct {
	code byte
	n    int
}

// parse 拆解格式串，返回字节序（小端为 true）与字段列表。
func parse(spec string) (little bool, fields []field, size int) {
	little = true
	i := 0
	if i < len(spec) && (spec[i] == '<' || spec[i] == '>' || spec[i] == '=' || spec[i] == '!' || spec[i] == '@') {
		little = spec[i] != '>' && spec[i] != '!'
		i++
	}
	for i < len(spec) {
		n := 0
		seen := false
		for i < len(spec) && spec[i] >= '0' && spec[i] <= '9' {
			n = n*10 + int(spec[i]-'0')
			i++
			seen = true
		}
		if i >= len(spec) {
			rt.Raise("ValueError", "struct 格式串不完整：%s", rt.QuoteStr(spec))
		}
		c := spec[i]
		i++
		if !seen {
			n = 1
		}
		w, ok := codeSize(c)
		if !ok {
			rt.Raise("ValueError", "struct 不支持的格式码 %s", rt.QuoteStr(string(c)))
		}
		fields = append(fields, field{code: c, n: n})
		size += n * w
	}
	return little, fields, size
}

func codeSize(c byte) (int, bool) {
	switch c {
	case 'b', 'B', 'x':
		return 1, true
	case 'h', 'H':
		return 2, true
	case 'i', 'I', 'f':
		return 4, true
	case 'q', 'Q', 'd':
		return 8, true
	}
	return 0, false
}

// CalcSize 返回格式串对应的字节数（对应 struct.calcsize）。
func CalcSize(spec string) int {
	_, _, n := parse(spec)
	return n
}

// Pack 按格式串把值打包为字节列表（对应 struct.pack）。
// 值按顺序取用；x（填充）不消耗值。
func Pack(spec string, vals *rt.List[any]) *rt.List[uint8] {
	little, fields, size := parse(spec)
	out := make([]byte, 0, size)
	order := binary.ByteOrder(binary.LittleEndian)
	if !little {
		order = binary.BigEndian
	}
	vi := 0
	for _, f := range fields {
		if f.code == 'x' {
			for k := 0; k < f.n; k++ {
				out = append(out, 0)
			}
			continue
		}
		for k := 0; k < f.n; k++ {
			if vi >= len(vals.S) {
				rt.Raise("ValueError", "struct.pack 需要 %d 个值，只给了 %d 个", countValues(fields), len(vals.S))
			}
			v := vals.S[vi]
			vi++
			switch f.code {
			case 'b':
				out = append(out, byte(packSigned(v, f.code, -128, 127)))
			case 'B':
				out = append(out, byte(packUnsigned(v, f.code, 255)))
			case 'h':
				var b [2]byte
				order.PutUint16(b[:], uint16(packSigned(v, f.code, -32768, 32767)))
				out = append(out, b[:]...)
			case 'H':
				var b [2]byte
				order.PutUint16(b[:], uint16(packUnsigned(v, f.code, 65535)))
				out = append(out, b[:]...)
			case 'i':
				var b [4]byte
				order.PutUint32(b[:], uint32(packSigned(v, f.code, -2147483648, 2147483647)))
				out = append(out, b[:]...)
			case 'I':
				var b [4]byte
				order.PutUint32(b[:], uint32(packUnsigned(v, f.code, 4294967295)))
				out = append(out, b[:]...)
			case 'q':
				var b [8]byte
				order.PutUint64(b[:], uint64(packInt64(v, f.code)))
				out = append(out, b[:]...)
			case 'Q':
				var b [8]byte
				order.PutUint64(b[:], packUint64(v, f.code))
				out = append(out, b[:]...)
			case 'f':
				x, ok := asFloat(v)
				if !ok {
					rt.Raise("TypeError", "struct.pack 的 f 需要数字")
				}
				var b [4]byte
				order.PutUint32(b[:], math.Float32bits(float32(x)))
				out = append(out, b[:]...)
			case 'd':
				x, ok := asFloat(v)
				if !ok {
					rt.Raise("TypeError", "struct.pack 的 d 需要数字")
				}
				var b [8]byte
				order.PutUint64(b[:], math.Float64bits(x))
				out = append(out, b[:]...)
			}
		}
	}
	return &rt.List[uint8]{S: out}
}

func countValues(fields []field) int {
	n := 0
	for _, f := range fields {
		if f.code != 'x' {
			n += f.n
		}
	}
	return n
}

// toI64 把值转成 int64；同时报告它原本是不是 u64（用于 q/Q 的无符号判定）。
func toI64(v any) (i int64, unsigned bool, ok bool) {
	if u, isU := v.(uint64); isU {
		return int64(u), true, true
	}
	i, got, _ := rt.ToInt(v)
	return i, false, got
}

// pyInt 把值提取为 Python 整数语义（int64 或 uint64 两种视图），
// 遇到非整数或超出 64 位的浮点就抛 TypeError，与 Python struct 一致。
func pyInt(v any, code byte) (i int64, unsigned bool, isU64 bool, u uint64) {
	switch x := v.(type) {
	case uint64:
		return int64(x), true, true, x
	case float64:
		if x != math.Trunc(x) {
			rt.Raise("TypeError", "struct.pack 的 %s 需要整数，收到浮点数", rt.QuoteStr(string(code)))
		}
		if x < math.MinInt64 || x > math.MaxUint64 {
			rt.Raise("TypeError", "struct.pack 的 %s 超出 64 位整数范围", rt.QuoteStr(string(code)))
		}
		u = uint64(x)
		return int64(u), false, false, u
	}
	i, ok, _ := rt.ToInt(v)
	if !ok {
		rt.Raise("TypeError", "struct.pack 的 %s 需要整数", rt.QuoteStr(string(code)))
	}
	return i, false, false, uint64(i)
}

// packSigned 打包有符号格式码并检查范围（Python 会抛 struct.error，这里抛 ValueError）。
func packSigned(v any, code byte, lo, hi int64) int64 {
	i, _, _, _ := pyInt(v, code)
	if i < lo || i > hi {
		rt.Raise("ValueError", "struct.pack 的 %s 超出范围 [%d, %d]", rt.QuoteStr(string(code)), lo, hi)
	}
	return i
}

// packUnsigned 打包无符号格式码并检查范围。
// 负数在 Python 里同样越界（struct.error），这里保持一致。
func packUnsigned(v any, code byte, hi uint64) uint64 {
	i, unsigned, isU64, u := pyInt(v, code)
	if isU64 {
		if u > hi {
			rt.Raise("ValueError", "struct.pack 的 %s 超出范围 [0, %d]", rt.QuoteStr(string(code)), hi)
		}
		return u
	}
	_ = unsigned
	if i < 0 || uint64(i) > hi {
		rt.Raise("ValueError", "struct.pack 的 %s 超出范围 [0, %d]", rt.QuoteStr(string(code)), hi)
	}
	return uint64(i)
}

// packInt64 打包 q：接受 [-2^63, 2^63-1]；传入 uint64 视图时按位重解释。
func packInt64(v any, code byte) int64 {
	i, _, isU64, u := pyInt(v, code)
	if isU64 {
		return int64(u)
	}
	return i
}

// packUint64 打包 Q：接受 [0, 2^64-1]。
func packUint64(v any, code byte) uint64 {
	i, _, isU64, u := pyInt(v, code)
	if isU64 {
		return u
	}
	if i < 0 {
		rt.Raise("ValueError", "struct.pack 的 Q 超出范围 [0, %s]", rt.QuoteStr(strconv.FormatUint(math.MaxUint64, 10)))
	}
	return uint64(i)
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	if i, ok, _ := rt.ToInt(v); ok {
		return float64(i), true
	}
	return 0, false
}

// UnpackFrom 从 data 的 offset 开始按格式串解出值（对应 struct.unpack_from）。
// 返回的整数一律为 int（q/Q 超出 int64 范围时给出 uint64）。
func UnpackFrom(spec string, data *rt.List[uint8], offset int) *rt.List[any] {
	little, fields, size := parse(spec)
	if offset < 0 {
		offset += len(data.S)
	}
	if offset < 0 || offset+size > len(data.S) {
		rt.Raise("ValueError", "struct.unpack_from 数据不足：需要 %d 字节，实际可用 %d 字节", size, len(data.S)-offset)
	}
	order := binary.ByteOrder(binary.LittleEndian)
	if !little {
		order = binary.BigEndian
	}
	out := rt.NewList[any](0)
	pos := offset
	for _, f := range fields {
		if f.code == 'x' {
			pos += f.n
			continue
		}
		for k := 0; k < f.n; k++ {
			switch f.code {
			case 'b':
				out.Append(int(int8(data.S[pos])))
				pos++
			case 'B':
				out.Append(int(data.S[pos]))
				pos++
			case 'h':
				out.Append(int(int16(order.Uint16(data.S[pos:]))))
				pos += 2
			case 'H':
				out.Append(int(order.Uint16(data.S[pos:])))
				pos += 2
			case 'i':
				out.Append(int(int32(order.Uint32(data.S[pos:]))))
				pos += 4
			case 'I':
				out.Append(int(order.Uint32(data.S[pos:])))
				pos += 4
			case 'q':
				// 'q' 在 Python 里是 signed long long：必须按有符号解释。
				// 之前这里和 'Q' 共用一段逻辑，负值会被读成 18446744073709551615。
				out.Append(int(int64(order.Uint64(data.S[pos:]))))
				pos += 8
			case 'Q':
				u := order.Uint64(data.S[pos:])
				if u > math.MaxInt64 {
					out.Append(u)
				} else {
					out.Append(int(u))
				}
				pos += 8
			case 'f':
				out.Append(float64(math.Float32frombits(order.Uint32(data.S[pos:]))))
				pos += 4
			case 'd':
				out.Append(math.Float64frombits(order.Uint64(data.S[pos:])))
				pos += 8
			}
		}
	}
	return out
}

// Unpack 从 data 的开头解出值（对应 struct.unpack）。
func Unpack(spec string, data *rt.List[uint8]) *rt.List[any] {
	return UnpackFrom(spec, data, 0)
}

// PackInts 与 Pack 相同，但直接接受整数列表，避免调用方手工构造 list[any]。
// 二进制协议里绝大多数字段都是整数，用它可以让端口代码保持整洁。
func PackInts(spec string, vals *rt.List[int]) *rt.List[uint8] {
	boxed := rt.NewList[any](len(vals.S))
	for _, v := range vals.S {
		boxed.Append(v)
	}
	return Pack(spec, boxed)
}

// UnpackInts 与 UnpackFrom 相同，但要求所有字段都是整数，并直接返回整数列表。
// 超出 int 范围的无符号值会退化为 uint64 时抛出 ValueError，避免静默截断。
func UnpackInts(spec string, data *rt.List[uint8], offset int) *rt.List[int] {
	raw := UnpackFrom(spec, data, offset)
	out := rt.NewList[int](len(raw.S))
	for _, v := range raw.S {
		if u, ok := v.(uint64); ok {
			if u > math.MaxInt64 {
				rt.Raise("ValueError", "struct 字段 %d 超出 int 范围，请改用 unpack", u)
			}
			out.Append(int(u))
			continue
		}
		i, ok, _ := rt.ToInt(v) // 返回 int64
		if !ok {
			rt.Raise("ValueError", "struct 字段不是整数")
		}
		out.Append(int(i))
	}
	return out
}

// IndexI64 把字节列表的 [pos, pos+8) 按小端解释为 int（对应协议头里的 q 字段）。
func IndexI64(data *rt.List[uint8], pos int) int {
	u := IndexU64(data, pos)
	if u > math.MaxInt64 {
		rt.Raise("ValueError", "IndexI64 读到的值超出 int 范围")
	}
	return int(u)
}

// IndexI32 把字节列表的 [pos, pos+4) 按小端解释为 int（对应协议头里的 i 字段）。
func IndexI32(data *rt.List[uint8], pos int) int {
	if pos < 0 || pos+4 > len(data.S) {
		rt.Raise("ValueError", "字节列表越界：pos=%d len=%d", int64(pos), int64(len(data.S)))
	}
	return int(int32(binary.LittleEndian.Uint32(data.S[pos:])))
}

// IndexU64 把字节列表的 [pos, pos+8) 按小端解释为 u64（供二进制协议里的计数/标识使用）。
func IndexU64(data *rt.List[uint8], pos int) uint64 {
	if pos < 0 || pos+8 > len(data.S) {
		rt.Raise("ValueError", "字节列表越界：pos=%d len=%d", int64(pos), int64(len(data.S)))
	}
	return binary.LittleEndian.Uint64(data.S[pos:])
}

// Concat 拼接多个字节列表与字符串（字节按 UTF-8 追加）。
func Concat(parts *rt.List[any]) *rt.List[uint8] {
	out := []byte{}
	for _, p := range parts.S {
		switch x := p.(type) {
		case *rt.List[uint8]:
			out = append(out, x.S...)
		case string:
			out = append(out, []byte(x)...)
		default:
			rt.Raise("TypeError", "Concat 只接受字节列表或字符串，收到 %s", rt.TypeName(p))
		}
	}
	return &rt.List[uint8]{S: out}
}

// Slice 返回 data[from:to] 的副本（to 为 -1 表示到末尾）。
func Slice(data *rt.List[uint8], from, to int) *rt.List[uint8] {
	n := len(data.S)
	if to < 0 || to > n {
		to = n
	}
	if from < 0 {
		from += n
	}
	if from < 0 || from > n || from > to {
		rt.Raise("ValueError", "字节切片范围无效：%d:%d（长度 %d）", int64(from), int64(to), int64(n))
	}
	return &rt.List[uint8]{S: append([]byte{}, data.S[from:to]...)}
}

// Describe 用于调试：返回 "bytes(len=N)" 形式的字符串。
func Describe(data *rt.List[uint8]) string {
	return fmt.Sprintf("bytes(len=%d)", len(data.S))
}

// HexOf 返回字节列表的小写十六进制串（对应 Python 的 bytes.hex()）。
// 二进制协议的对齐验证需要逐字节比较，靠它把 packet 编码结果导出成文本。
func HexOf(data *rt.List[uint8]) string {
	return hex.EncodeToString(data.S)
}