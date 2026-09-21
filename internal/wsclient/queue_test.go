package wsclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueuePushPopFIFO(t *testing.T) {
	ctx := context.Background()
	q := NewQueue[int]()

	if ok := q.Push(1); !ok {
		t.Fatal("Push(1) = false, want true")
	}
	if ok := q.Push(2); !ok {
		t.Fatal("Push(2) = false, want true")
	}
	if ok := q.Push(3); !ok {
		t.Fatal("Push(3) = false, want true")
	}

	for _, want := range []int{1, 2, 3} {
		got, ok, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("Pop() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("Pop() ok = false, want true")
		}
		if got != want {
			t.Fatalf("Pop() = %d, want %d", got, want)
		}
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}

func TestQueuePopBlocksUntilPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	q := NewQueue[string]()
	gotCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		got, ok, err := q.Pop(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if !ok {
			errCh <- errors.New("Pop() ok = false, want true")
			return
		}
		gotCh <- got
	}()

	select {
	case got := <-gotCh:
		t.Fatalf("Pop() returned early with %q", got)
	case err := <-errCh:
		t.Fatalf("Pop() returned early with error %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	if ok := q.Push("hello"); !ok {
		t.Fatal("Push() = false, want true")
	}

	select {
	case got := <-gotCh:
		if got != "hello" {
			t.Fatalf("Pop() = %q, want hello", got)
		}
	case err := <-errCh:
		t.Fatalf("Pop() error = %v, want nil", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for Pop()")
	}
}

func TestQueuePopContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	q := NewQueue[int]()

	cancel()

	_, ok, err := q.Pop(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pop() error = %v, want context.Canceled", err)
	}
	if ok {
		t.Fatal("Pop() ok = true, want false")
	}
}

func TestQueuePopContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	q := NewQueue[int]()

	_, ok, err := q.Pop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Pop() error = %v, want context.DeadlineExceeded", err)
	}
	if ok {
		t.Fatal("Pop() ok = true, want false")
	}
}

func TestQueueCloseWakesBlockedPop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	q := NewQueue[int]()
	done := make(chan error, 1)

	go func() {
		_, ok, err := q.Pop(ctx)
		if err != nil {
			done <- err
			return
		}
		if ok {
			done <- errors.New("Pop() ok = true, want false")
			return
		}
		done <- nil
	}()

	time.Sleep(25 * time.Millisecond)
	q.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("blocked Pop() result error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for Pop() to wake after Close()")
	}
}

func TestQueueCloseDrainsExistingItems(t *testing.T) {
	ctx := context.Background()
	q := NewQueue[int]()

	q.Push(1)
	q.Push(2)
	q.Close()

	for _, want := range []int{1, 2} {
		got, ok, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("Pop() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("Pop() ok = false, want true while draining")
		}
		if got != want {
			t.Fatalf("Pop() = %d, want %d", got, want)
		}
	}

	_, ok, err := q.Pop(ctx)
	if err != nil {
		t.Fatalf("Pop() after drain error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Pop() after drain ok = true, want false")
	}
}

func TestQueuePushAfterCloseFails(t *testing.T) {
	q := NewQueue[int]()
	q.Close()

	if ok := q.Push(1); ok {
		t.Fatal("Push() after Close() = true, want false")
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}

func TestQueueDrain(t *testing.T) {
	q := NewQueue[string]()

	q.Push("a")
	q.Push("b")
	q.Push("c")

	got := q.Drain()
	want := []string{"a", "b", "c"}

	if len(got) != len(want) {
		t.Fatalf("len(Drain()) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Drain()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("Len() after Drain() = %d, want 0", got)
	}
}

func TestQueuePushManyWithoutConsumerDoesNotBlock(t *testing.T) {
	q := NewQueue[int]()

	const count = 100_000

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < count; i++ {
			if ok := q.Push(i); !ok {
				t.Errorf("Push(%d) = false, want true", i)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pushing many items without consumer blocked too long")
	}

	if got := q.Len(); got != count {
		t.Fatalf("Len() = %d, want %d", got, count)
	}
}

func TestQueueConcurrentProducersConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	q := NewQueue[int]()

	const producers = 5
	const perProducer = 1000
	const total = producers * perProducer

	doneProducers := make(chan struct{}, producers)
	for p := 0; p < producers; p++ {
		p := p
		go func() {
			defer func() { doneProducers <- struct{}{} }()
			base := p * perProducer
			for i := 0; i < perProducer; i++ {
				q.Push(base + i)
			}
		}()
	}

	for p := 0; p < producers; p++ {
		<-doneProducers
	}
	q.Close()

	seen := map[int]bool{}
	for {
		value, ok, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("Pop() error = %v", err)
		}
		if !ok {
			break
		}
		if seen[value] {
			t.Fatalf("duplicate value popped: %d", value)
		}
		seen[value] = true
	}

	if len(seen) != total {
		t.Fatalf("popped %d unique values, want %d", len(seen), total)
	}
}
