//go:build linux

// Command setup prepares the VM started by vmtest before the tests run: it
// loads the kernel modules embedded in the initramfs, creates a dummy
// interface with an IPv4 address and creates an rxe device on top of it.
// vmtest runs it as the first executable and passes the device name to the
// tests in RDMA_TEST_DEVICE.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"

	"github.com/mdlayher/netlink"
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

// setupVM loads the kernel modules embedded in the initramfs and creates the
// rxe device.
func setupVM() error {
	if mod := os.Getenv("RDMA_VM_KDEBUG"); mod != "" {
		ctl := []byte("module " + mod + " +p\n")
		if err := os.WriteFile("/proc/dynamic_debug/control", ctl, 0); err != nil {
			return fmt.Errorf("enable dynamic debug: %w", err)
		}
	}

	// vmtest prefixes the modules with their load order.
	mods, err := filepath.Glob("/*.ko")
	if err != nil {
		return err
	}

	slices.Sort(mods)
	for _, m := range mods {
		b, err := os.ReadFile(m)
		if err != nil {
			return err
		}

		if err := unix.InitModule(b, ""); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("load %s: %w", filepath.Base(m), err)
		}
	}

	if err := setupNetdev(); err != nil {
		return err
	}

	return addRXELink(vmDevice, vmNetdev)
}

// setupNetdev creates a dummy interface with an IPv4 address and brings it
// and the loopback interface up.
func setupNetdev() error {
	c, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	ae := netlink.NewAttributeEncoder()
	ae.String(unix.IFLA_IFNAME, vmNetdev)
	ae.Nested(unix.IFLA_LINKINFO, func(nae *netlink.AttributeEncoder) error {
		nae.String(unix.IFLA_INFO_KIND, "dummy")
		return nil
	})

	attrs, err := ae.Encode()
	if err != nil {
		return err
	}

	if err := request(c, unix.RTM_NEWLINK, netlink.Create|netlink.Excl, append(ifinfomsg(0, 0), attrs...)); err != nil {
		return fmt.Errorf("create %s: %w", vmNetdev, err)
	}

	for _, name := range []string{"lo", vmNetdev} {
		ifi, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}

		if err := request(c, unix.RTM_NEWLINK, 0, ifinfomsg(ifi.Index, unix.IFF_UP)); err != nil {
			return fmt.Errorf("set %s up: %w", name, err)
		}
	}

	ifi, err := net.InterfaceByName(vmNetdev)
	if err != nil {
		return err
	}

	ip := net.ParseIP(vmIPv4).To4()
	ae = netlink.NewAttributeEncoder()
	ae.Bytes(unix.IFA_LOCAL, ip)
	ae.Bytes(unix.IFA_ADDRESS, ip)
	attrs, err = ae.Encode()
	if err != nil {
		return err
	}

	// struct ifaddrmsg for a /24 IPv4 address.
	msg := []byte{unix.AF_INET, 24, 0, 0, 0, 0, 0, 0}
	binary.NativeEndian.PutUint32(msg[4:], uint32(ifi.Index))

	if err := request(c, unix.RTM_NEWADDR, netlink.Create|netlink.Excl, append(msg, attrs...)); err != nil {
		return fmt.Errorf("add address: %w", err)
	}

	return nil
}

// ifinfomsg returns a struct ifinfomsg that sets the given interface flags.
func ifinfomsg(index int, flags uint32) []byte {
	b := make([]byte, 16)
	binary.NativeEndian.PutUint32(b[4:], uint32(index))
	binary.NativeEndian.PutUint32(b[8:], flags)
	binary.NativeEndian.PutUint32(b[12:], flags)
	return b
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

func request(c *netlink.Conn, typ uint16, flags netlink.HeaderFlags, data []byte) error {
	_, err := c.Execute(netlink.Message{
		Header: netlink.Header{
			Type:  netlink.HeaderType(typ),
			Flags: netlink.Request | netlink.Acknowledge | flags,
		},
		Data: data,
	})

	return err
}
