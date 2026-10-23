//go:build linux

// Package nldev implements the device interface of RDMA netlink
// (NETLINK_RDMA, client RDMA_NL_NLDEV), the interface the iproute2 rdma tool
// uses. It lists RDMA devices, finds their character devices and adds or
// removes software links such as rxe.
package nldev

import (
	"fmt"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// Constants from <rdma/rdma_netlink.h>.
const (
	clientNLDev = 5 // RDMA_NL_NLDEV

	cmdGet        = 1  // RDMA_NLDEV_CMD_GET
	cmdNewLink    = 3  // RDMA_NLDEV_CMD_NEWLINK
	cmdDelLink    = 4  // RDMA_NLDEV_CMD_DELLINK
	cmdGetChardev = 15 // RDMA_NLDEV_CMD_GET_CHARDEV

	attrDevIndex       = 1  // RDMA_NLDEV_ATTR_DEV_INDEX
	attrDevName        = 2  // RDMA_NLDEV_ATTR_DEV_NAME
	attrPortIndex      = 3  // RDMA_NLDEV_ATTR_PORT_INDEX
	attrFWVersion      = 5  // RDMA_NLDEV_ATTR_FW_VERSION
	attrNodeGUID       = 6  // RDMA_NLDEV_ATTR_NODE_GUID
	attrSysImageGUID   = 7  // RDMA_NLDEV_ATTR_SYS_IMAGE_GUID
	attrNdevName       = 51 // RDMA_NLDEV_ATTR_NDEV_NAME
	attrLinkType       = 65 // RDMA_NLDEV_ATTR_LINK_TYPE
	attrChardevType    = 69 // RDMA_NLDEV_ATTR_CHARDEV_TYPE
	attrChardevName    = 70 // RDMA_NLDEV_ATTR_CHARDEV_NAME
	attrChardevABI     = 71 // RDMA_NLDEV_ATTR_CHARDEV_ABI
	attrUverbsDriverID = 73 // RDMA_NLDEV_ATTR_UVERBS_DRIVER_ID
)

// A Conn is a connection to RDMA netlink.
type Conn struct {
	c *netlink.Conn
}

type Config struct {
	// NetNS is the network namespace to use. If nil, the current namespace is
	// used.
	NetNS int
}

// Dial opens a connection to RDMA netlink. cfg may be nil.
func Dial(cfg *Config) (*Conn, error) {
	ncfg := &netlink.Config{}
	if cfg != nil {
		ncfg.NetNS = cfg.NetNS
	}

	c, err := netlink.Dial(unix.NETLINK_RDMA, ncfg)
	if err != nil {
		return nil, err
	}

	return &Conn{c: c}, nil
}

// Close closes the connection.
func (c *Conn) Close() error { return c.c.Close() }

// A Device is an RDMA device.
type Device struct {
	// Index is the kernel index of the device.
	Index uint32

	// Name is the kernel name of the device, such as "mlx5_0" or "rxe0".
	Name string

	// Ports is the number of physical ports.
	Ports uint32

	// FirmwareVersion is the device's firmware version, if it has one.
	FirmwareVersion string

	// NodeGUID and SysImageGUID identify the device, in host byte order.
	NodeGUID     uint64
	SysImageGUID uint64
}

// Devices returns the RDMA devices in the connection's network namespace.
func (c *Conn) Devices() ([]*Device, error) {
	msgs, err := c.execute(cmdGet, netlink.Dump, nil)
	if err != nil {
		return nil, fmt.Errorf("nldev: list devices: %w", err)
	}

	devs := make([]*Device, 0, len(msgs))
	for _, m := range msgs {
		d, err := parseDevice(m.Data)
		if err != nil {
			return nil, err
		}

		devs = append(devs, d)
	}

	return devs, nil
}

func parseDevice(b []byte) (*Device, error) {
	ad, err := netlink.NewAttributeDecoder(b)
	if err != nil {
		return nil, err
	}

	var d Device
	for ad.Next() {
		switch ad.Type() {
		case attrDevIndex:
			d.Index = ad.Uint32()
		case attrDevName:
			d.Name = ad.String()
		case attrPortIndex:
			d.Ports = ad.Uint32()
		case attrFWVersion:
			d.FirmwareVersion = ad.String()
		case attrNodeGUID:
			d.NodeGUID = ad.Uint64()
		case attrSysImageGUID:
			d.SysImageGUID = ad.Uint64()
		}
	}

	if err := ad.Err(); err != nil {
		return nil, fmt.Errorf("nldev: parse device: %w", err)
	}

	return &d, nil
}

// A Chardev is a character device of an RDMA device, such as its uverbs
// device.
type Chardev struct {
	// Name is the name of the character device under /dev/infiniband, such
	// as "uverbs0".
	Name string

	// ABI is the ABI version of the character device.
	ABI uint64

	// DriverID is the device's driver (enum rdma_driver_id). It is only
	// reported for the "uverbs" type.
	DriverID uint32
}

// Chardev returns the character device of the given type, such as "uverbs",
// of the device with the given index. The kernel reports an error, as a
// *netlink.OpError, if the device has no such character device, for example
// because the module providing it (ib_uverbs) is not loaded.
func (c *Conn) Chardev(index uint32, typ string) (*Chardev, error) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(attrDevIndex, index)
	ae.String(attrChardevType, typ)

	msgs, err := c.execute(cmdGetChardev, 0, ae)
	if err != nil {
		return nil, fmt.Errorf("nldev: query %s device of device %d: %w", typ, index, err)
	}

	var cd Chardev
	for _, m := range msgs {
		ad, err := netlink.NewAttributeDecoder(m.Data)
		if err != nil {
			return nil, err
		}

		for ad.Next() {
			switch ad.Type() {
			case attrChardevName:
				cd.Name = ad.String()
			case attrChardevABI:
				cd.ABI = ad.Uint64()
			case attrUverbsDriverID:
				cd.DriverID = ad.Uint32()
			}
		}

		if err := ad.Err(); err != nil {
			return nil, fmt.Errorf("nldev: parse %s device of device %d: %w", typ, index, err)
		}
	}

	return &cd, nil
}

