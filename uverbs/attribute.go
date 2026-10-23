//go:build linux

package uverbs

import "fmt"

// An AttributeType describes how an Attribute is carried in a uverbs ioctl.
type AttributeType uint8

// Attribute types.
const (
	// AttributeIn is an input buffer (UVERBS_ATTR_TYPE_PTR_IN). It also
	// covers constants, flags and arrays of object handles. Data up to
	// 8 bytes is sent inline in the attribute, anything longer by pointer.
	AttributeIn AttributeType = iota

	// AttributeOut is an output buffer (UVERBS_ATTR_TYPE_PTR_OUT) that the
	// kernel fills in.
	AttributeOut

	// AttributeObject refers to an existing object by its handle or file
	// descriptor (UVERBS_ATTR_TYPE_IDR, UVERBS_ATTR_TYPE_FD or
	// UVERBS_ATTR_TYPE_RAW_FD).
	AttributeObject

	// AttributeNewObject asks the kernel to create an object. On success,
	// the kernel stores the new handle or file descriptor in Value.
	AttributeNewObject

	// AttributeEnum is an input buffer tagged with an element of an enum
	// (UVERBS_ATTR_TYPE_ENUM_IN).
	AttributeEnum
)

// AttributeFlags are the flags of an Attribute.
type AttributeFlags uint16

// Attribute flags.
const (
	// FlagMandatory makes the kernel reject the request if it does not
	// know the attribute. Unknown attributes are ignored otherwise.
	FlagMandatory AttributeFlags = 1 << 0

	// FlagValidOutput is set by the kernel on output attributes it wrote.
	FlagValidOutput AttributeFlags = 1 << 1
)

// An Attribute is an attribute of a uverbs method, the Go form of struct
// ib_uverbs_attr. Numbers and structures in Data are in native byte order.
// Execute copies the kernel's Flags and, for AttributeNewObject, Value back
// into the Attribute, and the kernel writes outputs directly into Data.
type Attribute struct {
	// ID is the attribute ID, specific to the method being invoked.
	ID uint16

	// Type describes how the attribute is passed to the kernel.
	Type AttributeType

	// Flags are sent to the kernel, which may set FlagValidOutput.
	Flags AttributeFlags

	// EnumID selects the enum element of an AttributeEnum.
	EnumID uint8

	// Data holds the payload of AttributeIn and AttributeEnum, and the
	// buffer the kernel writes for AttributeOut.
	Data []byte

	// Value holds the handle or file descriptor of AttributeObject and
	// AttributeNewObject.
	Value int64
}

// MaxAttributeLen is the largest payload of an attribute, which must fit the
// 16-bit length of struct ib_uverbs_attr.
const MaxAttributeLen = 1<<16 - 1

// validate checks attrs before they are sent to the kernel.
func validate(attrs []Attribute) error {
	seen := make(map[uint16]bool, len(attrs))
	for _, a := range attrs {
		if len(a.Data) > MaxAttributeLen {
			return fmt.Errorf("uverbs: attribute %d: %d bytes exceeds maximum of %d",
				a.ID, len(a.Data), MaxAttributeLen)
		}

		// The kernel rejects duplicate attributes with EINVAL; give a
		// better error up front.
		if seen[a.ID] {
			return fmt.Errorf("uverbs: duplicate attribute %d", a.ID)
		}

		seen[a.ID] = true
	}

	return nil
}
