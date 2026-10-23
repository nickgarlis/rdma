//go:build linux

package rdma

import (
	"errors"
	"testing"
)

func TestWCErr(t *testing.T) {
	if err := (&WC{Status: WCSuccess}).Err(); err != nil {
		t.Fatalf("Err of a successful completion: %v", err)
	}

	err := (&WC{WRID: 42, Status: WCRemoteAccessError}).Err()
	var wcErr *WCError
	if !errors.As(err, &wcErr) || wcErr.Status != WCRemoteAccessError {
		t.Fatalf("Err of a failed completion = %v, want a *WCError with WCRemoteAccessError", err)
	}
}
