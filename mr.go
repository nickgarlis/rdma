//go:build linux

package rdma

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// AccessFlags control the operations allowed on a memory region or by a QP's
// remote peers.
type AccessFlags uint32

// Access flags, from enum ib_uverbs_access_flags.
const (
	AccessLocalWrite   AccessFlags = unix.IB_UVERBS_ACCESS_LOCAL_WRITE
	AccessRemoteWrite  AccessFlags = unix.IB_UVERBS_ACCESS_REMOTE_WRITE
	AccessRemoteRead   AccessFlags = unix.IB_UVERBS_ACCESS_REMOTE_READ
	AccessRemoteAtomic AccessFlags = unix.IB_UVERBS_ACCESS_REMOTE_ATOMIC
	AccessMWBind       AccessFlags = unix.IB_UVERBS_ACCESS_MW_BIND
)

// An MR is a memory region: a buffer registered with the device so that it
// can be read and written by work requests.
type MR struct {
	// PD is the protection domain the MR belongs to.
	PD *PD

	// Handle is the kernel handle of the MR.
	Handle uint32

	// LKey is the local key used in SGEs, and RKey the remote key peers use
	// to access the MR.
	LKey uint32
	RKey uint32

	// Addr is the I/O virtual address of the start of the MR, and Length
	// its length in bytes.
	Addr   uint64
	Length uint64

	providerData
}

// RegMR registers the memory of pool as a memory region of pd with the given
// access rights, like ibv_reg_mr. The MR's I/O virtual address, which peers
// use in RDMA reads and writes, is the address of the pool's first buffer.
// The device may access the memory until the MR is deregistered, so
// deregister it before closing the pool.
func (d *Device) RegMR(pd *PD, pool *BufferPool, access AccessFlags) (*MR, error) {
	if err := d.owns(pd.Device); err != nil {
		return nil, err
	}

	addr, length := pool.span()
	if length == 0 {
		return nil, fmt.Errorf("rdma: register MR: %w", ErrPoolClosed)
	}

	return d.provider.RegMR(pd, addr, length, access)
}

// RegMRUnsafe registers length bytes at addr as a memory region of pd, for
// memory that does not come from a BufferPool, such as memory mapped by
// another library. The caller promises that the memory stays mapped, is not
// moved and is not used for anything else until the MR is deregistered. Go
// heap memory breaks that promise: the garbage collector may reuse it while
// the device still writes into it.
func (d *Device) RegMRUnsafe(pd *PD, addr, length uint64, access AccessFlags) (*MR, error) {
	if err := d.owns(pd.Device); err != nil {
		return nil, err
	}

	if length == 0 {
		return nil, errors.New("rdma: register MR: empty memory")
	}

	return d.provider.RegMR(pd, addr, length, access)
}

// DeregMR deregisters mr. mr must not be used afterwards: the kernel reuses
// its handle for new objects.
func (d *Device) DeregMR(mr *MR) error {
	if err := d.owns(mr.PD.Device); err != nil {
		return err
	}

	return d.provider.DeregMR(mr)
}
