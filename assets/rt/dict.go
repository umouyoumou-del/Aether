package rt

import (
	"iter"
	"strings"
)

// Dict 是保持插入顺序的字典 dict[K, V]。
type Dict[K comparable, V any] struct {
	idx  map[K]int
	keys []K
	vals []V
	dead []bool
	ndel int
}

func NewDict[K comparable, V any](capacity int) *Dict[K, V] {
	return &Dict[K, V]{idx: make(map[K]int, capacity)}
}

// DictLit 由字面量构造字典（键重复时后者覆盖前者）。
func DictLit[K comparable, V any](keys []K, vals []V) *Dict[K, V] {
	d := NewDict[K, V](len(keys))
	for i, k := range keys {
		d.Set(k, vals[i])
	}
	return d
}

func DictFrom[K comparable, V any](it iter.Seq[T2[K, V]]) *Dict[K, V] {
	d := NewDict[K, V](0)
	for p := range it {
		d.Set(p.V0, p.V1)
	}
	return d
}

func (d *Dict[K, V]) Len() int { return len(d.keys) - d.ndel }

func (d *Dict[K, V]) Lookup(k K) (V, bool) {
	if i, ok := d.idx[k]; ok {
		return d.vals[i], true
	}
	var zero V
	return zero, false
}

func (d *Dict[K, V]) Get(k K) V {
	if i, ok := d.idx[k]; ok {
		return d.vals[i]
	}
	Raise("KeyError", "%s", Repr(k))
	var zero V
	return zero
}

func (d *Dict[K, V]) GetOpt(k K) Opt[V] {
	if i, ok := d.idx[k]; ok {
		return Opt[V]{V: d.vals[i], Ok: true}
	}
	return Opt[V]{}
}

func (d *Dict[K, V]) GetOr(k K, def V) V {
	if i, ok := d.idx[k]; ok {
		return d.vals[i]
	}
	return def
}

func (d *Dict[K, V]) Set(k K, v V) {
	if d.idx == nil {
		d.idx = map[K]int{}
	}
	if i, ok := d.idx[k]; ok {
		d.vals[i] = v
		return
	}
	d.idx[k] = len(d.keys)
	d.keys = append(d.keys, k)
	d.vals = append(d.vals, v)
	if d.dead != nil {
		d.dead = append(d.dead, false)
	}
}

func (d *Dict[K, V]) Has(k K) bool {
	_, ok := d.idx[k]
	return ok
}

func (d *Dict[K, V]) remove(i int) {
	k := d.keys[i]
	delete(d.idx, k)
	if d.dead == nil {
		d.dead = make([]bool, len(d.keys))
	}
	d.dead[i] = true
	var zk K
	var zv V
	d.keys[i] = zk
	d.vals[i] = zv
	d.ndel++
	if d.ndel > 16 && d.ndel*2 > len(d.keys) {
		d.compact()
	}
}

func (d *Dict[K, V]) compact() {
	keys := make([]K, 0, len(d.keys)-d.ndel)
	vals := make([]V, 0, len(d.keys)-d.ndel)
	for i := range d.keys {
		if !d.dead[i] {
			d.idx[d.keys[i]] = len(keys)
			keys = append(keys, d.keys[i])
			vals = append(vals, d.vals[i])
		}
	}
	d.keys, d.vals, d.dead, d.ndel = keys, vals, nil, 0
}

// Delete 实现 del d[k]（键不存在时抛出 KeyError）。
func (d *Dict[K, V]) Delete(k K) {
	i, ok := d.idx[k]
	if !ok {
		Raise("KeyError", "%s", Repr(k))
	}
	d.remove(i)
}

func (d *Dict[K, V]) Discard(k K) bool {
	if i, ok := d.idx[k]; ok {
		d.remove(i)
		return true
	}
	return false
}

func (d *Dict[K, V]) Pop(k K) V {
	i, ok := d.idx[k]
	if !ok {
		Raise("KeyError", "%s", Repr(k))
	}
	v := d.vals[i]
	d.remove(i)
	return v
}

func (d *Dict[K, V]) PopOr(k K, def V) V {
	i, ok := d.idx[k]
	if !ok {
		return def
	}
	v := d.vals[i]
	d.remove(i)
	return v
}

func (d *Dict[K, V]) SetDefault(k K, v V) V {
	if i, ok := d.idx[k]; ok {
		return d.vals[i]
	}
	d.Set(k, v)
	return v
}

func (d *Dict[K, V]) Clear() {
	clear(d.idx)
	d.keys, d.vals, d.dead, d.ndel = nil, nil, nil, 0
}

