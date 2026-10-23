//go:build linux

package rxe_test

import (
	"context"
	"testing"
	"time"

	"github.com/nickgarlis/rdma"
)

func TestCompChannel(t *testing.T) {
	p := newPair(t)

	if err := p.dev.ReqNotifyCQ(p.cqB, false); err != nil {
		t.Fatalf("ReqNotifyCQ: %v", err)
	}

	_, dstSGE := p.slot(1, regionSize)
	if err := p.dev.PostRecv(p.qpB, rdma.RecvWR{WRID: 1, SGList: []rdma.SGE{dstSGE}}); err != nil {
		t.Fatalf("PostRecv: %v", err)
	}

	_, srcSGE := p.slot(0, 16)
	if err := p.dev.PostSend(p.qpA, rdma.SendWR{WRID: 2, Opcode: rdma.WRSend, SGList: []rdma.SGE{srcSGE}}); err != nil {
		t.Fatalf("PostSend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cq, err := p.dev.GetCQEvent(ctx, p.ch)
	if err != nil {
		t.Fatalf("GetCQEvent: %v", err)
	}

	if cq != p.cqB {
		t.Fatalf("notification for CQ %d, want %d", cq.Handle, p.cqB.Handle)
	}

	poll(t, p.dev, p.cqB, 1)

	// With nothing armed, GetCQEvent blocks until the context is done.
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := p.dev.GetCQEvent(ctx, p.ch); err == nil {
		t.Fatal("GetCQEvent returned a notification for an unarmed CQ")
	}
}
