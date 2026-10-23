//go:build linux

package rdma

// The structures in this file mirror the kernel's uverbs structures from
// <rdma/ib_user_verbs.h> and <rdma/ib_user_ioctl_verbs.h>: the requests and
// responses of the write commands, and the payloads of the ioctl methods.
// They have the kernel's exact layout, for encoding with encoding/binary in
// native byte order, but Go field names. Those the verbs take or return are
// exported. Padding is spelled out so that binary.Size matches the C sizeof;
// abi_test.go checks this, also against golang.org/x/sys/unix where it
// defines the same structure.

// QueryDeviceResp is struct ib_uverbs_query_device_resp.
type QueryDeviceResp struct {
	FWVer                 uint64
	NodeGUID              [8]byte // __be64
	SysImageGUID          [8]byte // __be64
	MaxMRSize             uint64
	PageSizeCap           uint64
	VendorID              uint32
	VendorPartID          uint32
	HWVer                 uint32
	MaxQP                 uint32
	MaxQPWR               uint32
	DeviceCapFlags        uint32
	MaxSGE                uint32
	MaxSGERD              uint32
	MaxCQ                 uint32
	MaxCQE                uint32
	MaxMR                 uint32
	MaxPD                 uint32
	MaxQPRDAtom           uint32
	MaxEERDAtom           uint32
	MaxResRDAtom          uint32
	MaxQPInitRDAtom       uint32
	MaxEEInitRDAtom       uint32
	AtomicCap             uint32
	MaxEE                 uint32
	MaxRDD                uint32
	MaxMW                 uint32
	MaxRawIPv6QP          uint32
	MaxRawEthyQP          uint32
	MaxMcastGrp           uint32
	MaxMcastQPAttach      uint32
	MaxTotalMcastQPAttach uint32
	MaxAH                 uint32
	MaxFMR                uint32
	MaxMapPerFMR          uint32
	MaxSRQ                uint32
	MaxSRQWR              uint32
	MaxSRQSGE             uint32
	MaxPKeys              uint16
	LocalCAAckDelay       uint8
	PhysPortCnt           uint8
	Reserved              [4]byte
}

// QPDest is struct ib_uverbs_qp_dest.
type QPDest struct {
	DGID         [16]byte
	FlowLabel    uint32
	DLID         uint16
	Reserved     uint16
	SGIDIndex    uint8
	HopLimit     uint8
	TrafficClass uint8
	SL           uint8
	SrcPathBits  uint8
	StaticRate   uint8
	IsGlobal     uint8
	PortNum      uint8
}

// ModifyQP is struct ib_uverbs_modify_qp.
type ModifyQP struct {
	Dest             QPDest
	AltDest          QPDest
	QPHandle         uint32
	AttrMask         uint32
	QKey             uint32
	RQPSN            uint32
	SQPSN            uint32
	DestQPNum        uint32
	QPAccessFlags    uint32
	PKeyIndex        uint16
	AltPKeyIndex     uint16
	QPState          uint8
	CurQPState       uint8
	PathMTU          uint8
	PathMigState     uint8
	EnSQDAsyncNotify uint8
	MaxRDAtomic      uint8
	MaxDestRDAtomic  uint8
	MinRNRTimer      uint8
	PortNum          uint8
	Timeout          uint8
	RetryCnt         uint8
	RNRRetry         uint8
	AltPortNum       uint8
	AltTimeout       uint8
	Reserved         [2]byte
}

// QueryQPResp is struct ib_uverbs_query_qp_resp, without driver data.
type QueryQPResp struct {
	Dest            QPDest
	AltDest         QPDest
	MaxSendWR       uint32
	MaxRecvWR       uint32
	MaxSendSGE      uint32
	MaxRecvSGE      uint32
	MaxInlineData   uint32
	QKey            uint32
	RQPSN           uint32
	SQPSN           uint32
	DestQPNum       uint32
	QPAccessFlags   uint32
	PKeyIndex       uint16
	AltPKeyIndex    uint16
	QPState         uint8
	CurQPState      uint8
	PathMTU         uint8
	PathMigState    uint8
	SQDraining      uint8
	MaxRDAtomic     uint8
	MaxDestRDAtomic uint8
	MinRNRTimer     uint8
	PortNum         uint8
	Timeout         uint8
	RetryCnt        uint8
	RNRRetry        uint8
	AltPortNum      uint8
	AltTimeout      uint8
	SQSigAll        uint8
	Reserved        [5]byte
}

// QueryPortResp is struct ib_uverbs_query_port_resp.
type QueryPortResp struct {
	PortCapFlags  uint32
	MaxMsgSz      uint32
	BadPKeyCntr   uint32
	QKeyViolCntr  uint32
	GIDTblLen     uint32
	PKeyTblLen    uint16
	LID           uint16
	SMLID         uint16
	State         uint8
	MaxMTU        uint8
	ActiveMTU     uint8
	LMC           uint8
	MaxVLNum      uint8
	SMSL          uint8
	SubnetTimeout uint8
	InitTypeReply uint8
	ActiveWidth   uint8
	ActiveSpeed   uint8
	PhysState     uint8
	LinkLayer     uint8
	Flags         uint8
	Reserved      uint8
}

// QueryPortRespEx is struct ib_uverbs_query_port_resp_ex, the response of
// UVERBS_METHOD_QUERY_PORT.
type QueryPortRespEx struct {
	LegacyResp    QueryPortResp
	PortCapFlags2 uint16
	Reserved      [2]byte
	ActiveSpeedEx uint32
}

// GIDEntry is struct ib_uverbs_gid_entry, the response of
// UVERBS_METHOD_QUERY_GID_ENTRY and UVERBS_METHOD_QUERY_GID_TABLE.
type GIDEntry struct {
	GID           [16]byte
	GIDIndex      uint32
	PortNum       uint32
	GIDType       uint32
	NetdevIfindex uint32
}

// QPCap is struct ib_uverbs_qp_cap, the capacities of a queue pair's work
// queues.
type QPCap struct {
	MaxSendWR     uint32
	MaxRecvWR     uint32
	MaxSendSGE    uint32
	MaxRecvSGE    uint32
	MaxInlineData uint32
}

// createCompChannel is struct ib_uverbs_create_comp_channel.
type createCompChannel struct {
	Response uint64
}

// createCompChannelResp is struct ib_uverbs_create_comp_channel_resp.
type createCompChannelResp struct {
	FD uint32
}

// queryDevice is struct ib_uverbs_query_device, without driver data.
type queryDevice struct {
	Response uint64
}

// allocPD is struct ib_uverbs_alloc_pd, without driver data.
type allocPD struct {
	Response uint64
}

// allocPDResp is struct ib_uverbs_alloc_pd_resp, without driver data.
type allocPDResp struct {
	PDHandle uint32
}

// regMR is struct ib_uverbs_reg_mr, without driver data.
type regMR struct {
	Response    uint64
	Start       uint64
	Length      uint64
	HCAVA       uint64
	PDHandle    uint32
	AccessFlags uint32
}

// regMRResp is struct ib_uverbs_reg_mr_resp, without driver data.
type regMRResp struct {
	MRHandle uint32
	LKey     uint32
	RKey     uint32
}

// reqNotifyCQ is struct ib_uverbs_req_notify_cq.
type reqNotifyCQ struct {
	CQHandle      uint32
	SolicitedOnly uint32
}

// queryQP is struct ib_uverbs_query_qp, without driver data.
type queryQP struct {
	Response uint64
	QPHandle uint32
	AttrMask uint32
}
