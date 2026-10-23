//go:build linux

package rdma

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/nickgarlis/rdma/uverbs"
	"golang.org/x/sys/unix"
)

// The Cmd methods run the generic kernel commands of the verbs, like the
// ibv_cmd_* functions of <infiniband/driver.h>. Providers use them to
// implement their verbs; applications use the Device's verbs.
//
// Methods that create objects also take a UData with the provider's private
// data for its kernel driver, like the cmd and resp arguments in C. If a
// provider fails after a create method succeeded, it destroys the object
// with the matching destroy method.
//
// The methods only talk to the kernel and return what it reports. They do
// not change the Device's state: the Device's verbs do that.
type Cmd struct {
	d *Device
}

// Cmd returns the kernel commands of the Device, for its Provider.
func (d *Device) Cmd() Cmd { return Cmd{d: d} }

// ne is the byte order of numbers and structures in attributes.
var ne = binary.NativeEndian

// UData is a provider's private part of a kernel command, the Go form of the
// kernel's struct ib_udata: In, its request to the kernel driver, sent as
// unix.UVERBS_ATTR_UHW_IN, and Out, room for the driver's response,
// unix.UVERBS_ATTR_UHW_OUT, which the kernel writes directly into it. Either
// may be nil. The provider encodes and decodes its driver's structures, such
// as struct rxe_create_cq_resp from <rdma/rdma_user_rxe.h>, and sizes Out for
// its own version of the response: the kernel writes at most len(Out) bytes,
// so older and newer structure versions work together.
type UData struct {
	In  []byte
	Out []byte
}

// lastUserHandle is the last user handle given to a CQ or QP. The kernel
// reports it back in the object's events, and the Device registers the
// object under it, so it only has to be unique, like the object pointers rdma-core
// uses.
var lastUserHandle atomic.Uint64

// GetContext creates the Device's kernel context (struct ib_ucontext) with
// unix.UVERBS_METHOD_GET_CONTEXT, like ibv_cmd_get_context. A Provider's
// Init calls it exactly once, with its driver data if its kernel driver
// takes any, may then map device pages the driver's response announces, and
// returns the Context.
func (c Cmd) GetContext(drv *UData) (*Context, error) {
	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_GET_CONTEXT,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_GET_CONTEXT_NUM_COMP_VECTORS, Type: uverbs.AttributeOut, Data: make([]byte, 4)},
		},
	}

	n := len(cmd.Attrs)
	appendUData(cmd, drv)

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: create context: %w", err)
	}

	if err := checkUDataResp(cmd.Attrs[n:], drv); err != nil {
		return nil, fmt.Errorf("rdma: create context: %w", err)
	}

	return &Context{NumCompVectors: ne.Uint32(cmd.Attrs[0].Data)}, nil
}

// appendUData appends drv's request and response buffer to cmd.
func appendUData(cmd *uverbs.Command, drv *UData) {
	if drv == nil {
		return
	}

	if drv.In != nil {
		cmd.Attrs = append(cmd.Attrs, uverbs.Attribute{ID: unix.UVERBS_ATTR_UHW_IN, Data: drv.In})
	}

	if drv.Out != nil {
		cmd.Attrs = append(cmd.Attrs, uverbs.Attribute{ID: unix.UVERBS_ATTR_UHW_OUT, Type: uverbs.AttributeOut, Data: drv.Out})
	}
}

// checkUDataResp reports an error if drv asked for a response that the
// kernel did not write. attrs are the driver attributes of the executed
// command.
func checkUDataResp(attrs []uverbs.Attribute, drv *UData) error {
	if drv == nil || drv.Out == nil {
		return nil
	}

	for _, a := range attrs {
		if a.ID == unix.UVERBS_ATTR_UHW_OUT && a.Flags&uverbs.FlagValidOutput != 0 {
			return nil
		}
	}

	return errors.New("kernel returned no driver response")
}

