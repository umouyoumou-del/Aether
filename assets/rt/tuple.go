// 由脚本生成：元组类型 T2..T8。
package rt

import "strings"

type T2[A any, B any] struct {
	V0 A
	V1 B
}

func (t T2[A, B]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteByte(')')
	return sb.String()
}

func (t T2[A, B]) Eqꞏ(o any) bool {
	r, ok := o.(T2[A, B])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1)
}

func (t T2[A, B]) Cmpꞏ(o any) int {
	r := o.(T2[A, B])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	return 0
}

type T3[A any, B any, C any] struct {
	V0 A
	V1 B
	V2 C
}

func (t T3[A, B, C]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteByte(')')
	return sb.String()
}

func (t T3[A, B, C]) Eqꞏ(o any) bool {
	r, ok := o.(T3[A, B, C])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2)
}

func (t T3[A, B, C]) Cmpꞏ(o any) int {
	r := o.(T3[A, B, C])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	return 0
}

type T4[A any, B any, C any, D any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
}

func (t T4[A, B, C, D]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V3))
	sb.WriteByte(')')
	return sb.String()
}

func (t T4[A, B, C, D]) Eqꞏ(o any) bool {
	r, ok := o.(T4[A, B, C, D])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2) && Equal(t.V3, r.V3)
}

func (t T4[A, B, C, D]) Cmpꞏ(o any) int {
	r := o.(T4[A, B, C, D])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	if c := Compare(t.V3, r.V3); c != 0 {
		return c
	}
	return 0
}

type T5[A any, B any, C any, D any, E any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
}

func (t T5[A, B, C, D, E]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V3))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V4))
	sb.WriteByte(')')
	return sb.String()
}

func (t T5[A, B, C, D, E]) Eqꞏ(o any) bool {
	r, ok := o.(T5[A, B, C, D, E])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2) && Equal(t.V3, r.V3) && Equal(t.V4, r.V4)
}

func (t T5[A, B, C, D, E]) Cmpꞏ(o any) int {
	r := o.(T5[A, B, C, D, E])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	if c := Compare(t.V3, r.V3); c != 0 {
		return c
	}
	if c := Compare(t.V4, r.V4); c != 0 {
		return c
	}
	return 0
}

type T6[A any, B any, C any, D any, E any, F any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
}

func (t T6[A, B, C, D, E, F]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V3))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V4))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V5))
	sb.WriteByte(')')
	return sb.String()
}

func (t T6[A, B, C, D, E, F]) Eqꞏ(o any) bool {
	r, ok := o.(T6[A, B, C, D, E, F])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2) && Equal(t.V3, r.V3) && Equal(t.V4, r.V4) && Equal(t.V5, r.V5)
}

func (t T6[A, B, C, D, E, F]) Cmpꞏ(o any) int {
	r := o.(T6[A, B, C, D, E, F])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	if c := Compare(t.V3, r.V3); c != 0 {
		return c
	}
	if c := Compare(t.V4, r.V4); c != 0 {
		return c
	}
	if c := Compare(t.V5, r.V5); c != 0 {
		return c
	}
	return 0
}

type T7[A any, B any, C any, D any, E any, F any, G any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
	V6 G
}

func (t T7[A, B, C, D, E, F, G]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V3))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V4))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V5))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V6))
	sb.WriteByte(')')
	return sb.String()
}

func (t T7[A, B, C, D, E, F, G]) Eqꞏ(o any) bool {
	r, ok := o.(T7[A, B, C, D, E, F, G])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2) && Equal(t.V3, r.V3) && Equal(t.V4, r.V4) && Equal(t.V5, r.V5) && Equal(t.V6, r.V6)
}

func (t T7[A, B, C, D, E, F, G]) Cmpꞏ(o any) int {
	r := o.(T7[A, B, C, D, E, F, G])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	if c := Compare(t.V3, r.V3); c != 0 {
		return c
	}
	if c := Compare(t.V4, r.V4); c != 0 {
		return c
	}
	if c := Compare(t.V5, r.V5); c != 0 {
		return c
	}
	if c := Compare(t.V6, r.V6); c != 0 {
		return c
	}
	return 0
}

type T8[A any, B any, C any, D any, E any, F any, G any, H any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
	V6 G
	V7 H
}

func (t T8[A, B, C, D, E, F, G, H]) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	sb.WriteString(Repr(t.V0))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V1))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V2))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V3))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V4))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V5))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V6))
	sb.WriteString(", ")
	sb.WriteString(Repr(t.V7))
	sb.WriteByte(')')
	return sb.String()
}

func (t T8[A, B, C, D, E, F, G, H]) Eqꞏ(o any) bool {
	r, ok := o.(T8[A, B, C, D, E, F, G, H])
	return ok && Equal(t.V0, r.V0) && Equal(t.V1, r.V1) && Equal(t.V2, r.V2) && Equal(t.V3, r.V3) && Equal(t.V4, r.V4) && Equal(t.V5, r.V5) && Equal(t.V6, r.V6) && Equal(t.V7, r.V7)
}

func (t T8[A, B, C, D, E, F, G, H]) Cmpꞏ(o any) int {
	r := o.(T8[A, B, C, D, E, F, G, H])
	if c := Compare(t.V0, r.V0); c != 0 {
		return c
	}
	if c := Compare(t.V1, r.V1); c != 0 {
		return c
	}
	if c := Compare(t.V2, r.V2); c != 0 {
		return c
	}
	if c := Compare(t.V3, r.V3); c != 0 {
		return c
	}
	if c := Compare(t.V4, r.V4); c != 0 {
		return c
	}
	if c := Compare(t.V5, r.V5); c != 0 {
		return c
	}
	if c := Compare(t.V6, r.V6); c != 0 {
		return c
	}
	if c := Compare(t.V7, r.V7); c != 0 {
		return c
	}
	return 0
}
