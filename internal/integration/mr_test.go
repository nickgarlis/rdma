//go:build linux

package integration

import (
	"testing"
	"unsafe"

	"github.com/nickgarlis/rdma"
	"golang.org/x/sys/unix"
)

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

	// Memory mapped by the caller goes through the escape hatch.
	addr := uint64(uintptr(unsafe.Pointer(&b[0])))
	mr, err := dev.RegMRUnsafe(pd, addr, uint64(len(b)), rdma.AccessLocalWrite)
	if err != nil {
		t.Fatalf("RegMRUnsafe: %v", err)
	}

	if mr.Addr != addr || mr.Length != uint64(len(b)) {
		t.Fatalf("MR covers %#x+%d, want %#x+%d", mr.Addr, mr.Length, addr, len(b))
	}

	if err := dev.DeregMR(mr); err != nil {
		t.Fatalf("DeregMR: %v", err)
	}
}
