//go:build linux

package rdma

import "fmt"

// A PortState is the logical state of a port.
type PortState uint8

// Port states, from enum ibv_port_state.
const (
	PortNop PortState = iota
	PortDown
	PortInit
	PortArmed
	PortActive
	PortActiveDefer
)

var portStateNames = [...]string{"NOP", "DOWN", "INIT", "ARMED", "ACTIVE", "ACTIVE_DEFER"}

func (s PortState) String() string {
	if int(s) < len(portStateNames) {
		return portStateNames[s]
	}

	return fmt.Sprintf("PortState(%d)", uint8(s))
}

// A LinkLayer is the link layer protocol of a port.
type LinkLayer uint8

// Link layers.
const (
	LinkLayerUnspecified LinkLayer = iota
	LinkLayerInfiniBand
	LinkLayerEthernet
)

func (l LinkLayer) String() string {
	switch l {
	case LinkLayerInfiniBand:
		return "InfiniBand"
	case LinkLayerEthernet:
		return "Ethernet"
	default:
		return "unspecified"
	}
}

// QueryDevice returns the attributes of the device, as the kernel reports
// them in struct ib_uverbs_query_device_resp, like ibv_query_device.
func (d *Device) QueryDevice() (*QueryDeviceResp, error) {
	return d.provider.QueryDevice()
}

// QueryPort returns the attributes of a port, as the kernel reports them in
// struct ib_uverbs_query_port_resp_ex. Ports are numbered from 1.
func (d *Device) QueryPort(port uint8) (*QueryPortRespEx, error) {
	return d.provider.QueryPort(port)
}
