//go:build linux

package rdma

import (
	"encoding/binary"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestABISizes(t *testing.T) {
	// Sizes of the C structures, from sizeof on linux/amd64.
	tests := []struct {
		name string
		v    any
		want int
	}{
		{"ib_uverbs_query_device", queryDevice{}, 8},
		{"ib_uverbs_alloc_pd", allocPD{}, 8},
		{"ib_uverbs_reg_mr", regMR{}, 40},
		{"ib_uverbs_reg_mr_resp", regMRResp{}, 12},
		{"ib_uverbs_req_notify_cq", reqNotifyCQ{}, 8},
		{"ib_uverbs_query_qp", queryQP{}, 16},
		{"ib_uverbs_query_device_resp", QueryDeviceResp{}, 176},
		{"ib_uverbs_qp_dest", QPDest{}, 32},
		{"ib_uverbs_modify_qp", ModifyQP{}, 112},
		{"ib_uverbs_query_qp_resp", QueryQPResp{}, 128},
		{"ib_uverbs_query_port_resp_ex", QueryPortRespEx{}, 48},
		{"ib_uverbs_gid_entry", GIDEntry{}, 32},
		{"ib_uverbs_qp_cap", QPCap{}, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := binary.Size(tt.v); got != tt.want {
				t.Fatalf("binary.Size = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestABIMatchesUnix(t *testing.T) {
	// golang.org/x/sys/unix generates these from the kernel headers.
	tests := []struct {
		name      string
		ours, sys any
	}{
		{"ib_uverbs_query_port_resp", QueryPortResp{}, unix.IBUverbsQueryPortResp{}},
		{"ib_uverbs_query_port_resp_ex", QueryPortRespEx{}, unix.IBUverbsQueryPortRespEx{}},
		{"ib_uverbs_gid_entry", GIDEntry{}, unix.IBUverbsGIDEntry{}},
		{"ib_uverbs_qp_cap", QPCap{}, unix.IBUverbsQPCap{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := binary.Size(tt.ours), int(reflect.TypeOf(tt.sys).Size()); got != want {
				t.Fatalf("binary.Size = %d, x/sys size = %d", got, want)
			}
		})
	}
}
