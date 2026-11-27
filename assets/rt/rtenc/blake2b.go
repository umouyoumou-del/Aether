// BLAKE2b：补在 rtenc 里的手写实现（Go 标准库没有 BLAKE2，且构建时 GOPROXY=off 无法取用
// golang.org/x/crypto）。语义对齐 Python 的 hashlib.blake2b：
//
//	hashlib.blake2b(data, digest_size=64, key=b"")
//
// 要点：digest_size 与 key 长度都会异或进参数块（h[0]），因此**不能**用「先算 64 字节再截断」
// 来冒充小摘要——那样得到的字节完全不同。
package rtenc

import (
	"encoding/binary"
	"hash"
	"math/bits"
	"strconv"

	"aether/assets/rt"
)

// BLAKE2b 的 8 个初始向量（= SHA-512 的初始哈希值，取前 8 个）。
var b2bIV = [8]uint64{
	0x6a09e667f3bcc908, 0xbb67ae8584caa73b,
	0x3c6ef372fe94f82b, 0xa54ff53a5f1d36f1,
	0x510e527fade682d1, 0x9b05688c2b3e6c1f,
	0x1f83d9abfb41bd6b, 0x5be0cd19137e2179,
}

// 12 轮的消息字置换表（第 11、12 轮循环复用前 2 行，与参考实现一致）。
var b2bSigma = [12][16]byte{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
	{11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4},
	{7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8},
	{9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13},
	{2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9},
	{12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11},
	{13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10},
	{6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5},
	{10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0},
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
}

// b2b 实现 hash.Hash，供 Hash 结构体统一包一层。
type b2b struct {
	h       [8]uint64
	t       [2]uint64 // 128 位已处理字节计数
	buf     [128]byte
	n       int   // buf 中已填充的字节数
	size    int   // 输出摘要长度（1..64）
	keyOrig []byte
}

func newB2b(size int, key string) *b2b {
	if size < 1 || size > 64 {
		rt.Raise("ValueError", "blake2b 的 digest_size 必须在 1..64 之间，实际是 %d", int64(size))
	}
	if len(key) > 64 {
		rt.Raise("ValueError", "blake2b 的 key 最长 64 字节，实际是 %d", int64(len(key)))
	}
	d := &b2b{size: size}
	if key != "" {
		d.keyOrig = []byte(key)
	}
	d.reset()
	return d
}

func (d *b2b) reset() {
	d.h = b2bIV
	// 参数块前 8 字节：digest_length | key_length<<8 | fanout<<16 | depth<<24
	p0 := uint64(d.size) | uint64(len(d.keyOrig))<<8 | 1<<16 | 1<<24
	d.h[0] ^= p0
	d.t = [2]uint64{}
	d.buf = [128]byte{}
	d.n = 0
	if len(d.keyOrig) > 0 {
		// 带密钥时，密钥填零到 128 字节后作为第一个块；若消息为空，它就是最后一个块。
		copy(d.buf[:], d.keyOrig)
		d.n = 128
	}
}

// incr 把 n 累加进 128 位计数器。
func (d *b2b) incr(n uint64) {
	lo := d.t[0] + n
	if lo < d.t[0] {
		d.t[1]++
	}
	d.t[0] = lo
}

// compress 用 buf 的 128 字节做一轮压缩。final 为 true 时置最后一个标志位。
// 压缩后把 buf 清零：末块必须按零补齐，否则上一块残留的字节会混进摘要。
func (d *b2b) compress(final bool) {
	var m [16]uint64
	for i := 0; i < 16; i++ {
		m[i] = binary.LittleEndian.Uint64(d.buf[i*8:])
	}
	d.buf = [128]byte{}
	var v [16]uint64
	copy(v[:8], d.h[:])
	copy(v[8:], b2bIV[:])
	v[12] ^= d.t[0]
	v[13] ^= d.t[1]
	if final {
		v[14] = ^v[14]
	}
	for i := 0; i < 12; i++ {
		s := &b2bSigma[i]
		gB2b(&v, 0, 4, 8, 12, m[s[0]], m[s[1]])
		gB2b(&v, 1, 5, 9, 13, m[s[2]], m[s[3]])
		gB2b(&v, 2, 6, 10, 14, m[s[4]], m[s[5]])
		gB2b(&v, 3, 7, 11, 15, m[s[6]], m[s[7]])
		gB2b(&v, 0, 5, 10, 15, m[s[8]], m[s[9]])
		gB2b(&v, 1, 6, 11, 12, m[s[10]], m[s[11]])
		gB2b(&v, 2, 7, 8, 13, m[s[12]], m[s[13]])
		gB2b(&v, 3, 4, 9, 14, m[s[14]], m[s[15]])
	}
	for i := 0; i < 8; i++ {
		d.h[i] ^= v[i] ^ v[i+8]
	}
}

