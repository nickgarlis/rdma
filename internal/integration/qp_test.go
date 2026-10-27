//go:build linux

package integration

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/nickgarlis/rdma"
)

// pair is two RC QPs on the same device, connected to each other.
type pair struct {
	dev    *rdma.Device
	mem    *rdma.Memory
	mr     *rdma.MR
	ch     *rdma.CompChannel
	cqA    *rdma.CQ
	cqB    *rdma.CQ
	qpA    *rdma.QP
	qpB    *rdma.QP
	buf    []byte
	region int
}

const regionSize = 4096

func newPair(t *testing.T) *pair {
	t.Helper()

	dev := openDevice(t)
	gid := roceGID(t, dev)

	p := &pair{dev: dev, region: regionSize}

	pd, err := dev.AllocPD()
	if err != nil {
		t.Fatalf("AllocPD: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "PD", func() error { return dev.DeallocPD(pd) }) })

	p.mem, err = rdma.AllocMemory(4 * regionSize)
	if err != nil {
		t.Fatalf("AllocMemory: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "Memory", p.mem.Close) })
	p.buf = p.mem.Bytes()

	p.mr, err = dev.RegMR(pd, p.mem, rdma.AccessLocalWrite|rdma.AccessRemoteWrite|
		rdma.AccessRemoteRead|rdma.AccessRemoteAtomic)
	if err != nil {
		t.Fatalf("RegMR: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "MR", func() error { return dev.DeregMR(p.mr) }) })

	p.ch, err = dev.CreateCompChannel()
	if err != nil {
		t.Fatalf("CreateCompChannel: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "CompChannel", func() error { return dev.DestroyCompChannel(p.ch) }) })

	for _, cq := range []**rdma.CQ{&p.cqA, &p.cqB} {
		*cq, err = dev.CreateCQ(32, p.ch, 0)
		if err != nil {
			t.Fatalf("CreateCQ: %v", err)
		}

		t.Cleanup(func() { closeOK(t, "CQ", func() error { return dev.DestroyCQ(*cq) }) })
	}

	for _, qp := range []struct {
		qp **rdma.QP
		cq *rdma.CQ
	}{{&p.qpA, p.cqA}, {&p.qpB, p.cqB}} {
		*qp.qp, err = dev.CreateQP(pd, &rdma.QPInitAttr{
			QPType: rdma.QPTypeRC,
			SendCQ: qp.cq,
			RecvCQ: qp.cq,
			Cap: rdma.QPCap{
				MaxSendWR:     16,
				MaxRecvWR:     16,
				MaxSendSGE:    2,
				MaxRecvSGE:    2,
				MaxInlineData: 64,
			},
		})
		if err != nil {
			t.Fatalf("CreateQP: %v", err)
		}

		t.Cleanup(func() { closeOK(t, "QP", func() error { return dev.DestroyQP(*qp.qp) }) })
	}

	connect(t, dev, p.qpA, p.qpB, gid)
	connect(t, dev, p.qpB, p.qpA, gid)
	return p
}

// connect moves qp from RESET to RTS, connected to remote.
func connect(t *testing.T, dev *rdma.Device, qp, remote *rdma.QP, gid rdma.GIDEntry) {
	t.Helper()

	steps := []rdma.ModifyQP{
		{
			AttrMask:      uint32(rdma.QPAttrState | rdma.QPAttrPKeyIndex | rdma.QPAttrPort | rdma.QPAttrAccessFlags),
			QPState:       uint8(rdma.QPStateInit),
			PortNum:       1,
			QPAccessFlags: uint32(rdma.AccessRemoteWrite | rdma.AccessRemoteRead | rdma.AccessRemoteAtomic),
		},
		{
			AttrMask: uint32(rdma.QPAttrState | rdma.QPAttrAV | rdma.QPAttrPathMTU | rdma.QPAttrDestQPN |
				rdma.QPAttrRQPSN | rdma.QPAttrMaxDestRDAtomic | rdma.QPAttrMinRNRTimer),
			QPState:         uint8(rdma.QPStateRTR),
			PathMTU:         uint8(rdma.MTU1024),
			DestQPNum:       remote.QPNum,
			MaxDestRDAtomic: 1,
			MinRNRTimer:     12,
			Dest: rdma.QPDest{
				DGID:      gid.GID,
				SGIDIndex: uint8(gid.GIDIndex),
				HopLimit:  64,
				IsGlobal:  1,
				PortNum:   1,
			},
		},
		{
			AttrMask: uint32(rdma.QPAttrState | rdma.QPAttrTimeout | rdma.QPAttrRetryCount |
				rdma.QPAttrRNRRetry | rdma.QPAttrSQPSN | rdma.QPAttrMaxQPRDAtomic),
			QPState:     uint8(rdma.QPStateRTS),
			Timeout:     14,
			RetryCnt:    7,
			RNRRetry:    7,
			MaxRDAtomic: 1,
		},
	}

	for _, s := range steps {
		if err := dev.ModifyQP(qp, &s); err != nil {
			t.Fatalf("ModifyQP to %s: %v", rdma.QPState(s.QPState), err)
		}
	}

	attr, err := dev.QueryQP(qp, rdma.QPAttrState|rdma.QPAttrDestQPN)
	if err != nil {
		t.Fatalf("QueryQP: %v", err)
	}

	if rdma.QPState(attr.QPState) != rdma.QPStateRTS || attr.DestQPNum != remote.QPNum {
		t.Fatalf("QP state %s, dest QPN %d; want RTS, %d", rdma.QPState(attr.QPState), attr.DestQPNum, remote.QPNum)
	}
}

// slot returns region i of the shared buffer and its SGE.
func (p *pair) slot(i int, n uint32) ([]byte, rdma.SGE) {
	off := i * p.region
	return p.buf[off : off+int(n)], p.mr.SGE(uint32(off), n)
}

// poll waits for one completion on cq and checks its ID and status.
func poll(t *testing.T, dev *rdma.Device, cq *rdma.CQ, id uint64) rdma.WC {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	wc := make([]rdma.WC, 1)
	for {
		n, err := dev.PollCQ(cq, wc)
		if err != nil {
			t.Fatalf("PollCQ: %v", err)
		}

		if n == 1 {
			if err := wc[0].Err(); err != nil {
				t.Fatalf("completion: %v", err)
			}

			if wc[0].WRID != id {
				t.Fatalf("completion for work request %d, want %d", wc[0].WRID, id)
			}

			return wc[0]
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for work request %d", id)
		}

		time.Sleep(time.Millisecond)
	}
}

func TestSendRecv(t *testing.T) {
	p := newPair(t)

	msg := []byte("hello from qp A over soft-RoCE")
	src, srcSGE := p.slot(0, uint32(len(msg)))
	copy(src, msg)

	dst, dstSGE := p.slot(1, regionSize)
	if err := p.dev.PostRecv(p.qpB, rdma.RecvWR{WRID: 1, SGList: []rdma.SGE{dstSGE}}); err != nil {
		t.Fatalf("PostRecv: %v", err)
	}

	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:      2,
		Opcode:    rdma.WRSendWithImm,
		SendFlags: rdma.SendSignaled,
		SGList:    []rdma.SGE{srcSGE},
		ImmData:   0x12345678,
	}); err != nil {
		t.Fatalf("PostSend: %v", err)
	}

	if wc := poll(t, p.dev, p.cqA, 2); wc.Opcode != rdma.WCSend {
		t.Fatalf("send completion opcode %d, want WCSend", wc.Opcode)
	}

	wc := poll(t, p.dev, p.cqB, 1)
	if wc.Opcode != rdma.WCRecv || wc.ByteLen != uint32(len(msg)) || wc.QPNum != p.qpB.QPNum {
		t.Fatalf("unexpected receive completion: %+v", wc)
	}

	if wc.WCFlags&rdma.WCWithImm == 0 || wc.ImmData != 0x12345678 {
		t.Fatalf("immediate data %#x (flags %#x), want 0x12345678", wc.ImmData, wc.WCFlags)
	}

	if !bytes.Equal(dst[:len(msg)], msg) {
		t.Fatalf("received %q, want %q", dst[:len(msg)], msg)
	}
}

func TestSendInline(t *testing.T) {
	p := newPair(t)

	msg := []byte("inline")
	src, srcSGE := p.slot(0, uint32(len(msg)))
	copy(src, msg)

	dst, dstSGE := p.slot(1, regionSize)
	if err := p.dev.PostRecv(p.qpB, rdma.RecvWR{WRID: 1, SGList: []rdma.SGE{dstSGE}}); err != nil {
		t.Fatalf("PostRecv: %v", err)
	}

	// Inline data is copied at post time, so the source may change
	// right after.
	srcSGE.LKey = 0
	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:      2,
		Opcode:    rdma.WRSend,
		SendFlags: rdma.SendSignaled | rdma.SendInline,
		SGList:    []rdma.SGE{srcSGE},
	}); err != nil {
		t.Fatalf("PostSend: %v", err)
	}

	clear(src)

	poll(t, p.dev, p.cqA, 2)
	poll(t, p.dev, p.cqB, 1)

	if !bytes.Equal(dst[:len(msg)], msg) {
		t.Fatalf("received %q, want %q", dst[:len(msg)], msg)
	}
}

func TestRDMAWriteRead(t *testing.T) {
	p := newPair(t)

	src, srcSGE := p.slot(0, 512)
	for i := range src {
		src[i] = byte(i)
	}

	remote, _ := p.slot(1, 512)
	remoteAddr := p.mr.Addr + uint64(regionSize)

	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:       1,
		Opcode:     rdma.WRRDMAWrite,
		SendFlags:  rdma.SendSignaled,
		SGList:     []rdma.SGE{srcSGE},
		RemoteAddr: remoteAddr,
		RKey:       p.mr.RKey,
	}); err != nil {
		t.Fatalf("PostSend write: %v", err)
	}

	if wc := poll(t, p.dev, p.cqA, 1); wc.Opcode != rdma.WCRDMAWrite {
		t.Fatalf("write completion opcode %d, want WCRDMAWrite", wc.Opcode)
	}

	if !bytes.Equal(remote, src) {
		t.Fatal("RDMA write did not update the remote buffer")
	}

	// Read the data back into a third region.
	dst, dstSGE := p.slot(2, 512)
	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:       2,
		Opcode:     rdma.WRRDMARead,
		SendFlags:  rdma.SendSignaled,
		SGList:     []rdma.SGE{dstSGE},
		RemoteAddr: remoteAddr,
		RKey:       p.mr.RKey,
	}); err != nil {
		t.Fatalf("PostSend read: %v", err)
	}

	if wc := poll(t, p.dev, p.cqA, 2); wc.Opcode != rdma.WCRDMARead {
		t.Fatalf("read completion opcode %d, want WCRDMARead", wc.Opcode)
	}

	if !bytes.Equal(dst, src) {
		t.Fatal("RDMA read returned different data")
	}
}

