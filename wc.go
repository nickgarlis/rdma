//go:build linux

package rdma

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// A WCStatus is the status of a work completion.
type WCStatus uint32

// Work completion statuses, from enum ibv_wc_status.
const (
	WCSuccess WCStatus = iota
	WCLocalLengthError
	WCLocalQPOpError
	WCLocalEECOpError
	WCLocalProtectionError
	WCWRFlushError
	WCMWBindError
	WCBadResponseError
	WCLocalAccessError
	WCRemoteInvalidRequestError
	WCRemoteAccessError
	WCRemoteOpError
	WCRetryExceededError
	WCRNRRetryExceededError
	WCLocalRDDViolationError
	WCRemoteInvalidRDRequest
	WCRemoteAbortError
	WCInvalidEECN
	WCInvalidEECState
	WCFatalError
	WCResponseTimeoutError
	WCGeneralError
	WCTagMatchingError
	WCTagMatchingRndvIncomplete
)

var wcStatusNames = [...]string{
	"success",
	"local length error",
	"local QP operation error",
	"local EE context operation error",
	"local protection error",
	"work request flushed error",
	"memory bind operation error",
	"bad response error",
	"local access error",
	"remote invalid request error",
	"remote access error",
	"remote operation error",
	"transport retry counter exceeded",
	"RNR retry counter exceeded",
	"local RDD violation error",
	"remote invalid RD request",
	"aborted error",
	"invalid EE context number",
	"invalid EE context state",
	"fatal error",
	"response timeout error",
	"general error",
	"tag matching error",
	"tag matching rendezvous incomplete",
}

func (s WCStatus) String() string {
	if int(s) < len(wcStatusNames) {
		return wcStatusNames[s]
	}

	return fmt.Sprintf("WCStatus(%d)", uint32(s))
}

// A WCOpcode is the operation that produced a work completion.
type WCOpcode uint32

// Work completion opcodes, from enum ibv_wc_opcode.
const (
	WCSend            WCOpcode = unix.IB_UVERBS_WC_SEND
	WCRDMAWrite       WCOpcode = unix.IB_UVERBS_WC_RDMA_WRITE
	WCRDMARead        WCOpcode = unix.IB_UVERBS_WC_RDMA_READ
	WCCompSwap        WCOpcode = unix.IB_UVERBS_WC_COMP_SWAP
	WCFetchAdd        WCOpcode = unix.IB_UVERBS_WC_FETCH_ADD
	WCBindMW          WCOpcode = unix.IB_UVERBS_WC_BIND_MW
	WCLocalInv        WCOpcode = unix.IB_UVERBS_WC_LOCAL_INV
	WCTSO             WCOpcode = unix.IB_UVERBS_WC_TSO
	WCRecv            WCOpcode = 1 << 7
	WCRecvRDMAWithImm WCOpcode = WCRecv + 1
)

// WCFlags describe the contents of a work completion.
type WCFlags uint32

// Work completion flags, from enum ibv_wc_flags.
const (
	WCWithGRH  WCFlags = 1 << 0
	WCWithImm  WCFlags = 1 << 1
	WCIPCsumOK WCFlags = 1 << 2
	WCWithInv  WCFlags = 1 << 3
)

// A WC reports the outcome of a work request.
type WC struct {
	WRID      uint64
	Status    WCStatus
	Opcode    WCOpcode
	VendorErr uint32
	ByteLen   uint32

	// ImmData holds the immediate data, in host byte order, when Flags has
	// WCWithImm, or the invalidated remote key when Flags has WCWithInv.
	ImmData uint32

	QPNum        uint32
	SrcQP        uint32
	WCFlags      WCFlags
	PKeyIndex    uint16
	SLID         uint16
	SL           uint8
	DLIDPathBits uint8
	PortNum      uint8
}

// Err returns an error describing a failed completion, or nil on success.
func (wc *WC) Err() error {
	if wc.Status == WCSuccess {
		return nil
	}

	return &WCError{WRID: wc.WRID, Status: wc.Status, VendorErr: wc.VendorErr}
}

// A WCError is a work completion with an error status.
type WCError struct {
	WRID      uint64
	Status    WCStatus
	VendorErr uint32
}

func (e *WCError) Error() string {
	return fmt.Sprintf("rdma: work request %d failed: %s (vendor error %#x)", e.WRID, e.Status, e.VendorErr)
}
