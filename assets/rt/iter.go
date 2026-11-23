package rt

import (
	"cmp"
	"iter"
	"slices"
)

// Range 实现作为值使用的 range(a, b, step)。
func Range[T Integer](a, b, step T) iter.Seq[T] {
	if step == 0 {
		Raise("ValueError", "range 的步长不能为 0")
	}
	return func(yield func(T) bool) {
		if step > 0 {
			for i := a; i < b; i += step {
				if !yield(i) {
					return
				}
			}
		} else {
			for i := a; i > b; i += step {
				if !yield(i) {
					return
				}
			}
		}
	}
}

func CheckStep(step int) int {
	if step == 0 {
		Raise("ValueError", "range 的步长不能为 0")
	}
	return step
}

func Enumerate[T any](it iter.Seq[T], start int) iter.Seq[T2[int, T]] {
	return func(yield func(T2[int, T]) bool) {
		i := start
		for x := range it {
			if !yield(T2[int, T]{i, x}) {
				return
			}
			i++
		}
	}
}

func Zip2[A, B any](a iter.Seq[A], b iter.Seq[B]) iter.Seq[T2[A, B]] {
	return func(yield func(T2[A, B]) bool) {
		next, stop := iter.Pull(b)
		defer stop()
		for x := range a {
			y, ok := next()
			if !ok {
				return
			}
			if !yield(T2[A, B]{x, y}) {
				return
			}
		}
	}
}

func Zip3[A, B, C any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C]) iter.Seq[T3[A, B, C]] {
	return func(yield func(T3[A, B, C]) bool) {
		nb, sb := iter.Pull(b)
		defer sb()
		nc, sc := iter.Pull(c)
		defer sc()
		for x := range a {
			y, ok := nb()
			if !ok {
				return
			}
			z, ok := nc()
			if !ok {
				return
			}
			if !yield(T3[A, B, C]{x, y, z}) {
				return
			}
		}
	}
}

func ChanIter[T any](ch chan T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for x := range ch {
			if !yield(x) {
				return
			}
		}
	}
}

func Sum[T Number](it iter.Seq[T], start T) T {
	s := start
	for x := range it {
		s += x
	}
	return s
}

func SumStr(it iter.Seq[string], start string) string {
	s := start
	for x := range it {
		s += x
	}
	return s
}

func MinIter[T cmp.Ordered](it iter.Seq[T], def Opt[T]) T {
	var m T
	first := true
	for x := range it {
		if first || x < m {
			m = x
			first = false
		}
	}
	if first {
		if def.Ok {
			return def.V
		}
		Raise("ValueError", "min() 的参数是空序列")
	}
	return m
}

func MaxIter[T cmp.Ordered](it iter.Seq[T], def Opt[T]) T {
	var m T
	first := true
	for x := range it {
		if first || x > m {
			m = x
			first = false
		}
	}
	if first {
		if def.Ok {
			return def.V
		}
		Raise("ValueError", "max() 的参数是空序列")
	}
	return m
}

func MinIterBy[T, K any](it iter.Seq[T], key func(T) K, def Opt[T]) T {
	var m T
	var mk K
	first := true
	for x := range it {
		k := key(x)
		if first || Compare(k, mk) < 0 {
			m, mk = x, k
			first = false
		}
	}
	if first {
		if def.Ok {
			return def.V
		}
		Raise("ValueError", "min() 的参数是空序列")
	}
	return m
}

func MaxIterBy[T, K any](it iter.Seq[T], key func(T) K, def Opt[T]) T {
	var m T
	var mk K
	first := true
	for x := range it {
		k := key(x)
		if first || Compare(k, mk) > 0 {
			m, mk = x, k
			first = false
		}
	}
	if first {
		if def.Ok {
			return def.V
		}
		Raise("ValueError", "max() 的参数是空序列")
	}
	return m
}

func MinIterCmp[T any](it iter.Seq[T], def Opt[T]) T {
	return MinIterBy(it, func(x T) T { return x }, def)
}

func MaxIterCmp[T any](it iter.Seq[T], def Opt[T]) T {
	return MaxIterBy(it, func(x T) T { return x }, def)
}

func Sorted[T cmp.Ordered](it iter.Seq[T], reverse bool) *List[T] {
	l := ListFrom(it)
	ListSort(l, reverse)
	return l
}

func SortedBy[T, K any](it iter.Seq[T], key func(T) K, reverse bool) *List[T] {
	l := ListFrom(it)
	ListSortBy(l, key, reverse)
	return l
}

