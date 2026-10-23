//go:build linux

package rdma

import "golang.org/x/sys/unix"

// An SGE is a scatter/gather element: a buffer in a registered memory region.
type SGE struct {
	Addr   uint64
	Length uint32
	LKey   uint32
}

// A WROpcode is the operation of a send work request.
type WROpcode uint32

// Send work request opcodes, from enum ib_uverbs_wr_opcode.
const (
	WRRDMAWrite         WROpcode = unix.IB_UVERBS_WR_RDMA_WRITE
	WRRDMAWriteWithImm  WROpcode = unix.IB_UVERBS_WR_RDMA_WRITE_WITH_IMM
	WRSend              WROpcode = unix.IB_UVERBS_WR_SEND
	WRSendWithImm       WROpcode = unix.IB_UVERBS_WR_SEND_WITH_IMM
	WRRDMARead          WROpcode = unix.IB_UVERBS_WR_RDMA_READ
	WRAtomicCmpAndSwp   WROpcode = unix.IB_UVERBS_WR_ATOMIC_CMP_AND_SWP
	WRAtomicFetchAndAdd WROpcode = unix.IB_UVERBS_WR_ATOMIC_FETCH_AND_ADD
	WRSendWithInv       WROpcode = unix.IB_UVERBS_WR_SEND_WITH_INV
)

// SendFlags modify a send work request.
type SendFlags uint32

// Send flags, from enum ibv_send_flags.
const (
	SendFence     SendFlags = 1 << 0
	SendSignaled  SendFlags = 1 << 1
	SendSolicited SendFlags = 1 << 2
	SendInline    SendFlags = 1 << 3
	SendIPCsum    SendFlags = 1 << 4
)

// A SendWR is a work request posted to a QP's send queue.
type SendWR struct {
	// WRID is returned in the WC of the request.
	WRID      uint64
	Opcode    WROpcode
	SendFlags SendFlags

	// SGList lists the local buffers to send from or, for reads and atomics,
	// to write into. With SendInline, the data is copied into the work
	// request and the LKeys are ignored.
	SGList []SGE

	// ImmData is sent with the *WithImm opcodes, in host byte order.
	ImmData uint32

	// InvalidateRKey is the remote key invalidated by WRSendWithInv.
	InvalidateRKey uint32

	// RemoteAddr and RKey address remote memory for RDMA and atomic
	// operations.
	RemoteAddr uint64
	RKey       uint32

	// CompareAdd and Swap are the operands of atomic operations.
	CompareAdd uint64
	Swap       uint64
}

// A RecvWR is a work request posted to a QP's receive queue.
type RecvWR struct {
	// WRID is returned in the WC of the request.
	WRID uint64

	// SGList lists the buffers incoming data is scattered into.
	SGList []SGE
}
