package rt

import (
	"cmp"
	"iter"
	"slices"
	"strings"
)

// List 是五行的 list[T]（引用语义，与 Python 列表一致）。
type List[T any] struct {
	S []T
}

func NewList[T any](capacity int) *List[T] {
	return &List[T]{S: make([]T, 0, capacity)}
}

func ListOf[T any](xs ...T) *List[T] {
	return &List[T]{S: xs}
}

func ListFrom[T any](it iter.Seq[T]) *List[T] {
	l := &List[T]{}
	for x := range it {
		l.S = append(l.S, x)
	}
	return l
}

func (l *List[T]) Len() int { return len(l.S) }

// Ix 把可能为负的下标转换为实际下标。
func Ix(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		Raise("IndexError", "列表下标 %d 超出范围（长度为 %d）", i, n)
	}
	return i
}

func (l *List[T]) At(i int) T {
	n := len(l.S)
	if uint(i) < uint(n) {
		return l.S[i]
	}
	return l.S[Ix(i, n)]
}

func (l *List[T]) Set(i int, v T) {
	n := len(l.S)
	if uint(i) < uint(n) {
		l.S[i] = v
		return
	}
	l.S[Ix(i, n)] = v
}

func (l *List[T]) Ptr(i int) *T {
	return &l.S[Ix(i, len(l.S))]
}

func (l *List[T]) Append(v T) { l.S = append(l.S, v) }

func (l *List[T]) Extend(it iter.Seq[T]) {
	for x := range it {
		l.S = append(l.S, x)
	}
}

func (l *List[T]) ExtendList(o *List[T]) {
	if o == l {
		l.S = append(l.S, slices.Clone(o.S)...)
		return
	}
	l.S = append(l.S, o.S...)
}

func (l *List[T]) Insert(i int, v T) {
	n := len(l.S)
	if i < 0 {
		i += n
		if i < 0 {
			i = 0
		}
	}
	if i > n {
		i = n
	}
	l.S = slices.Insert(l.S, i, v)
}

func (l *List[T]) Pop() T {
	n := len(l.S)
	if n == 0 {
		Raise("IndexError", "不能从空列表中 pop")
	}
	v := l.S[n-1]
	var zero T
	l.S[n-1] = zero
	l.S = l.S[:n-1]
	return v
}

func (l *List[T]) PopAt(i int) T {
	n := len(l.S)
	if n == 0 {
		Raise("IndexError", "不能从空列表中 pop")
	}
	i = Ix(i, n)
	v := l.S[i]
	l.S = slices.Delete(l.S, i, i+1)
	return v
}

func (l *List[T]) RemoveAt(i int) {
	i = Ix(i, len(l.S))
	l.S = slices.Delete(l.S, i, i+1)
}

func (l *List[T]) Clear() {
	clear(l.S)
	l.S = l.S[:0]
}

func (l *List[T]) Copy() *List[T] {
	return &List[T]{S: slices.Clone(l.S)}
}

func (l *List[T]) Reverse() { slices.Reverse(l.S) }

func (l *List[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, x := range l.S {
			if !yield(x) {
				return
			}
		}
	}
}

func (l *List[T]) Concat(o *List[T]) *List[T] {
	s := make([]T, 0, len(l.S)+len(o.S))
	s = append(s, l.S...)
	s = append(s, o.S...)
	return &List[T]{S: s}
}

func (l *List[T]) Repeat(n int) *List[T] {
	if n <= 0 {
		return &List[T]{}
	}
	s := make([]T, 0, len(l.S)*n)
	for i := 0; i < n; i++ {
		s = append(s, l.S...)
	}
	return &List[T]{S: s}
}

