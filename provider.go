//go:build linux

package rdma

import "errors"

// ErrNotSupported is returned by a Provider for an operation its device does
// not support.
var ErrNotSupported = errors.New("rdma: operation not supported by provider")

// A Provider implements the verbs of one kind of device for one opened
// Device, like an rdma-core provider library and its struct
// verbs_context_ops. Open calls Init with the Device, and the Device's verbs
// call the matching method after checking that the objects passed in belong
// to the Device. A Provider serves a single Device.
//
// Providers implement every method, using the Device's Cmd for the generic
// kernel commands, and attach their own state to objects with
// SetProviderData.
type Provider interface {
	// Init is called by Open with the newly opened Device. It creates the
	// kernel context with Cmd.GetContext and returns the Context, like the
	// alloc_context of an rdma-core provider.
	Init(dev *Device) (*Context, error)

	// DriverID returns the kernel driver the provider supports.
	DriverID() DriverID

	AllocPD() (*PD, error)
	DeallocPD(pd *PD) error
	RegMR(pd *PD, addr, length uint64, access AccessFlags) (*MR, error)
	DeregMR(mr *MR) error

	CreateCQ(cqe uint32, channel *CompChannel, compVector uint32) (*CQ, error)
	DestroyCQ(cq *CQ) error
	// PollCQ must not block.
	PollCQ(cq *CQ, wc []WC) (int, error)
	ReqNotifyCQ(cq *CQ, solicitedOnly bool) error

	CreateQP(pd *PD, attr *QPInitAttr) (*QP, error)
	ModifyQP(qp *QP, req *ModifyQP) error
	QueryQP(qp *QP, mask QPAttrMask) (*QueryQPResp, error)
	DestroyQP(qp *QP) error
	PostSend(qp *QP, wr ...SendWR) error
	PostRecv(qp *QP, wr ...RecvWR) error

	QueryDevice() (*QueryDeviceResp, error)
	QueryPort(port uint8) (*QueryPortRespEx, error)

	// Close releases the provider's resources when the Device is closed.
	Close() error
}

// A Context holds what the core keeps of the kernel context (struct
// ib_ucontext) a Provider's Init creates, like the fields of struct
// ibv_context that ibv_cmd_get_context fills in.
type Context struct {
	NumCompVectors uint32
}

// providerData holds a provider's own state for a PD, MR, CQ or QP, like
// the provider structure that embeds struct ibv_cq or struct ibv_qp in C.
// The objects embed it.
type providerData struct {
	v any
}

// SetProviderData attaches the provider's state to an object it created. It
// is for providers; applications must not call it.
func (p *providerData) SetProviderData(v any) { p.v = v }

// ProviderData returns the state the provider attached with
// SetProviderData, and whether it has type T.
func (p *providerData) ProviderData[T any]() (T, bool) {
	v, ok := p.v.(T)
	return v, ok
}
