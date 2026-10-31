//go:build linux

#include "textflag.h"

// DMB and DSB options: OSHLD = 1, OSHST = 2, ST = 14.

TEXT ·OrderReads(SB), NOSPLIT, $0-0
	DMB $1 // dmb oshld
	RET

TEXT ·OrderWrites(SB), NOSPLIT, $0-0
	DMB $2 // dmb oshst
	RET

TEXT ·FlushWrites(SB), NOSPLIT, $0-0
	DSB $14 // dsb st
	RET

TEXT ·store64(SB), NOSPLIT, $0-16
	MOVD p+0(FP), R0
	MOVD v+8(FP), R1
	MOVD R1, (R0)
	RET

TEXT ·load8(SB), NOSPLIT, $0-9
	MOVD p+0(FP), R0
	MOVBU (R0), R1
	MOVB R1, ret+8(FP)
	RET

TEXT ·load32(SB), NOSPLIT, $0-12
	MOVD p+0(FP), R0
	MOVWU (R0), R1
	MOVW R1, ret+8(FP)
	RET

TEXT ·load64(SB), NOSPLIT, $0-16
	MOVD p+0(FP), R0
	MOVD (R0), R1
	MOVD R1, ret+8(FP)
	RET

TEXT ·store32(SB), NOSPLIT, $0-12
	MOVD p+0(FP), R0
	MOVW v+8(FP), R1
	MOVW R1, (R0)
	RET
