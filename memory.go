//go:build linux

package rdma

import (
	"fmt"

	"github.com/nickgarlis/rdma/internal/mmap"
)

// Memory is a buffer mapped with mmap, outside the Go heap. It never moves
// and stays valid until Close, which makes it a safe MemoryRange to register
// with the device. Memory is returned by AllocMemory.
type Memory struct {
	r *mmap.Region
}

// AllocMemory allocates size bytes of zeroed, page-aligned memory from an
// anonymous private mapping. The size is rounded up to a whole number of
// pages. The memory is excluded from child processes (MADV_DONTFORK) so that
// fork cannot break registered regions.
func AllocMemory(size int) (*Memory, error) {
	r, err := mmap.Anonymous(size)
	if err != nil {
		return nil, fmt.Errorf("rdma: allocate memory: %w", err)
	}

	return &Memory{r: r}, nil
}

// Bytes returns the memory as a byte slice. It must not be used after Close.
func (m *Memory) Bytes() []byte { return m.r.Bytes() }

// Len returns the size of the memory in bytes.
func (m *Memory) Len() int { return m.r.Len() }

// Addr returns the virtual address of the memory.
func (m *Memory) Addr() uint64 { return m.r.Addr() }

// Close unmaps the memory. Memory regions registered on it must be
// deregistered first. Calling it again has no effect.
func (m *Memory) Close() error { return m.r.Close() }
