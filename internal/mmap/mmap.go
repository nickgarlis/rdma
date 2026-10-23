//go:build linux

// Package mmap manages memory mappings shared by package rdma, for buffers,
// and package uverbs, for device files.
package mmap

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Prot is the memory protection of a mapping.
type Prot int

// Protection flags.
const (
	Read Prot = 1 << iota
	Write
)

// A Region is a mapped range of memory. It stays valid until Close.
type Region struct {
	b    []byte
	once sync.Once
	err  error
}

// Anonymous maps size bytes of zeroed, private memory, rounded up to whole
// pages, and excludes it from child processes (MADV_DONTFORK).
func Anonymous(size int) (*Region, error) {
	if size <= 0 {
		return nil, fmt.Errorf("mmap: invalid size %d", size)
	}

	page := os.Getpagesize()
	size = (size + page - 1) &^ (page - 1)

	b, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, os.NewSyscallError("mmap", err)
	}

	// Pages registered with an RDMA device must not be shared copy-on-write
	// with a child after fork.
	if err := unix.Madvise(b, unix.MADV_DONTFORK); err != nil {
		_ = unix.Munmap(b)
		return nil, os.NewSyscallError("madvise", err)
	}

	return &Region{b: b}, nil
}

// File maps length bytes at offset of the file fd, shared with the file's
// owner, such as a device driver.
func File(fd int, offset int64, length int, prot Prot) (*Region, error) {
	var p int
	if prot&Read != 0 {
		p |= unix.PROT_READ
	}

	if prot&Write != 0 {
		p |= unix.PROT_WRITE
	}

	b, err := unix.Mmap(fd, offset, length, p, unix.MAP_SHARED)
	if err != nil {
		return nil, os.NewSyscallError("mmap", err)
	}

	return &Region{b: b}, nil
}

// Bytes returns the mapped memory. It is nil after Close.
func (r *Region) Bytes() []byte { return r.b }

// Len returns the size of the region in bytes.
func (r *Region) Len() int { return len(r.b) }

// Addr returns the virtual address of the region, or 0 after Close.
func (r *Region) Addr() uint64 {
	if len(r.b) == 0 {
		return 0
	}

	return uint64(uintptr(unsafe.Pointer(&r.b[0])))
}

// Close unmaps the region. Calling it again has no effect and returns the
// same result.
func (r *Region) Close() error {
	r.once.Do(func() {
		if err := unix.Munmap(r.b); err != nil {
			r.err = os.NewSyscallError("munmap", err)
		}

		r.b = nil
	})

	return r.err
}
