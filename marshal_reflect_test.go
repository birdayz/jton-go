package jton

import (
	"math/rand"
	"strconv"
	"testing"
)

type xrow struct {
	ID     int
	Name   string  `json:"name"`
	Dept   string  `json:"dept"`
	Score  float64 `json:"score"`
	Active bool    `json:"active"`
	Ratio  float32 `json:"ratio"`
	Big    uint64  `json:"big"`
	Opt    *int    `json:"opt"`
	Raw    []byte  `json:"raw"`
	Skip   string  `json:"-"`
}

// TestStructTableMatchesTree is the safety net: the direct struct-table fast
// path must produce byte-identical output to the value-tree path for every
// option combination, including string values that force a JSON fallback.
func TestStructTableMatchesTree(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	opts := optionMatrix()
	for iter := 0; iter < 4000; iter++ {
		n := rng.Intn(6)
		rows := make([]xrow, n)
		for i := range rows {
			rows[i] = xrow{
				ID: rng.Intn(2000) - 500, Name: randCellString(rng), Dept: randCellString(rng),
				Score: float64(rng.Intn(1000)) / 8, Active: rng.Intn(2) == 0,
				Ratio: float32(rng.Intn(100)) / 3, Big: rng.Uint64(),
			}
			if rng.Intn(2) == 0 {
				v := rng.Intn(100)
				rows[i].Opt = &v
			}
			if rng.Intn(2) == 0 {
				rows[i].Raw = []byte(randCellString(rng))
			}
		}
		for _, o := range opts {
			got, err := MarshalOptions(rows, o)
			if err != nil {
				t.Fatalf("fast path err: %v", err)
			}
			want, err := marshalViaTree(rows, o)
			if err != nil {
				t.Fatalf("tree err: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("mismatch n=%d opts=%+v\n fast=%q\n tree=%q", n, o, got, want)
			}
		}
	}
}

func randCellString(rng *rand.Rand) string {
	n := rng.Intn(8)
	b := make([]byte, n)
	for i := range b {
		switch rng.Intn(10) {
		case 0:
			b[i] = ';' // structural: forces JSON fallback when sampled
		case 1:
			b[i] = ','
		case 2:
			b[i] = ' '
		case 3:
			b[i] = '"'
		default:
			b[i] = byte('a' + rng.Intn(26))
		}
	}
	return string(b)
}

func optionMatrix() []Options {
	return []Options{
		{},
		{NoRowCount: true},
		{BareStrings: true},
		{ImplicitNull: true},
		{MultilineZen: true},
		{Delimiter: DelimiterTab},
		{Delimiter: DelimiterPipe},
		{Indent: 2},
		{BareStrings: true, NoRowCount: true},
		{Delimiter: DelimiterTab, ImplicitNull: true},
		{MultilineZen: true, Indent: 4},
	}
}

func BenchmarkMarshalStructTableFast(b *testing.B) {
	type row struct {
		ID     int     `json:"id"`
		Name   string  `json:"name"`
		Dept   string  `json:"dept"`
		Score  float64 `json:"score"`
		Active bool    `json:"active"`
	}
	data := make([]row, 1000)
	for i := range data {
		data[i] = row{i, "User" + strconv.Itoa(i), "Engineering", float64(i) * 1.5, i%2 == 0}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}
