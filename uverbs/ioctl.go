//go:build linux

package uverbs

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ioctlHdrLen  = int(unsafe.Sizeof(unix.IBUverbsIoctlHdr{}))
	ioctlAttrLen = int(unsafe.Sizeof(unix.IBUverbsAttr{}))

	// maxAttributes is how many attributes fit the kernel's limit of one
	// page for the header and attribute array.
	maxAttributes = (4096 - ioctlHdrLen) / ioctlAttrLen
)

// ioctl marshals attrs into a struct ib_uverbs_ioctl_hdr followed by its
// struct ib_uverbs_attr array, issues RDMA_VERBS_IOCTL and copies the
// kernel's per-attribute results back into attrs.
func (c *Conn) ioctl(object ObjectID, method MethodID, attrs []Attribute) error {
	n := len(attrs)
	if n > maxAttributes {
		return fmt.Errorf("uverbs: %d attributes exceeds maximum of %d", n, maxAttributes)
	}

	// Back the buffer with uint64s so the __aligned_u64 fields are
	// aligned.
	words := make([]uint64, (ioctlHdrLen+n*ioctlAttrLen)/8)
	hdr := (*unix.IBUverbsIoctlHdr)(unsafe.Pointer(&words[0]))
	uattrs := unsafe.Slice((*unix.IBUverbsAttr)(unsafe.Add(unsafe.Pointer(hdr), ioctlHdrLen)), n)

	*hdr = unix.IBUverbsIoctlHdr{
		Length:    uint16(len(words) * 8),
		Object_id: uint16(object),
		Method_id: uint16(method),
		Num_attrs: uint16(n),
		Driver_id: c.driverID,
	}

	// The kernel follows the pointers in the attributes, so the buffers
	// they refer to must not move until the ioctl returns.
	var pinner runtime.Pinner
	defer pinner.Unpin()

	pinner.Pin(&words[0])

	for i := range attrs {
		a, ua := &attrs[i], &uattrs[i]
		ua.Attr_id = a.ID
		ua.Flags = uint16(a.Flags)

		switch a.Type {
		case AttributeIn, AttributeEnum:
			ua.Len = uint16(len(a.Data))
			if a.Type == AttributeEnum {
				ua.Attr_data[0] = a.EnumID
			}

			if len(a.Data) <= 8 {
				// Short inputs are passed inline in the data field.
				copy((*[8]byte)(unsafe.Pointer(&ua.Data))[:], a.Data)
			} else {
				ua.Data = pin(&pinner, a.Data)
			}
		case AttributeOut:
			ua.Len = uint16(len(a.Data))
			if len(a.Data) > 0 {
				ua.Data = pin(&pinner, a.Data)
			}
		case AttributeObject, AttributeNewObject:
			ua.Data = uint64(a.Value)
		default:
			return fmt.Errorf("uverbs: attribute %d has unknown type %d", a.ID, a.Type)
		}
	}

	if err := c.file.Ioctl(unix.RDMA_VERBS_IOCTL, unsafe.Pointer(hdr)); err != nil {
		return err
	}

	for i := range attrs {
		a, ua := &attrs[i], &uattrs[i]
		a.Flags = AttributeFlags(ua.Flags)
		if a.Type == AttributeNewObject {
			a.Value = int64(ua.Data)
		}
	}

	return nil
}

// pin pins b until the ioctl returns and returns its address.
func pin(p *runtime.Pinner, b []byte) uint64 {
	p.Pin(&b[0])
	return uint64(uintptr(unsafe.Pointer(&b[0])))
}
