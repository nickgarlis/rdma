//go:build linux

package nldev

import (
	"os"
	"slices"
	"strings"
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

// The tests below need an rxe device named by RDMA_TEST_DEVICE on the
// interface named by RDMA_TEST_NETDEV. internal/vmtest runs them in a virtual
// machine after creating them.
func testDevice(t *testing.T) (*Conn, *Device) {
	t.Helper()

	name := os.Getenv("RDMA_TEST_DEVICE")
	if name == "" {
		t.Skip("RDMA_TEST_DEVICE is not set")
	}

	c, err := Dial(nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	t.Cleanup(func() { _ = c.Close() })

	d := lookup(t, c, name)
	if d == nil {
		t.Fatalf("device %q not found", name)
	}

	return c, d
}

func lookup(t *testing.T, c *Conn, name string) *Device {
	t.Helper()

	devs, err := c.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}

	i := slices.IndexFunc(devs, func(d *Device) bool { return d.Name == name })
	if i < 0 {
		return nil
	}

	return devs[i]
}

func TestDevices(t *testing.T) {
	_, d := testDevice(t)

	if d.Ports != 1 || d.NodeGUID == 0 {
		t.Fatalf("unexpected device: %+v", d)
	}
}

func TestChardev(t *testing.T) {
	c, d := testDevice(t)

	cd, err := c.Chardev(d.Index, "uverbs")
	if err != nil {
		t.Fatalf("Chardev: %v", err)
	}

	const driverRXE = 14 // RDMA_DRIVER_RXE
	if !strings.HasPrefix(cd.Name, "uverbs") || cd.DriverID != driverRXE {
		t.Fatalf("unexpected uverbs device: %+v", cd)
	}
}

func TestLink(t *testing.T) {
	c, d := testDevice(t)

	netdev := os.Getenv("RDMA_TEST_NETDEV")
	if netdev == "" {
		t.Skip("RDMA_TEST_NETDEV is not set")
	}

	// An rxe device can only exist once per interface, so remove the test
	// device and create it again.
	if err := c.DelLink(d.Index); err != nil {
		t.Fatalf("DelLink: %v", err)
	}

	if lookup(t, c, d.Name) != nil {
		t.Fatalf("device %q still exists after DelLink", d.Name)
	}

	if err := c.NewLink(d.Name, "rxe", netdev); err != nil {
		t.Fatalf("NewLink: %v", err)
	}

	if lookup(t, c, d.Name) == nil {
		t.Fatalf("device %q missing after NewLink", d.Name)
	}
}
