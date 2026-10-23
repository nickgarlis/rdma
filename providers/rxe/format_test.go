//go:build linux

package rxe

import (
	"encoding/binary"
	"testing"

	"github.com/nickgarlis/rdma"
)

var ne = binary.NativeEndian

func TestSendWQEEncode(t *testing.T) {
	w := rxeSendWQE{
		WRID:        7,
		Opcode:      uint32(rdma.WRRDMAWriteWithImm),
		Flags:       uint32(rdma.SendSignaled),
		Ex:          0xdeadbeef,
		ExBigEndian: true,
		RemoteAddr:  0x1000,
		RKey:        0x55,
		IOVA:        0x1000,
		DMA: rxeDMAInfo{
			Length: 16,
			Resid:  16,
			NumSGE: 1,
			SGL:    []rdma.SGE{{Addr: 0x2000, Length: 16, LKey: 0x66}},
		},
	}

	slot := make([]byte, 256)
	for i := range slot {
		slot[i] = 0xff // encode must clear what it does not set
	}

	if err := w.encode(slot); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dma := slot[sendWQEDMA:]
	sge := dma[dmaSGE:]
	for _, c := range []struct {
		name      string
		got, want uint64
	}{
		{"wr_id", ne.Uint64(slot[sendWRID:]), 7},
		{"opcode", uint64(ne.Uint32(slot[sendWROpcode:])), uint64(rdma.WRRDMAWriteWithImm)},
		{"imm_data (big endian)", uint64(binary.BigEndian.Uint32(slot[sendWREx:])), 0xdeadbeef},
		{"remote_addr", ne.Uint64(slot[sendWRRemote:]), 0x1000},
		{"rkey", uint64(ne.Uint32(slot[sendWRRDMARKey:])), 0x55},
		{"iova", ne.Uint64(slot[sendWQEIOVA:]), 0x1000},
		{"dma.length", uint64(ne.Uint32(dma[dmaLength:])), 16},
		{"dma.num_sge", uint64(ne.Uint32(dma[dmaNumSGE:])), 1},
		{"sge.addr", ne.Uint64(sge[0:]), 0x2000},
		{"sge.lkey", uint64(ne.Uint32(sge[12:])), 0x66},
		{"after the SGE", uint64(sge[sgeSize]), 0},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
}

func TestSendWQEAtomicLayout(t *testing.T) {
	w := rxeSendWQE{RemoteAddr: 0x1000, RKey: 0x55, CompareAdd: 1, Swap: 2, Atomic: true}

	slot := make([]byte, 256)
	if err := w.encode(slot); err != nil {
		t.Fatalf("encode: %v", err)
	}

	if ne.Uint64(slot[sendWRCompare:]) != 1 || ne.Uint64(slot[sendWRSwap:]) != 2 ||
		ne.Uint32(slot[sendWRAtomRKey:]) != 0x55 {
		t.Fatal("atomic fields are not in the atomic layout of the wr union")
	}
}

func TestRecvWQEEncode(t *testing.T) {
	w := rxeRecvWQE{
		WRID: 9,
		DMA: rxeDMAInfo{
			Length: 8,
			Resid:  8,
			NumSGE: 1,
			SGL:    []rdma.SGE{{Addr: 0x3000, Length: 8, LKey: 0x77}},
		},
	}

	slot := make([]byte, 64)
	if err := w.encode(slot); err != nil {
		t.Fatalf("encode: %v", err)
	}

	if ne.Uint64(slot[recvWQEID:]) != 9 || ne.Uint64(slot[recvWQEDMA+dmaSGE:]) != 0x3000 {
		t.Fatalf("receive WQE = %x", slot)
	}

	// Too many SGEs for the slot are an error, not an overflow.
	w.DMA.SGL = make([]rdma.SGE, 8)
	if err := w.encode(slot); err == nil {
		t.Fatal("encode accepted more SGEs than fit the WQE")
	}
}

func TestCQEDecode(t *testing.T) {
	b := make([]byte, rxeCQESize)
	ne.PutUint64(b[0:], 42)
	ne.PutUint32(b[8:], uint32(rdma.WCRemoteAccessError))
	ne.PutUint32(b[12:], uint32(rdma.WCRecv))
	ne.PutUint32(b[20:], 128)
	binary.BigEndian.PutUint32(b[24:], 0xdeadbeef)
	ne.PutUint32(b[28:], 17)
	ne.PutUint32(b[36:], uint32(rdma.WCWithImm))
	b[46] = 1

	var c rxeCQE
	if err := c.decode(b); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := rdma.WC{
		WRID:    42,
		Status:  rdma.WCRemoteAccessError,
		Opcode:  rdma.WCRecv,
		ByteLen: 128,
		ImmData: 0xdeadbeef,
		QPNum:   17,
		WCFlags: rdma.WCWithImm,
		PortNum: 1,
	}

	if got := c.wc(); got != want {
		t.Fatalf("wc = %+v, want %+v", got, want)
	}

	if err := c.decode(b[:rxeCQESize-1]); err == nil {
		t.Fatal("decode accepted a short CQ entry")
	}
}
