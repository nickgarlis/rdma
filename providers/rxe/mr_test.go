//go:build linux

package rxe_test

import (
	"testing"
	"unsafe"

	"github.com/nickgarlis/rdma"
	"golang.org/x/sys/unix"
)

// mapped is memory the caller maps itself, registered through MemoryRange.
type mapped []byte

func (m mapped) Addr() uint64 { return uint64(uintptr(unsafe.Pointer(&m[0]))) }
func (m mapped) Len() int     { return len(m) }

func TestRegMRCallerMemory(t *testing.T) {
	dev := openDevice(t)

	pd, err := dev.AllocPD()
	if err != nil {
		t.Fatalf("AllocPD: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "PD", func() error { return dev.DeallocPD(pd) }) })

	b, err := unix.Mmap(-1, 0, 8192, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		t.Fatalf("mmap: %v", err)
	}

	t.Cleanup(func() { _ = unix.Munmap(b) })

	mr, err := dev.RegMR(pd, mapped(b), rdma.AccessLocalWrite)
	if err != nil {
		t.Fatalf("RegMR: %v", err)
	}

	if mr.Addr != mapped(b).Addr() || mr.Length != uint64(len(b)) {
		t.Fatalf("MR covers %#x+%d, want %#x+%d", mr.Addr, mr.Length, mapped(b).Addr(), len(b))
	}

	if err := dev.DeregMR(mr); err != nil {
		t.Fatalf("DeregMR: %v", err)
	}
}
