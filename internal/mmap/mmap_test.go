//go:build linux

package mmap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAnonymous(t *testing.T) {
	r, err := Anonymous(100)
	if err != nil {
		t.Fatalf("Anonymous: %v", err)
	}

	if r.Len() != os.Getpagesize() {
		t.Fatalf("Len = %d, want one page (%d)", r.Len(), os.Getpagesize())
	}

	if r.Addr() == 0 || r.Addr()%uint64(os.Getpagesize()) != 0 {
		t.Fatalf("Addr = %#x, want a page-aligned address", r.Addr())
	}

	b := r.Bytes()
	b[0], b[len(b)-1] = 1, 2

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if r.Bytes() != nil || r.Addr() != 0 {
		t.Fatal("Bytes or Addr still set after Close")
	}
}

func TestAnonymousInvalidSize(t *testing.T) {
	if _, err := Anonymous(0); err == nil {
		t.Fatal("Anonymous(0) succeeded")
	}
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, make([]byte, os.Getpagesize()), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, err := File(int(f.Fd()), 0, os.Getpagesize(), Read|Write)
	if err != nil {
		t.Fatalf("File: %v", err)
	}

	// The mapping is shared, so writes reach the file.
	copy(r.Bytes(), "shared")
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(got, []byte("shared")) {
		t.Fatalf("file starts with %q, want %q", got[:6], "shared")
	}
}
