//go:build linux

package rdma

import (
	"errors"
	"fmt"
	"sync"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/socket"
	"github.com/nickgarlis/rdma/nldev"
	"github.com/nickgarlis/rdma/uverbs"
	"golang.org/x/sys/unix"
)

// A Device is an open RDMA device: the equivalent of struct ibv_context.
// Every verb is a method of Device, and every object is created through one.
// The kernel destroys the objects that are still open when the Device is
// closed.
//
// A Device is safe for concurrent use. The data path of its CQs and QPs
// follows the thread-safety rules of its Provider.
type Device struct {
	name     string
	driverID DriverID
	conn     *uverbs.Conn
	provider Provider
	async    *socket.Conn

	numCompVectors uint32

	// cqs and qps map the user handles sent to the kernel back to their
	// objects, for events. mu guards them.
	mu     sync.Mutex
	cqs    map[uint64]*CQ
	qps    map[uint64]*QP
	closed bool
}

// Open opens the RDMA device with the given name, such as "rxe0" or
// "mlx5_0", with p implementing its verbs, like ibv_open_device: it opens the
// uverbs device file, initializes p, which creates the kernel context with
// Cmd.GetContext, and allocates the asynchronous event file. Applications
// usually call the provider's Open instead, such as rxe.Open.
func Open(name string, p Provider) (*Device, error) {
	if p == nil {
		return nil, errors.New("rdma: open: no provider")
	}

	path, driverID, err := lookupDevice(name)
	if err != nil {
		return nil, err
	}

	if id := p.DriverID(); id != driverID {
		return nil, fmt.Errorf("rdma: provider supports driver %d, device %q has driver %d", id, name, driverID)
	}

	conn, err := uverbs.Open(path, uint32(driverID))
	if err != nil {
		return nil, err
	}

	d := &Device{
		name:     name,
		driverID: driverID,
		conn:     conn,
		cqs:      make(map[uint64]*CQ),
		qps:      make(map[uint64]*QP),
	}

	d.provider = p
	ctx, err := p.Init(d)
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("rdma: init provider: %w", err)
	}

	if ctx == nil {
		_ = d.Close()
		return nil, errors.New("rdma: init provider: no context returned")
	}

	d.numCompVectors = ctx.NumCompVectors

	if err := d.allocAsyncEventFile(); err != nil {
		_ = d.Close()
		return nil, err
	}

	return d, nil
}

// An eventFile is a uverbs event file, such as the async event file or a
// completion channel. Reads block in Go's runtime network poller (epoll)
// rather than in the kernel.
func newEventFile(fd int, name string) (*socket.Conn, error) {
	f, err := socket.New(fd, name)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}

	return f, nil
}

// lookupDevice finds the RDMA device with the given name with RDMA netlink
// and returns its uverbs character device and driver ID.
func lookupDevice(name string) (path string, driverID DriverID, err error) {
	conn, err := nldev.Dial(nil)
	if err != nil {
		return "", 0, fmt.Errorf("rdma: dial RDMA netlink: %w", err)
	}
	defer conn.Close()

	devs, err := conn.Devices()
	if err != nil {
		return "", 0, fmt.Errorf("rdma: %w", err)
	}

	for _, dev := range devs {
		if dev.Name != name {
			continue
		}

		cd, err := conn.Chardev(dev.Index, "uverbs")
		if err != nil {
			if _, ok := errors.AsType[*netlink.OpError](err); ok {
				return "", 0, fmt.Errorf("rdma: device %q has no uverbs character device; is ib_uverbs loaded?", name)
			}

			return "", 0, fmt.Errorf("rdma: %w", err)
		}

		return "/dev/infiniband/" + cd.Name, DriverID(cd.DriverID), nil
	}

	return "", 0, fmt.Errorf("rdma: device %q not found", name)
}

var errOtherDevice = errors.New("rdma: object belongs to another device")

var errDestroyed = errors.New("rdma: object was already destroyed")

// owns reports an error unless an object created on dev may be used with d.
// Handles are only meaningful on the device file that created them, so an
// object from another Device would refer to an unrelated object on this one.
func (d *Device) owns(dev *Device) error {
	if dev != d {
		return errOtherDevice
	}

	return nil
}

// Close closes the Device. The kernel destroys every object that is still
// open, but resources owned by Drivers, such as mapped queues, are only
// released by destroying those objects first.
func (d *Device) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}

	d.closed = true
	d.mu.Unlock()

	var errs []error
	if d.provider != nil {
		errs = append(errs, d.provider.Close())
	}

	if d.async != nil {
		errs = append(errs, d.async.Close())
	}

	errs = append(errs, d.conn.Close())
	return errors.Join(errs...)
}

// NumCompVectors returns the number of completion vectors of the device.
func (d *Device) NumCompVectors() int { return int(d.numCompVectors) }

// Conn returns the uverbs device file the Device runs on. Use it to call
// kernel methods this package does not wrap, with Conn.Execute.
func (d *Device) Conn() *uverbs.Conn { return d.conn }

// registerCQ and registerQP record a new object under its user handle,
// which the kernel reports back in the object's events.
func (d *Device) registerCQ(cq *CQ) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cqs[cq.userHandle] = cq
}

func (d *Device) registerQP(qp *QP) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.qps[qp.userHandle] = qp
}

func (d *Device) unregisterCQ(h uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.cqs, h)
}

func (d *Device) unregisterQP(h uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.qps, h)
}

// lookupCQ and lookupQP return the object with user handle h, or nil if it
// was destroyed.
func (d *Device) lookupCQ(h uint64) *CQ {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.cqs[h]
}

func (d *Device) lookupQP(h uint64) *QP {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.qps[h]
}
