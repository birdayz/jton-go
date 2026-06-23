package jton

import "math/bits"

// Ryū f64 shortest-decimal digit generation, ported from the reference
// implementation (Ulf Adams, https://github.com/ulfjack/ryu, Apache-2.0 / Boost)
// via the small-table variant of the `ryu` Rust crate. d2d returns the shortest
// (mantissa, exponent) with value == mantissa * 10^exponent; appendFloat formats
// it. This replaces the previous strconv round-trip with one pass.

const (
	doubleMantissaBits = 52
	doubleBias         = 1023
	doublePow5InvBits  = 125
	doublePow5Bits     = 125
)

func log10Pow2(e int32) uint32 { return (uint32(e) * 78913) >> 18 }  // 0 <= e <= 1650
func log10Pow5(e int32) uint32 { return (uint32(e) * 732923) >> 20 } // 0 <= e <= 2620
func pow5bits(e int32) int32   { return int32((uint32(e)*1217359)>>19) + 1 }

func decimalLength17(v uint64) int {
	switch {
	case v >= 10000000000000000:
		return 17
	case v >= 1000000000000000:
		return 16
	case v >= 100000000000000:
		return 15
	case v >= 10000000000000:
		return 14
	case v >= 1000000000000:
		return 13
	case v >= 100000000000:
		return 12
	case v >= 10000000000:
		return 11
	case v >= 1000000000:
		return 10
	case v >= 100000000:
		return 9
	case v >= 10000000:
		return 8
	case v >= 1000000:
		return 7
	case v >= 100000:
		return 6
	case v >= 10000:
		return 5
	case v >= 1000:
		return 4
	case v >= 100:
		return 3
	case v >= 10:
		return 2
	default:
		return 1
	}
}

func pow5Factor(value uint64) uint32 {
	const mInv5 = 14757395258967641293 // 5 * mInv5 == 1 (mod 2^64)
	const nDiv5 = 3689348814741910323  // 2^64 / 5
	count := uint32(0)
	for {
		value *= mInv5
		if value > nDiv5 {
			return count
		}
		count++
	}
}

func multipleOfPowerOf5(value uint64, p uint32) bool { return pow5Factor(value) >= p }

func multipleOfPowerOf2(value uint64, p uint32) bool { return value&((uint64(1)<<p)-1) == 0 }

// mulShift64 returns ((m*mul[lo,hi] >> 64) >> (j-64)).
func mulShift64(m, mulLo, mulHi uint64, j uint32) uint64 {
	b0Hi, _ := bits.Mul64(m, mulLo)
	b2Hi, b2Lo := bits.Mul64(m, mulHi)
	sumLo, c := bits.Add64(b0Hi, b2Lo, 0)
	sumHi := b2Hi + c
	s := j - 64
	return (sumLo >> s) | (sumHi << (64 - s))
}

func mulShiftAll64(m, mulLo, mulHi uint64, j, mmShift uint32) (vr, vp, vm uint64) {
	vp = mulShift64(4*m+2, mulLo, mulHi, j)
	vm = mulShift64(4*m-1-uint64(mmShift), mulLo, mulHi, j)
	vr = mulShift64(4*m, mulLo, mulHi, j)
	return
}

