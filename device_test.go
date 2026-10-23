//go:build linux

package rdma

import (
	"errors"
	"testing"
)

func TestOtherDevice(t *testing.T) {
	a, b := &Device{}, &Device{}
	pd := &PD{Device: a}
	cq := &CQ{Device: a}
	qp := &QP{PD: pd}

	// The check runs before anything reaches the kernel, so these Devices
	// need no device file.
	for name, err := range map[string]error{
		"DeallocPD":   b.DeallocPD(pd),
		"PollCQ":      func() error { _, err := b.PollCQ(cq, nil); return err }(),
		"ReqNotifyCQ": b.ReqNotifyCQ(cq, false),
		"PostSend":    b.PostSend(qp),
		"DestroyQP":   b.DestroyQP(qp),
		"CreateQP":    func() error { _, err := b.CreateQP(pd, &QPInitAttr{SendCQ: cq, RecvCQ: cq}); return err }(),
	} {
		if !errors.Is(err, errOtherDevice) {
			t.Errorf("%s: got %v, want %v", name, err, errOtherDevice)
		}
	}
}

func TestOpenNeedsProvider(t *testing.T) {
	if _, err := Open("rxe0", nil); err == nil {
		t.Fatal("Open without a provider succeeded")
	}
}
