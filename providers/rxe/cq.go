//go:build linux

package rxe

import (
	"encoding/binary"
	"fmt"

	"github.com/nickgarlis/rdma"
)

func (p *provider) CreateCQ(cqe uint32, channel *rdma.CompChannel, compVector uint32) (*rdma.CQ, error) {
	var resp rxeCreateCQResp
	out := make([]byte, binary.Size(resp))
	cq, err := p.dev.Cmd().CreateCQ(cqe, channel, compVector, &rdma.UData{Out: out})
	if err != nil {
		return nil, err
	}

	var q *queue
	if _, err = binary.Decode(out, binary.NativeEndian, &resp); err != nil {
		err = fmt.Errorf("rxe: decoding create CQ response: %w", err)
	} else {
		q, err = mapQueue(p.dev, resp.CQ)
	}

	if err == nil && q.elemSize() < rxeCQESize {
		_ = q.close()
		err = fmt.Errorf("rxe: CQ element size %d is smaller than a work completion", q.elemSize())
	}

	if err != nil {
		_ = p.dev.Cmd().DestroyCQ(cq)
		return nil, err
	}

	if err := p.addCQ(q); err != nil {
		_ = q.close()
		_ = p.dev.Cmd().DestroyCQ(cq)
		return nil, err
	}

	cq.SetProviderData(q)
	return cq, nil
}

func (p *provider) DestroyCQ(cq *rdma.CQ) error {
	q, err := cqOf(cq)
	if err != nil {
		return err
	}

	// Holding the ring's lock makes a concurrent poll wait, and the closed
	// ring makes later polls fail.
	if err := q.lock(); err != nil {
		return err
	}
	defer q.unlock()

	if err := p.dev.Cmd().DestroyCQ(cq); err != nil {
		return err
	}

	p.removeCQ(q)
	return q.closeLocked()
}

func (p *provider) PollCQ(cq *rdma.CQ, wc []rdma.WC) (int, error) {
	q, err := cqOf(cq)
	if err != nil {
		return 0, err
	}

	return pollCQ(q, wc)
}

// cqOf returns the ring of cq, the provider state of a CQ.
func cqOf(cq *rdma.CQ) (*queue, error) {
	q, ok := cq.ProviderData[*queue]()
	if !ok || q == nil {
		return nil, errForeignCQ
	}

	return q, nil
}

// pollCQ consumes the ib_uverbs_wc entries the kernel produced into the CQ
// ring q.
func pollCQ(q *queue, wc []rdma.WC) (int, error) {
	if err := q.lock(); err != nil {
		return 0, err
	}
	defer q.unlock()

	cons := q.consumer()
	prod := q.producer()

	n := 0
	for ; n < len(wc) && cons != prod; n++ {
		var cqe rxeCQE
		if err := cqe.decode(q.elem(cons)); err != nil {
			return n, err
		}

		wc[n] = cqe.wc()

		cons = (cons + 1) & q.mask
		q.setConsumer(cons)
	}

	return n, nil
}