func TestAtomics(t *testing.T) {
	p := newPair(t)

	counter, _ := p.slot(1, 8)
	binary.NativeEndian.PutUint64(counter, 40)
	remoteAddr := p.mr.Addr + uint64(regionSize)

	old, oldSGE := p.slot(0, 8)
	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:       1,
		Opcode:     rdma.WRAtomicFetchAndAdd,
		SendFlags:  rdma.SendSignaled,
		SGList:     []rdma.SGE{oldSGE},
		RemoteAddr: remoteAddr,
		RKey:       p.mr.RKey,
		CompareAdd: 2,
	}); err != nil {
		t.Fatalf("PostSend fetch and add: %v", err)
	}

	poll(t, p.dev, p.cqA, 1)

	if got := binary.NativeEndian.Uint64(old); got != 40 {
		t.Fatalf("fetch and add returned %d, want 40", got)
	}

	if err := p.dev.PostSend(p.qpA, rdma.SendWR{
		WRID:       2,
		Opcode:     rdma.WRAtomicCmpAndSwp,
		SendFlags:  rdma.SendSignaled,
		SGList:     []rdma.SGE{oldSGE},
		RemoteAddr: remoteAddr,
		RKey:       p.mr.RKey,
		CompareAdd: 42,
		Swap:       7,
	}); err != nil {
		t.Fatalf("PostSend compare and swap: %v", err)
	}

	poll(t, p.dev, p.cqA, 2)

	if got := binary.NativeEndian.Uint64(counter); got != 7 {
		t.Fatalf("counter is %d after compare and swap, want 7", got)
	}
}
