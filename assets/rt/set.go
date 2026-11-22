package rt

import (
	"iter"
	"strings"
)

// Set 是保持插入顺序的集合 set[T]。
type Set[T comparable] struct {
	d Dict[T, struct{}]
}

func NewSet[T comparable](capacity int) *Set[T] {
	return &Set[T]{d: Dict[T, struct{}]{idx: make(map[T]int, capacity)}}
}

func SetOf[T comparable](xs ...T) *Set[T] {
	s := NewSet[T](len(xs))
	for _, x := range xs {
		s.Add(x)
	}
	return s
}

func SetFrom[T comparable](it iter.Seq[T]) *Set[T] {
	s := NewSet[T](0)
	for x := range it {
		s.Add(x)
	}
	return s
}

func (s *Set[T]) Len() int       { return s.d.Len() }
func (s *Set[T]) Has(x T) bool   { return s.d.Has(x) }
func (s *Set[T]) Add(x T)        { s.d.Set(x, struct{}{}) }
func (s *Set[T]) Discard(x T)    { s.d.Discard(x) }
func (s *Set[T]) Clear()         { s.d.Clear() }
func (s *Set[T]) All() iter.Seq[T] { return s.d.KeysIter() }

func (s *Set[T]) Remove(x T) {
	if !s.d.Discard(x) {
		Raise("KeyError", "%s", Repr(x))
	}
}

func (s *Set[T]) Pop() T {
	for x := range s.All() {
		s.d.Discard(x)
		return x
	}
	Raise("KeyError", "不能从空集合中 pop")
	var zero T
	return zero
}

func (s *Set[T]) Copy() *Set[T] {
	n := NewSet[T](s.Len())
	for x := range s.All() {
		n.Add(x)
	}
	return n
}

func (s *Set[T]) Update(o *Set[T]) {
	for x := range o.All() {
		s.Add(x)
	}
}

func (s *Set[T]) UpdateIter(it iter.Seq[T]) {
	for x := range it {
		s.Add(x)
	}
}

func (s *Set[T]) Union(o *Set[T]) *Set[T] {
	n := s.Copy()
	n.Update(o)
	return n
}

func (s *Set[T]) Intersect(o *Set[T]) *Set[T] {
	n := NewSet[T](0)
	for x := range s.All() {
		if o.Has(x) {
			n.Add(x)
		}
	}
	return n
}

func (s *Set[T]) Diff(o *Set[T]) *Set[T] {
	n := NewSet[T](0)
	for x := range s.All() {
		if !o.Has(x) {
			n.Add(x)
		}
	}
	return n
}

func (s *Set[T]) SymDiff(o *Set[T]) *Set[T] {
	n := s.Diff(o)
	for x := range o.All() {
		if !s.Has(x) {
			n.Add(x)
		}
	}
	return n
}

func (s *Set[T]) IsSubset(o *Set[T]) bool {
	for x := range s.All() {
		if !o.Has(x) {
			return false
		}
	}
	return true
}

func (s *Set[T]) IsSuperset(o *Set[T]) bool { return o.IsSubset(s) }

func (s *Set[T]) IsDisjoint(o *Set[T]) bool {
	for x := range s.All() {
		if o.Has(x) {
			return false
		}
	}
	return true
}

func (s *Set[T]) ToList() *List[T] {
	return s.d.Keys()
}

func (s *Set[T]) String() string {
	if s == nil {
		return "null"
	}
	if s.Len() == 0 {
		return "set()"
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for x := range s.All() {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(Repr(x))
	}
	sb.WriteByte('}')
	return sb.String()
}

func (s *Set[T]) Eqꞏ(o any) bool {
	r, ok := o.(*Set[T])
	if !ok {
		return false
	}
	return s.Len() == r.Len() && s.IsSubset(r)
}

func SetMap[T comparable, U comparable](s *Set[T], f func(T) U) *Set[U] {
	n := NewSet[U](s.Len())
	for x := range s.All() {
		n.Add(f(x))
	}
	return n
}

func SetFilter[T comparable](s *Set[T], f func(T) bool) *Set[T] {
	n := NewSet[T](0)
	for x := range s.All() {
		if f(x) {
			n.Add(x)
		}
	}
	return n
}