// CreateCQ creates a CQ with unix.UVERBS_METHOD_CQ_CREATE. The returned CQ
// has its Handle and actual CQE count.
func (c Cmd) CreateCQ(cqe uint32, channel *CompChannel, compVector uint32, drv *UData) (*CQ, error) {
	cq := &CQ{Device: c.d, Channel: channel, userHandle: lastUserHandle.Add(1)}

	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_CQ,
		Method: unix.UVERBS_METHOD_CQ_CREATE,
		Attrs: []uverbs.Attribute{
			// Outputs first: they are read by index after the call.
			{ID: unix.UVERBS_ATTR_CREATE_CQ_HANDLE, Type: uverbs.AttributeNewObject},
			{ID: unix.UVERBS_ATTR_CREATE_CQ_RESP_CQE, Type: uverbs.AttributeOut, Data: make([]byte, 4)},
			{ID: unix.UVERBS_ATTR_CREATE_CQ_CQE, Data: ne.AppendUint32(nil, cqe)},
			{ID: unix.UVERBS_ATTR_CREATE_CQ_USER_HANDLE, Data: ne.AppendUint64(nil, cq.userHandle)},
			{ID: unix.UVERBS_ATTR_CREATE_CQ_COMP_VECTOR, Data: ne.AppendUint32(nil, compVector)},
		},
	}

	if channel != nil {
		cmd.Attrs = append(cmd.Attrs, uverbs.Attribute{
			ID: unix.UVERBS_ATTR_CREATE_CQ_COMP_CHANNEL, Type: uverbs.AttributeObject, Value: int64(channel.FD),
		})
	}

	n := len(cmd.Attrs)
	appendUData(cmd, drv)

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: create CQ: %w", err)
	}

	cq.Handle = uint32(cmd.Attrs[0].Value)
	cq.CQE = ne.Uint32(cmd.Attrs[1].Data)

	if err := checkUDataResp(cmd.Attrs[n:], drv); err != nil {
		_ = c.DestroyCQ(cq)
		return nil, fmt.Errorf("rdma: create CQ: %w", err)
	}

	return cq, nil
}

// DestroyCQ destroys cq with unix.UVERBS_METHOD_CQ_DESTROY.
func (c Cmd) DestroyCQ(cq *CQ) error {
	if err := c.d.conn.Execute(&uverbs.Command{
		Object: unix.UVERBS_OBJECT_CQ,
		Method: unix.UVERBS_METHOD_CQ_DESTROY,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_DESTROY_CQ_HANDLE, Type: uverbs.AttributeObject, Value: int64(cq.Handle)},
			// struct ib_uverbs_destroy_cq_resp
			{ID: unix.UVERBS_ATTR_DESTROY_CQ_RESP, Type: uverbs.AttributeOut, Data: make([]byte, 8)},
		},
	}); err != nil {
		return fmt.Errorf("rdma: destroy CQ: %w", err)
	}

	return nil
}

// ReqNotifyCQ requests a notification for cq with the write command
// REQ_NOTIFY_CQ, for drivers whose CQs are armed by the kernel.
func (c Cmd) ReqNotifyCQ(cq *CQ, solicitedOnly bool) error {
	req := reqNotifyCQ{CQHandle: cq.Handle}
	if solicitedOnly {
		req.SolicitedOnly = 1
	}

	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_REQ_NOTIFY_CQ, &req, nil); err != nil {
		return fmt.Errorf("rdma: request CQ notification: %w", err)
	}

	return nil
}

