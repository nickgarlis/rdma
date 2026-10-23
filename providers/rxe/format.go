//go:build linux

package rxe

import (
	"encoding/binary"
	"fmt"

	"github.com/nickgarlis/rdma"
)

// Layout of struct rxe_send_wqe and struct rxe_recv_wqe.
const (
	sendWRID       = 0
	sendWROpcode   = 12
	sendWRFlags    = 16
	sendWREx       = 20
	sendWRRemote   = 24 // wr.rdma.remote_addr and wr.atomic.remote_addr
	sendWRRDMARKey = 32 // wr.rdma.rkey
	sendWRCompare  = 32 // wr.atomic.compare_add
	sendWRSwap     = 40 // wr.atomic.swap
	sendWRAtomRKey = 48 // wr.atomic.rkey
	sendWQEIOVA    = 152
	sendWQEDMA     = 184

	recvWQEID  = 0
	recvWQEDMA = 16

	// Offsets within struct rxe_dma_info.
	dmaLength = 0
	dmaResid  = 4
	dmaNumSGE = 12
	dmaSGE    = 24

	sgeSize = 16 // sizeof(struct rxe_sge)
)

// rxeCQESize is sizeof(struct ib_uverbs_wc), the entry rxe's kernel driver
// writes into a CQ.
const rxeCQESize = 48

// rxeDMAInfo is the part of struct rxe_dma_info that user space fills in,
// followed by its scatter/gather list (struct rxe_sge) or, for inline sends,
// the data the SGL refers to.
type rxeDMAInfo struct {
	Length uint32
	Resid  uint32
	NumSGE uint32
	SGL    []rdma.SGE
	Inline bool // copy the SGL's data into the WQE instead of the SGL
}

func (d *rxeDMAInfo) encode(b []byte) error {
	ne := binary.NativeEndian
	ne.PutUint32(b[dmaLength:], d.Length)
	ne.PutUint32(b[dmaResid:], d.Resid)
	ne.PutUint32(b[dmaNumSGE:], d.NumSGE)

	data := b[dmaSGE:]
	if d.Inline {
		if int(d.Length) > len(data) {
			return fmt.Errorf("%d bytes of inline data do not fit a %d-byte WQE", d.Length, len(data))
		}

		off := 0
		for _, sge := range d.SGL {
			off += copy(data[off:], userBytes(sge.Addr, sge.Length))
		}

		return nil
	}

	if len(d.SGL)*sgeSize > len(data) {
		return fmt.Errorf("%d SGEs do not fit a %d-byte WQE", len(d.SGL), len(data))
	}

	for i, sge := range d.SGL {
		e := data[i*sgeSize:]
		ne.PutUint64(e[0:], sge.Addr)
		ne.PutUint32(e[8:], sge.Length)
		ne.PutUint32(e[12:], sge.LKey)
	}

	return nil
}

// rxeSendWQE is struct rxe_send_wqe, an entry of an rxe send queue.
type rxeSendWQE struct {
	WRID   uint64
	Opcode uint32
	Flags  uint32

	// Ex is the immediate data, sent in network byte order, or the remote
	// key to invalidate.
	Ex          uint32
	ExBigEndian bool

	// The wr union: the RDMA layout, or the atomic layout, which moves RKey
	// after CompareAdd and Swap.
	RemoteAddr uint64
	RKey       uint32
	CompareAdd uint64
	Swap       uint64
	Atomic     bool

	IOVA uint64
	DMA  rxeDMAInfo
}

func (w *rxeSendWQE) encode(slot []byte) error {
	clear(slot)

	ne := binary.NativeEndian
	ne.PutUint64(slot[sendWRID:], w.WRID)
	ne.PutUint32(slot[sendWROpcode:], w.Opcode)
	ne.PutUint32(slot[sendWRFlags:], w.Flags)

	if w.ExBigEndian {
		binary.BigEndian.PutUint32(slot[sendWREx:], w.Ex)
	} else {
		ne.PutUint32(slot[sendWREx:], w.Ex)
	}

	ne.PutUint64(slot[sendWRRemote:], w.RemoteAddr)
	if w.Atomic {
		ne.PutUint64(slot[sendWRCompare:], w.CompareAdd)
		ne.PutUint64(slot[sendWRSwap:], w.Swap)
		ne.PutUint32(slot[sendWRAtomRKey:], w.RKey)
	} else {
		ne.PutUint32(slot[sendWRRDMARKey:], w.RKey)
	}

	ne.PutUint64(slot[sendWQEIOVA:], w.IOVA)
	return w.DMA.encode(slot[sendWQEDMA:])
}

