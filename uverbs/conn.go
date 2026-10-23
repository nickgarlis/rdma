//go:build linux

package uverbs

import (
	"errors"
	"os"
	"sync"

	"github.com/mdlayher/socket"
	"golang.org/x/sys/unix"
)

// A Conn is an open uverbs device file, such as /dev/infiniband/uverbs0.
type Conn struct {
	file     *socket.Conn
	driverID uint32

	mu       sync.Mutex
	mappings map[*Mapping]struct{}
	closed   bool

	closeOnce sync.Once
	closeErr  error
}

// Open opens the uverbs device file at path. driverID is the device's
// driver ID (enum rdma_driver_id), which every ioctl must carry.
func Open(path string, driverID uint32) (*Conn, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	file, err := socket.New(fd, "uverbs")
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}

	return &Conn{file: file, driverID: driverID, mappings: make(map[*Mapping]struct{})}, nil
}

// Close unmaps every Mapping that is still open, then closes the device
// file. The kernel then destroys every object created through it. Calling it
// again has no effect.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		err := c.closeMappings()
		c.closeErr = errors.Join(err, c.file.Close())
	})

	return c.closeErr
}
