//go:build linux

package uverbs

import (
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

		m := &Mapping{r: r, conn: c}
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

	if len(c.mappings) != 0 || left.Bytes() != nil || !c.closed {
		t.Fatalf("after closeMappings: %d open, left.Bytes() nil: %v, closed: %v",
			len(c.mappings), left.Bytes() == nil, c.closed)
	}

	// Closing a mapping the Conn already closed has no effect.
	if err := left.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