func SortedCmp[T any](it iter.Seq[T], reverse bool) *List[T] {
	l := ListFrom(it)
	ListSortAny(l, reverse)
	return l
}

func Reversed[T any](it iter.Seq[T]) *List[T] {
	l := ListFrom(it)
	slices.Reverse(l.S)
	return l
}

func AnyOf(it iter.Seq[bool]) bool {
	for x := range it {
		if x {
			return true
		}
	}
	return false
}

func AllOf(it iter.Seq[bool]) bool {
	for x := range it {
		if !x {
			return false
		}
	}
	return true
}

// ZipLists 实现 zip(*rows)：按列遍历（以最短的一行为准），每一列是一个列表。
func ZipLists[T any](rows *List[*List[T]]) iter.Seq[*List[T]] {
	return func(yield func(*List[T]) bool) {
		if len(rows.S) == 0 {
			return
		}
		n := len(rows.S[0].S)
		for _, r := range rows.S {
			n = min(n, len(r.S))
		}
		for i := 0; i < n; i++ {
			col := make([]T, len(rows.S))
			for k, r := range rows.S {
				col[k] = r.S[i]
			}
			if !yield(&List[T]{S: col}) {
				return
			}
		}
	}
}

// ZipStrs 实现 zip(*strs)：按位置取出各个字符串的字符。
func ZipStrs(rows *List[string]) iter.Seq[*List[Char]] {
	lists := &List[*List[Char]]{}
	for _, s := range rows.S {
		lists.S = append(lists.S, &List[Char]{S: []Char(func() []Char {
			var cs []Char
			for _, r := range s {
				cs = append(cs, Char(r))
			}
			return cs
		}())})
	}
	return ZipLists(lists)
}

// ===== 迭代器的扩展方法 =====

func IterMap[T, U any](it iter.Seq[T], f func(T) U) iter.Seq[U] {
	return func(yield func(U) bool) {
		for x := range it {
			if !yield(f(x)) {
				return
			}
		}
	}
}

func IterFilter[T any](it iter.Seq[T], f func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for x := range it {
			if f(x) && !yield(x) {
				return
			}
		}
	}
}

func IterTake[T any](it iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for x := range it {
			if !yield(x) {
				return
			}
			i++
			if i >= n {
				return
			}
		}
	}
}

func IterSkip[T any](it iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		i := 0
		for x := range it {
			if i < n {
				i++
				continue
			}
			if !yield(x) {
				return
			}
		}
	}
}

func IterTakeWhile[T any](it iter.Seq[T], f func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for x := range it {
			if !f(x) || !yield(x) {
				return
			}
		}
	}
}

func IterToList[T any](it iter.Seq[T]) *List[T] { return ListFrom(it) }

func IterCount[T any](it iter.Seq[T]) int {
	n := 0
	for range it {
		n++
	}
	return n
}

func IterFirst[T any](it iter.Seq[T]) Opt[T] {
	for x := range it {
		return Opt[T]{V: x, Ok: true}
	}
	return Opt[T]{}
}

func IterReduce[T, A any](it iter.Seq[T], f func(A, T) A, init A) A {
	acc := init
	for x := range it {
		acc = f(acc, x)
	}
	return acc
}

func IterForEach[T any](it iter.Seq[T], f func(T)) {
	for x := range it {
		f(x)
	}
}

func IterAny[T any](it iter.Seq[T], f func(T) bool) bool {
	for x := range it {
		if f(x) {
			return true
		}
	}
	return false
}

func IterAll[T any](it iter.Seq[T], f func(T) bool) bool {
	for x := range it {
		if !f(x) {
			return false
		}
	}
	return true
}

func IterEnumerate[T any](it iter.Seq[T]) iter.Seq[T2[int, T]] { return Enumerate(it, 0) }

func IterChain[T any](a, b iter.Seq[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for x := range a {
			if !yield(x) {
				return
			}
		}
		for x := range b {
			if !yield(x) {
				return
			}
		}
	}
}

func IterSum[T Number](it iter.Seq[T]) T { return Sum(it, 0) }

func IterJoin[T any](it iter.Seq[T], sep string) string {
	return StrJoin(sep, IterMap(it, func(x T) string { return Str(x) }))
}

// Count 生成无限递增序列。
func Count(start, step int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := start; ; i += step {
			if !yield(i) {
				return
			}
		}
	}
}
