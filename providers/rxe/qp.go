//go:build linux

package rxe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/nickgarlis/rdma"
	"golang.org/x/sys/unix"
)

func (p *provider) CreateQP(pd *rdma.PD, attr *rdma.QPInitAttr) (*rdma.QP, error) {
	switch attr.QPType {
	case rdma.QPTypeRC, rdma.QPTypeUC:
	default:
		return nil, fmt.Errorf("rxe: unsupported QP type %d", attr.QPType)
	}

	var resp rxeCreateQPResp
	out := make([]byte, binary.Size(resp))
	qp, err := p.dev.Cmd().CreateQP(pd, attr, &rdma.UData{Out: out})
	if err != nil {
		return nil, err
	}

	var sq, rq *queue
	if _, err = binary.Decode(out, binary.NativeEndian, &resp); err != nil {
		err = fmt.Errorf("rxe: decoding create QP response: %w", err)
	} else if rq, err = newQueue(p.dev, resp.RQ); err == nil {
		if sq, err = newQueue(p.dev, resp.SQ); err != nil {
			_ = rq.close()
		}
	}

	if err != nil {
		_ = p.dev.Cmd().DestroyQP(qp)
		return nil, err
	}

	qp.SetProviderData(&queuePair{
		sq: &sendQueue{
			q:         sq,
			dev:       p.dev,
			qp:        qp,
			maxSGE:    qp.Cap.MaxSendSGE,
			maxInline: qp.Cap.MaxInlineData,
		},
		rq: &recvQueue{q: rq, qp: qp, maxSGE: qp.Cap.MaxRecvSGE},
	})
	return qp, nil
}

func (p *provider) DestroyQP(qp *rdma.QP) error {
	pair, ok := qp.ProviderData[*queuePair]()
	if !ok || pair == nil {
		return errForeignQP
	}

	if err := p.dev.Cmd().DestroyQP(qp); err != nil {
		return err
	}

	// Closing waits for running posts, and later posts fail.
	return errors.Join(pair.sq.q.close(), pair.rq.q.close())
}

func (p *provider) PostSend(qp *rdma.QP, wr ...rdma.SendWR) error {
	pair, ok := qp.ProviderData[*queuePair]()
	if !ok || pair == nil {
		return errForeignQP
	}

	return pair.sq.post(wr)
}

func (p *provider) PostRecv(qp *rdma.QP, wr ...rdma.RecvWR) error {
	pair, ok := qp.ProviderData[*queuePair]()
	if !ok || pair == nil {
		return errForeignQP
	}

	return pair.rq.post(wr)
}

// queuePair is the provider state of a QP: its send and receive queues.
type queuePair struct {
	sq *sendQueue
	rq *recvQueue
}

// A sendQueue is a QP's send queue: the ring of send WQEs that the kernel
// consumes after a doorbell.
type sendQueue struct {
	q         *queue
	dev       *rdma.Device
	qp        *rdma.QP
	maxSGE    uint32
	maxInline uint32
}

// post writes wrs to the ring and rings the doorbell.
func (s *sendQueue) post(wrs []rdma.SendWR) error {
	posted, err := s.q.produce(len(wrs), func(i int, slot []byte) error {
		w, err := s.wqe(&wrs[i])
		if err != nil {
			return err
		}

		return w.encode(slot)
	})
	if err != nil && posted < len(wrs) {
		err = fmt.Errorf("rxe: post send work request %d: %w", wrs[posted].WRID, err)
	}

	// Ring the doorbell for what was posted, even if a later work request
	// failed.
	if posted > 0 {
		if derr := s.ringDoorbell(); derr != nil {
			return errors.Join(err, derr)
		}
	}

	return err
}

