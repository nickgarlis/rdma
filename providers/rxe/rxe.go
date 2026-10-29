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
}

func (p *provider) Init(dev *rdma.Device) (*rdma.Context, error) {
	if p.dev != nil {
		return nil, errProviderInUse
	}

	p.dev = dev

	// rxe takes no driver data for its context.
	return dev.Cmd().GetContext(nil)
}

func (*provider) DriverID() rdma.DriverID { return rdma.DriverRXE }

// Close has nothing to release: closing the device file unmaps the rings of
// the CQs and QPs still open, after any running poll or post returns.
func (*provider) Close() error { return nil }

func (p *provider) AllocPD() (*rdma.PD, error)  { return p.dev.Cmd().AllocPD() }
func (p *provider) DeallocPD(pd *rdma.PD) error { return p.dev.Cmd().DeallocPD(pd) }
func (p *provider) DeregMR(mr *rdma.MR) error   { return p.dev.Cmd().DeregMR(mr) }

func (p *provider) RegMR(pd *rdma.PD, addr, length uint64, access rdma.AccessFlags) (*rdma.MR, error) {
	return p.dev.Cmd().RegMR(pd, addr, length, access)
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
