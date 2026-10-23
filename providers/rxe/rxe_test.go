//go:build linux

package rxe_test

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/nickgarlis/rdma"
	"github.com/nickgarlis/rdma/providers/rxe"
	"github.com/nickgarlis/rdma/uverbs"
	"golang.org/x/sys/unix"
)

// The tests need an rxe device named by RDMA_TEST_DEVICE. internal/vmtest
// runs them in a virtual machine after creating one.
func openDevice(t *testing.T) *rdma.Device {
	t.Helper()

	name := os.Getenv("RDMA_TEST_DEVICE")
	if name == "" {
		t.Skip("RDMA_TEST_DEVICE is not set")
	}

	// Open fails if the device is not an rxe device, since the provider
	// only supports rxe.
	dev, err := rxe.Open(name)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() {
		if err := dev.Close(); err != nil {
			t.Errorf("Device.Close: %v", err)
		}
	})

	return dev
}

// roceGID waits for an IPv4-based RoCEv2 GID on port 1, which the kernel
// adds shortly after the device appears.
func roceGID(t *testing.T, dev *rdma.Device) rdma.GIDEntry {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, err := dev.QueryGIDTable(64)
		if err != nil {
			t.Fatalf("QueryGIDTable: %v", err)
		}

		for _, e := range entries {
			if rdma.GIDType(e.GIDType) == rdma.GIDTypeRoCEv2 && net.IP(e.GID[:]).To4() != nil {
				return e
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("no IPv4 RoCEv2 GID in %v", entries)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func TestQuery(t *testing.T) {
	dev := openDevice(t)

	attr, err := dev.QueryDevice()
	if err != nil {
		t.Fatalf("QueryDevice: %v", err)
	}

	if attr.PhysPortCnt != 1 || attr.MaxQP == 0 || attr.MaxCQE == 0 {
		t.Fatalf("unexpected device attributes: %+v", attr)
	}

	port, err := dev.QueryPort(1)
	if err != nil {
		t.Fatalf("QueryPort: %v", err)
	}

	p := port.LegacyResp
	if rdma.LinkLayer(p.LinkLayer) != rdma.LinkLayerEthernet || p.GIDTblLen == 0 {
		t.Fatalf("unexpected port attributes: %+v", port)
	}

	e := roceGID(t, dev)

	got, err := dev.QueryGID(1, e.GIDIndex)
	if err != nil {
		t.Fatalf("QueryGID: %v", err)
	}

	if *got != e {
		t.Fatalf("QueryGID = %+v, want %+v", got, e)
	}

	t.Logf("port 1: state %s, MTU %d, GID %d %s (%s)", rdma.PortState(p.State), rdma.MTU(p.ActiveMTU).Bytes(),
		e.GIDIndex, net.IP(e.GID[:]), rdma.GIDType(e.GIDType))
}

func TestExecuteUnknownMethod(t *testing.T) {
	dev := openDevice(t)

	// Raw requests reach the kernel unchanged, so an unknown method is
	// rejected by it.
	if err := dev.Conn().Execute(&uverbs.Command{Object: unix.UVERBS_OBJECT_DEVICE, Method: 0xfff}); err == nil {
		t.Fatal("Execute of an unknown method succeeded")
	}
}

func closeOK(t *testing.T, what string, fn func() error) {
	t.Helper()

	if err := fn(); err != nil {
		t.Errorf("destroy %s: %v", what, err)
	}
}