// sliceBounds 按 Python 语义计算切片的起止。
func sliceBounds(n, lo, hi, step int) (int, int, int) {
	if step == 0 {
		Raise("ValueError", "切片步长不能为 0")
	}
	if step > 0 {
		if lo == NoIdx {
			lo = 0
		} else if lo < 0 {
			lo += n
			if lo < 0 {
				lo = 0
			}
		} else if lo > n {
			lo = n
		}
		if hi == NoIdx {
			hi = n
		} else if hi < 0 {
			hi += n
			if hi < 0 {
				hi = 0
			}
		} else if hi > n {
			hi = n
		}
	} else {
		if lo == NoIdx {
			lo = n - 1
		} else if lo < 0 {
			lo += n
			if lo < 0 {
				lo = -1
			}
		} else if lo >= n {
			lo = n - 1
		}
		if hi == NoIdx {
			hi = -1
		} else if hi < 0 {
			hi += n
			if hi < 0 {
				hi = -1
			}
		} else if hi >= n {
			hi = n - 1
		}
	}
	return lo, hi, step
}

func (l *List[T]) Slice(lo, hi, step int) *List[T] {
	n := len(l.S)
	lo, hi, step = sliceBounds(n, lo, hi, step)
	if step == 1 {
		if hi <= lo {
			return &List[T]{}
		}
		return &List[T]{S: slices.Clone(l.S[lo:hi])}
	}
	var out []T
	if step > 0 {
		for i := lo; i < hi; i += step {
			out = append(out, l.S[i])
		}
	} else {
		for i := lo; i > hi; i += step {
			out = append(out, l.S[i])
		}
	}
	return &List[T]{S: out}
}

func (l *List[T]) String() string {
	if l == nil {
		return "null"
	}
	return reprList(l.S)
}

func reprList[T any](s []T) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, x := range s {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(Repr(x))
	}
	sb.WriteByte(']')
	return sb.String()
}

func (l *List[T]) Eqꞏ(o any) bool {
	r, ok := o.(*List[T])
	if !ok {
		return false
	}
	if l == r {
		return true
	}
	if l == nil || r == nil || len(l.S) != len(r.S) {
		return false
	}
	for i := range l.S {
		if !Equal(l.S[i], r.S[i]) {
			return false
		}
	}
	return true
}

