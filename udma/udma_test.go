//go:build linux && (amd64 || arm64)

package udma

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBarriers(t *testing.T) {
	// The barriers have no visible effect in a single goroutine; this
	// only checks that they run on this architecture.
	OrderReads()
	OrderWrites()
	FlushWrites()
}

func TestAccess(t *testing.T) {
	b := make([]byte, 192)

	WriteBE32(b, 0x10, 0x11223344)
	if got := b[0x10:0x14]; !bytes.Equal(got, []byte{0x11, 0x22, 0x33, 0x44}) {
		t.Fatalf("WriteBE32 wrote % x", got)
	}

	if got := ReadBE32(b, 0x10); got != 0x11223344 {
		t.Fatalf("ReadBE32 = %#x", got)
	}

	WriteBE64(b, 0x20, 0x1122334455667788)
	if got := binary.BigEndian.Uint64(b[0x20:]); got != 0x1122334455667788 {
		t.Fatalf("WriteBE64 wrote % x", b[0x20:0x28])
	}

	if got := ReadBE64(b, 0x20); got != 0x1122334455667788 {
		t.Fatalf("ReadBE64 = %#x", got)
	}

	b[63] = 0xf1
	if got := ReadByte(b, 63); got != 0xf1 {
		t.Fatalf("ReadByte = %#x, want 0xf1", got)
	}

	src := make([]byte, 128)
	for i := range src {
		src[i] = byte(i)
	}

	Copy64(b, 64, src)
	if !bytes.Equal(b[64:], src) {
		t.Fatalf("Copy64 copied % x", b[64:])
	}
}

func TestPanics(t *testing.T) {
	b := make([]byte, 64)

	for name, f := range map[string]func(){
		"WriteBE32 unaligned":    func() { WriteBE32(b, 2, 1) },
		"WriteBE32 out of range": func() { WriteBE32(b, 62, 1) },
		"ReadByte out of range":  func() { ReadByte(b, 64) },
		"ReadBE32 unaligned":     func() { ReadBE32(b, 1) },
		"WriteBE64 unaligned":    func() { WriteBE64(b, 0x21, 1) },
		"WriteBE64 out of range": func() { WriteBE64(b, 60, 1) },
		"ReadBE64 unaligned":     func() { ReadBE64(b, 4) },
		"Copy64 empty":           func() { Copy64(b, 0, nil) },
		"Copy64 partial":         func() { Copy64(b, 0, make([]byte, 32)) },
		"Copy64 unaligned":       func() { Copy64(append(b, b...), 4, b) },
		"Copy64 out of range":    func() { Copy64(b, 8, b) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()

			f()
		}()
	}

	// A copy out of range fails before writing anything.
	if !bytes.Equal(b, make([]byte, 64)) {
		t.Errorf("failed copy wrote % x", b)
	}
}
