//go:build linux && (amd64 || arm64)

// Package udma provides memory barriers and accesses for memory shared with
// a device, like rdma-core's <util/udma_barrier.h> and <util/mmio.h>.
// Unaligned or out-of-range accesses panic.
package udma

import (
	"encoding/binary"
	"unsafe"
)

// OrderWrites keeps earlier writes ahead of later ones, without waiting, as
// between WQEs and the counter announcing them. It is
// udma_to_device_barrier.
func OrderWrites()

// FlushWrites waits until earlier writes are delivered, as before and after
// ringing a doorbell. It is mmio_flush_writes.
func FlushWrites()

// OrderReads keeps later reads and writes behind earlier reads, as between
// checking a CQE's owner bit and reading the CQE. It is
// udma_from_device_barrier.
func OrderReads()

// WriteBE32 writes v big-endian to the 32-bit word at b[off:].
func WriteBE32(b []byte, off int, v uint32) {
	store32(aligned32(b, off), be32(v))
}

// WriteBE64 writes v big-endian to the 64-bit word at b[off:].
func WriteBE64(b []byte, off int, v uint64) {
	store64(aligned64(b, off), be64(v))
}

// ReadByte reads b[off].
func ReadByte(b []byte, off int) byte {
	return load8(&b[off])
}

// ReadBE32 reads the big-endian 32-bit word at b[off:].
func ReadBE32(b []byte, off int) uint32 {
	return be32(load32(aligned32(b, off)))
}

// ReadBE64 reads the big-endian 64-bit word at b[off:].
func ReadBE64(b []byte, off int) uint64 {
	return be64(load64(aligned64(b, off)))
}

// Copy64 copies src to b[off:] in 64-bit writes in ascending order, as a
// WQE to a BlueFlame register. It is mmio_memcpy_x64. len(src) must be a
// non-zero multiple of 64.
func Copy64(b []byte, off int, src []byte) {
	if len(src) == 0 || len(src)%64 != 0 {
		panic("udma: copy is not a multiple of 64 bytes")
	}

	_ = b[off+len(src)-1]
	for i := 0; i < len(src); i += 8 {
		store64(aligned64(b, off+i), binary.NativeEndian.Uint64(src[i:]))
	}
}

// aligned32 returns the 4-byte aligned word at b[off:].
func aligned32(b []byte, off int) *uint32 {
	_ = b[off+3]
	p := unsafe.Pointer(&b[off])
	if uintptr(p)%4 != 0 {
		panic("udma: unaligned 32-bit word")
	}

	return (*uint32)(p)
}

// aligned64 returns the 8-byte aligned word at b[off:].
func aligned64(b []byte, off int) *uint64 {
	_ = b[off+7]
	p := unsafe.Pointer(&b[off])
	if uintptr(p)%8 != 0 {
		panic("udma: unaligned 64-bit word")
	}

	return (*uint64)(p)
}

// be32 converts v between host and big-endian byte order.
func be32(v uint32) uint32 {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return binary.NativeEndian.Uint32(b[:])
}

// be64 converts v between host and big-endian byte order.
func be64(v uint64) uint64 {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return binary.NativeEndian.Uint64(b[:])
}

// Single load and store instructions, in assembly so the compiler can't
// split, merge or move them.

func load8(p *byte) byte
func load32(p *uint32) uint32
func load64(p *uint64) uint64
func store32(p *uint32, v uint32)
func store64(p *uint64, v uint64)
