//go:build linux

package rdma

import "golang.org/x/sys/unix"

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

// A MemoryRange is memory that can be registered as a memory region, such as
// a *Memory. An implementation promises that the range stays at Addr, Len
// bytes long, and is not freed or moved until the MR is deregistered. Memory
// from the Go heap breaks that promise: the garbage collector may free it
// while the device still writes into it. Use AllocMemory, or memory mapped
// or allocated outside Go, such as huge pages from unix.Mmap.
type MemoryRange interface {
	Addr() uint64
	Len() int
}

// RegMR registers mem as a memory region of pd with the given access rights,
// like ibv_reg_mr. The MR's I/O virtual address, which peers use in RDMA
// reads and writes, is mem.Addr(). The device may access mem until the MR is
// deregistered, so deregister it before releasing mem.
func (d *Device) RegMR(pd *PD, mem MemoryRange, access AccessFlags) (*MR, error) {
	if err := d.owns(pd.Device); err != nil {
		return nil, err
	}

	return d.provider.RegMR(pd, mem, access)
}

// SGE returns a scatter/gather element for length bytes at offset in the MR.
func (mr *MR) SGE(offset, length uint32) SGE {
	return SGE{Addr: mr.Addr + uint64(offset), Length: length, LKey: mr.LKey}
}

// DeregMR deregisters mr. mr must not be used afterwards: the kernel reuses
// its handle for new objects.
func (d *Device) DeregMR(mr *MR) error {
	if err := d.owns(mr.PD.Device); err != nil {
		return err
	}

	return d.provider.DeregMR(mr)
}
