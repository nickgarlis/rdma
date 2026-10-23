//go:build linux

package uverbs

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// An ObjectID identifies a uverbs object type, such as a CQ or QP: one of the
// unix.UVERBS_OBJECT_* constants, or a driver-specific ID from
// unix.UVERBS_ID_DRIVER_NS.
type ObjectID uint16

// A MethodID identifies a method on an object type: one of the
// unix.UVERBS_METHOD_* constants. Its attribute IDs are the matching
// unix.UVERBS_ATTR_* constants.
type MethodID uint16

// A WriteCommand is a uverbs write command number: one of the
// unix.IB_USER_VERBS_CMD_* constants, or unix.IB_USER_VERBS_EX_CMD_* ORed
// with unix.IB_USER_VERBS_CMD_FLAG_EXTENDED. It is passed in
// unix.UVERBS_ATTR_WRITE_CMD to unix.UVERBS_METHOD_INVOKE_WRITE for verbs
// that have no native ioctl method.
type WriteCommand uint32

// A Command is one uverbs request: a method on an object type and its
// attributes.
type Command struct {
	Object ObjectID
	Method MethodID
	Attrs  []Attribute
}

// Execute runs cmd with one ioctl. The kernel's results are stored in
// cmd.Attrs: output Data is written directly, and Flags and, for
// AttributeNewObject, Value are copied back into each element. Read them by
// index after Execute returns.
func (c *Conn) Execute(cmd *Command) error {
	if err := validate(cmd.Attrs); err != nil {
		return err
	}

	return c.ioctl(cmd.Object, cmd.Method, cmd.Attrs)
}

// Write returns the Command that runs the write command wc through
// unix.UVERBS_METHOD_INVOKE_WRITE, with in as its encoded request structure
// and, if outLen is not zero, an outLen-byte buffer for its response, the
// Command's last attribute. Use it to add driver attributes before
// executing; otherwise use ExecuteWrite.
func Write(wc WriteCommand, in []byte, outLen int) *Command {
	cmd := &Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_INVOKE_WRITE,
		Attrs: []Attribute{
			{ID: unix.UVERBS_ATTR_WRITE_CMD, Type: AttributeIn, Data: binary.NativeEndian.AppendUint64(nil, uint64(wc))},
			{ID: unix.UVERBS_ATTR_CORE_IN, Type: AttributeIn, Data: in},
		},
	}

	if outLen > 0 {
		cmd.Attrs = append(cmd.Attrs, Attribute{ID: unix.UVERBS_ATTR_CORE_OUT, Type: AttributeOut, Data: make([]byte, outLen)})
	}

	return cmd
}

// ExecuteWrite runs the write command wc, for verbs that have no native
// ioctl method. in is its request and out, if not nil, a pointer to its
// response: fixed-size structures such as those of <rdma/ib_user_verbs.h>,
// encoded in native byte order.
func (c *Conn) ExecuteWrite(wc WriteCommand, in, out any) error {
	b, err := binary.Append(nil, binary.NativeEndian, in)
	if err != nil {
		return fmt.Errorf("uverbs: encoding write command %d: %w", wc, err)
	}

	outLen := 0
	if out != nil {
		if outLen = binary.Size(out); outLen < 0 {
			return fmt.Errorf("uverbs: write command %d: %T is not a fixed-size response", wc, out)
		}
	}

	cmd := Write(wc, b, outLen)
	if err := c.Execute(cmd); err != nil {
		return err
	}

	if out == nil {
		return nil
	}

	resp := cmd.Attrs[len(cmd.Attrs)-1]
	if resp.Flags&FlagValidOutput == 0 {
		return fmt.Errorf("uverbs: write command %d: kernel returned no response", wc)
	}

	_, err = binary.Decode(resp.Data, binary.NativeEndian, out)
	return err
}
