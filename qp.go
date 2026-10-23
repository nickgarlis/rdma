//go:build linux

package rdma

import (
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

// A QPType is the transport type of a queue pair.
type QPType uint8

// QP types, from enum ib_uverbs_qp_type.
const (
	QPTypeRC QPType = unix.IB_UVERBS_QPT_RC
	QPTypeUC QPType = unix.IB_UVERBS_QPT_UC
	QPTypeUD QPType = unix.IB_UVERBS_QPT_UD
)

// A QPState is the state of a queue pair.
type QPState uint8

// QP states, from enum ib_qp_state.
const (
	QPStateReset QPState = iota
	QPStateInit
	QPStateRTR
	QPStateRTS
	QPStateSQD
	QPStateSQE
	QPStateError
)

var qpStateNames = [...]string{"RESET", "INIT", "RTR", "RTS", "SQD", "SQE", "ERR"}

func (s QPState) String() string {
	if int(s) < len(qpStateNames) {
		return qpStateNames[s]
	}

	return fmt.Sprintf("QPState(%d)", uint8(s))
}

// An MTU is an InfiniBand path MTU.
type MTU uint8

// MTU values, from enum ib_mtu.
const (
	MTU256 MTU = iota + 1
	MTU512
	MTU1024
	MTU2048
	MTU4096
)

// Bytes returns the MTU in bytes, or 0 if m is not a valid MTU.
func (m MTU) Bytes() int {
	if m < MTU256 || m > MTU4096 {
		return 0
	}

	return 128 << m
}

// QPAttrMask selects the attributes of a QP that Device.ModifyQP applies or
// Device.QueryQP returns (the attr_mask of the kernel structures).
type QPAttrMask uint32

// QP attribute mask bits, from enum ibv_qp_attr_mask.
const (
	QPAttrState            QPAttrMask = 1 << 0
	QPAttrCurState         QPAttrMask = 1 << 1
	QPAttrEnSQDAsyncNotify QPAttrMask = 1 << 2
	QPAttrAccessFlags      QPAttrMask = 1 << 3
	QPAttrPKeyIndex        QPAttrMask = 1 << 4
	QPAttrPort             QPAttrMask = 1 << 5
	QPAttrQKey             QPAttrMask = 1 << 6
	QPAttrAV               QPAttrMask = 1 << 7
	QPAttrPathMTU          QPAttrMask = 1 << 8
	QPAttrTimeout          QPAttrMask = 1 << 9
	QPAttrRetryCount       QPAttrMask = 1 << 10
	QPAttrRNRRetry         QPAttrMask = 1 << 11
	QPAttrRQPSN            QPAttrMask = 1 << 12
	QPAttrMaxQPRDAtomic    QPAttrMask = 1 << 13
	QPAttrAltPath          QPAttrMask = 1 << 14
	QPAttrMinRNRTimer      QPAttrMask = 1 << 15
	QPAttrSQPSN            QPAttrMask = 1 << 16
	QPAttrMaxDestRDAtomic  QPAttrMask = 1 << 17
	QPAttrPathMigState     QPAttrMask = 1 << 18
	QPAttrCap              QPAttrMask = 1 << 19
	QPAttrDestQPN          QPAttrMask = 1 << 20
)

// QPInitAttr holds the attributes of a new QP, like struct ibv_qp_init_attr.
type QPInitAttr struct {
	// SendCQ and RecvCQ receive the completions of the send and receive
	// queues. They may be the same CQ.
	SendCQ *CQ
	RecvCQ *CQ

	// Cap holds the requested queue capacities. The device may round them
	// up; CreateQP writes the actual values back.
	Cap QPCap

	QPType QPType

	// SQSigAll generates a completion for every send work request, not
	// just those with SendSignaled.
	SQSigAll bool
}

// A QP is a queue pair.
type QP struct {
	// PD is the protection domain the QP belongs to.
	PD *PD

	// Handle is the kernel handle of the QP.
	Handle uint32

	// QPNum is the QP number peers address the QP by.
	QPNum uint32

	// QPType is the transport type of the QP.
	QPType QPType

	// Cap holds the actual capacities of the QP's queues, which may be more
	// than were asked for.
	Cap QPCap

	// SendCQ and RecvCQ receive the completions of the send and receive
	// queues.
	SendCQ *CQ
	RecvCQ *CQ

	userHandle uint64

	providerData

	mu    sync.Mutex
	state QPState
}

// CreateQP creates a queue pair in pd, like ibv_create_qp. The QP starts in
// QPStateReset. The actual capacities, which may be more than requested, are
// written back to attr.Cap.
func (d *Device) CreateQP(pd *PD, attr *QPInitAttr) (*QP, error) {
	if attr.SendCQ == nil || attr.RecvCQ == nil {
		return nil, fmt.Errorf("rdma: create QP: send and receive CQs are required")
	}

	for _, dev := range []*Device{pd.Device, attr.SendCQ.Device, attr.RecvCQ.Device} {
		if err := d.owns(dev); err != nil {
			return nil, fmt.Errorf("rdma: create QP: %w", err)
		}
	}

	qp, err := d.provider.CreateQP(pd, attr)
	if err != nil {
		return nil, err
	}

	d.registerQP(qp)
	return qp, nil
}

// State returns the state the QP was last moved to with Device.ModifyQP.
func (qp *QP) State() QPState {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	return qp.state
}

// ModifyQP changes the attributes of qp selected by req.AttrMask (see
// QPAttrMask), like ibv_modify_qp. It is used to move a QP through its
// states, from QPStateReset to QPStateRTS. req is sent to the kernel as is,
// except that its QP handle is set to qp's.
func (d *Device) ModifyQP(qp *QP, req *ModifyQP) error {
	if err := d.owns(qp.PD.Device); err != nil {
		return err
	}

	if err := d.provider.ModifyQP(qp, req); err != nil {
		return err
	}

	if QPAttrMask(req.AttrMask)&QPAttrState != 0 {
		qp.mu.Lock()
		qp.state = QPState(req.QPState)
		qp.mu.Unlock()
	}

	return nil
}

// QueryQP returns the attributes of qp selected by mask. The kernel may
// return more than was asked for.
func (d *Device) QueryQP(qp *QP, mask QPAttrMask) (*QueryQPResp, error) {
	if err := d.owns(qp.PD.Device); err != nil {
		return nil, err
	}

	return d.provider.QueryQP(qp, mask)
}

// PostSend posts work requests to qp's send queue.
func (d *Device) PostSend(qp *QP, wr ...SendWR) error {
	if err := d.owns(qp.PD.Device); err != nil {
		return err
	}

	return d.provider.PostSend(qp, wr...)
}

// PostRecv posts work requests to qp's receive queue.
func (d *Device) PostRecv(qp *QP, wr ...RecvWR) error {
	if err := d.owns(qp.PD.Device); err != nil {
		return err
	}

	return d.provider.PostRecv(qp, wr...)
}

// DestroyQP destroys qp. qp must not be used afterwards: the kernel reuses
// its handle for new objects.
func (d *Device) DestroyQP(qp *QP) error {
	if err := d.owns(qp.PD.Device); err != nil {
		return err
	}

	if err := d.provider.DestroyQP(qp); err != nil {
		return err
	}

	d.unregisterQP(qp.userHandle)
	return nil
}
