package wsclient

import (
	"context"
	"sync"
)

// Queue is an unbounded FIFO queue.
//
// Push does not block waiting for consumers, which is important for keeping CLI
// file walking/reconciliation/checksum work from stalling behind upload or
// websocket I/O.
type Queue[T any] struct {
	mu     sync.Mutex
	notify chan struct{}
	items  []T
	closed bool
}

// NewQueue creates an empty unbounded queue.
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		notify: make(chan struct{}, 1),
	}
}

// Push appends item unless the queue has been closed.
//
// Push is intentionally not back-pressured by consumers. It only waits long
// enough to acquire the queue mutex.
func (q *Queue[T]) Push(item T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return false
	}

	q.items = append(q.items, item)
	q.signalLocked()

	return true
}

// Pop blocks until an item is available, ctx is cancelled, or the queue closes.
//
// The boolean return value is false when the queue is closed and drained.
func (q *Queue[T]) Pop(ctx context.Context) (T, bool, error) {
	var zero T

	for {
		q.mu.Lock()

		if len(q.items) > 0 {
			item := q.items[0]

			var zeroItem T
			q.items[0] = zeroItem
			q.items = q.items[1:]

			q.mu.Unlock()
			return item, true, nil
		}

		if q.closed {
			q.mu.Unlock()
			return zero, false, nil
		}

		notify := q.notify
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return zero, false, ctx.Err()
		case <-notify:
		}
	}
}

// Drain removes and returns all currently queued items in FIFO order.
func (q *Queue[T]) Drain() []T {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]T, len(q.items))
	copy(items, q.items)

	var zero T
	for i := range q.items {
		q.items[i] = zero
	}
	q.items = nil

	return items
}

// Close prevents future pushes and wakes blocked consumers.
//
// Already queued items remain available. Pop returns them first, then returns
// ok=false after the queue is drained.
func (q *Queue[T]) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}

	q.closed = true
	q.signalLocked()
}

// Len returns the current queue length.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.items)
}

// Closed reports whether Close has been called.
func (q *Queue[T]) Closed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.closed
}

func (q *Queue[T]) signalLocked() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
