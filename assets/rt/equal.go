package rt

import (
	"cmp"
	"fmt"
	"reflect"
)

type eqer interface{ Eqꞏ(o any) bool }
type cmper interface{ Cmpꞏ(o any) int }

// Equal 按 Aether 语义比较两个值：容器逐元素比较，类按身份（或 __eq__），结构体按字段。
func Equal[T any](a, b T) bool {
	return equalAny(any(a), any(b))
}

func equalAny(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case int:
		y, ok := b.(int)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case Char:
		y, ok := b.(Char)
		return ok && x == y
	case eqer:
		return x.Eqꞏ(b)
	}
	if b == nil {
		return false
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta.Comparable() {
		defer func() { recover() }()
		return a == b
	}
	return reflect.DeepEqual(a, b)
}

// Compare 比较两个可排序的值（数字、字符串、字符、元组、列表）。
func Compare[T any](a, b T) int {
	// 常见类型直接比较，避免把值装箱为 any（例如 T: Sortable 的泛型代码实例化为 int）
	switch x := any(a).(type) {
	case int:
		return cmp.Compare(x, any(b).(int))
	case float64:
		return cmp.Compare(x, any(b).(float64))
	case string:
		return cmp.Compare(x, any(b).(string))
	}
	// 元组：右操作数按原类型传递，不需要装箱
	if t, ok := any(a).(interface{ CmpSameꞏ(T) int }); ok {
		return t.CmpSameꞏ(b)
	}
	return compareAny(any(a), any(b))
}

func compareAny(a, b any) int {
	switch x := a.(type) {
	case int:
		return cmp.Compare(x, b.(int))
	case float64:
		return cmp.Compare(x, b.(float64))
	case string:
		return cmp.Compare(x, b.(string))
	case Char:
		return cmp.Compare(x, b.(Char))
	case bool:
		y := b.(bool)
		if x == y {
			return 0
		}
		if !x {
			return -1
		}
		return 1
	case int8:
		return cmp.Compare(x, b.(int8))
	case int16:
		return cmp.Compare(x, b.(int16))
	case int32:
		return cmp.Compare(x, b.(int32))
	case int64:
		return cmp.Compare(x, b.(int64))
	case uint8:
		return cmp.Compare(x, b.(uint8))
	case uint16:
		return cmp.Compare(x, b.(uint16))
	case uint32:
		return cmp.Compare(x, b.(uint32))
	case uint64:
		return cmp.Compare(x, b.(uint64))
	case float32:
		return cmp.Compare(x, b.(float32))
	case cmper:
		return x.Cmpꞏ(b)
	}
	// 其他整数类型（例如枚举）
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch va.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmp.Compare(va.Int(), vb.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return cmp.Compare(va.Uint(), vb.Uint())
	case reflect.Float32, reflect.Float64:
		return cmp.Compare(va.Float(), vb.Float())
	case reflect.String:
		return cmp.Compare(va.String(), vb.String())
	}
	Raise("TypeError", "%s 类型的值不能比较大小", TypeName(a))
	return 0
}

// Less 用于元组比较 a < b。
func Less[T any](a, b T) bool { return Compare(a, b) < 0 }

func panicf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}
