//go:build amd64

#include "textflag.h"

// 32-byte broadcasted constants for the AVX2 scanners.
DATA quoteConst<>+0(SB)/8, $0x2222222222222222
DATA quoteConst<>+8(SB)/8, $0x2222222222222222
DATA quoteConst<>+16(SB)/8, $0x2222222222222222
DATA quoteConst<>+24(SB)/8, $0x2222222222222222
GLOBL quoteConst<>(SB), RODATA|NOPTR, $32

DATA bslashConst<>+0(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA bslashConst<>+8(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA bslashConst<>+16(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA bslashConst<>+24(SB)/8, $0x5c5c5c5c5c5c5c5c
GLOBL bslashConst<>(SB), RODATA|NOPTR, $32

// bias = 0x80: adding it maps control bytes 0x00..0x1F to 0x80..0x9F.
DATA biasConst<>+0(SB)/8, $0x8080808080808080
DATA biasConst<>+8(SB)/8, $0x8080808080808080
DATA biasConst<>+16(SB)/8, $0x8080808080808080
DATA biasConst<>+24(SB)/8, $0x8080808080808080
GLOBL biasConst<>(SB), RODATA|NOPTR, $32

// limit = 0xA0 (signed -96): (limit > biased) is true exactly for bytes < 0x20.
DATA limitConst<>+0(SB)/8, $0xa0a0a0a0a0a0a0a0
DATA limitConst<>+8(SB)/8, $0xa0a0a0a0a0a0a0a0
DATA limitConst<>+16(SB)/8, $0xa0a0a0a0a0a0a0a0
DATA limitConst<>+24(SB)/8, $0xa0a0a0a0a0a0a0a0
GLOBL limitConst<>(SB), RODATA|NOPTR, $32

// func escapeIndexAVX2(p *byte, n int) int
// Returns the index of the first byte that is '"', '\\', or < 0x20, else n.
TEXT ·escapeIndexAVX2(SB), NOSPLIT, $0-24
	MOVQ p+0(FP), SI
	MOVQ n+8(FP), CX
	XORQ DX, DX
	VMOVDQU quoteConst<>(SB), Y1
	VMOVDQU bslashConst<>(SB), Y2
	VMOVDQU biasConst<>(SB), Y3
	VMOVDQU limitConst<>(SB), Y4

loop:
	MOVQ CX, AX
	SUBQ DX, AX
	CMPQ AX, $32
	JL   tail
	VMOVDQU   (SI)(DX*1), Y0
	VPCMPEQB  Y0, Y1, Y5     // == '"'
	VPCMPEQB  Y0, Y2, Y6     // == '\\'
	VPOR      Y6, Y5, Y5
	VPADDB    Y0, Y3, Y7     // biased = byte + 0x80
	VPCMPGTB  Y7, Y4, Y6     // limit > biased  => byte < 0x20
	VPOR      Y6, Y5, Y5
	VPMOVMSKB Y5, AX
	TESTL     AX, AX
	JNZ       found
	ADDQ      $32, DX
	JMP       loop

found:
	BSFL AX, AX
	ADDQ DX, AX
	MOVQ AX, ret+16(FP)
	VZEROUPPER
	RET

tail:
	CMPQ    DX, CX
	JGE     none
	MOVBLZX (SI)(DX*1), AX
	CMPL    AX, $0x20
	JL      retdx
	CMPL    AX, $0x22
	JE      retdx
	CMPL    AX, $0x5c
	JE      retdx
	INCQ    DX
	JMP     tail

retdx:
	MOVQ DX, ret+16(FP)
	VZEROUPPER
	RET

none:
	MOVQ CX, ret+16(FP)
	VZEROUPPER
	RET

// func scanStringAVX2(p *byte, n int) int
// Returns the index of the first '"' or '\\', else n.
TEXT ·scanStringAVX2(SB), NOSPLIT, $0-24
	MOVQ p+0(FP), SI
	MOVQ n+8(FP), CX
	XORQ DX, DX
	VMOVDQU quoteConst<>(SB), Y1
	VMOVDQU bslashConst<>(SB), Y2

loop2:
	MOVQ CX, AX
	SUBQ DX, AX
	CMPQ AX, $32
	JL   tail2
	VMOVDQU   (SI)(DX*1), Y0
	VPCMPEQB  Y0, Y1, Y5
	VPCMPEQB  Y0, Y2, Y6
	VPOR      Y6, Y5, Y5
	VPMOVMSKB Y5, AX
	TESTL     AX, AX
	JNZ       found2
	ADDQ      $32, DX
	JMP       loop2

found2:
	BSFL AX, AX
	ADDQ DX, AX
	MOVQ AX, ret+16(FP)
	VZEROUPPER
	RET

tail2:
	CMPQ    DX, CX
	JGE     none2
	MOVBLZX (SI)(DX*1), AX
	CMPL    AX, $0x22
	JE      retdx2
	CMPL    AX, $0x5c
	JE      retdx2
	INCQ    DX
	JMP     tail2

retdx2:
	MOVQ DX, ret+16(FP)
	VZEROUPPER
	RET

none2:
	MOVQ CX, ret+16(FP)
	VZEROUPPER
	RET

// func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL eaxArg+0(FP), AX
	MOVL ecxArg+4(FP), CX
	CPUID
	MOVL AX, eax+8(FP)
	MOVL BX, ebx+12(FP)
	MOVL CX, ecx+16(FP)
	MOVL DX, edx+20(FP)
	RET

// func xgetbv() (eax, edx uint32)  // reads XCR0 (ECX=0)
TEXT ·xgetbv(SB), NOSPLIT, $0-8
	XORL CX, CX
	XGETBV
	MOVL AX, eax+0(FP)
	MOVL DX, edx+4(FP)
	RET
