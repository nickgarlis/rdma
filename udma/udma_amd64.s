//go:build linux

#include "textflag.h"

// x86 keeps stores in order and loads in order, so only lfence and sfence
// are needed, as in rdma-core.

TEXT ·OrderReads(SB), NOSPLIT, $0-0
	LFENCE
	RET

TEXT ·OrderWrites(SB), NOSPLIT, $0-0
	RET

TEXT ·FlushWrites(SB), NOSPLIT, $0-0
	SFENCE
	RET

TEXT ·store64(SB), NOSPLIT, $0-16
	MOVQ p+0(FP), AX
	MOVQ v+8(FP), BX
	MOVQ BX, (AX)
	RET

TEXT ·load8(SB), NOSPLIT, $0-9
	MOVQ p+0(FP), AX
	MOVB (AX), BX
	MOVB BX, ret+8(FP)
	RET

TEXT ·load32(SB), NOSPLIT, $0-12
	MOVQ p+0(FP), AX
	MOVL (AX), BX
	MOVL BX, ret+8(FP)
	RET

TEXT ·load64(SB), NOSPLIT, $0-16
	MOVQ p+0(FP), AX
	MOVQ (AX), BX
	MOVQ BX, ret+8(FP)
	RET

TEXT ·store32(SB), NOSPLIT, $0-12
	MOVQ p+0(FP), AX
	MOVL v+8(FP), BX
	MOVL BX, (AX)
	RET