func (d *Dict[K, V]) Copy() *Dict[K, V] {
	n := NewDict[K, V](d.Len())
	for k, v := range d.All() {
		n.Set(k, v)
	}
	return n
}

func (d *Dict[K, V]) Update(o *Dict[K, V]) {
	for k, v := range o.All() {
		d.Set(k, v)
	}
}

func (d *Dict[K, V]) Merge(o *Dict[K, V]) *Dict[K, V] {
	n := d.Copy()
	n.Update(o)
	return n
}

// All 按插入顺序遍历键值对。遍历开始后新增的键不会被访问。
func (d *Dict[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		n := len(d.keys)
		for i := 0; i < n && i < len(d.keys); i++ {
			if d.dead != nil && d.dead[i] {
				continue
			}
			if !yield(d.keys[i], d.vals[i]) {
				return
			}
		}
	}
}

func (d *Dict[K, V]) KeysIter() iter.Seq[K] {
	return func(yield func(K) bool) {
		n := len(d.keys)
		for i := 0; i < n && i < len(d.keys); i++ {
			if d.dead != nil && d.dead[i] {
				continue
			}
			if !yield(d.keys[i]) {
				return
			}
		}
	}
}

func (d *Dict[K, V]) ValuesIter() iter.Seq[V] {
	return func(yield func(V) bool) {
		n := len(d.keys)
		for i := 0; i < n && i < len(d.keys); i++ {
			if d.dead != nil && d.dead[i] {
				continue
			}
			if !yield(d.vals[i]) {
				return
			}
		}
	}
}

func (d *Dict[K, V]) ItemsIter() iter.Seq[T2[K, V]] {
	return func(yield func(T2[K, V]) bool) {
		for k, v := range d.All() {
			if !yield(T2[K, V]{k, v}) {
				return
			}
		}
	}
}

func (d *Dict[K, V]) Keys() *List[K] {
	out := make([]K, 0, d.Len())
	for k := range d.KeysIter() {
		out = append(out, k)
	}
	return &List[K]{S: out}
}

func (d *Dict[K, V]) Values() *List[V] {
	out := make([]V, 0, d.Len())
	for v := range d.ValuesIter() {
		out = append(out, v)
	}
	return &List[V]{S: out}
}

func (d *Dict[K, V]) Items() *List[T2[K, V]] {
	out := make([]T2[K, V], 0, d.Len())
	for k, v := range d.All() {
		out = append(out, T2[K, V]{k, v})
	}
	return &List[T2[K, V]]{S: out}
}

func (d *Dict[K, V]) String() string {
	if d == nil {
		return "null"
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for k, v := range d.All() {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(Repr(k))
		sb.WriteString(": ")
		sb.WriteString(Repr(v))
	}
	sb.WriteByte('}')
	return sb.String()
}

func (d *Dict[K, V]) Eqꞏ(o any) bool {
	r, ok := o.(*Dict[K, V])
	if !ok {
		return false
	}
	if d == r {
		return true
	}
	if d.Len() != r.Len() {
		return false
	}
	for k, v := range d.All() {
		rv, ok := r.Lookup(k)
		if !ok || !Equal(v, rv) {
			return false
		}
	}
	return true
}

// ===== 字典的扩展方法 =====

func DictGetOr[K comparable, V any](d *Dict[K, V], k K, def V) V { return d.GetOr(k, def) }

func DictFilter[K comparable, V any](d *Dict[K, V], f func(K, V) bool) *Dict[K, V] {
	n := NewDict[K, V](0)
	for k, v := range d.All() {
		if f(k, v) {
			n.Set(k, v)
		}
	}
	return n
}

func DictMapValues[K comparable, V, U any](d *Dict[K, V], f func(V) U) *Dict[K, U] {
	n := NewDict[K, U](d.Len())
	for k, v := range d.All() {
		n.Set(k, f(v))
	}
	return n
}

// Counter 统计元素出现次数。
func Counter[T comparable](it iter.Seq[T]) *Dict[T, int] {
	d := NewDict[T, int](0)
	for x := range it {
		if i, ok := d.idx[x]; ok {
			d.vals[i]++
		} else {
			d.Set(x, 1)
		}
	}
	return d
}

// DictSortedByValue 返回按值排序的键值对列表。
func DictSortedByValue[K comparable, V any](d *Dict[K, V], reverse bool) *List[T2[K, V]] {
	items := d.Items()
	ListSortBy(items, func(p T2[K, V]) V { return p.V1 }, reverse)
	return items
}

func DictSortedByKey[K comparable, V any](d *Dict[K, V], reverse bool) *List[T2[K, V]] {
	items := d.Items()
	ListSortBy(items, func(p T2[K, V]) K { return p.V0 }, reverse)
	return items
}
