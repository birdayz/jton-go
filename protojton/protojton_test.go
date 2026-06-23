package protojton_test

import (
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/birdayz/jton-go"
	"github.com/birdayz/jton-go/protojton"
	"github.com/birdayz/jton-go/protojton/internal/testpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func fullAllTypes() *testpb.AllTypes {
	return &testpb.AllTypes{
		B: true, I32: -7, I64: -1 << 40, U32: 42, U64: math.MaxUint64,
		S32: -123, S64: 1 << 40, F32: 7, F64: 1 << 50, Sf32: -9, Sf64: -(1 << 41),
		Fl: 1.5, Db: 3.141592653589793, Str: "héllo, 世界",
		By: []byte{0, 1, 2, 250, 255}, Color: testpb.Color_GREEN,
		Inner:    &testpb.Inner{Id: 9, Label: "deep"},
		RepI32:   []int32{1, 2, 3},
		RepStr:   []string{"a", "b"},
		RepInner: []*testpb.Inner{{Id: 1, Label: "x"}, {Id: 2, Label: "y"}},
		MapSi:    map[string]int32{"a": 1, "b": 2},
		MapIs:    map[int32]string{1: "one", 2: "two"},
		MapSmsg:  map[string]*testpb.Inner{"k": {Id: 5, Label: "m"}},
		RepEnum:  []testpb.Color{testpb.Color_RED, testpb.Color_BLUE},
		Choice:   &testpb.AllTypes_ChoiceInt{ChoiceInt: 77},
		OptI32:   proto.Int32(0), // explicit presence, set to zero
	}
}

func TestRoundTripAllTypes(t *testing.T) {
	in := fullAllTypes()
	data, err := protojton.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("JTON: %s", data)
	out := &testpb.AllTypes{}
	if err := protojton.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%v\nout=%v", in, out)
	}
}

func TestOneofVariants(t *testing.T) {
	cases := []*testpb.AllTypes{
		{Choice: &testpb.AllTypes_ChoiceStr{ChoiceStr: "hi"}},
		{Choice: &testpb.AllTypes_ChoiceInt{ChoiceInt: 5}},
		{Choice: &testpb.AllTypes_ChoiceMsg{ChoiceMsg: &testpb.Inner{Id: 1}}},
		{}, // no oneof set
	}
	for _, in := range cases {
		data, err := protojton.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		out := &testpb.AllTypes{}
		if err := protojton.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(in, out) {
			t.Errorf("oneof round-trip mismatch: %s\n in=%v out=%v", data, in, out)
		}
	}
}

