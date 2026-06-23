package jton

import (
	"bytes"
	"math"
	"strconv"
	"testing"
)

// FuzzParse asserts the parser never panics on arbitrary bytes, and that for any
// input it accepts, re-encoding is a fixed point (Marshal is idempotent over the
// parsed value) and the value round-trips.
func FuzzParse(f *testing.F) {
	seeds := []string{
		`[3: id, name, score; 1, "Alice", 95; 2, "Bob", 87; 3, "Carol", 92 ]`,
		`{"a":1,"b":[1,2,3],"c":{"d":true}}`,
		`[1,2,3]`, `null`, `Infinity`, `NaN`, `-0.0`, `1e308`, `"é😀"`,
		`{name: "Alice", age: 30} // comment`, `[: a, b; 1, 2; 3, 4 ]`,
		`{"big":123456789012345678901234567890}`, `[2: a,b; x, y ]`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Parse(data)
		if err != nil {
			return // rejecting malformed input is fine
		}
		b1, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal after successful Parse failed: %v\ninput=%q", err, data)
		}
		v2, err := Parse(b1)
		if err != nil {
			t.Fatalf("re-Parse of our own output failed: %v\noutput=%q", err, b1)
		}
		b2, err := Marshal(v2)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("Marshal not idempotent:\n b1=%q\n b2=%q\n input=%q", b1, b2, data)
		}
		if !eqVal(v, v2) {
			t.Fatalf("round-trip value mismatch\n input=%q\n b1=%q", data, b1)
		}
	})
}

// FuzzRyu asserts appendFloat never panics and that its output parses back to the
// exact same float64 bits (round-trip correctness for every double).
func FuzzRyu(f *testing.F) {
	for _, x := range []float64{0, 1, 1.5, 3.14, 1e308, 5e-324, 0.1, 123456789} {
		f.Add(math.Float64bits(x))
	}
	f.Fuzz(func(t *testing.T, bits uint64) {
		x := math.Float64frombits(bits)
		s := formatFloat(x)
		switch {
		case math.IsNaN(x):
			if s != "NaN" {
				t.Fatalf("NaN -> %q", s)
			}
		case math.IsInf(x, 1):
			if s != "Infinity" {
				t.Fatalf("+Inf -> %q", s)
			}
		case math.IsInf(x, -1):
			if s != "-Infinity" {
				t.Fatalf("-Inf -> %q", s)
			}
		default:
			back, err := strconv.ParseFloat(s, 64)
			if err != nil || math.Float64bits(back) != bits {
				t.Fatalf("ryu %v (bits %#x) -> %q -> %v (bits %#x)", x, bits, s, back, math.Float64bits(back))
			}
		}
	})
}