// CreateQP creates a QP in pd with unix.UVERBS_METHOD_QP_CREATE. The
// returned QP has its Handle, QPNum and actual Cap, which is also written
// back to attr.Cap, like ibv_cmd_create_qp does.
func (c Cmd) CreateQP(pd *PD, attr *QPInitAttr, drv *UData) (*QP, error) {
	caps, err := binary.Append(nil, ne, attr.Cap)
	if err != nil {
		return nil, fmt.Errorf("rdma: create QP: %w", err)
	}

	qp := &QP{
		PD:         pd,
		QPType:     attr.QPType,
		SendCQ:     attr.SendCQ,
		RecvCQ:     attr.RecvCQ,
		userHandle: lastUserHandle.Add(1),
	}

	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_QP,
		Method: unix.UVERBS_METHOD_QP_CREATE,
		Attrs: []uverbs.Attribute{
			// Outputs first: they are read by index after the call.
			{ID: unix.UVERBS_ATTR_CREATE_QP_HANDLE, Type: uverbs.AttributeNewObject},
			{ID: unix.UVERBS_ATTR_CREATE_QP_RESP_QP_NUM, Type: uverbs.AttributeOut, Data: make([]byte, 4)},
			{ID: unix.UVERBS_ATTR_CREATE_QP_RESP_CAP, Type: uverbs.AttributeOut, Data: make([]byte, len(caps))},
			{ID: unix.UVERBS_ATTR_CREATE_QP_PD_HANDLE, Type: uverbs.AttributeObject, Value: int64(pd.Handle)},
			{ID: unix.UVERBS_ATTR_CREATE_QP_SEND_CQ_HANDLE, Type: uverbs.AttributeObject, Value: int64(attr.SendCQ.Handle)},
			{ID: unix.UVERBS_ATTR_CREATE_QP_RECV_CQ_HANDLE, Type: uverbs.AttributeObject, Value: int64(attr.RecvCQ.Handle)},
			{ID: unix.UVERBS_ATTR_CREATE_QP_USER_HANDLE, Data: ne.AppendUint64(nil, qp.userHandle)},
			{ID: unix.UVERBS_ATTR_CREATE_QP_CAP, Data: caps},
			// UVERBS_ATTR_CONST_IN must be 8 bytes.
			{ID: unix.UVERBS_ATTR_CREATE_QP_TYPE, Data: ne.AppendUint64(nil, uint64(attr.QPType))},
		},
	}

	if attr.SQSigAll {
		cmd.Attrs = append(cmd.Attrs, uverbs.Attribute{
			ID: unix.UVERBS_ATTR_CREATE_QP_FLAGS, Data: ne.AppendUint32(nil, unix.IB_UVERBS_QP_CREATE_SQ_SIG_ALL),
		})
	}

	n := len(cmd.Attrs)
	appendUData(cmd, drv)

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: create QP: %w", err)
	}

	qp.Handle = uint32(cmd.Attrs[0].Value)
	qp.QPNum = ne.Uint32(cmd.Attrs[1].Data)
	// The buffer was sized for QPCap, so decoding cannot fail.
	_, _ = binary.Decode(cmd.Attrs[2].Data, ne, &qp.Cap)
	attr.Cap = qp.Cap

	if err := checkUDataResp(cmd.Attrs[n:], drv); err != nil {
		_ = c.DestroyQP(qp)
		return nil, fmt.Errorf("rdma: create QP: %w", err)
	}

	return qp, nil
}

// ModifyQP modifies qp with the write command MODIFY_QP. req is sent as is,
// except that its QP handle is set to qp's.
func (c Cmd) ModifyQP(qp *QP, req *ModifyQP) error {
	r := *req
	r.QPHandle = qp.Handle

	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_MODIFY_QP, &r, nil); err != nil {
		return fmt.Errorf("rdma: modify QP: %w", err)
	}

	return nil
}

// DestroyQP destroys qp with unix.UVERBS_METHOD_QP_DESTROY.
func (c Cmd) DestroyQP(qp *QP) error {
	if err := c.d.conn.Execute(&uverbs.Command{
		Object: unix.UVERBS_OBJECT_QP,
		Method: unix.UVERBS_METHOD_QP_DESTROY,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_DESTROY_QP_HANDLE, Type: uverbs.AttributeObject, Value: int64(qp.Handle)},
			// struct ib_uverbs_destroy_qp_resp
			{ID: unix.UVERBS_ATTR_DESTROY_QP_RESP, Type: uverbs.AttributeOut, Data: make([]byte, 4)},
		},
	}); err != nil {
		return fmt.Errorf("rdma: destroy QP: %w", err)
	}

	return nil
}

// AllocPD allocates a PD with the write command ALLOC_PD.
func (c Cmd) AllocPD() (*PD, error) {
	var resp allocPDResp
	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_ALLOC_PD, &allocPD{}, &resp); err != nil {
		return nil, fmt.Errorf("rdma: allocate PD: %w", err)
	}

	return &PD{Device: c.d, Handle: resp.PDHandle}, nil
}