func d2d(ieeeMantissa uint64, ieeeExponent uint32) (mantissa uint64, exponent int32) {
	var e2 int32
	var m2 uint64
	if ieeeExponent == 0 {
		e2 = 1 - doubleBias - doubleMantissaBits - 2
		m2 = ieeeMantissa
	} else {
		e2 = int32(ieeeExponent) - doubleBias - doubleMantissaBits - 2
		m2 = (uint64(1) << doubleMantissaBits) | ieeeMantissa
	}
	acceptBounds := m2&1 == 0
	mv := 4 * m2
	mmShift := uint32(0)
	if ieeeMantissa != 0 || ieeeExponent <= 1 {
		mmShift = 1
	}

	var vr, vp, vm uint64
	var e10 int32
	vmIsTZ, vrIsTZ := false, false

	if e2 >= 0 {
		q := log10Pow2(e2)
		if e2 > 3 {
			q--
		}
		e10 = int32(q)
		k := doublePow5InvBits + pow5bits(int32(q)) - 1
		i := -e2 + int32(q) + k
		mulLo, mulHi := computeInvPow5(q)
		vr, vp, vm = mulShiftAll64(m2, mulLo, mulHi, uint32(i), mmShift)
		if q <= 21 {
			mvMod5 := uint32(mv) - 5*uint32(mv/5)
			if mvMod5 == 0 {
				vrIsTZ = multipleOfPowerOf5(mv, q)
			} else if acceptBounds {
				vmIsTZ = multipleOfPowerOf5(mv-1-uint64(mmShift), q)
			} else if multipleOfPowerOf5(mv+2, q) {
				vp--
			}
		}
	} else {
		q := log10Pow5(-e2)
		if -e2 > 1 {
			q--
		}
		e10 = int32(q) + e2
		i := -e2 - int32(q)
		k := pow5bits(i) - doublePow5Bits
		j := int32(q) - k
		mulLo, mulHi := computePow5(uint32(i))
		vr, vp, vm = mulShiftAll64(m2, mulLo, mulHi, uint32(j), mmShift)
		if q <= 1 {
			vrIsTZ = true
			if acceptBounds {
				vmIsTZ = mmShift == 1
			} else {
				vp--
			}
		} else if q < 63 {
			vrIsTZ = multipleOfPowerOf2(mv, q)
		}
	}

	var removed int32
	var lastRemoved uint8
	var output uint64
	if vmIsTZ || vrIsTZ {
		for {
			vpd := vp / 10
			vmd := vm / 10
			if vpd <= vmd {
				break
			}
			vmMod10 := uint32(vm) - 10*uint32(vmd)
			vrd := vr / 10
			vrMod10 := uint32(vr) - 10*uint32(vrd)
			if vmMod10 != 0 {
				vmIsTZ = false
			}
			if lastRemoved != 0 {
				vrIsTZ = false
			}
			lastRemoved = uint8(vrMod10)
			vr, vp, vm = vrd, vpd, vmd
			removed++
		}
		if vmIsTZ {
			for {
				vmd := vm / 10
				vmMod10 := uint32(vm) - 10*uint32(vmd)
				if vmMod10 != 0 {
					break
				}
				vpd := vp / 10
				vrd := vr / 10
				vrMod10 := uint32(vr) - 10*uint32(vrd)
				if lastRemoved != 0 {
					vrIsTZ = false
				}
				lastRemoved = uint8(vrMod10)
				vr, vp, vm = vrd, vpd, vmd
				removed++
			}
		}
		if vrIsTZ && lastRemoved == 5 && vr%2 == 0 {
			lastRemoved = 4
		}
		output = vr
		if (vr == vm && (!acceptBounds || !vmIsTZ)) || lastRemoved >= 5 {
			output++
		}
	} else {
		roundUp := false
		vpd100 := vp / 100
		vmd100 := vm / 100
		if vpd100 > vmd100 {
			vrd100 := vr / 100
			vrMod100 := uint32(vr) - 100*uint32(vrd100)
			roundUp = vrMod100 >= 50
			vr, vp, vm = vrd100, vpd100, vmd100
			removed += 2
		}
		for {
			vpd := vp / 10
			vmd := vm / 10
			if vpd <= vmd {
				break
			}
			vrd := vr / 10
			vrMod10 := uint32(vr) - 10*uint32(vrd)
			roundUp = vrMod10 >= 5
			vr, vp, vm = vrd, vpd, vmd
			removed++
		}
		output = vr
		if vr == vm || roundUp {
			output++
		}
	}
	return output, e10 + removed
}

// ── small tables (computed multipliers) ─────────────────────────────────────

var doublePow5InvSplit2 = [15][2]uint64{
	{1, 2305843009213693952}, {5955668970331000884, 1784059615882449851},
	{8982663654677661702, 1380349269358112757}, {7286864317269821294, 2135987035920910082},
	{7005857020398200553, 1652639921975621497}, {17965325103354776697, 1278668206209430417},
	{8928596168509315048, 1978643211784836272}, {10075671573058298858, 1530901034580419511},
	{597001226353042382, 1184477304306571148}, {1527430471115325346, 1832889850782397517},
	{12533209867169019542, 1418129833677084982}, {5577825024675947042, 2194449627517475473},
	{11006974540203867551, 1697873161311732311}, {10313493231639821582, 1313665730009899186},
	{12701016819766672773, 2032799256770390445},
}

