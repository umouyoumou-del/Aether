package rt

// Opt 是可空类型 T? 的表示。Ok 为 false 时 V 必须是零值。
type Opt[T any] struct {
	V  T
	Ok bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{V: v, Ok: true} }

func None[T any]() Opt[T] { return Opt[T]{} }

// Must 实现 x!：为 null 时抛出 NullError。
func (o Opt[T]) Must() T {
	if !o.Ok {
		Raise("NullError", "值为 null（使用了 ! 断言非空）")
	}
	return o.V
}

func (o Opt[T]) Or(d T) T {
	if o.Ok {
		return o.V
	}
	return d
}

func (o Opt[T]) OrOpt(d Opt[T]) Opt[T] {
	if o.Ok {
		return o
	}
	return d
}

func (o Opt[T]) Any() any {
	if o.Ok {
		return o.V
	}
	return nil
}

func (o Opt[T]) String() string {
	if !o.Ok {
		return "null"
	}
	return Str(o.V)
}

func (o Opt[T]) Reprꞏ() string {
	if !o.Ok {
		return "null"
	}
	return Repr(o.V)
}

func (o Opt[T]) Eqꞏ(x any) bool {
	r, ok := x.(Opt[T])
	if !ok {
		return false
	}
	if o.Ok != r.Ok {
		return false
	}
	return !o.Ok || Equal(o.V, r.V)
}

func OptMap[A, B any](o Opt[A], f func(A) B) Opt[B] {
	if !o.Ok {
		return Opt[B]{}
	}
	return Opt[B]{V: f(o.V), Ok: true}
}

// OptOf 把可能为 nil 的 Go 值包装为 Opt。
func OptOf[T comparable](v T) Opt[T] {
	var zero T
	if v == zero {
		return Opt[T]{}
	}
	return Opt[T]{V: v, Ok: true}
}
