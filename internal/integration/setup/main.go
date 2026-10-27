//go:build linux

// Command setup prepares the VM booted by scripts/integration.sh before the
// tests run: it creates a dummy interface with an IPv4 address and an rxe
// device on top of it. The VM runs it first, then the integration tests,
// which find the device in RDMA_TEST_DEVICE.
package main

import (
	"fmt"
	"net"
	"os"

	"github.com/jsimonetti/rtnetlink/v2"
	"github.com/nickgarlis/rdma/nldev"
	"golang.org/x/sys/unix"
)

// The rxe device on top of a dummy interface whose IPv4 address gives the
// device a RoCEv2 GID.
const (
	vmDevice = "rxe0"
	vmNetdev = "vmtest0"
	vmIPv4   = "10.11.12.1"
)

func main() {
	if err := setupVM(); err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("setup: %s is ready on %s (%s)\n", vmDevice, vmNetdev, vmIPv4)
}

// setupVM creates the rxe device.
func setupVM() error {
	if err := setupNetdev(); err != nil {
		return err
	}

	return addRXELink(vmDevice, vmNetdev)
}

// setupNetdev creates a dummy interface with an IPv4 address and brings it
// and the loopback interface up.
func setupNetdev() error {
	c, err := rtnetlink.Dial(nil)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Link.New(&rtnetlink.LinkMessage{
		Family: unix.AF_UNSPEC,
		Attributes: &rtnetlink.LinkAttributes{
			Name: vmNetdev,
			Info: &rtnetlink.LinkInfo{Kind: "dummy"},
		},
	}); err != nil {
		return fmt.Errorf("create %s: %w", vmNetdev, err)
	}

	for _, name := range []string{"lo", vmNetdev} {
		ifi, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}

		if err := c.Link.Set(&rtnetlink.LinkMessage{
			Family: unix.AF_UNSPEC,
			Index:  uint32(ifi.Index),
			Flags:  unix.IFF_UP,
			Change: unix.IFF_UP,
		}); err != nil {
			return fmt.Errorf("set %s up: %w", name, err)
		}
	}

	ifi, err := net.InterfaceByName(vmNetdev)
	if err != nil {
		return err
	}

	ip := net.ParseIP(vmIPv4).To4()
	if err := c.Address.New(&rtnetlink.AddressMessage{
		Family:       unix.AF_INET,
		PrefixLength: 24,
		Index:        uint32(ifi.Index),
		Attributes:   &rtnetlink.AddressAttributes{Address: ip, Local: ip},
	}); err != nil {
		return fmt.Errorf("add address: %w", err)
	}

	return nil
}

// addRXELink is the equivalent of "rdma link add <name> type rxe netdev
// <netdev>".
func addRXELink(name, netdev string) error {
	c, err := nldev.Dial(nil)
	if err != nil {
		return err
	}
	defer c.Close()

	return c.NewLink(name, "rxe", netdev)
}
