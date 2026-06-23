package jton

import "testing"

func TestSmoke(t *testing.T) {
	users := []any{
		Obj("id", int64(1), "name", "Alice", "score", int64(95)),
		Obj("id", int64(2), "name", "Bob", "score", int64(87)),
		Obj("id", int64(3), "name", "Carol", "score", int64(92)),
	}
	got, err := Dumps(users)
	if err != nil {
		t.Fatal(err)
	}
	want := `[3: id, name, score; 1, "Alice", 95; 2, "Bob", 87; 3, "Carol", 92 ]`
	if got != want {
		t.Fatalf("dumps mismatch:\n got=%q\nwant=%q", got, want)
	}

	// round-trip
	parsed, err := Loads(got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Dumps(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if again != want {
		t.Fatalf("round-trip mismatch:\n got=%q\nwant=%q", again, want)
	}

	for _, tc := range []struct{ in, want string }{
		{`3.14`, "3.14"}, {`1.0`, "1.0"}, {`1e16`, "1e16"}, {`Infinity`, "Infinity"},
		{`[1,2,3]`, "[1,2,3]"}, {`{"a":1}`, `{"a":1}`}, {`[:]`, "[]"},
		{`[: name, age; "Alice", 30; "Bob", 25 ]`, `[2: name, age; "Alice", 30; "Bob", 25 ]`},
	} {
		v, err := Loads(tc.in)
		if err != nil {
			t.Errorf("Loads(%q) error: %v", tc.in, err)
			continue
		}
		out, err := Dumps(v)
		if err != nil {
			t.Errorf("Dumps error for %q: %v", tc.in, err)
			continue
		}
		if out != tc.want {
			t.Errorf("Loads/Dumps(%q) = %q, want %q", tc.in, out, tc.want)
		}
	}
}
