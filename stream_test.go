package jton

import (
	"math/rand"
	"testing"
)

// replay walks a canonical value tree, driving the streaming Writer. Its output
// must equal MarshalOptions of the same tree (with Zen Grid disabled, since the
// replay writes arrays as plain arrays).
func replay(w *Writer, v any) {
	switch t := v.(type) {
	case nil:
		w.Null()
	case bool:
		w.Bool(t)
	case int64:
		w.Int(t)
	case float64:
		w.Float(t)
	case string:
		w.String(t)
	case *Object:
		w.BeginObject()
		for i := 0; i < t.Len(); i++ {
			k, val := t.At(i)
			w.Field(k)
			replay(w, val)
		}
		w.EndObject()
	case []any:
		w.BeginArray()
		for _, e := range t {
			replay(w, e)
		}
		w.EndArray()
	}
}

func randTree(rng *rand.Rand, depth int) any {
	if depth <= 0 || rng.Intn(3) == 0 {
		switch rng.Intn(5) {
		case 0:
			return nil
		case 1:
			return rng.Intn(2) == 0
		case 2:
			return int64(rng.Intn(2000) - 1000)
		case 3:
			return float64(rng.Intn(1000)) / 4
		default:
			return randCellString(rng)
		}
	}
	if rng.Intn(2) == 0 {
		o := &Object{}
		for n := rng.Intn(5); n > 0; n-- {
			o.Set("k"+randCellString(rng), randTree(rng, depth-1))
		}
		return o
	}
	var a []any
	for n := rng.Intn(5); n > 0; n-- {
		a = append(a, randTree(rng, depth-1))
	}
	return a
}

func TestStreamWriterMatchesEncoder(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	opts := []Options{
		{NoZenGrid: true},
		{NoZenGrid: true, Indent: 2},
		{NoZenGrid: true, UnquotedKeys: true},
		{NoZenGrid: true, Indent: 4, UnquotedKeys: true},
	}
	for iter := 0; iter < 5000; iter++ {
		tree := randTree(rng, 4)
		for _, o := range opts {
			want, err := MarshalOptions(tree, o)
			if err != nil {
				t.Fatal(err)
			}
			w := NewWriter(o)
			replay(w, tree)
			if string(w.Bytes()) != string(want) {
				t.Fatalf("stream != encoder, opts=%+v\n stream=%q\n enc   =%q", o, w.Bytes(), want)
			}
		}
	}
}
