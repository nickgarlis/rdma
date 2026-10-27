//go:build linux

// Package rxe is the provider for rxe, the Linux soft-RoCE driver
// (rdma_rxe), which implements RoCEv2 in software on top of any Ethernet
// interface.
//
// rxe shares its work and completion queues with the kernel through memory
// mapped from the uverbs device. Posting a receive or polling a CQ only
// touches that memory; posting a send also rings a doorbell ioctl so the
// kernel starts processing the send queue.
//
// The provider supports RC and UC queue pairs with send, send with
// immediate, RDMA write (with immediate), RDMA read and atomic work requests,
// including inline data.
package rxe

import (
	"errors"
	"sync"

	"github.com/nickgarlis/rdma"
)

// Open opens the rxe device with the given name, such as "rxe0".
func Open(name string) (*rdma.Device, error) {
	return rdma.Open(name, &provider{})
}

// provider implements rdma.Provider for one rxe device. Most control verbs
// are the generic kernel commands, like in rdma-core's rxe provider. Arming
// a CQ is one, and so is modifying a QP: the kernel resets the shared queues
// itself when a QP moves to RESET.
type provider struct {
	dev *rdma.Device

	// cqs and qps hold the live CQs and QPs, whose rings Close unmaps.
	mu  sync.Mutex
	cqs map[*cqState]struct{}
	qps map[*queuePair]struct{}
}

func (p *provider) Init(dev *rdma.Device) (*rdma.Context, error) {
	if p.dev != nil {
		return nil, errProviderInUse
	}

	p.dev = dev
	p.cqs = make(map[*cqState]struct{})
	p.qps = make(map[*queuePair]struct{})

	// rxe takes no driver data for its context.
	return dev.Cmd().GetContext(nil)
}

func (*provider) DriverID() rdma.DriverID { return rdma.DriverRXE }

// Close unmaps the rings of the CQs and QPs that are still open. The Device
// calls it before closing the device file, which would unmap them under any
// poll or post still running.
func (p *provider) Close() error {
	p.mu.Lock()
	cqs, qps := p.cqs, p.qps
	p.cqs, p.qps = nil, nil
	p.mu.Unlock()

	var errs []error
	for c := range cqs {
		errs = append(errs, c.close())
	}

	for q := range qps {
		errs = append(errs, q.close())
	}

	return errors.Join(errs...)
}

// addCQ and addQP record a new CQ or QP, so that Close unmaps its rings. They
// fail once Close has started, since it would not see the object.
func (p *provider) addCQ(c *cqState) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cqs == nil {
		return errProviderClosed
	}

	p.cqs[c] = struct{}{}
	return nil
}

func (p *provider) addQP(q *queuePair) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.qps == nil {
		return errProviderClosed
	}

	p.qps[q] = struct{}{}
	return nil
}

// removeCQ and removeQP forget a destroyed CQ or QP.
func (p *provider) removeCQ(c *cqState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.cqs, c)
}

func (p *provider) removeQP(q *queuePair) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.qps, q)
}

func (p *provider) AllocPD() (*rdma.PD, error)  { return p.dev.Cmd().AllocPD() }
func (p *provider) DeallocPD(pd *rdma.PD) error { return p.dev.Cmd().DeallocPD(pd) }
func (p *provider) DeregMR(mr *rdma.MR) error   { return p.dev.Cmd().DeregMR(mr) }

func (p *provider) RegMR(pd *rdma.PD, mem rdma.MemoryRange, access rdma.AccessFlags) (*rdma.MR, error) {
	return p.dev.Cmd().RegMR(pd, mem, access)
}

func (p *provider) ReqNotifyCQ(cq *rdma.CQ, solicitedOnly bool) error {
	return p.dev.Cmd().ReqNotifyCQ(cq, solicitedOnly)
}

func (p *provider) ModifyQP(qp *rdma.QP, req *rdma.ModifyQP) error {
	return p.dev.Cmd().ModifyQP(qp, req)
}

func (p *provider) QueryQP(qp *rdma.QP, mask rdma.QPAttrMask) (*rdma.QueryQPResp, error) {
	return p.dev.Cmd().QueryQP(qp, mask)
}

func (p *provider) QueryDevice() (*rdma.QueryDeviceResp, error) { return p.dev.Cmd().QueryDevice() }

func (p *provider) QueryPort(port uint8) (*rdma.QueryPortRespEx, error) {
	return p.dev.Cmd().QueryPort(port)
}
