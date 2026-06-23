// Package jton implements JTON (JSON Tabular Object Notation), a strict JSON
// superset whose central feature, the Zen Grid, factors shared object keys into
// a single header row to cut token counts when feeding tabular data to LLMs.
//
// It is a Go port of the reference implementation
// (github.com/gowthamkumar-nandakishore/JTON, "JTON: A Token-Efficient JSON
// Superset with Zen Grid Tabular Encoding for Large Language Models",
// arXiv:2604.05865). Parsing and serialization are matched byte-for-byte
// against that reference via a cross-language conformance suite.
//
// # Format
//
// JTON accepts everything JSON does, plus:
//
//   - Zen Grid tables: [N: h1, h2; v1, v2; v3, v4 ] is equivalent to the JSON
//     array [{"h1":v1,"h2":v2},{"h1":v3,"h2":v4}]. N is an optional row count.
//   - Comments: // line and /* block */.
//   - Unquoted object keys: {name: "Alice"}.
//   - Special numbers: Infinity, -Infinity, NaN (case-insensitive, with an
//     optional sign).
//
// # Value model
//
// Parse and Loads decode into a tree of: nil, bool, string, int64, *big.Int
// (integers overflowing int64), float64 (any number with a fraction, exponent,
// or special value), *Object (insertion-ordered objects), and []any. Key order
// is preserved because the Zen Grid column order derives from it.
//
// # Serialization
//
//	users := []any{
//		jton.Obj("id", int64(1), "name", "Alice", "score", int64(95)),
//		jton.Obj("id", int64(2), "name", "Bob", "score", int64(87)),
//	}
//	s, _ := jton.Dumps(users)
//	// [2: id, name, score; 1, "Alice", 95; 2, "Bob", 87 ]
//
// Marshal also accepts ordinary Go values (structs, maps, slices); foreign
// types are bridged through encoding/json. A list becomes a Zen Grid only when
// it has at least two elements that are all objects with scalar cells and a
// homogeneous schema; otherwise it stays a standard JSON array.
package jton