// NewLink creates a software RDMA device named name of the given link type,
// such as "rxe" or "siw", on top of the network interface netdev. It is the
// equivalent of "rdma link add <name> type <typ> netdev <netdev>".
func (c *Conn) NewLink(name, typ, netdev string) error {
	ae := netlink.NewAttributeEncoder()
	ae.String(attrDevName, name)
	ae.String(attrLinkType, typ)
	ae.String(attrNdevName, netdev)

	if _, err := c.execute(cmdNewLink, netlink.Acknowledge, ae); err != nil {
		return fmt.Errorf("nldev: add %s link %q: %w", typ, name, err)
	}

	return nil
}

// DelLink removes the software RDMA device with the given index. It is the
// equivalent of "rdma link delete".
func (c *Conn) DelLink(index uint32) error {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(attrDevIndex, index)

	if _, err := c.execute(cmdDelLink, netlink.Acknowledge, ae); err != nil {
		return fmt.Errorf("nldev: delete link of device %d: %w", index, err)
	}

	return nil
}

// execute sends an RDMA_NL_NLDEV request with the attributes in ae, which
// may be nil, and returns the replies.
func (c *Conn) execute(cmd uint16, flags netlink.HeaderFlags, ae *netlink.AttributeEncoder) ([]netlink.Message, error) {
	var data []byte
	if ae != nil {
		b, err := ae.Encode()
		if err != nil {
			return nil, err
		}

		data = b
	}

	return c.c.Execute(netlink.Message{
		Header: netlink.Header{
			Type:  netlink.HeaderType(clientNLDev<<10 | cmd),
			Flags: netlink.Request | flags,
		},
		Data: data,
	})
}