func (l *List[T]) Cmpꞏ(o any) int {
	r := o.(*List[T])
	for i := 0; i < len(l.S) && i < len(r.S); i++ {
		if c := Compare(l.S[i], r.S[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(l.S), len(r.S))
}

// CheckLen 用于把列表解构为固定数量的变量。
func CheckLen[T any](l *List[T], n int) *List[T] {
	if len(l.S) != n {
		Raise("ValueError", "解构需要 %d 个元素，但列表有 %d 个", n, len(l.S))
	}
	return l
}

// CheckLenMin 用于带 *rest 的解构：列表至少要有 n 个元素。
func CheckLenMin[T any](l *List[T], n int) *List[T] {
	if len(l.S) < n {
		Raise("ValueError", "解构需要至少 %d 个元素，但列表只有 %d 个", n, len(l.S))
	}
	return l
}

// ListRest 返回解构时 *rest 收集的元素：去掉开头 before 个和末尾 after 个之后剩下的部分。
func ListRest[T any](l *List[T], before, after int) *List[T] {
	return &List[T]{S: slices.Clone(l.S[before : len(l.S)-after])}
}

// sliceIndexes 返回扩展切片（步长不为 1）选中的下标。
func sliceIndexes(lo, hi, step int) []int {
	var idx []int
	if step > 0 {
		for i := lo; i < hi; i += step {
			idx = append(idx, i)
		}
	} else {
		for i := lo; i > hi; i += step {
			idx = append(idx, i)
		}
	}
	return idx
}

// SetSlice 实现 xs[lo:hi:step] = src：步长为 1 时用 src 的元素替换这一段（长度可以不同）；
// 否则 src 的长度必须与切片选中的元素个数相同。
func (l *List[T]) SetSlice(lo, hi, step int, src *List[T]) {
	vals := slices.Clone(src.S)
	lo, hi, step = sliceBounds(len(l.S), lo, hi, step)
	if step == 1 {
		if hi < lo {
			hi = lo
		}
		l.S = slices.Concat(l.S[:lo:lo], vals, l.S[hi:])
		return
	}
	idx := sliceIndexes(lo, hi, step)
	if len(idx) != len(vals) {
		Raise("ValueError", "不能把 %d 个元素赋给长度为 %d 的扩展切片", len(vals), len(idx))
	}
	for k, i := range idx {
		l.S[i] = vals[k]
	}
}

// DelSlice 实现 del xs[lo:hi:step]。
func (l *List[T]) DelSlice(lo, hi, step int) {
	lo, hi, step = sliceBounds(len(l.S), lo, hi, step)
	if step == 1 {
		if hi > lo {
			l.S = slices.Delete(l.S, lo, hi)
		}
		return
	}
	drop := make([]bool, len(l.S))
	for _, i := range sliceIndexes(lo, hi, step) {
		drop[i] = true
	}
	out := l.S[:0]
	for i, x := range l.S {
		if !drop[i] {
			out = append(out, x)
		}
	}
	clear(l.S[len(out):])
	l.S = out
}

// ===== 列表的扩展方法（由 prelude 声明） =====

func ListContains[T any](l *List[T], v T) bool {
	for _, x := range l.S {
		if Equal(x, v) {
			return true
		}
	}
	return false
}

// ListHas 是 x in list 的快速路径（元素类型可直接比较时）。
func ListHas[T comparable](l *List[T], v T) bool {
	return slices.Contains(l.S, v)
}

func ListIndex[T any](l *List[T], v T, start int) int {
	for i := start; i < len(l.S); i++ {
		if Equal(l.S[i], v) {
			return i
		}
	}
	Raise("ValueError", "%s 不在列表中", Repr(v))
	return -1
}

func ListFind[T any](l *List[T], v T) int {
	for i, x := range l.S {
		if Equal(x, v) {
			return i
		}
	}
	return -1
}

func ListCount[T any](l *List[T], v T) int {
	n := 0
	for _, x := range l.S {
		if Equal(x, v) {
			n++
		}
	}
	return n
}

func ListRemove[T any](l *List[T], v T) {
	for i, x := range l.S {
		if Equal(x, v) {
			l.S = slices.Delete(l.S, i, i+1)
			return
		}
	}
	Raise("ValueError", "%s 不在列表中", Repr(v))
}

func ListSort[T cmp.Ordered](l *List[T], reverse bool) {
	if reverse {
		slices.SortStableFunc(l.S, func(a, b T) int { return cmp.Compare(b, a) })
		return
	}
	slices.SortStableFunc(l.S, cmp.Compare[T])
}

func ListSortBy[T any, K any](l *List[T], key func(T) K, reverse bool) {
	keys := make([]K, len(l.S))
	for i, x := range l.S {
		keys[i] = key(x)
	}
	idx := make([]int, len(l.S))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		c := Compare(keys[a], keys[b])
		if reverse {
			return -c
		}
		return c
	})
	out := make([]T, len(l.S))
	for i, j := range idx {
		out[i] = l.S[j]
	}
	copy(l.S, out)
}

func ListSortCmp[T any](l *List[T], f func(T, T) int) {
	slices.SortStableFunc(l.S, f)
}

func ListSortAny[T any](l *List[T], reverse bool) {
	slices.SortStableFunc(l.S, func(a, b T) int {
		c := Compare(a, b)
		if reverse {
			return -c
		}
		return c
	})
}

func ListMap[T, U any](l *List[T], f func(T) U) *List[U] {
	out := make([]U, len(l.S))
	for i, x := range l.S {
		out[i] = f(x)
	}
	return &List[U]{S: out}
}

func ListFilter[T any](l *List[T], f func(T) bool) *List[T] {
	var out []T
	for _, x := range l.S {
		if f(x) {
			out = append(out, x)
		}
	}
	return &List[T]{S: out}
}

func ListReduce[T, A any](l *List[T], f func(A, T) A, init A) A {
	acc := init
	for _, x := range l.S {
		acc = f(acc, x)
	}
	return acc
}

func ListReduce1[T any](l *List[T], f func(T, T) T) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表做 reduce")
	}
	acc := l.S[0]
	for _, x := range l.S[1:] {
		acc = f(acc, x)
	}
	return acc
}

