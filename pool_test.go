//go:build linux

package rdma

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBufferPool(t *testing.T) {
	if _, err := NewBufferPool(0, 1024); err == nil {
		t.Fatal("NewBufferPool accepted an empty pool")
	}

	pool, err := NewBufferPool(4, 1024)
	if err != nil {
		t.Fatalf("NewBufferPool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	got := make([]*Buffer, 4)
	for i := range got {
		if got[i], err = pool.Get(ctx); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}

		if pool.Lookup(got[i].ID()) != got[i] {
			t.Fatalf("Lookup(%d) is not the buffer with that ID", got[i].ID())
		}
	}

	// Buffers are consecutive parts of one block.
	b0, b3 := pool.Lookup(0), pool.Lookup(3)
	if b3.Addr() != b0.Addr()+3072 || b3.Len() != 1024 {
		t.Fatalf("buffer 3 at %#x, %d bytes; want %#x, 1024", b3.Addr(), b3.Len(), b0.Addr()+3072)
	}

	// All buffers are out: Get waits until ctx is done.
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	if _, err := pool.Get(short); err == nil {
		t.Fatal("Get returned a buffer from an empty pool")
	}

	// Put makes a buffer available to a waiting Get.
	done := make(chan *Buffer)
	go func() {
		b, _ := pool.Get(ctx)
		done <- b
	}()

	pool.Put(got[2])
	if b := <-done; b != got[2] {
		t.Fatalf("Get returned buffer %d, want 2", b.ID())
	}

	if pool.Lookup(4) != nil {
		t.Fatal("Lookup returned a buffer for an unknown ID")
	}

	other, err := NewBufferPool(1, 1)
	if err != nil {
		t.Fatalf("NewBufferPool: %v", err)
	}
	defer other.Close()

	for _, c := range []struct {
		name string
		fn   func()
	}{
		{"double Put", func() { pool.Put(got[0]); pool.Put(got[0]) }},
		{"Put into another pool", func() { other.Put(got[1]) }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", c.name)
				}
			}()

			c.fn()
		}()
	}
}

func TestBufferPoolClose(t *testing.T) {
	pool, err := NewBufferPool(4, 1024)
	if err != nil {
		t.Fatalf("NewBufferPool: %v", err)
	}

	// A buffer's bytes are its own part of the memory.
	if err := pool.Lookup(2).Do(func(b []byte) error {
		if len(b) != 1024 {
			t.Errorf("buffer has %d bytes, want 1024", len(b))
		}
		b[0] = 7
		return nil
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	// A Get waiting on an empty pool wakes up when it is closed.
	for range 4 {
		if _, err := pool.Get(context.Background()); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}

	waiting := make(chan error)
	go func() {
		_, err := pool.Get(context.Background())
		waiting <- err
	}()

	// Close waits for a running Do, then everything fails cleanly.
	inside, release, closed := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		_ = pool.Lookup(1).Do(func(b []byte) error {
			close(inside)
			<-release
			b[0] = 1 // still mapped: Close is waiting
			return nil
		})
	}()

	<-inside
	go func() { closed <- pool.Close() }()
	close(release)

	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := <-waiting; !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("waiting Get: %v, want ErrPoolClosed", err)
	}

	for i := range uint64(4) {
		if err := pool.Lookup(i).Do(func([]byte) error { return nil }); !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("Do on buffer %d after Close: %v, want ErrPoolClosed", i, err)
		}
	}

	if pool.Lookup(3).Addr() != 0 {
		t.Fatalf("buffer address after Close %#x, want 0", pool.Lookup(3).Addr())
	}

	if err := pool.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
