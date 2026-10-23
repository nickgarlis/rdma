//go:build linux

package rxe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

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

	cq.SetProviderData(&cqState{q: q})
	return cq, nil
}

func (p *provider) DestroyCQ(cq *rdma.CQ) error {
	c, err := cqOf(cq)
	if err != nil {
		return err
	}

	if err := p.dev.Cmd().DestroyCQ(cq); err != nil {
		return err
	}

	return c.q.close()
}

func (p *provider) PollCQ(cq *rdma.CQ, wc []rdma.WC) (int, error) {
	c, err := cqOf(cq)
	if err != nil {
		return 0, err
	}

	return c.poll(wc)
}

var errForeignCQ = errors.New("rxe: CQ has no rxe state")

// cqOf returns the rxe state of cq.
func cqOf(cq *rdma.CQ) (*cqState, error) {
	c, ok := cq.ProviderData[*cqState]()
	if !ok || c == nil {
		return nil, errForeignCQ
	}

	return c, nil
}

// cqState is the provider state of a CQ. The kernel produces ib_uverbs_wc
// entries and poll consumes them.
type cqState struct {
	mu sync.Mutex
	q  *queue
}

func (c *cqState) poll(wc []rdma.WC) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.q
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
