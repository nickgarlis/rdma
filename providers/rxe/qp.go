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

	var rq, sq *queue
	if _, err = binary.Decode(out, binary.NativeEndian, &resp); err != nil {
		err = fmt.Errorf("rxe: decoding create QP response: %w", err)
	} else if rq, err = mapQueue(p.dev, resp.RQ); err == nil {
		if sq, err = mapQueue(p.dev, resp.SQ); err != nil {
			_ = rq.close()
		}
	}

	if err != nil {
		_ = p.dev.Cmd().DestroyQP(qp)
		return nil, err
	}

	q := &queuePair{dev: p.dev, qp: qp, cap: qp.Cap, sq: sq, rq: rq}
	if err := p.addQP(q); err != nil {
		_ = q.close()
		_ = p.dev.Cmd().DestroyQP(qp)
		return nil, err
	}

	qp.SetProviderData(q)
	return qp, nil
}

func (p *provider) DestroyQP(qp *rdma.QP) error {
	q, err := qpOf(qp)
	if err != nil {
		return err
	}

	// Holding the rings' locks makes concurrent posts wait, and the closed
	// rings make later posts fail. The send queue's lock comes first.
	if err := q.sq.lock(); err != nil {
		return err
	}
	defer q.sq.unlock()

	if err := q.rq.lock(); err != nil {
		return err
	}
	defer q.rq.unlock()

	if err := p.dev.Cmd().DestroyQP(qp); err != nil {
		return err
	}

	p.removeQP(q)
	return errors.Join(q.sq.closeLocked(), q.rq.closeLocked())
}

func (p *provider) PostSend(qp *rdma.QP, wr ...rdma.SendWR) error {
	q, err := qpOf(qp)
	if err != nil {
		return err
	}

	return q.postSend(wr)
}

func (p *provider) PostRecv(qp *rdma.QP, wr ...rdma.RecvWR) error {
	q, err := qpOf(qp)
	if err != nil {
		return err
	}

	return q.postRecv(wr)
}

// qpOf returns the rxe state of qp.
func qpOf(qp *rdma.QP) (*queuePair, error) {
	q, ok := qp.ProviderData[*queuePair]()
	if !ok || q == nil {
		return nil, errForeignQP
	}

	return q, nil
}

// queuePair is the provider state of a QP: its mapped send and receive
// queues.
type queuePair struct {
	dev *rdma.Device
	qp  *rdma.QP
	cap rdma.QPCap

	sq *queue
	rq *queue
}

func (p *queuePair) postSend(wrs []rdma.SendWR) error {
	if err := p.sq.lock(); err != nil {
		return err
	}

	var (
		err    error
		posted int
	)

	for i := range wrs {
		if err = p.postOneSend(&wrs[i]); err != nil {
			err = fmt.Errorf("rxe: post send work request %d: %w", wrs[i].WRID, err)
			break
		}

		posted++
	}

	p.sq.unlock()

	if posted > 0 {
		if derr := p.ringSendDoorbell(); derr != nil {
			return errors.Join(err, derr)
		}
	}

	return err
}

func (p *queuePair) postOneSend(wr *rdma.SendWR) error {
	q := p.sq
	prod := q.producer()
	if q.full(prod) {
		return errQueueFull
	}

	if uint32(len(wr.SGList)) > p.cap.MaxSendSGE {
		return fmt.Errorf("%d SGEs exceeds maximum of %d", len(wr.SGList), p.cap.MaxSendSGE)
	}

	var length uint64
	for _, s := range wr.SGList {
		length += uint64(s.Length)
	}

	inline := wr.SendFlags&rdma.SendInline != 0
	if inline && length > uint64(p.cap.MaxInlineData) {
		return fmt.Errorf("%d bytes of inline data exceeds maximum of %d", length, p.cap.MaxInlineData)
	}

	isAtomic := wr.Opcode == rdma.WRAtomicCmpAndSwp || wr.Opcode == rdma.WRAtomicFetchAndAdd
	if isAtomic && (length < 8 || wr.RemoteAddr&7 != 0) {
		return errAtomicAlignment
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

	if err := w.encode(q.elem(prod)); err != nil {
		return err
	}

	q.setProducer((prod + 1) & q.mask)
	return nil
}

// ringSendDoorbell tells the kernel there is new work on the send queue
// with an empty POST_SEND command.
func (p *queuePair) ringSendDoorbell() error {
	// struct ib_uverbs_post_send with no work requests.
	in := make([]byte, 24)
	ne := binary.NativeEndian
	ne.PutUint32(in[8:], p.qp.Handle)
	ne.PutUint32(in[20:], sizeofUverbsSendWR)

	// struct ib_uverbs_post_send_resp.
	var badWR uint32
	if err := p.dev.Conn().ExecuteWrite(unix.IB_USER_VERBS_CMD_POST_SEND, in, &badWR); err != nil {
		return fmt.Errorf("rxe: ring send doorbell: %w", err)
	}

	return nil
}

func (p *queuePair) postRecv(wrs []rdma.RecvWR) error {
	if p.qp.State() == rdma.QPStateReset {
		return errQPReset
	}

	q := p.rq
	if err := q.lock(); err != nil {
		return err
	}
	defer q.unlock()

	for i := range wrs {
		wr := &wrs[i]

		prod := q.producer()
		if q.full(prod) {
			return fmt.Errorf("rxe: post receive work request %d: %w", wr.WRID, errQueueFull)
		}

		if uint32(len(wr.SGList)) > p.cap.MaxRecvSGE {
			return fmt.Errorf("rxe: post receive work request %d: %d SGEs exceeds maximum of %d",
				wr.WRID, len(wr.SGList), p.cap.MaxRecvSGE)
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

		if err := w.encode(q.elem(prod)); err != nil {
			return fmt.Errorf("rxe: post receive work request %d: %w", wr.WRID, err)
		}

		q.setProducer((prod + 1) & q.mask)
	}

	return nil
}

// close unmaps the QP's rings.
func (p *queuePair) close() error {
	return errors.Join(p.sq.close(), p.rq.close())
}

// userBytes returns the n bytes at virtual address addr, which an SGE
// refers to.
func userBytes(addr uint64, n uint32) []byte {
	if n == 0 {
		return nil
	}

	return unsafe.Slice((*byte)(unsafe.Add(nil, uintptr(addr))), n)
}
