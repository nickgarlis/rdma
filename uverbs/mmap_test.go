//go:build linux

package uverbs

import (
	"errors"
	"testing"

	"github.com/nickgarlis/rdma/internal/mmap"
)

func TestConnClosesMappings(t *testing.T) {
	c := &Conn{mappings: make(map[*Mapping]struct{})}

	// Register mappings the way Mmap does, without a device file.
	newMapping := func() *Mapping {
		r, err := mmap.Anonymous(4096)
		if err != nil {
			t.Fatalf("mmap: %v", err)
		}

		m := &Mapping{r: r, len: r.Len(), conn: c}
		c.mappings[m] = struct{}{}
		return m
	}

	early, left := newMapping(), newMapping()

	// Closing a mapping early removes it from the Conn.
	if err := early.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(c.mappings) != 1 {
		t.Fatalf("%d open mappings after closing one, want 1", len(c.mappings))
	}

	// Closing the Conn unmaps the rest.
	if err := c.closeMappings(); err != nil {
		t.Fatalf("closeMappings: %v", err)
	}

	if len(c.mappings) != 0 || !c.closed {
		t.Fatalf("after closeMappings: %d open, closed: %v", len(c.mappings), c.closed)
	}

	// A closed mapping no longer runs Do.
	ran := false
	if err := left.Do(func([]byte) error { ran = true; return nil }); !errors.Is(err, ErrClosed) || ran {
		t.Fatalf("Do after close: err %v, ran %v; want ErrClosed, not run", err, ran)
	}

	// Closing a mapping the Conn already closed has no effect.
	if err := left.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseWaitsForDo(t *testing.T) {
	r, err := mmap.Anonymous(4096)
	if err != nil {
		t.Fatalf("mmap: %v", err)
	}

	m := &Mapping{r: r, len: r.Len()}

	inside, release := make(chan struct{}), make(chan struct{})
	done := make(chan error)
	go func() {
		done <- m.Do(func(b []byte) error {
			close(inside)
			<-release
			b[0] = 1 // still mapped: Close is waiting
			return nil
		})
	}()

	<-inside
	closed := make(chan error)
	go func() { closed <- m.Close() }()

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Do: %v", err)
	}

	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
}