var pow5InvOffsets = [19]uint32{
	0x54544554, 0x04055545, 0x10041000, 0x00400414, 0x40010000, 0x41155555, 0x00000454, 0x00010044,
	0x40000000, 0x44000041, 0x50454450, 0x55550054, 0x51655554, 0x40004000, 0x01000001, 0x00010500,
	0x51515411, 0x05555554, 0x00000000,
}

var doublePow5Split2 = [13][2]uint64{
	{0, 1152921504606846976}, {0, 1490116119384765625},
	{1032610780636961552, 1925929944387235853}, {7910200175544436838, 1244603055572228341},
	{16941905809032713930, 1608611746708759036}, {13024893955298202172, 2079081953128979843},
	{6607496772837067824, 1343575221513417750}, {17332926989895652603, 1736530273035216783},
	{13037379183483547984, 2244412773384604712}, {1605989338741628675, 1450417759929778918},
	{9630225068416591280, 1874621017369538693}, {665883850346957067, 1211445438634777304},
	{14931890668723713708, 1565756531257009982},
}

var pow5Offsets = [21]uint32{
	0x00000000, 0x00000000, 0x00000000, 0x00000000, 0x40000000, 0x59695995, 0x55545555, 0x56555515,
	0x41150504, 0x40555410, 0x44555145, 0x44504540, 0x45555550, 0x40004000, 0x96440440, 0x55565565,
	0x54454045, 0x40154151, 0x55559155, 0x51405555, 0x00000105,
}

var doublePow5Table = [26]uint64{
	1, 5, 25, 125, 625, 3125, 15625, 78125, 390625, 1953125, 9765625, 48828125, 244140625,
	1220703125, 6103515625, 30517578125, 152587890625, 762939453125, 3814697265625, 19073486328125,
	95367431640625, 476837158203125, 2384185791015625, 11920928955078125, 59604644775390625,
	298023223876953125,
}

func computePow5(i uint32) (lo, hi uint64) {
	base := i / 26
	base2 := base * 26
	offset := i - base2
	mul := doublePow5Split2[base]
	if offset == 0 {
		return mul[0], mul[1]
	}
	m := doublePow5Table[offset]
	b0Hi, b0Lo := bits.Mul64(m, mul[0])
	b2Hi, b2Lo := bits.Mul64(m, mul[1])
	delta := uint(pow5bits(int32(i)) - pow5bits(int32(base2)))
	rLo, rHi := shiftRightAddLeft(b0Hi, b0Lo, b2Hi, b2Lo, delta)
	corr := uint64((pow5Offsets[i/16] >> ((i % 16) << 1)) & 3)
	rLo, c := bits.Add64(rLo, corr, 0)
	rHi += c
	return rLo, rHi
}

func computeInvPow5(i uint32) (lo, hi uint64) {
	base := (i + 25) / 26
	base2 := base * 26
	offset := base2 - i
	mul := doublePow5InvSplit2[base]
	if offset == 0 {
		return mul[0], mul[1]
	}
	m := doublePow5Table[offset]
	b0Hi, b0Lo := bits.Mul64(m, mul[0]-1)
	b2Hi, b2Lo := bits.Mul64(m, mul[1])
	delta := uint(pow5bits(int32(base2)) - pow5bits(int32(i)))
	rLo, rHi := shiftRightAddLeft(b0Hi, b0Lo, b2Hi, b2Lo, delta)
	corr := uint64(1) + uint64((pow5InvOffsets[i/16]>>((i%16)<<1))&3)
	rLo, c := bits.Add64(rLo, corr, 0)
	rHi += c
	return rLo, rHi
}

// shiftRightAddLeft computes (b0 >> delta) + (b2 << (64-delta)) over 128-bit
// values b0 = (b0Hi:b0Lo) and b2 = (b2Hi:b2Lo), returning the low 128 bits.
// delta is in (0, 64).
func shiftRightAddLeft(b0Hi, b0Lo, b2Hi, b2Lo uint64, delta uint) (lo, hi uint64) {
	r0Lo := b0Lo>>delta | b0Hi<<(64-delta)
	r0Hi := b0Hi >> delta
	s2 := 64 - delta
	r2Lo := b2Lo << s2
	r2Hi := b2Hi<<s2 | b2Lo>>(64-s2)
	lo, c := bits.Add64(r0Lo, r2Lo, 0)
	hi = r0Hi + r2Hi + c
	return lo, hi
}