// wqe checks wr against the queue's limits and builds its WQE.
func (s *sendQueue) wqe(wr *rdma.SendWR) (rxeSendWQE, error) {
	if uint32(len(wr.SGList)) > s.maxSGE {
		return rxeSendWQE{}, fmt.Errorf("%d SGEs exceeds maximum of %d", len(wr.SGList), s.maxSGE)
	}

	var length uint64
	for _, s := range wr.SGList {
		length += uint64(s.Length)
	}

	inline := wr.SendFlags&rdma.SendInline != 0
	if inline && length > uint64(s.maxInline) {
		return rxeSendWQE{}, fmt.Errorf("%d bytes of inline data exceeds maximum of %d", length, s.maxInline)
	}

	isAtomic := wr.Opcode == rdma.WRAtomicCmpAndSwp || wr.Opcode == rdma.WRAtomicFetchAndAdd
	if isAtomic && (length < 8 || wr.RemoteAddr&7 != 0) {
		return rxeSendWQE{}, errAtomicAlignment
	}

	w := rxeSendWQE{
		WRID:   wr.WRID,
		Opcode: uint32(wr.Opcode),
		Flags:  uint32(wr.SendFlags),
		IOVA:   wr.RemoteAddr,
		DMA: rxeDMAInfo{
			Length: uint32(length),
			Resid:  uint32(length),
			NumSGE: uint32(len(wr.SGList)),
			SGL:    wr.SGList,
			Inline: inline,
		},
	}

	switch wr.Opcode {
	case rdma.WRSendWithImm, rdma.WRRDMAWriteWithImm:
		w.Ex, w.ExBigEndian = wr.ImmData, true
	case rdma.WRSendWithInv:
		w.Ex = wr.InvalidateRKey
	}

	switch wr.Opcode {
	case rdma.WRRDMAWrite, rdma.WRRDMAWriteWithImm, rdma.WRRDMARead:
		w.RemoteAddr, w.RKey = wr.RemoteAddr, wr.RKey
	case rdma.WRAtomicCmpAndSwp, rdma.WRAtomicFetchAndAdd:
		w.RemoteAddr, w.RKey = wr.RemoteAddr, wr.RKey
		w.Atomic, w.CompareAdd, w.Swap = true, wr.CompareAdd, wr.Swap
	}

	return w, nil
}

// ringDoorbell tells the kernel there is new work on the send queue with an
// empty POST_SEND command.
func (s *sendQueue) ringDoorbell() error {
	// struct ib_uverbs_post_send with no work requests.
	in := make([]byte, 24)
	ne := binary.NativeEndian
	ne.PutUint32(in[8:], s.qp.Handle)
	ne.PutUint32(in[20:], sizeofUverbsSendWR)

	// struct ib_uverbs_post_send_resp.
	var badWR uint32
	if err := s.dev.Conn().ExecuteWrite(unix.IB_USER_VERBS_CMD_POST_SEND, in, &badWR); err != nil {
		return fmt.Errorf("rxe: ring send doorbell: %w", err)
	}

	return nil
}

// A recvQueue is a QP's receive queue: the ring of receive WQEs that the
// kernel consumes as messages arrive.
type recvQueue struct {
	q      *queue
	qp     *rdma.QP
	maxSGE uint32
}

// post writes wrs to the ring.
func (r *recvQueue) post(wrs []rdma.RecvWR) error {
	if r.qp.State() == rdma.QPStateReset {
		return errQPReset
	}

	posted, err := r.q.produce(len(wrs), func(i int, slot []byte) error {
		wr := &wrs[i]
		if uint32(len(wr.SGList)) > r.maxSGE {
			return fmt.Errorf("%d SGEs exceeds maximum of %d", len(wr.SGList), r.maxSGE)
		}

		var length uint32
		for _, sge := range wr.SGList {
			length += sge.Length
		}

		w := rxeRecvWQE{
			WRID: wr.WRID,
			DMA: rxeDMAInfo{
				Length: length,
				Resid:  length,
				NumSGE: uint32(len(wr.SGList)),
				SGL:    wr.SGList,
			},
		}

		return w.encode(slot)
	})
	if err != nil && posted < len(wrs) {
		err = fmt.Errorf("rxe: post receive work request %d: %w", wrs[posted].WRID, err)
	}

	return err
}

// userBytes returns the n bytes at virtual address addr, which an SGE
// refers to.
func userBytes(addr uint64, n uint32) []byte {
	if n == 0 {
		return nil
	}

	return unsafe.Slice((*byte)(unsafe.Add(nil, uintptr(addr))), n)
}
