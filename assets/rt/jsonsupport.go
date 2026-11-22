package rt

// JSON 等子包使用的容器方法。

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

