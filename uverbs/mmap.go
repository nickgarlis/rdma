//go:build linux

package uverbs

import (
	"errors"

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

// errClosed is returned by Mmap after Close.
var errClosed = errors.New("uverbs: device file closed")

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

	m := &Mapping{r: r, conn: c}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		_ = r.Close()
		return nil, errClosed
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

// A Mapping is memory of the device file mapped by Conn.Mmap. It stays valid
// until it is closed, by Close or by closing its Conn. Using Bytes after that
// crashes the program, so close a Mapping before its Conn.
type Mapping struct {
	r    *mmap.Region
	conn *Conn
}

// Bytes returns the mapped memory. It must not be used after Close.
func (m *Mapping) Bytes() []byte { return m.r.Bytes() }

// Len returns the size of the mapping in bytes.
func (m *Mapping) Len() int { return m.r.Len() }

// Close unmaps the memory. Calling it again has no effect.
func (m *Mapping) Close() error {
	if m.conn != nil {
		m.conn.forget(m)
	}

	return m.r.Close()
}
