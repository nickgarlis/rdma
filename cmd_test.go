//go:build linux

package rdma

import (
	"bytes"
	"testing"

	"github.com/nickgarlis/rdma/uverbs"
	"golang.org/x/sys/unix"
)

func TestUData(t *testing.T) {
	drv := &UData{In: []byte{1, 2, 3, 4}, Out: make([]byte, 8)}

	cmd := &uverbs.Command{}
	appendUData(cmd, drv)

	if len(cmd.Attrs) != 2 ||
		cmd.Attrs[0].ID != unix.UVERBS_ATTR_UHW_IN || !bytes.Equal(cmd.Attrs[0].Data, drv.In) ||
		cmd.Attrs[1].ID != unix.UVERBS_ATTR_UHW_OUT || cmd.Attrs[1].Type != uverbs.AttributeOut {
		t.Fatalf("driver attributes = %+v", cmd.Attrs)
	}

	// The kernel writes the response straight into Out.
	cmd.Attrs[1].Data[0] = 7
	if drv.Out[0] != 7 {
		t.Fatal("the response buffer is not Out")
	}

	// A response the kernel did not mark as written is an error.
	if err := checkUDataResp(cmd.Attrs, drv); err == nil {
		t.Fatal("checkUDataResp accepted an unwritten response")
	}

	cmd.Attrs[1].Flags |= uverbs.FlagValidOutput
	if err := checkUDataResp(cmd.Attrs, drv); err != nil {
		t.Fatalf("checkUDataResp: %v", err)
	}

	// Without UData, nothing is sent and nothing is expected.
	empty := &uverbs.Command{}
	appendUData(empty, nil)
	if len(empty.Attrs) != 0 || checkUDataResp(nil, nil) != nil {
		t.Fatalf("nil UData added %+v", empty.Attrs)
	}
}