// gB2b 是 BLAKE2b 的混合函数 G（旋转量为 32/24/16/63）。
func gB2b(v *[16]uint64, a, b, c, dd int, x, y uint64) {
	v[a] = v[a] + v[b] + x
	v[dd] = bits.RotateLeft64(v[dd]^v[a], -32)
	v[c] = v[c] + v[dd]
	v[b] = bits.RotateLeft64(v[b]^v[c], -24)
	v[a] = v[a] + v[b] + y
	v[dd] = bits.RotateLeft64(v[dd]^v[a], -16)
	v[c] = v[c] + v[dd]
	v[b] = bits.RotateLeft64(v[b]^v[c], -63)
}

func (d *b2b) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		if d.n == 128 {
			// 缓冲区已满，且后面还有数据：先压掉，不能置 final 标志。
			d.incr(128)
			d.compress(false)
			d.n = 0
		}
		c := copy(d.buf[d.n:], p)
		d.n += c
		p = p[c:]
	}
	return total, nil
}

// Sum 不改动自身状态：在副本上补齐最后一块再输出。
func (d *b2b) Sum(b []byte) []byte {
	c := *d
	c.incr(uint64(c.n))
	c.compress(true)
	var out [64]byte
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(out[i*8:], c.h[i])
	}
	return append(b, out[:c.size]...)
}

func (d *b2b) Reset()     { d.reset() }
func (d *b2b) Size() int  { return d.size }
func (d *b2b) BlockSize() int { return 128 }

var _ hash.Hash = (*b2b)(nil)

// NewBlake2b 创建 BLAKE2b 摘要（对应 Python 的 hashlib.blake2b(data, digest_size, key)）。
func NewBlake2b(data string, digestSize int, key string) *Hash {
	h := &Hash{h: newB2b(digestSize, key), name: "blake2b"}
	if data != "" {
		h.h.Write([]byte(data))
	}
	return h
}

// B2bLEInt 直接把摘要的前 8 字节按小端解释为整数，等价于 Python 的
// int.from_bytes(hashlib.blake2b(data, digest_size=8).digest(), "little")。
// 返回值按 64 位有符号整数解释同一份比特（Python 得到的是无符号值，可能 ≥ 2^63）；
// 需要与 Python 逐字节对照时用 B2bLEUintStr。
func B2bLEInt(data string, digestSize int) int {
	return int(B2bLEInt64(data, digestSize))
}

// B2bLEUintStr 返回无符号十进制字符串，可与 Python 的 stable_id 直接做字符串比较。
func B2bLEUintStr(data string, digestSize int) string {
	return strconv.FormatUint(B2bLEInt64(data, digestSize), 10)
}

// B2bLEU64 直接给出 uint64，供 Aether 的 u64 变量使用（避免经字符串转换时溢出）。
func B2bLEU64(data string, digestSize int) uint64 { return B2bLEInt64(data, digestSize) }

// B2bLEInt64 返回摘要前 8 字节（小端）的无符号值。
func B2bLEInt64(data string, digestSize int) uint64 {
	d := newB2b(digestSize, "")
	d.Write([]byte(data))
	sum := d.Sum(nil)
	var out [8]byte
	copy(out[:], sum)
	return binary.LittleEndian.Uint64(out[:])
}