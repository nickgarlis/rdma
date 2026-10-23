//go:build linux

package rdma

import "testing"

func TestProviderData(t *testing.T) {
	type state struct{ n int }

	cq := &CQ{}
	if _, ok := cq.ProviderData[*state](); ok {
		t.Fatal("ProviderData found state that was never set")
	}

	cq.SetProviderData(&state{7})
	if s, ok := cq.ProviderData[*state](); !ok || s.n != 7 {
		t.Fatalf("ProviderData = %v, %v, want 7, true", s, ok)
	}

	if _, ok := cq.ProviderData[int](); ok {
		t.Fatal("ProviderData returned state of the wrong type")
	}

	// Every object type carries its own.
	pd, mr, qp := &PD{}, &MR{}, &QP{}
	pd.SetProviderData(1)
	mr.SetProviderData(2)
	qp.SetProviderData(3)
	for _, c := range []struct {
		got  func() (int, bool)
		want int
	}{{pd.ProviderData[int], 1}, {mr.ProviderData[int], 2}, {qp.ProviderData[int], 3}} {
		if got, ok := c.got(); !ok || got != c.want {
			t.Errorf("ProviderData = %d, %v, want %d", got, ok, c.want)
		}
	}
}