func ListForEach[T any](l *List[T], f func(T)) {
	for _, x := range l.S {
		f(x)
	}
}

func ListAny[T any](l *List[T], f func(T) bool) bool {
	for _, x := range l.S {
		if f(x) {
			return true
		}
	}
	return false
}

func ListAll[T any](l *List[T], f func(T) bool) bool {
	for _, x := range l.S {
		if !f(x) {
			return false
		}
	}
	return true
}

func ListCountIf[T any](l *List[T], f func(T) bool) int {
	n := 0
	for _, x := range l.S {
		if f(x) {
			n++
		}
	}
	return n
}

func ListFirst[T any](l *List[T], f func(T) bool) Opt[T] {
	for _, x := range l.S {
		if f(x) {
			return Opt[T]{V: x, Ok: true}
		}
	}
	return Opt[T]{}
}

func ListLast[T any](l *List[T], f func(T) bool) Opt[T] {
	for i := len(l.S) - 1; i >= 0; i-- {
		if f(l.S[i]) {
			return Opt[T]{V: l.S[i], Ok: true}
		}
	}
	return Opt[T]{}
}

func ListFindIndex[T any](l *List[T], f func(T) bool) int {
	for i, x := range l.S {
		if f(x) {
			return i
		}
	}
	return -1
}

func ListTake[T any](l *List[T], n int) *List[T] {
	if n > len(l.S) {
		n = len(l.S)
	}
	if n < 0 {
		n = 0
	}
	return &List[T]{S: slices.Clone(l.S[:n])}
}

func ListSkip[T any](l *List[T], n int) *List[T] {
	if n > len(l.S) {
		n = len(l.S)
	}
	if n < 0 {
		n = 0
	}
	return &List[T]{S: slices.Clone(l.S[n:])}
}

func ListTakeWhile[T any](l *List[T], f func(T) bool) *List[T] {
	i := 0
	for i < len(l.S) && f(l.S[i]) {
		i++
	}
	return &List[T]{S: slices.Clone(l.S[:i])}
}

func ListSkipWhile[T any](l *List[T], f func(T) bool) *List[T] {
	i := 0
	for i < len(l.S) && f(l.S[i]) {
		i++
	}
	return &List[T]{S: slices.Clone(l.S[i:])}
}

func ListFlatMap[T, U any](l *List[T], f func(T) *List[U]) *List[U] {
	var out []U
	for _, x := range l.S {
		out = append(out, f(x).S...)
	}
	return &List[U]{S: out}
}

func ListFlatten[T any](l *List[*List[T]]) *List[T] {
	var out []T
	for _, x := range l.S {
		out = append(out, x.S...)
	}
	return &List[T]{S: out}
}

func ListDistinct[T comparable](l *List[T]) *List[T] {
	seen := make(map[T]bool, len(l.S))
	var out []T
	for _, x := range l.S {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return &List[T]{S: out}
}

func ListGroupBy[T any, K comparable](l *List[T], key func(T) K) *Dict[K, *List[T]] {
	d := NewDict[K, *List[T]](0)
	for _, x := range l.S {
		k := key(x)
		if g, ok := d.Lookup(k); ok {
			g.S = append(g.S, x)
		} else {
			d.Set(k, &List[T]{S: []T{x}})
		}
	}
	return d
}

func ListToDict[T any, K comparable, V any](l *List[T], key func(T) K, val func(T) V) *Dict[K, V] {
	d := NewDict[K, V](len(l.S))
	for _, x := range l.S {
		d.Set(key(x), val(x))
	}
	return d
}

func ListToSet[T comparable](l *List[T]) *Set[T] {
	s := NewSet[T](len(l.S))
	for _, x := range l.S {
		s.Add(x)
	}
	return s
}

func ListSum[T Number](l *List[T]) T {
	var s T
	for _, x := range l.S {
		s += x
	}
	return s
}

func ListMin[T cmp.Ordered](l *List[T]) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最小值")
	}
	return slices.Min(l.S)
}