// DeallocPD deallocates pd with unix.UVERBS_METHOD_PD_DESTROY.
func (c Cmd) DeallocPD(pd *PD) error {
	if err := c.d.conn.Execute(&uverbs.Command{
		Object: unix.UVERBS_OBJECT_PD,
		Method: unix.UVERBS_METHOD_PD_DESTROY,
		Attrs:  []uverbs.Attribute{{ID: unix.UVERBS_ATTR_DESTROY_PD_HANDLE, Type: uverbs.AttributeObject, Value: int64(pd.Handle)}},
	}); err != nil {
		return fmt.Errorf("rdma: deallocate PD: %w", err)
	}

	return nil
}

// RegMR registers mem as a memory region of pd with the write command
// REG_MR. The native UVERBS_METHOD_REG_MR does not record the MR's IOVA and
// length in the core (as of Linux 7.3), which breaks drivers such as rxe
// that check ranges against them.
func (c Cmd) RegMR(pd *PD, mem MemoryRange, access AccessFlags) (*MR, error) {
	addr, length := mem.Addr(), uint64(mem.Len())
	if length == 0 {
		return nil, fmt.Errorf("rdma: register MR: memory is empty or closed")
	}

	var resp regMRResp
	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_REG_MR, &regMR{
		Start:       addr,
		Length:      length,
		HCAVA:       addr,
		PDHandle:    pd.Handle,
		AccessFlags: uint32(access),
	}, &resp); err != nil {
		return nil, fmt.Errorf("rdma: register MR: %w", err)
	}

	return &MR{PD: pd, Handle: resp.MRHandle, LKey: resp.LKey, RKey: resp.RKey, Addr: addr, Length: length}, nil
}

// DeregMR deregisters mr with unix.UVERBS_METHOD_MR_DESTROY.
func (c Cmd) DeregMR(mr *MR) error {
	if err := c.d.conn.Execute(&uverbs.Command{
		Object: unix.UVERBS_OBJECT_MR,
		Method: unix.UVERBS_METHOD_MR_DESTROY,
		Attrs:  []uverbs.Attribute{{ID: unix.UVERBS_ATTR_DESTROY_MR_HANDLE, Type: uverbs.AttributeObject, Value: int64(mr.Handle)}},
	}); err != nil {
		return fmt.Errorf("rdma: deregister MR: %w", err)
	}

	return nil
}

// QueryQP queries qp's attributes selected by mask with the write command
// QUERY_QP.
func (c Cmd) QueryQP(qp *QP, mask QPAttrMask) (*QueryQPResp, error) {
	var r QueryQPResp
	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_QUERY_QP,
		&queryQP{QPHandle: qp.Handle, AttrMask: uint32(mask)}, &r); err != nil {
		return nil, fmt.Errorf("rdma: query QP: %w", err)
	}

	return &r, nil
}

// QueryDevice queries the device's attributes with the write command
// QUERY_DEVICE.
func (c Cmd) QueryDevice() (*QueryDeviceResp, error) {
	var resp QueryDeviceResp
	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_QUERY_DEVICE, &queryDevice{}, &resp); err != nil {
		return nil, fmt.Errorf("rdma: query device: %w", err)
	}

	return &resp, nil
}

// QueryPort queries a port's attributes with unix.UVERBS_METHOD_QUERY_PORT.
func (c Cmd) QueryPort(port uint8) (*QueryPortRespEx, error) {
	var r QueryPortRespEx
	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_QUERY_PORT,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_QUERY_PORT_RESP, Type: uverbs.AttributeOut, Data: make([]byte, binary.Size(r))},
			{ID: unix.UVERBS_ATTR_QUERY_PORT_PORT_NUM, Data: ne.AppendUint64(nil, uint64(port))},
		},
	}

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: query port %d: %w", port, err)
	}

	if _, err := binary.Decode(cmd.Attrs[0].Data, ne, &r); err != nil {
		return nil, fmt.Errorf("rdma: query port %d: %w", port, err)
	}

	return &r, nil
}

// The commands of the verbs the core implements itself, without a
// provider: they are the same for every device.

