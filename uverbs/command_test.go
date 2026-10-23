//go:build linux

package uverbs

import (
	"encoding/binary"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWrite(t *testing.T) {
	in := []byte{1, 2, 3, 4}

	got := Write(9, in, 8)
	want := &Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_INVOKE_WRITE,
		Attrs: []Attribute{
			{ID: unix.UVERBS_ATTR_WRITE_CMD, Type: AttributeIn, Data: binary.NativeEndian.AppendUint64(nil, 9)},
			{ID: unix.UVERBS_ATTR_CORE_IN, Type: AttributeIn, Data: in},
			{ID: unix.UVERBS_ATTR_CORE_OUT, Type: AttributeOut, Data: make([]byte, 8)},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Write:\n got %+v\nwant %+v", got, want)
	}

	if cmd := Write(9, in, 0); len(cmd.Attrs) != 2 {
		t.Fatalf("Write without a response has %d attributes, want 2", len(cmd.Attrs))
	}
}