func ListMax[T cmp.Ordered](l *List[T]) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最大值")
	}
	return slices.Max(l.S)
}

func ListMinBy[T any, K any](l *List[T], key func(T) K) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最小值")
	}
	best, bk := l.S[0], key(l.S[0])
	for _, x := range l.S[1:] {
		if k := key(x); Compare(k, bk) < 0 {
			best, bk = x, k
		}
	}
	return best
}

func ListMaxBy[T any, K any](l *List[T], key func(T) K) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最大值")
	}
	best, bk := l.S[0], key(l.S[0])
	for _, x := range l.S[1:] {
		if k := key(x); Compare(k, bk) > 0 {
			best, bk = x, k
		}
	}
	return best
}

func ListAverage[T Number](l *List[T]) float64 {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求平均值")
	}
	var s float64
	for _, x := range l.S {
		s += float64(x)
	}
	return s / float64(len(l.S))
}

func ListSorted[T cmp.Ordered](l *List[T], reverse bool) *List[T] {
	c := l.Copy()
	ListSort(c, reverse)
	return c
}

func ListSortedBy[T any, K any](l *List[T], key func(T) K, reverse bool) *List[T] {
	c := l.Copy()
	ListSortBy(c, key, reverse)
	return c
}

func ListReversed[T any](l *List[T]) *List[T] {
	c := l.Copy()
	slices.Reverse(c.S)
	return c
}

func ListJoin[T any](l *List[T], sep string) string {
	var sb strings.Builder
	for i, x := range l.S {
		if i > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(Str(x))
	}
	return sb.String()
}

func ListZip[T, U any](l *List[T], o *List[U]) *List[T2[T, U]] {
	n := min(len(l.S), len(o.S))
	out := make([]T2[T, U], n)
	for i := 0; i < n; i++ {
		out[i] = T2[T, U]{l.S[i], o.S[i]}
	}
	return &List[T2[T, U]]{S: out}
}

func ListEnumerate[T any](l *List[T]) *List[T2[int, T]] {
	out := make([]T2[int, T], len(l.S))
	for i, x := range l.S {
		out[i] = T2[int, T]{i, x}
	}
	return &List[T2[int, T]]{S: out}
}

func ListChunk[T any](l *List[T], size int) *List[*List[T]] {
	if size <= 0 {
		Raise("ValueError", "分块大小必须为正数")
	}
	var out []*List[T]
	for i := 0; i < len(l.S); i += size {
		j := min(i+size, len(l.S))
		out = append(out, &List[T]{S: slices.Clone(l.S[i:j])})
	}
	return &List[*List[T]]{S: out}
}

func ListIsEmpty[T any](l *List[T]) bool { return len(l.S) == 0 }

func ListPartition[T any](l *List[T], f func(T) bool) T2[*List[T], *List[T]] {
	a, b := &List[T]{}, &List[T]{}
	for _, x := range l.S {
		if f(x) {
			a.S = append(a.S, x)
		} else {
			b.S = append(b.S, x)
		}
	}
	return T2[*List[T], *List[T]]{a, b}
}

func ListFirstOr[T any](l *List[T]) Opt[T] {
	if len(l.S) == 0 {
		return Opt[T]{}
	}
	return Opt[T]{V: l.S[0], Ok: true}
}

func ListLastOr[T any](l *List[T]) Opt[T] {
	if len(l.S) == 0 {
		return Opt[T]{}
	}
	return Opt[T]{V: l.S[len(l.S)-1], Ok: true}
}

func ListGet[T any](l *List[T], i int) Opt[T] {
	n := len(l.S)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return Opt[T]{}
	}
	return Opt[T]{V: l.S[i], Ok: true}
}

func ListSwap[T any](l *List[T], i, j int) {
	n := len(l.S)
	i, j = Ix(i, n), Ix(j, n)
	l.S[i], l.S[j] = l.S[j], l.S[i]
}

func ListFill[T any](n int, v T) *List[T] {
	if n < 0 {
		n = 0
	}
	s := make([]T, n)
	for i := range s {
		s[i] = v
	}
	return &List[T]{S: s}
}