// allocAsyncFD allocates an asynchronous event file with
// unix.UVERBS_METHOD_ASYNC_EVENT_ALLOC and returns its file descriptor, like
// ibv_cmd_alloc_async_fd. It fails if the kernel context was not
// created.
func (c Cmd) allocAsyncFD() (int, error) {
	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_ASYNC_EVENT,
		Method: unix.UVERBS_METHOD_ASYNC_EVENT_ALLOC,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_ASYNC_EVENT_ALLOC_FD_HANDLE, Type: uverbs.AttributeNewObject},
		},
	}

	if err := c.d.conn.Execute(cmd); err != nil {
		return -1, fmt.Errorf("rdma: allocate async event file: %w", err)
	}

	return int(cmd.Attrs[0].Value), nil
}

// createCompChannel creates a completion channel with the write command
// CREATE_COMP_CHANNEL and returns its file descriptor.
func (c Cmd) createCompChannel() (int, error) {
	var resp createCompChannelResp
	if err := c.d.conn.ExecuteWrite(unix.IB_USER_VERBS_CMD_CREATE_COMP_CHANNEL, &createCompChannel{}, &resp); err != nil {
		return -1, fmt.Errorf("rdma: create completion channel: %w", err)
	}

	return int(resp.FD), nil
}

// queryGID queries the entry at index in the GID table of a port with
// unix.UVERBS_METHOD_QUERY_GID_ENTRY.
func (c Cmd) queryGID(port uint8, index uint32) (*GIDEntry, error) {
	var e GIDEntry
	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_QUERY_GID_ENTRY,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_QUERY_GID_ENTRY_RESP_ENTRY, Type: uverbs.AttributeOut, Data: make([]byte, binary.Size(e))},
			{ID: unix.UVERBS_ATTR_QUERY_GID_ENTRY_PORT, Data: ne.AppendUint64(nil, uint64(port))},
			{ID: unix.UVERBS_ATTR_QUERY_GID_ENTRY_GID_INDEX, Data: ne.AppendUint64(nil, uint64(index))},
			{ID: unix.UVERBS_ATTR_QUERY_GID_ENTRY_FLAGS, Data: ne.AppendUint32(nil, 0)},
		},
	}

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: query GID %d of port %d: %w", index, port, err)
	}

	if _, err := binary.Decode(cmd.Attrs[0].Data, ne, &e); err != nil {
		return nil, fmt.Errorf("rdma: query GID %d of port %d: %w", index, port, err)
	}

	return &e, nil
}

// queryGIDTable queries the valid entries of the GID tables of all ports
// with unix.UVERBS_METHOD_QUERY_GID_TABLE. maxEntries bounds the number of
// entries returned.
func (c Cmd) queryGIDTable(maxEntries int) ([]GIDEntry, error) {
	size := binary.Size(GIDEntry{})
	if maxEntries <= 0 || maxEntries*size > uverbs.MaxAttributeLen {
		return nil, fmt.Errorf("rdma: query GID table: invalid maximum of %d entries", maxEntries)
	}

	cmd := &uverbs.Command{
		Object: unix.UVERBS_OBJECT_DEVICE,
		Method: unix.UVERBS_METHOD_QUERY_GID_TABLE,
		Attrs: []uverbs.Attribute{
			{ID: unix.UVERBS_ATTR_QUERY_GID_TABLE_RESP_ENTRIES, Type: uverbs.AttributeOut, Data: make([]byte, maxEntries*size)},
			{ID: unix.UVERBS_ATTR_QUERY_GID_TABLE_RESP_NUM_ENTRIES, Type: uverbs.AttributeOut, Data: make([]byte, 8)},
			{ID: unix.UVERBS_ATTR_QUERY_GID_TABLE_ENTRY_SIZE, Data: ne.AppendUint64(nil, uint64(size))},
		},
	}

	if err := c.d.conn.Execute(cmd); err != nil {
		return nil, fmt.Errorf("rdma: query GID table: %w", err)
	}

	n := ne.Uint64(cmd.Attrs[1].Data)
	if n > uint64(maxEntries) {
		return nil, fmt.Errorf("rdma: query GID table: kernel returned %d entries for a buffer of %d", n, maxEntries)
	}

	entries := make([]GIDEntry, n)
	if _, err := binary.Decode(cmd.Attrs[0].Data, ne, entries); err != nil {
		return nil, fmt.Errorf("rdma: query GID table: %w", err)
	}

	return entries, nil
}
