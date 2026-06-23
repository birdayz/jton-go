# jton-go

JTON is a JSON superset that stores an array of objects as a table. The keys go
into one header row and the records follow, so you stop repeating keys on every
row. In the paper's benchmarks that cuts token counts by 15 to 60% versus
compact JSON when you feed tabular data to an LLM. This is a Go port.

i did not design any of this. JTON, the Zen Grid encoding, the research, and the
reference implementation are all by Gowthamkumar Nandakishore:

- Paper: "JTON: A Token-Efficient JSON Superset with Zen Grid Tabular Encoding
  for Large Language Models", arXiv:2604.05865
- Reference implementation, Rust and Python:
  https://github.com/gowthamkumar-nandakishore/JTON

This repo is just the port. It re-implements the reference behavior in Go and
checks that the two match byte for byte. If JTON is useful to you, cite the paper
and star the upstream repo, not this.

```
[{"id":1,"name":"Alice","score":95},   ->   [3: id, name, score;
 {"id":2,"name":"Bob","score":87},            1, "Alice", 95;
 {"id":3,"name":"Carol","score":92}]          2, "Bob", 87;
                                              3, "Carol", 92 ]
```

## The format

JTON is JSON plus four things:

- Zen Grid tables. `[N: h1, h2; v1, v2; v3, v4 ]` is the same as the JSON array
  `[{"h1":v1,"h2":v2},{"h1":v3,"h2":v4}]`. N is an optional row count, `[:` works
  too. Missing trailing cells decode to null.
- Comments, `//` and `/* */`, anywhere whitespace is allowed.
- Unquoted object keys, `{name: "Alice"}`.
- `Infinity`, `-Infinity`, `NaN`, case insensitive, with an optional sign.

## Usage

```go
import "jton"

// Loads: JTON or JSON into a Go value tree.
v, _ := jton.Loads(`[2: id, name; 1, "Alice"; 2, "Bob" ]`)
// v is []any{ *Object{id:1,name:"Alice"}, *Object{id:2,name:"Bob"} }

// Dumps: a list of objects becomes a Zen Grid on its own.
s, _ := jton.Dumps([]any{
    jton.Obj("id", int64(1), "name", "Alice"),
    jton.Obj("id", int64(2), "name", "Bob"),
})
// s is `[2: id, name; 1, "Alice"; 2, "Bob" ]`

// Ordinary Go values work too, structs included.
type Row struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}
s, _ = jton.Dumps([]Row{{1, "Alice"}, {2, "Bob"}})   // same Zen Grid

var rows []Row
jton.Unmarshal([]byte(s), &rows)
```

### Value model

`Parse` and `Loads` return one of: nil, bool, string, int64, `*big.Int` for
integers that overflow int64, float64 for anything with a fraction, an exponent,
or a special value, `*jton.Object`, and `[]any`. Objects keep their key order
because the Zen Grid column order comes from it, so `Object` is an ordered type
and not a Go map.

### Options

`MarshalOptions(v, jton.Options{...})`. The zero value matches the reference
defaults: Zen Grid on, row count on, comma delimiter, compact, quoted keys.

| Option | Effect |
|---|---|
| `NoZenGrid` | Emit standard JSON, no Zen Grid. |
| `NoRowCount` | `[: ...]` instead of `[N: ...]`. |
| `UnquotedKeys` | Identifier object keys without quotes. |
| `BareStrings` | Identifier string values without quotes in cells. |
| `ImplicitNull` | Null cells written empty instead of `null`. |
| `MultilineZen` | TOON-style multi-line table. |
| `Delimiter` | `DelimiterComma` (default), `DelimiterTab`, `DelimiterPipe`. |
| `Indent` | Pretty-print with N spaces, 0 is compact. |

A list becomes a Zen Grid only when it has at least two elements, they are all
objects with scalar cells (no nested objects or arrays, no strings containing
structural characters), and the schema is homogeneous, meaning at least 70% of
rows share the first row's keys. Otherwise it stays a normal JSON array. Same
rule as the reference's `detect_zen_grid_candidate`.

### CLI

```
go run ./cmd/jton < data.json            # JSON to Zen Grid
go run ./cmd/jton --decode < data.jton   # Zen Grid to JSON
go run ./cmd/jton --bare-strings --tab < data.json
go run ./cmd/jton --hint zen_grid_rowcount
```

## Conformance

`go test ./conformance` runs the real reference jton, the Rust core behind its
Python module, as a subprocess oracle, and checks the Go port agrees with it:

- dumps output over a 26-input corpus times 16 option combinations, byte for
  byte.
- dumps of 20000 random doubles plus the obvious edge cases. The Ryu float
  formatting is reproduced exactly.
- loads over the full JSONTestSuite reference vectors, 601 files. The two have to
  agree on accept versus reject, and on the re-serialized value when they both
  accept.

Build the reference once into a venv. Use Python 3.12 or older, pyo3 0.20 does
not build against newer ones.

```sh
uv venv --python 3.12 .venv
uv pip install --python .venv maturin
( cd ../JTON && VIRTUAL_ENV="$PWD/../jton-go/.venv" ../jton-go/.venv/bin/maturin develop --release )
go test ./conformance      # finds .venv on its own, skips if it is not there
```

### Where it diverges

Three kinds of input behave differently. In all three the reference is wrong and
the Go port is right, and the reference's own Python suite either crashes on them
or marks them xfail.

- `-9223372036854775808`, the smallest int64, panics the reference in
  `fast_number.rs:215` on a negate overflow. Go decodes it.
- Malformed objects like `{a-b: 1}` or `{"a"/* : 1` get accepted by the
  reference, because its structural-index parser jumps to the next indexed colon
  even across junk between key and colon, or a colon sitting inside a comment. Go
  rejects them as the syntax errors they are.
- Deeply nested input overflows the reference's recursive parser. Go returns an
  error.

## Build

go and Bazel both work. Bazel is bzlmod with rules_go and gazelle, set up the
same way as fdb-record-layer-go.

```sh
go build ./...    && go test ./...
bazel build //... && bazel test //...     # unit tests run, conformance skips without python
bazel run  //:gazelle                     # regenerate BUILD files

# conformance under Bazel, pointed at the venv:
bazel test //conformance:conformance_test \
  --test_env=JTON_ORACLE_PYTHON=$PWD/.venv/bin/python \
  --test_env=JTON_REPO_ROOT=$PWD --spawn_strategy=local
```

## Performance

The parser is a single pass and scalar. No SIMD. That is a Rust micro
optimization and it is not idiomatic in Go. The serializer skips the copy when
the tree is already canonical and writes floats straight into the output buffer.
For a 1000 row, 5 column table, Marshal is around 0.37 ms and Parse around
0.74 ms.

## License

The format, the paper, and the reference implementation are Gowthamkumar
Nandakishore's, under MIT. This port is a derivative under the same license, and
none of the design is mine.
