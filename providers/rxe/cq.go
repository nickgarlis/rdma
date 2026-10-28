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

	// The CQ's ring is its provider state.
	var q *queue
	if _, err = binary.Decode(out, binary.NativeEndian, &resp); err != nil {
		err = fmt.Errorf("rxe: decoding create CQ response: %w", err)
	} else if q, err = newQueue(p.dev, resp.CQ); err == nil && q.elemSize() < rxeCQESize {
		_ = q.close()
		err = fmt.Errorf("rxe: CQ element size %d is smaller than a work completion", q.elemSize())
	}

	if err != nil {
		_ = p.dev.Cmd().DestroyCQ(cq)
		return nil, err
	}

	cq.SetProviderData(q)
	return cq, nil
}

func (p *provider) DestroyCQ(cq *rdma.CQ) error {
	q, ok := cq.ProviderData[*queue]()
	if !ok || q == nil {
		return errForeignCQ
	}

	if err := p.dev.Cmd().DestroyCQ(cq); err != nil {
		return err
	}

	// Closing waits for a running poll, and later polls fail.
	return q.close()
}

func (p *provider) PollCQ(cq *rdma.CQ, wc []rdma.WC) (int, error) {
	q, ok := cq.ProviderData[*queue]()
	if !ok || q == nil {
		return 0, errForeignCQ
	}

	return q.consume(len(wc), func(i int, slot []byte) error {
		var cqe rxeCQE
		if err := cqe.decode(slot); err != nil {
			return err
		}

		wc[i] = cqe.wc()
		return nil
	})
}
