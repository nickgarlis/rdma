//go:build linux

package rdma

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// A GIDType is the type of a GID table entry.
type GIDType uint32

// GID types, from enum ib_uverbs_gid_type.
const (
	GIDTypeIB     GIDType = unix.IB_UVERBS_GID_TYPE_IB
	GIDTypeRoCEv1 GIDType = unix.IB_UVERBS_GID_TYPE_ROCE_V1
	GIDTypeRoCEv2 GIDType = unix.IB_UVERBS_GID_TYPE_ROCE_V2
)

func (t GIDType) String() string {
	switch t {
	case GIDTypeIB:
		return "IB"
	case GIDTypeRoCEv1:
		return "RoCEv1"
	case GIDTypeRoCEv2:
		return "RoCEv2"
	default:
		return fmt.Sprintf("GIDType(%d)", uint32(t))
	}
}

// QueryGID returns the entry at index in the GID table of a port.
func (d *Device) QueryGID(port uint8, index uint32) (*GIDEntry, error) {
	return d.Cmd().queryGID(port, index)
}

// QueryGIDTable returns the valid entries of the GID tables of all ports.
// maxEntries bounds the number of entries returned.
func (d *Device) QueryGIDTable(maxEntries int) ([]GIDEntry, error) {
	return d.Cmd().queryGIDTable(maxEntries)
}
