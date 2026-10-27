//go:build linux

package nldev

import (
	"testing"

	"github.com/mdlayher/netlink"
)

func TestParseDevice(t *testing.T) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(attrDevIndex, 3)
	ae.String(attrDevName, "rxe0")
	ae.Uint32(attrPortIndex, 1)
	ae.String(attrFWVersion, "1.2.3")
	ae.Uint64(attrNodeGUID, 0x1122334455667788)
	ae.Uint64(attrSysImageGUID, 0x99)
	ae.String(attrNdevName, "ignored")

	b, err := ae.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := parseDevice(b)
	if err != nil {
		t.Fatalf("parseDevice: %v", err)
	}

	want := Device{Index: 3, Name: "rxe0", Ports: 1, FirmwareVersion: "1.2.3", NodeGUID: 0x1122334455667788, SysImageGUID: 0x99}
	if *got != want {
		t.Fatalf("parseDevice = %+v, want %+v", *got, want)
	}
}
