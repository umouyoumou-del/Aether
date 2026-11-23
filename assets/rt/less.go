package rt

import (
	"iter"
	"slices"
)

// Lesser 是定义了 __lt__ 的 Aether 类型（编译器为其生成 Lessꞏ 方法）。
type Lesser[T any] interface {
	Lessꞏ(T) bool
}

func lessCmp[T Lesser[T]](a, b T) int {
	if a.Lessꞏ(b) {
		return -1
	}
	if b.Lessꞏ(a) {
		return 1
	}
	return 0
}

func ListSortLess[T Lesser[T]](l *List[T], reverse bool) {
	if reverse {
		slices.SortStableFunc(l.S, func(a, b T) int { return -lessCmp(a, b) })
		return
	}
	slices.SortStableFunc(l.S, lessCmp[T])
}

func ListSortedLess[T Lesser[T]](l *List[T], reverse bool) *List[T] {
	c := l.Copy()
	ListSortLess(c, reverse)
	return c
}

func ListMinLess[T Lesser[T]](l *List[T]) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最小值")
	}
	m := l.S[0]
	for _, x := range l.S[1:] {
		if x.Lessꞏ(m) {
			m = x
		}
	}
	return m
}

func ListMaxLess[T Lesser[T]](l *List[T]) T {
	if len(l.S) == 0 {
		Raise("ValueError", "不能对空列表求最大值")
	}
	m := l.S[0]
	for _, x := range l.S[1:] {
		if m.Lessꞏ(x) {
			m = x
		}
	}
	return m
}

func SortedLess[T Lesser[T]](it iter.Seq[T], reverse bool) *List[T] {
	l := ListFrom(it)
	ListSortLess(l, reverse)
	return l
}

func MinIterLess[T Lesser[T]](it iter.Seq[T], def Opt[T]) T {
	var m T
	first := true
	for x := range it {
		if first || x.Lessꞏ(m) {
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

func MaxIterLess[T Lesser[T]](it iter.Seq[T], def Opt[T]) T {
	var m T
	first := true
	for x := range it {
		if first || m.Lessꞏ(x) {
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

func MinLess[T Lesser[T]](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x.Lessꞏ(m) {
			m = x
		}
	}
	return m
}

func MaxLess[T Lesser[T]](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if m.Lessꞏ(x) {
			m = x
		}
	}
	return m
}
