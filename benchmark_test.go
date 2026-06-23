package jton

import (
	"strconv"
	"testing"
)

func benchTable(rows int) []any {
	out := make([]any, rows)
	for i := 0; i < rows; i++ {
		out[i] = Obj(
			"id", int64(i),
			"name", "User"+strconv.Itoa(i),
			"dept", "Engineering",
			"score", float64(i)*1.5,
			"active", i%2 == 0,
		)
	}
	return out
}

func BenchmarkMarshalTable100(b *testing.B) {
	data := benchTable(100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalTable1000(b *testing.B) {
	data := benchTable(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseZenGrid1000(b *testing.B) {
	src, _ := Marshal(benchTable(1000))
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseJSONArray1000(b *testing.B) {
	src, _ := MarshalOptions(benchTable(1000), Options{NoZenGrid: true})
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip1000(b *testing.B) {
	data := benchTable(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Marshal(data)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Parse(s); err != nil {
			b.Fatal(err)
		}
	}
}
