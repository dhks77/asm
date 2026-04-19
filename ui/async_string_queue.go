package ui

import (
	"sync"
	"time"
)

const trackerFetchConcurrency = 10
const trackerResultFlushDelay = 16 * time.Millisecond

type asyncQueue[T any] struct {
	queue       []T
	active      map[string]T
	queued      map[string]bool
	keyFn       func(T) string
	maxInFlight int
}

type asyncStringQueue = asyncQueue[string]

func newAsyncQueue[T any](maxInFlight int, keyFn func(T) string) asyncQueue[T] {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	return asyncQueue[T]{
		active:      make(map[string]T),
		queued:      make(map[string]bool),
		keyFn:       keyFn,
		maxInFlight: maxInFlight,
	}
}

func newAsyncStringQueue(keyFn func(string) string) asyncStringQueue {
	return newAsyncQueue[string](trackerFetchConcurrency, keyFn)
}

func (q *asyncQueue[T]) normalize(item T) string {
	if q.keyFn != nil {
		return q.keyFn(item)
	}
	return ""
}

func (q *asyncQueue[T]) ensure() {
	if q.active == nil {
		q.active = make(map[string]T)
	}
	if q.queued == nil {
		q.queued = make(map[string]bool)
	}
	if q.maxInFlight <= 0 {
		q.maxInFlight = 1
	}
}

func (q *asyncQueue[T]) Clear() {
	q.ensure()
	q.queue = nil
	clear(q.active)
	clear(q.queued)
}

func (q *asyncQueue[T]) Reset(items []T) {
	q.Clear()
	q.Enqueue(items...)
}

func (q *asyncQueue[T]) Contains(item T) bool {
	q.ensure()
	key := q.normalize(item)
	if key == "" {
		return false
	}
	if _, ok := q.active[key]; ok {
		return true
	}
	return q.queued[key]
}

func (q *asyncQueue[T]) Enqueue(items ...T) {
	q.ensure()
	for _, item := range items {
		key := q.normalize(item)
		if key == "" || q.Contains(item) {
			continue
		}
		q.queue = append(q.queue, item)
		q.queued[key] = true
	}
}

func (q *asyncQueue[T]) StartNext(skip func(T) bool) (T, bool) {
	q.ensure()
	var zero T
	if len(q.active) >= q.maxInFlight {
		return zero, false
	}
	for len(q.queue) > 0 {
		item := q.queue[0]
		q.queue = q.queue[1:]
		key := q.normalize(item)
		if key != "" {
			delete(q.queued, key)
		}
		if key == "" {
			continue
		}
		if skip != nil && skip(item) {
			continue
		}
		q.active[key] = item
		return item, true
	}
	return zero, false
}

func (q *asyncQueue[T]) StartAvailable(skip func(T) bool) []T {
	var started []T
	for len(q.active) < q.maxInFlight {
		item, ok := q.StartNext(skip)
		if !ok {
			break
		}
		started = append(started, item)
	}
	return started
}

func (q *asyncQueue[T]) Finish(item T) bool {
	q.ensure()
	key := q.normalize(item)
	if key == "" {
		return false
	}
	if _, ok := q.active[key]; !ok {
		return false
	}
	delete(q.active, key)
	return true
}

func (q *asyncQueue[T]) Active() bool {
	return len(q.active) > 0 || len(q.queue) > 0
}

func (q *asyncQueue[T]) InFlight() int {
	return len(q.active)
}

func (q *asyncQueue[T]) Queued() int {
	return len(q.queue)
}

func (q *asyncQueue[T]) PruneQueued(keep func(T) bool) {
	if keep == nil {
		return
	}
	q.ensure()
	filtered := q.queue[:0]
	clear(q.queued)
	for _, item := range q.queue {
		if !keep(item) {
			continue
		}
		key := q.normalize(item)
		if key == "" || q.queued[key] {
			continue
		}
		filtered = append(filtered, item)
		q.queued[key] = true
	}
	q.queue = filtered
}

type asyncResultBuffer[T any] struct {
	mu      sync.Mutex
	pending []T
}

func newAsyncResultBuffer[T any]() *asyncResultBuffer[T] {
	return &asyncResultBuffer[T]{}
}

func (b *asyncResultBuffer[T]) Push(result T) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.pending = append(b.pending, result)
	b.mu.Unlock()
}

func (b *asyncResultBuffer[T]) Drain() []T {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return nil
	}
	results := append([]T(nil), b.pending...)
	b.pending = nil
	return results
}

func (b *asyncResultBuffer[T]) HasPending() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) > 0
}

func (b *asyncResultBuffer[T]) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.pending = nil
	b.mu.Unlock()
}
