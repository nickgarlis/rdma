//go:build linux

package integration

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nickgarlis/rdma"
	"github.com/nickgarlis/rdma/uverbs"
)

// The rings of a destroyed CQ or QP, or of a closed device, are unmapped.
// Polling or posting afterwards, or at the same time, must fail rather than
// touch that memory.

func wantClosed(t *testing.T, what string, err error) {
	t.Helper()

	if !errors.Is(err, uverbs.ErrClosed) {
		t.Fatalf("%s: got error %v, want uverbs.ErrClosed", what, err)
	}
}

func wantDestroyed(t *testing.T, what string, err error) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), "already destroyed") {
		t.Fatalf("%s: got error %v, want an already-destroyed error", what, err)
	}
}

func TestUseAfterDestroy(t *testing.T) {
	dev := openDevice(t)

	pd, err := dev.AllocPD()
	if err != nil {
		t.Fatalf("AllocPD: %v", err)
	}

	t.Cleanup(func() { closeOK(t, "PD", func() error { return dev.DeallocPD(pd) }) })

	cq, err := dev.CreateCQ(16, nil, 0)
	if err != nil {
		t.Fatalf("CreateCQ: %v", err)
	}

	qp, err := dev.CreateQP(pd, &rdma.QPInitAttr{
		QPType: rdma.QPTypeRC,
		SendCQ: cq,
		RecvCQ: cq,
		Cap:    rdma.QPCap{MaxSendWR: 4, MaxRecvWR: 4, MaxSendSGE: 1, MaxRecvSGE: 1},
	})
	if err != nil {
		t.Fatalf("CreateQP: %v", err)
	}

	if err := dev.DestroyQP(qp); err != nil {
		t.Fatalf("DestroyQP: %v", err)
	}

	wantClosed(t, "PostSend after DestroyQP", dev.PostSend(qp, rdma.SendWR{Opcode: rdma.WRSend}))
	wantDestroyed(t, "DestroyQP twice", dev.DestroyQP(qp))

	if err := dev.DestroyCQ(cq); err != nil {
		t.Fatalf("DestroyCQ: %v", err)
	}

	_, err = dev.PollCQ(cq, make([]rdma.WC, 1))
	wantClosed(t, "PollCQ after DestroyCQ", err)
	wantDestroyed(t, "DestroyCQ twice", dev.DestroyCQ(cq))
}

func TestPollDuringDestroy(t *testing.T) {
	dev := openDevice(t)

	for range 100 {
		cq, err := dev.CreateCQ(16, nil, 0)
		if err != nil {
			t.Fatalf("CreateCQ: %v", err)
		}

		var wg sync.WaitGroup
		wg.Go(func() {
			wc := make([]rdma.WC, 4)
			for {
				if _, err := dev.PollCQ(cq, wc); err != nil {
					return
				}
			}
		})

		if err := dev.DestroyCQ(cq); err != nil {
			t.Fatalf("DestroyCQ: %v", err)
		}

		wg.Wait()
	}
}

func TestUseAfterClose(t *testing.T) {
	dev := openDevice(t)

	cq, err := dev.CreateCQ(16, nil, 0)
	if err != nil {
		t.Fatalf("CreateCQ: %v", err)
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = dev.PollCQ(cq, make([]rdma.WC, 1))
	wantClosed(t, "PollCQ after Close", err)
}

func TestCreateDuringClose(t *testing.T) {
	for range 20 {
		dev := openDevice(t)

		var wg sync.WaitGroup
		wg.Go(func() {
			wc := make([]rdma.WC, 4)
			for {
				cq, err := dev.CreateCQ(16, nil, 0)
				if err != nil {
					return
				}

				// The CQ may be closed under this poll; it must fail, not
				// crash.
				if _, err := dev.PollCQ(cq, wc); err != nil {
					return
				}
			}
		})

		if err := dev.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		wg.Wait()
	}
}
