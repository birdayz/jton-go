//go:build amd64

package jton

// AVX2 byte scanners. escapeIndexAVX2 and scanStringAVX2 are implemented in
// simd_amd64.s; cpuid/xgetbv back the one-time AVX2 feature detection.

//go:noescape
func escapeIndexAVX2(p *byte, n int) int

//go:noescape
func scanStringAVX2(p *byte, n int) int

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv() (eax, edx uint32)

var useAVX2 = detectAVX2()

func detectAVX2() bool {
	maxLeaf, _, _, _ := cpuid(0, 0)
	if maxLeaf < 7 {
		return false
	}
	_, _, c1, _ := cpuid(1, 0)
	const osxsave = 1 << 27
	const avx = 1 << 28
	if c1&osxsave == 0 || c1&avx == 0 {
		return false
	}
	xcr0, _ := xgetbv()
	if xcr0&0x6 != 0x6 { // XMM and YMM state must be OS-enabled
		return false
	}
	_, b7, _, _ := cpuid(7, 0)
	return b7&(1<<5) != 0 // AVX2
}

// avx2Threshold is the smallest slice worth dispatching to AVX2. Below it the
// per-call setup and VZEROUPPER outweigh the wide scan.
const avx2Threshold = 32

func escapeIndex(b []byte) int {
	if useAVX2 && len(b) >= avx2Threshold {
		return escapeIndexAVX2(&b[0], len(b))
	}
	return escapeIndexScalar(b)
}

func scanStringBody(b []byte) int {
	if useAVX2 && len(b) >= avx2Threshold {
		return scanStringAVX2(&b[0], len(b))
	}
	return scanStringScalar(b)
}