func TestRowZenGrid(t *testing.T) {
	rows := []*testpb.Row{
		{Id: 1, Name: "Alice", Dept: "Eng", Score: 95, Active: true},
		{Id: 2, Name: "Bob", Dept: "Mkt", Score: 87, Active: false},
		{Id: 3, Name: "Carol", Dept: "Eng", Score: 92, Active: true},
	}
	data, err := protojton.MarshalList(rows, protojton.MarshalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `[3: id, name, dept, score, active; 1, "Alice", "Eng", 95.0, true; 2, "Bob", "Mkt", 87.0, false; 3, "Carol", "Eng", 92.0, true ]`
	if got != want {
		t.Fatalf("zen grid:\n got=%q\nwant=%q", got, want)
	}
	// Round-trip the list back.
	msgs, err := protojton.UnmarshalList(data, func() proto.Message { return &testpb.Row{} }, protojton.UnmarshalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != len(rows) {
		t.Fatalf("got %d rows", len(msgs))
	}
	for i := range rows {
		if !proto.Equal(rows[i], msgs[i]) {
			t.Errorf("row %d mismatch: %v vs %v", i, rows[i], msgs[i])
		}
	}
}

func TestEnumsAsInts(t *testing.T) {
	in := &testpb.AllTypes{Color: testpb.Color_BLUE}
	data, err := protojton.MarshalOptions{EnumsAsInts: true}.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"color":3`) {
		t.Errorf("EnumsAsInts: %s", data)
	}
}

func TestJSONNames(t *testing.T) {
	in := &testpb.AllTypes{RepI32: []int32{1}}
	data, err := protojton.MarshalOptions{UseJSONNames: true}.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "repI32") {
		t.Errorf("JSON names: %s", data)
	}
}

func TestUnknownFieldError(t *testing.T) {
	err := protojton.Unmarshal([]byte(`{nope: 1}`), &testpb.Row{})
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
	if err := (protojton.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(`{nope: 1}`), &testpb.Row{}); err != nil {
		t.Fatalf("DiscardUnknown should ignore: %v", err)
	}
}

func TestUint64BigValue(t *testing.T) {
	in := &testpb.AllTypes{U64: math.MaxUint64}
	data, err := protojton.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strconv.FormatUint(math.MaxUint64, 10)) {
		t.Errorf("uint64 max not exact: %s", data)
	}
	out := &testpb.AllTypes{}
	if err := protojton.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
	if out.U64 != math.MaxUint64 {
		t.Errorf("u64 = %d", out.U64)
	}
}

func TestEmptyAndNil(t *testing.T) {
	// Default emits all implicit-presence fields (homogeneous schema for the Zen
	// Grid), so an empty message still round-trips to an empty message.
	data, err := protojton.Marshal(&testpb.AllTypes{})
	if err != nil {
		t.Fatal(err)
	}
	out := &testpb.AllTypes{}
	if err := protojton.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&testpb.AllTypes{}, out) {
		t.Errorf("empty round-trip mismatch: %v", out)
	}
	// OmitDefaults drops the zero fields entirely.
	bare, err := protojton.MarshalOptions{OmitDefaults: true}.Marshal(&testpb.Row{})
	if err != nil {
		t.Fatal(err)
	}
	if string(bare) != "{}" {
		t.Errorf("OmitDefaults empty Row = %q, want {}", bare)
	}
}

// ── fuzz: arbitrary wire bytes -> message -> JTON round-trip ────────────────

func FuzzProtoRoundTrip(f *testing.F) {
	for _, m := range []*testpb.AllTypes{fullAllTypes(), {}, {I32: 1}, {Str: "x"}} {
		b, _ := proto.Marshal(m)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		// Decode the arbitrary wire bytes, dropping unknown fields (mismatched
		// wire types). JTON, like protojson, does not preserve unknown fields, and
		// proto.Equal would compare them.
		seed := &testpb.AllTypes{}
		if (proto.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(wire, seed) != nil {
			return // not a valid AllTypes wire encoding
		}
		data, err := protojton.Marshal(seed)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out := &testpb.AllTypes{}
		if err := protojton.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if !proto.Equal(seed, out) {
			t.Fatalf("round-trip mismatch\n jton=%s\n in=%v\nout=%v", data, seed, out)
		}
	})
}

// ── randomized round-trip (runs without -fuzz) ──────────────────────────────

func TestRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 3000; i++ {
		m := randomAllTypes(rng)
		data, err := protojton.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out := &testpb.AllTypes{}
		if err := protojton.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if !proto.Equal(m, out) {
			t.Fatalf("mismatch\n jton=%s\n in=%v\nout=%v", data, m, out)
		}
	}
}

func randomAllTypes(rng *rand.Rand) *testpb.AllTypes {
	m := &testpb.AllTypes{}
	if rng.Intn(2) == 0 {
		m.B = true
	}
	m.I32 = rng.Int31() - (1 << 30)
	m.I64 = rng.Int63() - (1 << 62)
	m.U32 = rng.Uint32()
	m.U64 = rng.Uint64()
	m.Fl = float32(rng.NormFloat64())
	m.Db = rng.NormFloat64()
	m.Str = randText(rng)
	m.By = []byte(randText(rng))
	m.Color = testpb.Color(rng.Intn(4))
	if rng.Intn(2) == 0 {
		m.Inner = &testpb.Inner{Id: rng.Int31n(100), Label: randText(rng)}
	}
	for n := rng.Intn(4); n > 0; n-- {
		m.RepI32 = append(m.RepI32, rng.Int31n(1000))
		m.RepStr = append(m.RepStr, randText(rng))
		m.RepInner = append(m.RepInner, &testpb.Inner{Id: rng.Int31n(10), Label: randText(rng)})
		m.RepEnum = append(m.RepEnum, testpb.Color(rng.Intn(4)))
	}
	if rng.Intn(2) == 0 {
		m.MapSi = map[string]int32{randText(rng): rng.Int31n(100), randText(rng): rng.Int31n(100)}
	}
	if rng.Intn(2) == 0 {
		m.MapIs = map[int32]string{rng.Int31n(100): randText(rng)}
	}
	switch rng.Intn(4) {
	case 0:
		m.Choice = &testpb.AllTypes_ChoiceStr{ChoiceStr: randText(rng)}
	case 1:
		m.Choice = &testpb.AllTypes_ChoiceInt{ChoiceInt: rng.Int31n(100)}
	case 2:
		m.Choice = &testpb.AllTypes_ChoiceMsg{ChoiceMsg: &testpb.Inner{Id: rng.Int31n(10)}}
	}
	if rng.Intn(2) == 0 {
		m.OptI32 = proto.Int32(rng.Int31n(50))
	}
	return m
}

func randText(rng *rand.Rand) string {
	alphabet := []rune("abc 世界,;\"\\")
	n := rng.Intn(6)
	r := make([]rune, n)
	for i := range r {
		r[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(r)
}

// ── comparison + benchmarks vs protojson / proto wire ───────────────────────

func benchRowSlice(n int) []*testpb.Row {
	r := make([]*testpb.Row, n)
	for i := range r {
		r[i] = &testpb.Row{Id: int64(i), Name: "User" + strconv.Itoa(i), Dept: "Engineering", Score: float64(i) * 1.5, Active: i%2 == 0}
	}
	return r
}

func TestSizeComparison(t *testing.T) {
	rows := benchRowSlice(100)
	jt, _ := protojton.MarshalList(rows, protojton.MarshalOptions{})
	jtBare, _ := protojton.MarshalList(rows, protojton.MarshalOptions{JTON: jton.Options{BareStrings: true}})

	// protojson over an equivalent wrapper, and raw proto wire, as size baselines.
	var pjTotal, wireTotal int
	for _, r := range rows {
		b, _ := protojson.Marshal(r)
		pjTotal += len(b)
		w, _ := proto.Marshal(r)
		wireTotal += len(w)
	}
	t.Logf("100 rows: protojton=%d  protojton+bare=%d  protojson(sum)=%d  protowire(sum)=%d",
		len(jt), len(jtBare), pjTotal, wireTotal)
	if len(jt) >= pjTotal {
		t.Errorf("expected protojton (%d) smaller than summed protojson (%d)", len(jt), pjTotal)
	}
}

func BenchmarkMarshalRowsProtojton(b *testing.B) {
	rows := benchRowSlice(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := protojton.MarshalList(rows, protojton.MarshalOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalRowsProtojson(b *testing.B) {
	rows := benchRowSlice(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			if _, err := protojson.Marshal(r); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkUnmarshalRowsProtojton(b *testing.B) {
	data, _ := protojton.MarshalList(benchRowSlice(1000), protojton.MarshalOptions{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := protojton.UnmarshalList(data, func() proto.Message { return &testpb.Row{} }, protojton.UnmarshalOptions{})
		if err != nil {
			b.Fatal(err)
		}
	}
}
