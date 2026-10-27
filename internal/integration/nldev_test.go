//go:build linux

package integration

import (
	"slices"
	"strings"
	"testing"

	"github.com/nickgarlis/rdma/nldev"
)

// testDevice returns an RDMA netlink connection and the device named by
// RDMA_TEST_DEVICE.
func testDevice(t *testing.T) (*nldev.Conn, *nldev.Device) {
	t.Helper()

	name := testEnv(t, "RDMA_TEST_DEVICE")
	c, err := nldev.Dial(nil)
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

func lookup(t *testing.T, c *nldev.Conn, name string) *nldev.Device {
	t.Helper()

	devs, err := c.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}

	i := slices.IndexFunc(devs, func(d *nldev.Device) bool { return d.Name == name })
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

	netdev := testEnv(t, "RDMA_TEST_NETDEV")

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