// rxeRecvWQE is struct rxe_recv_wqe, an entry of an rxe receive queue.
type rxeRecvWQE struct {
	WRID uint64
	DMA  rxeDMAInfo
}

func (w *rxeRecvWQE) encode(slot []byte) error {
	clear(slot)
	binary.NativeEndian.PutUint64(slot[recvWQEID:], w.WRID)
	return w.DMA.encode(slot[recvWQEDMA:])
}

// rxeCQE is struct ib_uverbs_wc, the entry rxe's kernel driver writes into a
// CQ.
type rxeCQE struct {
	WRID         uint64
	Status       uint32
	Opcode       uint32
	VendorErr    uint32
	ByteLen      uint32
	Ex           [4]byte // imm_data (__be32) or invalidate_rkey (__u32)
	QPNum        uint32
	SrcQP        uint32
	WCFlags      uint32
	PKeyIndex    uint16
	SLID         uint16
	SL           uint8
	DLIDPathBits uint8
	PortNum      uint8
}

func (c *rxeCQE) decode(slot []byte) error {
	if len(slot) < rxeCQESize {
		return fmt.Errorf("rxe: CQ entry of %d bytes, need %d", len(slot), rxeCQESize)
	}

	ne := binary.NativeEndian
	*c = rxeCQE{
		WRID:         ne.Uint64(slot[0:]),
		Status:       ne.Uint32(slot[8:]),
		Opcode:       ne.Uint32(slot[12:]),
		VendorErr:    ne.Uint32(slot[16:]),
		ByteLen:      ne.Uint32(slot[20:]),
		Ex:           [4]byte(slot[24:28]),
		QPNum:        ne.Uint32(slot[28:]),
		SrcQP:        ne.Uint32(slot[32:]),
		WCFlags:      ne.Uint32(slot[36:]),
		PKeyIndex:    ne.Uint16(slot[40:]),
		SLID:         ne.Uint16(slot[42:]),
		SL:           slot[44],
		DLIDPathBits: slot[45],
		PortNum:      slot[46],
	}

	return nil
}

// wc returns the CQE as an rdma.WC.
func (c *rxeCQE) wc() rdma.WC {
	wc := rdma.WC{
		WRID:         c.WRID,
		Status:       rdma.WCStatus(c.Status),
		Opcode:       rdma.WCOpcode(c.Opcode),
		VendorErr:    c.VendorErr,
		ByteLen:      c.ByteLen,
		QPNum:        c.QPNum,
		SrcQP:        c.SrcQP,
		WCFlags:      rdma.WCFlags(c.WCFlags),
		PKeyIndex:    c.PKeyIndex,
		SLID:         c.SLID,
		SL:           c.SL,
		DLIDPathBits: c.DLIDPathBits,
		PortNum:      c.PortNum,
	}

	// The union holds immediate data in network byte order, or an
	// invalidated rkey in host byte order.
	if wc.WCFlags&rdma.WCWithImm != 0 {
		wc.ImmData = binary.BigEndian.Uint32(c.Ex[:])
	} else {
		wc.ImmData = binary.NativeEndian.Uint32(c.Ex[:])
	}

	return wc
}

// sizeofUverbsSendWR is sizeof(struct ib_uverbs_send_wr), the minimum WQE
// size the kernel accepts in a POST_SEND command.
const sizeofUverbsSendWR = 56

// mmapInfo is struct mminfo: where the kernel exposes a queue in the uverbs
// file's mmap space.
type mmapInfo struct {
	Offset uint64
	Size   uint32
	Pad    uint32
}

// rxeCreateCQResp is struct rxe_create_cq_resp, the rxe driver's response to
// creating a CQ.
type rxeCreateCQResp struct {
	CQ mmapInfo
}

// rxeCreateQPResp is struct rxe_create_qp_resp, the rxe driver's response to
// creating a QP.
type rxeCreateQPResp struct {
	RQ mmapInfo
	SQ mmapInfo
}
