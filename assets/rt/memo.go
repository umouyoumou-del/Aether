package rt

import (
	"container/list"
	"sync"
)

// Memo 是 @cache / @lru_cache 使用的缓存（并发安全）。max <= 0 表示不限大小，
// 否则只保留最近使用的 max 项。计算结果时不持有锁，因此被缓存的函数可以递归调用自己。
type Memo[K comparable, V any] struct {
	mu  sync.Mutex
	all map[K]V
	max int
	idx map[K]*list.Element
	lru *list.List
}

type memoEntry[K comparable, V any] struct {
	k K
	v V
}

func NewMemo[K comparable, V any](max int) *Memo[K, V] {
	if max > 0 {
		return &Memo[K, V]{max: max, idx: map[K]*list.Element{}, lru: list.New()}
	}
	return &Memo[K, V]{all: map[K]V{}}
}

func (m *Memo[K, V]) Get(k K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.max <= 0 {
		v, ok := m.all[k]
		return v, ok
	}
	if e, ok := m.idx[k]; ok {
		m.lru.MoveToFront(e)
		return e.Value.(memoEntry[K, V]).v, true
	}
	var zero V
	return zero, false
}

func (m *Memo[K, V]) Put(k K, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.max <= 0 {
		m.all[k] = v
		return
	}
	if e, ok := m.idx[k]; ok {
		e.Value = memoEntry[K, V]{k, v}
		m.lru.MoveToFront(e)
		return
	}
	m.idx[k] = m.lru.PushFront(memoEntry[K, V]{k, v})
	if m.lru.Len() > m.max {
		last := m.lru.Back()
		m.lru.Remove(last)
		delete(m.idx, last.Value.(memoEntry[K, V]).k)
	}
}
