package jton

import (
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

// ── helpers ────────────────────────────────────────────────────────────────

func mustDump(t *testing.T, v any, opts ...Options) string {
	t.Helper()
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	s, err := DumpsOptions(v, o)
	if err != nil {
		t.Fatalf("Dumps(%v): %v", v, err)
	}
	return s
}

func mustLoad(t *testing.T, s string) any {
	t.Helper()
	v, err := Loads(s)
	if err != nil {
		t.Fatalf("Loads(%q): %v", s, err)
	}
	return v
}

// eqVal compares two decoded values with Python-style semantics: objects are
// compared by content regardless of key order, NaN equals NaN for convenience.
func eqVal(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false
		}
		if math.IsNaN(av) && math.IsNaN(bv) {
			return true
		}
		return av == bv
	case *big.Int:
		bv, ok := b.(*big.Int)
		return ok && av.Cmp(bv) == 0
	case *Object:
		bv, ok := b.(*Object)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for i := 0; i < av.Len(); i++ {
			k, val := av.At(i)
			other, present := bv.Get(k)
			if !present || !eqVal(val, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !eqVal(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func i(n int64) int64 { return n }

// ── TestDumpsBasic ─────────────────────────────────────────────────────────

func TestDumpsBasic(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{Obj("name", "Alice", "age", i(30)), `{"name":"Alice","age":30}`},
		{[]any{i(1), i(2), i(3)}, "[1,2,3]"},
		{nil, "null"},
		{true, "true"},
		{false, "false"},
		{i(42), "42"},
		{i(-999), "-999"},
		{i(0), "0"},
		{3.14, "3.14"},
		{1.0, "1.0"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{math.NaN(), "NaN"},
		{`say "hi"`, `"say \"hi\""`},
		{"new\nline", `"new\nline"`},
		{"tab\there", `"tab\there"`},
		{Obj(), "{}"},
		{[]any{}, "[]"},
	}
	for _, c := range cases {
		if got := mustDump(t, c.val); got != c.want {
			t.Errorf("Dumps(%#v) = %q, want %q", c.val, got, c.want)
		}
	}
	// nested dict round-trips through standard JSON
	if got := mustDump(t, Obj("a", Obj("b", i(1)))); got != `{"a":{"b":1}}` {
		t.Errorf("nested dict = %q", got)
	}
}

// ── TestDumpsUnquotedKeys ──────────────────────────────────────────────────

func TestDumpsUnquotedKeys(t *testing.T) {
	uq := Options{UnquotedKeys: true}
	cases := []struct {
		val  any
		want string
	}{
		{Obj("name", "Alice"), `{name:"Alice"}`},
		{Obj("my-key", i(1)), `{my-key:1}`}, // hyphen is a valid JTON identifier
		{Obj("123", i(1)), `{"123":1}`},     // digit-initial stays quoted
		{Obj("my key", i(1)), `{"my key":1}`},
	}
	for _, c := range cases {
		if got := mustDump(t, c.val, uq); got != c.want {
			t.Errorf("Dumps(%#v, unquoted) = %q, want %q", c.val, got, c.want)
		}
	}
}

// ── TestDumpsIndent ────────────────────────────────────────────────────────

func TestDumpsIndent(t *testing.T) {
	got := mustDump(t, Obj("a", i(1)), Options{Indent: 2})
	if !strings.Contains(got, `"a": 1`) || !strings.Contains(got, "\n") {
		t.Errorf("indent object = %q", got)
	}
	got = mustDump(t, []any{i(1), i(2), i(3)}, Options{Indent: 2})
	if !strings.Contains(got, "\n") {
		t.Errorf("indent array = %q", got)
	}
}

// ── TestZenGridDumps ───────────────────────────────────────────────────────

func isZenGrid(s string) bool {
	if len(s) == 0 || s[0] != '[' {
		return false
	}
	i := 1
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i < len(s) && s[i] == ':'
}

func TestZenGridDumps(t *testing.T) {
	two := []any{Obj("id", i(1), "name", "Alice"), Obj("id", i(2), "name", "Bob")}
	got := mustDump(t, two)
	if !isZenGrid(got) {
		t.Errorf("two-row table not zen grid: %q", got)
	}
	want := `[2: id, name; 1, "Alice"; 2, "Bob" ]`
	if got != want {
		t.Errorf("two-row = %q, want %q", got, want)
	}

	three := []any{
		Obj("id", i(1), "name", "Alice", "score", i(95)),
		Obj("id", i(2), "name", "Bob", "score", i(87)),
		Obj("id", i(3), "name", "Carol", "score", i(91)),
	}
	got = mustDump(t, three)
	for _, h := range []string{"id", "name", "score"} {
		if strings.Count(got, h) != 1 {
			t.Errorf("header %q should appear once in %q", h, got)
		}
	}

	// zen_grid disabled -> JSON array
	got = mustDump(t, two, Options{NoZenGrid: true})
	if !strings.HasPrefix(got, "[{") {
		t.Errorf("NoZenGrid should give JSON array: %q", got)
	}

	// single item -> not a table
	got = mustDump(t, []any{Obj("id", i(1), "name", "Alice")})
	if isZenGrid(got) {
		t.Errorf("single-item array should not be zen grid: %q", got)
	}

	// mixed types -> not a table
	got = mustDump(t, []any{i(1), "hello", nil})
	if got != `[1,"hello",null]` {
		t.Errorf("mixed array = %q", got)
	}

	// token savings
	data := make([]any, 20)
	for k := range data {
		data[k] = Obj("employee_id", int64(k), "first_name", "Name", "department", "Engineering")
	}
	zen := mustDump(t, data)
	js := mustDump(t, data, Options{NoZenGrid: true})
	if len(zen) >= len(js) {
		t.Errorf("zen grid (%d) should be shorter than JSON (%d)", len(zen), len(js))
	}
}

// ── TestZenGridLoads ───────────────────────────────────────────────────────

func TestZenGridLoads(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`[: name, age; "Alice", 30; "Bob", 25 ]`, []any{Obj("name", "Alice", "age", i(30)), Obj("name", "Bob", "age", i(25))}},
		{`[:]`, []any{}},
		{`[: x, y; 1, 2 ]`, []any{Obj("x", i(1), "y", i(2))}},
		{`[: id, meta; 1, {"k": 10}; 2, {"k": 20} ]`, []any{Obj("id", i(1), "meta", Obj("k", i(10))), Obj("id", i(2), "meta", Obj("k", i(20)))}},
		{`[: id, tags; 1, ["a","b"]; 2, ["c"] ]`, []any{Obj("id", i(1), "tags", []any{"a", "b"}), Obj("id", i(2), "tags", []any{"c"})}},
		{`[: id, val; 1, null; 2, null ]`, []any{Obj("id", i(1), "val", nil), Obj("id", i(2), "val", nil)}},
		{`[: name, active; "Alice", true; "Bob", false ]`, []any{Obj("name", "Alice", "active", true), Obj("name", "Bob", "active", false)}},
		{`[2: a, b; 1, ; 3, 4 ]`, []any{Obj("a", i(1), "b", nil), Obj("a", i(3), "b", i(4))}}, // missing cell -> null
	}
	for _, c := range cases {
		got := mustLoad(t, c.src)
		if !eqVal(got, c.want) {
			t.Errorf("Loads(%q) =\n  %#v\nwant\n  %#v", c.src, got, c.want)
		}
	}
	// float cells
	got := mustLoad(t, `[: x, y; 1.5, 2.7; 3.0, 4.1 ]`).([]any)
	if x := got[0].(*Object); true {
		v, _ := x.Get("x")
		if math.Abs(v.(float64)-1.5) > 1e-9 {
			t.Errorf("float cell x = %v", v)
		}
	}
}

// ── TestRoundTrip ──────────────────────────────────────────────────────────

func TestRoundTrip(t *testing.T) {
	cases := []any{
		[]any{Obj("id", i(1), "active", true), Obj("id", i(2), "active", false), Obj("id", i(3), "active", true)},
		Obj("users", []any{Obj("id", i(0), "active", true), Obj("id", i(1), "active", true)}),
		[]any{nil, true, false, i(42), i(-7), 3.14, "hello"},
		Obj(),
		[]any{},
		[]any{Obj("name", "日本語", "val", "café")},
	}
	for _, c := range cases {
		s := mustDump(t, c)
		got := mustLoad(t, s)
		if !eqVal(got, c) {
			t.Errorf("round-trip mismatch for %q:\n got=%#v\nwant=%#v", s, got, c)
		}
	}
	// nested cells force JSON fallback (not a zen grid)
	nested := []any{Obj("id", i(1), "meta", Obj("k", i(1))), Obj("id", i(2), "meta", Obj("k", i(2)))}
	s := mustDump(t, nested)
	if isZenGrid(s) {
		t.Errorf("nested cells must fall back to JSON, got %q", s)
	}
	if got := mustLoad(t, s); !eqVal(got, nested) {
		t.Errorf("nested round-trip failed")
	}
	// structural string cells force JSON fallback
	structural := []any{Obj("id", i(1), "s", "a,b;c"), Obj("id", i(2), "s", "x:y")}
	s = mustDump(t, structural)
	if isZenGrid(s) {
		t.Errorf("structural string cells must fall back to JSON, got %q", s)
	}
	// special floats
	sp := Obj("inf", math.Inf(1), "neg", math.Inf(-1))
	got := mustLoad(t, mustDump(t, sp)).(*Object)
	if v, _ := got.Get("inf"); !math.IsInf(v.(float64), 1) {
		t.Errorf("inf round-trip failed")
	}
}

// ── TestBareStrings / TestImplicitNull ─────────────────────────────────────

func TestBareStrings(t *testing.T) {
	data := []any{Obj("name", "Alice", "dept", "Eng"), Obj("name", "Bob", "dept", "Mkt")}
	got := mustDump(t, data, Options{BareStrings: true})
	if strings.Contains(got, `"Alice"`) {
		t.Errorf("bare strings should drop quotes: %q", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("bare strings should keep value: %q", got)
	}
	// non-identifier stays quoted
	data2 := []any{Obj("name", "Alice Smith"), Obj("name", "Bob Jones")}
	got = mustDump(t, data2, Options{BareStrings: true})
	if !strings.Contains(got, `"Alice Smith"`) {
		t.Errorf("strings with spaces must stay quoted: %q", got)
	}
	// round-trip
	data3 := []any{Obj("status", "active", "role", "admin"), Obj("status", "idle", "role", "user")}
	rt := mustLoad(t, mustDump(t, data3, Options{BareStrings: true}))
	if !eqVal(rt, data3) {
		t.Errorf("bare strings round-trip failed")
	}
}

func TestImplicitNull(t *testing.T) {
	data := []any{Obj("id", i(1), "val", nil), Obj("id", i(2), "val", nil)}
	got := mustDump(t, data, Options{ImplicitNull: true})
	if strings.Contains(got, "null") {
		t.Errorf("implicit null should omit 'null': %q", got)
	}
	rt := mustLoad(t, got).([]any)
	first := rt[0].(*Object)
	if v, _ := first.Get("id"); v.(int64) != 1 {
		t.Errorf("implicit null id = %v", v)
	}
	if v, _ := first.Get("val"); v != nil {
		t.Errorf("implicit null val should be nil, got %v", v)
	}
}

// ── TestRowCount / TestMultilineZen / TestDelimiters ───────────────────────

func TestRowCount(t *testing.T) {
	data := []any{Obj("id", i(1)), Obj("id", i(2)), Obj("id", i(3))}
	if got := mustDump(t, data); !strings.HasPrefix(got, "[3:") {
		t.Errorf("default row_count should prefix [3:, got %q", got)
	}
	if got := mustDump(t, data, Options{NoRowCount: true}); !strings.HasPrefix(got, "[:") {
		t.Errorf("NoRowCount should prefix [:, got %q", got)
	}
}

func TestMultilineZen(t *testing.T) {
	data := []any{Obj("id", i(1), "name", "A"), Obj("id", i(2), "name", "B")}
	got := mustDump(t, data, Options{MultilineZen: true})
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "[2]{") || !strings.HasSuffix(lines[0], "}:") {
		t.Errorf("multiline header = %q", lines[0])
	}
	if len(lines) != 3 { // header + 2 rows
		t.Errorf("multiline lines = %d (%q)", len(lines), got)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("multiline row not indented: %q", l)
		}
	}
}

func TestDelimiters(t *testing.T) {
	data := []any{Obj("a", i(1), "b", i(2)), Obj("a", i(3), "b", i(4))}
	if got := mustDump(t, data); !strings.Contains(got, ", ") {
		t.Errorf("comma delimiter: %q", got)
	}
	tab := mustDump(t, data, Options{Delimiter: DelimiterTab})
	if !strings.Contains(tab, "\t") {
		t.Errorf("tab delimiter: %q", tab)
	}
	if !eqVal(mustLoad(t, tab), data) {
		t.Errorf("tab round-trip failed")
	}
	pipe := mustDump(t, data, Options{Delimiter: DelimiterPipe})
	if !strings.Contains(pipe, " | ") {
		t.Errorf("pipe delimiter: %q", pipe)
	}
	if !eqVal(mustLoad(t, pipe), data) {
		t.Errorf("pipe round-trip failed")
	}
}

// ── JSON compatibility ─────────────────────────────────────────────────────

func TestJSONPrimitives(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"null", nil}, {"true", true}, {"false", false},
		{"0", i(0)}, {"42", i(42)}, {"-17", i(-17)}, {"9223372036854775807", i(9223372036854775807)},
		{"3.14", 3.14}, {"-0.5", -0.5}, {"0.0", 0.0},
		{"1e10", 1e10}, {"1.5e-5", 1.5e-5}, {"-2.3e+7", -2.3e+7},
	}
	for _, c := range cases {
		if got := mustLoad(t, c.src); !eqVal(got, c.want) {
			t.Errorf("Loads(%q) = %#v, want %#v", c.src, got, c.want)
		}
	}
	for _, s := range []string{"Infinity", "-Infinity", "NaN", "Inf", "-Inf"} {
		v := mustLoad(t, s).(float64)
		switch s {
		case "NaN":
			if !math.IsNaN(v) {
				t.Errorf("Loads(%q) not NaN", s)
			}
		case "-Infinity", "-Inf":
			if !math.IsInf(v, -1) {
				t.Errorf("Loads(%q) not -Inf", s)
			}
		default:
			if !math.IsInf(v, 1) {
				t.Errorf("Loads(%q) not +Inf", s)
			}
		}
	}
}

func TestJSONStringsAndEscapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`""`, ""}, {`"hello"`, "hello"},
		{`"\n"`, "\n"}, {`"\t"`, "\t"}, {`"\r"`, "\r"}, {`"\b"`, "\b"}, {`"\f"`, "\f"},
		{`"\\"`, "\\"}, {`"\""`, "\""}, {`"\/"`, "/"},
		{`"A"`, "A"}, {`"❤"`, "❤"}, {`"Hello"`, "Hello"},
		{`"😀"`, "😀"}, // surrogate pair
	}
	for _, c := range cases {
		if got := mustLoad(t, c.src); got != c.want {
			t.Errorf("Loads(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestJTONExtensionsParse(t *testing.T) {
	if got := mustLoad(t, `{name: "Alice", age: 30}`); !eqVal(got, Obj("name", "Alice", "age", i(30))) {
		t.Errorf("unquoted keys: %#v", got)
	}
	if got := mustLoad(t, "{\"x\": 1, // comment\n \"y\": 2}"); !eqVal(got, Obj("x", i(1), "y", i(2))) {
		t.Errorf("line comment: %#v", got)
	}
	if got := mustLoad(t, "{\"x\": /* c */ 1, /* m\nl */ \"y\": 2}"); !eqVal(got, Obj("x", i(1), "y", i(2))) {
		t.Errorf("block comment: %#v", got)
	}
	// trailing commas
	if got := mustLoad(t, "[1, 2, 3,]"); !eqVal(got, []any{i(1), i(2), i(3)}) {
		t.Errorf("trailing comma array: %#v", got)
	}
	if got := mustLoad(t, `{"a": 1,}`); !eqVal(got, Obj("a", i(1))) {
		t.Errorf("trailing comma object: %#v", got)
	}
}

func TestErrorHandling(t *testing.T) {
	for _, s := range []string{`{invalid}`, `{"key": "value"`, `[1, 2, 3`, `{"key": "value}`, ``, `nope`, `[: a, b ]`} {
		if _, err := Loads(s); err == nil {
			t.Errorf("Loads(%q) should error", s)
		}
	}
}

func TestBigIntegers(t *testing.T) {
	v := mustLoad(t, "123456789012345678901234567890")
	bi, ok := v.(*big.Int)
	if !ok || bi.String() != "123456789012345678901234567890" {
		t.Errorf("big int = %#v", v)
	}
	if got := mustDump(t, bi); got != "123456789012345678901234567890" {
		t.Errorf("big int dump = %q", got)
	}
	// i64 min/max
	if v := mustLoad(t, "-9223372036854775808"); v.(int64) != math.MinInt64 {
		t.Errorf("i64 min = %v", v)
	}
}

func TestUnmarshalStruct(t *testing.T) {
	type Row struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var rows []Row
	if err := Unmarshal([]byte(`[2: id, name; 1, "Alice"; 2, "Bob" ]`), &rows); err != nil {
		t.Fatal(err)
	}
	want := []Row{{1, "Alice"}, {2, "Bob"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("Unmarshal struct = %#v, want %#v", rows, want)
	}
}

func TestMarshalStruct(t *testing.T) {
	type Row struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	got := mustDump(t, []Row{{1, "Alice"}, {2, "Bob"}})
	want := `[2: id, name; 1, "Alice"; 2, "Bob" ]`
	if got != want {
		t.Errorf("Marshal []struct = %q, want %q", got, want)
	}
}

// TestZenGridStructuralHeader guards a case the reference gets wrong: a Zen Grid
// header (object key) containing a structural character. The reference's dumps
// emits it but its SIMD-index loads cannot read it back; our recursive-descent
// parser round-trips it correctly.
func TestZenGridStructuralHeader(t *testing.T) {
	for _, key := range []string{"a]b", "x;y", "p,q", "{z}", "a:b", `q"r`} {
		v := []any{Obj(key, int64(1)), Obj(key, int64(2))}
		d := mustDump(t, v)
		if !isZenGrid(d) {
			t.Errorf("expected zen grid for key %q, got %q", key, d)
		}
		back := mustLoad(t, d)
		if !eqVal(back, v) {
			t.Errorf("round-trip failed for key %q: %q -> %#v", key, d, back)
		}
	}
}
