//go:build linux

package rdma

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/nickgarlis/rdma/internal/mmap"
)

// ErrPoolClosed is returned by BufferPool.Get and Buffer.Do once the pool is
// closed.
var ErrPoolClosed = errors.New("rdma: buffer pool closed")

// A BufferPool is a fixed set of equal buffers with a free list, in memory
// it allocates outside the Go heap: the memory never moves and stays valid
// until Close, which makes it safe to register with Device.RegMR. A single
// buffer works for one large region, such as the target of RDMA writes.
//
// Get hands out a free buffer and Put returns it, like sync.Pool, but a
// BufferPool never drops buffers, Get waits when none is free, and Put panics
// on a buffer that isn't handed out.
//
// A buffer posted in a work request is in use by the device until its
// completion arrives. Use the buffer's ID as the WRID, find the buffer again
// with Lookup when the completion arrives, and only then Put it or post it
// again.
type BufferPool struct {
	r            *mmap.Region
	addr, length uint64 // of r, kept so that reading them never races Close
	bufs         []*Buffer
	free         chan int

	closeOnce sync.Once
	closeErr  error
	done      chan struct{} // closed by Close
}

// A Buffer is one buffer of a BufferPool. Each buffer has its own lock, so
// goroutines using different buffers never contend.
type Buffer struct {
	pool   *BufferPool
	id     int
	off, n int
	out    atomic.Bool // handed out by Get, not yet Put

	mu     sync.Mutex
	closed bool // set by Close
}

// NewBufferPool allocates n buffers of size bytes each, zeroed, in
// page-aligned memory excluded from child processes (MADV_DONTFORK), so that
// fork cannot break registered regions.
func NewBufferPool(n, size int) (*BufferPool, error) {
	if n <= 0 || size <= 0 {
		return nil, fmt.Errorf("rdma: invalid buffer pool of %d buffers of %d bytes", n, size)
	}

	r, err := mmap.Anonymous(n * size)
	if err != nil {
		return nil, fmt.Errorf("rdma: allocate buffer pool: %w", err)
	}

	p := &BufferPool{
		r:      r,
		addr:   r.Addr(),
		length: uint64(r.Len()),
		bufs:   make([]*Buffer, n),
		free:   make(chan int, n),
		done:   make(chan struct{}),
	}
	for i := range p.bufs {
		p.bufs[i] = &Buffer{pool: p, id: i, off: i * size, n: size}
		p.free <- i
	}

	return p, nil
}

// Get returns a free buffer, waiting until one is free, the pool is closed
// or ctx is done.
func (p *BufferPool) Get(ctx context.Context) (*Buffer, error) {
	select {
	case <-p.done:
		return nil, ErrPoolClosed
	default:
	}

	select {
	case i := <-p.free:
		b := p.bufs[i]
		b.out.Store(true)
		return b, nil
	case <-p.done:
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns b to the pool. It panics if b belongs to another pool or isn't
// handed out, such as when it is put twice.
func (p *BufferPool) Put(b *Buffer) {
	if b.pool != p {
		panic("rdma: BufferPool.Put: buffer belongs to another pool")
	}

	if !b.out.CompareAndSwap(true, false) {
		panic(fmt.Sprintf("rdma: BufferPool.Put: buffer %d is not handed out", b.id))
	}

	p.free <- b.id
}

// Lookup returns the buffer with the given ID, such as the WRID of a
// completion, or nil if the pool has no such buffer.
func (p *BufferPool) Lookup(id uint64) *Buffer {
	if id >= uint64(len(p.bufs)) {
		return nil
	}

	return p.bufs[id]
}

// Close closes every buffer, waiting for their running Do calls, then frees
// the memory. Memory regions registered on the pool must be deregistered
// first. It must not be called from inside Buffer.Do. Calling it again has no
// effect and returns the same result.
func (p *BufferPool) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)

		// Closing a buffer waits for its running Do, so no Do runs once
		// the memory is unmapped.
		for _, b := range p.bufs {
			b.mu.Lock()
			b.closed = true
			b.mu.Unlock()
		}

		p.closeErr = p.r.Close()
	})

	return p.closeErr
}

// span returns the address and length of the pool's memory, or zeros once
// the pool is closed.
func (p *BufferPool) span() (addr, length uint64) {
	select {
	case <-p.done:
		return 0, 0
	default:
		return p.addr, p.length
	}
}

// ID returns the buffer's ID, for use as the WRID of a work request.
func (b *Buffer) ID() uint64 { return uint64(b.id) }

// Addr returns the virtual address of the buffer, or 0 once its pool is
// closed.
func (b *Buffer) Addr() uint64 {
	addr, _ := b.pool.span()
	if addr == 0 {
		return 0
	}

	return addr + uint64(b.off)
}

// Len returns the size of the buffer in bytes.
func (b *Buffer) Len() int { return b.n }

// Do runs fn with the buffer's bytes and returns fn's error. It fails with
// ErrPoolClosed, without running fn, once the pool is closed. Calls on one
// buffer are serialized. fn must not keep p after it returns, and must not
// close the pool.
func (b *Buffer) Do(fn func(p []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrPoolClosed
	}

	// Close unmaps only after closing every buffer under its lock, so the
	// bytes stay mapped while b is open.
	return fn(b.pool.r.Bytes()[b.off : b.off+b.n])
}
