//go:build linux

package uverbs

import (
	"errors"
	"sync"

	"github.com/nickgarlis/rdma/internal/mmap"
)

// Protection is the memory protection of a mapping made with Conn.Mmap.
type Protection int

// Protection flags.
const (
	ProtRead Protection = 1 << iota
	ProtWrite
)

// closeMappings marks the Conn closed and unmaps its open mappings.
func (c *Conn) closeMappings() error {
	c.mu.Lock()
	c.closed = true
	open := make([]*Mapping, 0, len(c.mappings))
	for m := range c.mappings {
		open = append(open, m)
	}
	c.mu.Unlock()

	var errs []error
	for _, m := range open {
		errs = append(errs, m.Close())
	}

	return errors.Join(errs...)
}

// ErrClosed is returned by Mapping.Do once the mapping is closed, and by
// Conn.Mmap once the Conn is closed.
var ErrClosed = errors.New("uverbs: mapping or device file closed")

// Mmap maps length bytes at offset of the device file, such as doorbell
// pages or queues shared with the kernel. The offset and its meaning are
// driver-specific.
func (c *Conn) Mmap(offset int64, length int, prot Protection) (*Mapping, error) {
	var p mmap.Prot
	if prot&ProtRead != 0 {
		p |= mmap.Read
	}

	if prot&ProtWrite != 0 {
		p |= mmap.Write
	}

	rc, err := c.file.SyscallConn()
	if err != nil {
		return nil, err
	}

	var r *mmap.Region
	if cerr := rc.Control(func(fd uintptr) {
		r, err = mmap.File(int(fd), offset, length, p)
	}); cerr != nil {
		return nil, cerr
	}

	if err != nil {
		return nil, err
	}

	m := &Mapping{r: r, len: r.Len(), conn: c}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		_ = r.Close()
		return nil, ErrClosed
	}

	c.mappings[m] = struct{}{}
	return m, nil
}

// forget removes m from the open mappings.
func (c *Conn) forget(m *Mapping) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.mappings, m)
}

// A Mapping is memory of the device file mapped by Conn.Mmap, such as a
// queue shared with the kernel. The memory is only reachable inside Do, which
// holds the mapping's lock, so it is never used while or after it is
// unmapped: Close, and closing the Conn, wait for running Do calls.
type Mapping struct {
	mu   sync.Mutex
	r    *mmap.Region // nil once closed
	len  int
	conn *Conn
}

// Do runs fn with the mapped memory and returns fn's error. It fails with
// ErrClosed, without running fn, once the mapping is closed. Calls are
// serialized. fn must not keep b after it returns, and must not close the
// mapping or its Conn, which would wait for fn forever.
func (m *Mapping) Do(fn func(b []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.r == nil {
		return ErrClosed
	}

	return fn(m.r.Bytes())
}

// Len returns the size of the mapping in bytes.
func (m *Mapping) Len() int { return m.len }

// Close unmaps the memory, after any running Do call returns. Calling it
// again has no effect.
func (m *Mapping) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.r == nil {
		return nil
	}

	if m.conn != nil {
		m.conn.forget(m)
	}

	err := m.r.Close()
	m.r = nil
	return err
}
