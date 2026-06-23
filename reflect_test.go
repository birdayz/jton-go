package jton

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestMarshalReflectTypes(t *testing.T) {
	type Inner struct {
		K int `json:"k"`
	}
	type S struct {
		Name    string         `json:"name"`
		Age     int            `json:"age"`
		Skip    string         `json:"-"`
		Opt     string         `json:"opt,omitempty"`
		Ptr     *int           `json:"ptr"`
		Bytes   []byte         `json:"bytes"`
		Tags    []string       `json:"tags"`
		M       map[string]int `json:"m"`
		Inner   Inner          `json:"inner"`
		private int
	}
	n := 7
	s := S{Name: "Alice", Age: 30, Skip: "x", Ptr: &n, Bytes: []byte("hi"),
		Tags: []string{"a", "b"}, M: map[string]int{"z": 1, "a": 2}, Inner: Inner{K: 9}, private: 1}

	got := mustDump(t, s, Options{NoZenGrid: true})
	want := `{"name":"Alice","age":30,"ptr":7,"bytes":"aGk=","tags":["a","b"],"m":{"a":2,"z":1},"inner":{"k":9}}`
	if got != want {
		t.Errorf("struct marshal:\n got=%q\nwant=%q", got, want)
	}
}

func TestMarshalEmbedded(t *testing.T) {
	type Base struct {
		ID int `json:"id"`
	}
	type Derived struct {
		Base
		Name string `json:"name"`
	}
	got := mustDump(t, Derived{Base: Base{ID: 1}, Name: "x"}, Options{NoZenGrid: true})
	if got != `{"id":1,"name":"x"}` {
		t.Errorf("embedded marshal = %q", got)
	}
}

func TestMarshalMapKeys(t *testing.T) {
	got := mustDump(t, map[int]string{3: "c", 1: "a", 2: "b"}, Options{NoZenGrid: true})
	if got != `{"1":"a","2":"b","3":"c"}` {
		t.Errorf("int-key map = %q", got)
	}
}

func TestMarshalTime(t *testing.T) {
	tm := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got := mustDump(t, map[string]any{"t": tm}, Options{NoZenGrid: true})
	want := `{"t":"2026-06-23T12:00:00Z"}`
	if got != want {
		t.Errorf("time marshal = %q want %q", got, want)
	}
}

func TestStructSliceBecomesZenGrid(t *testing.T) {
	type Row struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	got := mustDump(t, []Row{{1, "Alice"}, {2, "Bob"}})
	if got != `[2: id, name; 1, "Alice"; 2, "Bob" ]` {
		t.Errorf("struct slice = %q", got)
	}
}

func TestUnmarshalReflect(t *testing.T) {
	type Inner struct {
		K int `json:"k"`
	}
	type S struct {
		Name  string         `json:"name"`
		Age   int32          `json:"age"`
		F     float32        `json:"f"`
		U     uint16         `json:"u"`
		Ptr   *int           `json:"ptr"`
		Bytes []byte         `json:"bytes"`
		Tags  []string       `json:"tags"`
		M     map[string]int `json:"m"`
		Inner Inner          `json:"inner"`
	}
	src := `{"name":"Alice","age":30,"f":1.5,"u":7,"ptr":9,"bytes":"aGk=","tags":["a","b"],"m":{"a":2,"z":1},"inner":{"k":4}}`
	var s S
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	want := S{Name: "Alice", Age: 30, F: 1.5, U: 7, Ptr: ptr(9), Bytes: []byte("hi"),
		Tags: []string{"a", "b"}, M: map[string]int{"a": 2, "z": 1}, Inner: Inner{K: 4}}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("unmarshal:\n got=%#v\nwant=%#v", s, want)
	}
}

func TestUnmarshalFromZenGrid(t *testing.T) {
	type Row struct {
		ID    int     `json:"id"`
		Name  string  `json:"name"`
		Score float64 `json:"score"`
	}
	var rows []Row
	src := `[2: id, name, score; 1, "Alice", 95.5; 2, "Bob", 87 ]`
	if err := Unmarshal([]byte(src), &rows); err != nil {
		t.Fatal(err)
	}
	want := []Row{{1, "Alice", 95.5}, {2, "Bob", 87}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("unmarshal zen grid = %#v", rows)
	}
}

func TestRoundTripReflect(t *testing.T) {
	type T struct {
		A int               `json:"a"`
		B []float64         `json:"b"`
		C map[string]bool   `json:"c"`
		D map[int]string    `json:"d"`
		E *string           `json:"e"`
		F [][]int           `json:"f"`
		G map[string][]byte `json:"g"`
	}
	s := "hi"
	in := T{A: 5, B: []float64{1.5, 2.5}, C: map[string]bool{"x": true}, D: map[int]string{1: "a"},
		E: &s, F: [][]int{{1, 2}, {3}}, G: map[string][]byte{"k": []byte("v")}}
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip:\n in=%#v\nout=%#v", in, out)
	}
}

func TestMarshalMatchesJSONShape(t *testing.T) {
	// For non-tabular data, jton with NoZenGrid should agree with encoding/json
	// on structure (decode both and compare).
	type S struct {
		A int            `json:"a"`
		B string         `json:"b"`
		C []int          `json:"c"`
		D map[string]int `json:"d"`
	}
	v := S{A: 1, B: "x", C: []int{1, 2, 3}, D: map[string]int{"k": 9}}
	jt, _ := MarshalOptions(v, Options{NoZenGrid: true})
	js, _ := json.Marshal(v)
	var a, b any
	json.Unmarshal(jt, &a)
	json.Unmarshal(js, &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("shape mismatch:\n jton=%s\n json=%s", jt, js)
	}
}

func ptr[T any](v T) *T { return &v }

// ── benchmarks: reflection codec vs encoding/json ──────────────────────────

type benchRow struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Dept   string  `json:"dept"`
	Score  float64 `json:"score"`
	Active bool    `json:"active"`
}

func benchRows(n int) []benchRow {
	r := make([]benchRow, n)
	for i := range r {
		r[i] = benchRow{ID: i, Name: "User" + strconv.Itoa(i), Dept: "Engineering", Score: float64(i) * 1.5, Active: i%2 == 0}
	}
	return r
}

func BenchmarkMarshalStructsJTON(b *testing.B) {
	data := benchRows(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalStructsJSON(b *testing.B) {
	data := benchRows(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStructsJTON(b *testing.B) {
	src, _ := Marshal(benchRows(1000))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out []benchRow
		if err := Unmarshal(src, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStructsJSON(b *testing.B) {
	src, _ := json.Marshal(benchRows(1000))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out []benchRow
		if err := json.Unmarshal(src, &out); err != nil {
			b.Fatal(err)
		}
	}
}
