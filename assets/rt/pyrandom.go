package rt

import (
	"math"
	"math/bits"
)

// PyRandom 是 CPython 3.12 random.Random 的 MT19937 内核（Modules/_randommodule.c 逐行对应）：同一个 int 种子
// 给出与 CPython 逐位相同的随机流。随机方法里依赖语言层的部分（randrange / choice / shuffle / sample ...）在
// assets/lib/random.ae 的 Random 类里，同样逐行对应 Lib/random.py。
//
// 与 CPython 的差别只在取值范围：种子与 getrandbits 的结果是 64 位整数（Python 是任意精度），所以
// getrandbits(k) 只支持 k <= 63；种子的字符串 / bytes 形式（version 2 的 sha512）没有实现。
type PyRandom struct {
	mt    [mtN]uint32
	index int
}

const (
	mtN       = 624
	mtM       = 397
	mtMatrixA = 0x9908b0df
	mtUpper   = 0x80000000
	mtLower   = 0x7fffffff
)

// NewPyRandom 对应 random.Random(seed)（int 种子）。
func NewPyRandom(seed int) *PyRandom {
	r := &PyRandom{}
	r.Seed(seed)
	return r
}

func (r *PyRandom) initGenrand(s uint32) {
	r.mt[0] = s
	for i := 1; i < mtN; i++ {
		r.mt[i] = 1812433253*(r.mt[i-1]^(r.mt[i-1]>>30)) + uint32(i)
	}
	r.index = mtN
}

func (r *PyRandom) initByArray(key []uint32) {
	r.initGenrand(19650218)
	i, j := 1, 0
	k := mtN
	if len(key) > k {
		k = len(key)
	}
	for ; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= mtN {
			r.mt[0] = r.mt[mtN-1]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = mtN - 1; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= mtN {
			r.mt[0] = r.mt[mtN-1]
			i = 1
		}
	}
	r.mt[0] = 0x80000000
}

// Seed 对应 Random.seed(a)（int）：取绝对值，按 32 位从低到高切成 key，交给 init_by_array。
func (r *PyRandom) Seed(seed int) {
	u := uint64(seed)
	if seed < 0 {
		u = uint64(-seed) // MinInt64 也对：-MinInt64 溢出回 MinInt64，转成无符号正是 2^63
	}
	key := []uint32{uint32(u)}
	if u>>32 != 0 {
		key = append(key, uint32(u>>32))
	}
	r.initByArray(key)
}

func (r *PyRandom) genrand() uint32 {
	mag01 := [2]uint32{0, mtMatrixA}
	if r.index >= mtN {
		kk := 0
		for ; kk < mtN-mtM; kk++ {
			y := (r.mt[kk] & mtUpper) | (r.mt[kk+1] & mtLower)
			r.mt[kk] = r.mt[kk+mtM] ^ (y >> 1) ^ mag01[y&1]
		}
		for ; kk < mtN-1; kk++ {
			y := (r.mt[kk] & mtUpper) | (r.mt[kk+1] & mtLower)
			r.mt[kk] = r.mt[kk+(mtM-mtN)] ^ (y >> 1) ^ mag01[y&1]
		}
		y := (r.mt[mtN-1] & mtUpper) | (r.mt[0] & mtLower)
		r.mt[mtN-1] = r.mt[mtM-1] ^ (y >> 1) ^ mag01[y&1]
		r.index = 0
	}
	y := r.mt[r.index]
	r.index++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// Random 对应 random()：genrand_res53。
func (r *PyRandom) Random() float64 {
	a := r.genrand() >> 5
	b := r.genrand() >> 6
	return (float64(a)*67108864.0 + float64(b)) * (1.0 / 9007199254740992.0)
}

// GetRandBits 对应 getrandbits(k)：k <= 32 取一个字的高 k 位；更大时按 32 位字从低到高拼，最高字右移去掉多余位。
func (r *PyRandom) GetRandBits(k int) int {
	if k < 0 {
		Raise("ValueError", "number of bits must be non-negative")
	}
	if k == 0 {
		return 0
	}
	if k > 63 {
		Raise("OverflowError", "getrandbits(%d): Aether 的 int 是 64 位有符号整数，只支持 k <= 63", k)
	}
	if k <= 32 {
		return int(r.genrand() >> (32 - k))
	}
	var out uint64
	shift := 0
	for left := k; left > 0; left -= 32 {
		w := r.genrand()
		if left < 32 {
			w >>= 32 - left
		}
		out |= uint64(w) << shift
		shift += 32
	}
	return int(out)
}

// RandBelow 对应 _randbelow_with_getrandbits(n)（n > 0）：k = n.bit_length()，取 k 位，>= n 就重取。
func (r *PyRandom) RandBelow(n int) int {
	if n <= 0 {
		Raise("ValueError", "_randbelow(%d)：n 必须为正", n)
	}
	k := bits.Len64(uint64(n))
	v := r.GetRandBits(k)
	for v >= n {
		v = r.GetRandBits(k)
	}
	return v
}

// SampleSetSize 是 Lib/random.py sample() 里的 setsize：21，k > 5 时再加 4 ** ceil(log(k * 3, 4))。
func (r *PyRandom) SampleSetSize(k int) int {
	size := 21
	if k > 5 {
		size += int(math.Pow(4, math.Ceil(math.Log(float64(k*3))/math.Log(4))))
	}
	return size
}
