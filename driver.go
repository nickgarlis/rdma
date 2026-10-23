//go:build linux

package rdma

import "golang.org/x/sys/unix"

// A DriverID identifies the kernel driver behind a device, from enum
// rdma_driver_id. Every ioctl on the device carries it.
type DriverID uint32

// Driver IDs.
const (
	DriverUnknown   DriverID = unix.RDMA_DRIVER_UNKNOWN
	DriverMLX5      DriverID = unix.RDMA_DRIVER_MLX5
	DriverMLX4      DriverID = unix.RDMA_DRIVER_MLX4
	DriverCXGB3     DriverID = unix.RDMA_DRIVER_CXGB3
	DriverCXGB4     DriverID = unix.RDMA_DRIVER_CXGB4
	DriverMTHCA     DriverID = unix.RDMA_DRIVER_MTHCA
	DriverBNXTRE    DriverID = unix.RDMA_DRIVER_BNXT_RE
	DriverOCRDMA    DriverID = unix.RDMA_DRIVER_OCRDMA
	DriverNES       DriverID = unix.RDMA_DRIVER_NES
	DriverIRDMA     DriverID = unix.RDMA_DRIVER_IRDMA
	DriverVMWPVRDMA DriverID = unix.RDMA_DRIVER_VMW_PVRDMA
	DriverQEDR      DriverID = unix.RDMA_DRIVER_QEDR
	DriverHNS       DriverID = unix.RDMA_DRIVER_HNS
	DriverUSNIC     DriverID = unix.RDMA_DRIVER_USNIC
	DriverRXE       DriverID = unix.RDMA_DRIVER_RXE
	DriverHFI1      DriverID = unix.RDMA_DRIVER_HFI1
	DriverQIB       DriverID = unix.RDMA_DRIVER_QIB
	DriverEFA       DriverID = unix.RDMA_DRIVER_EFA
	DriverSIW       DriverID = unix.RDMA_DRIVER_SIW
	DriverERDMA     DriverID = unix.RDMA_DRIVER_ERDMA
	DriverMANA      DriverID = unix.RDMA_DRIVER_MANA
	DriverIONIC     DriverID = unix.RDMA_DRIVER_IONIC
)
