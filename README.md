# jton-go

A Go implementation of **JTON** (JSON Tabular Object Notation), a strict JSON
superset whose central feature — the **Zen Grid** — factors the shared keys of an
array of objects into a single header row, cutting token counts when feeding
tabular data to LLMs.

> ## 🙏 Credit & attribution
>
> **All credit for JTON — the format, the Zen Grid idea, the research, and the
> reference implementation — belongs to [Gowthamkumar
> Nandakishore](https://github.com/gowthamkumar-nandakishore).**
>
> - **Paper:** *"JTON: A Token-Efficient JSON Superset with Zen Grid Tabular
>   Encoding for Large Language Models"* — arXiv:**2604.05865**
> - **Reference implementation (Rust + Python):**
>   [github.com/gowthamkumar-nandakishore/JTON](https://github.com/gowthamkumar-nandakishore/JTON)
>
> This repository is **only a modest Go port**. It contributes no new ideas: the
> design, semantics, grammar, and every behavioral detail are the author's. The
> port simply re-expresses the reference behavior in Go and verifies it matches
> byte-for-byte. If you use JTON, please cite the paper and star the upstream
> project — the kudos go to the authors, not to this port.

This Go port is validated **byte-for-byte** against the reference (Rust + Python)
by a [cross-language conformance suite](#conformance).

```
[{"id":1,"name":"Alice","score":95},   ->   [3: id, name, score;
 {"id":2,"name":"Bob","score":87},            1, "Alice", 95;
 {"id":3,"name":"Carol","score":92}]          2, "Bob", 87;
                                              3, "Carol", 92 ]
```

## The format

JTON accepts everything JSON does, plus four extensions:

- **Zen Grid tables** — `[N: h1, h2; v1, v2; v3, v4 ]` is equivalent to the JSON
  array `[{"h1":v1,"h2":v2},{"h1":v3,"h2":v4}]`. `N` is an optional row count;
  `[:` (no count) is also valid. Missing trailing cells decode to `null`.
- **Comments** — `// line` and `/* block */`, anywhere whitespace is allowed.
- **Unquoted object keys** — `{name: "Alice"}`.
- **Special numbers** — `Infinity`, `-Infinity`, `NaN` (case-insensitive, with an
  optional sign).

## Usage

```go
import "jton"

// Decode (Loads): JTON/JSON -> Go value tree.
v, _ := jton.Loads(`[2: id, name; 1, "Alice"; 2, "Bob" ]`)
// v == []any{ *Object{id:1,name:"Alice"}, *Object{id:2,name:"Bob"} }

// Encode (Dumps): a list of objects auto-becomes a Zen Grid.
s, _ := jton.Dumps([]any{
    jton.Obj("id", int64(1), "name", "Alice"),
    jton.Obj("id", int64(2), "name", "Bob"),
})
// s == `[2: id, name; 1, "Alice"; 2, "Bob" ]`

// Works with ordinary Go values too (structs, maps, slices).
type Row struct{ ID int64 `json:"id"`; Name string `json:"name"` }
s, _ = jton.Dumps([]Row{{1, "Alice"}, {2, "Bob"}})   // same Zen Grid

// Decode into a struct.
var rows []Row
jton.Unmarshal([]byte(s), &rows)
```

### Value model

`Parse`/`Loads` decode into: `nil`, `bool`, `string`, `int64`, `*big.Int`
(integers overflowing int64), `float64` (any number with a fraction, exponent, or
special value), `*jton.Object` (an **insertion-ordered** object), and `[]any`.
Key order is preserved because the Zen Grid column order derives from it.

### Options

`MarshalOptions(v, jton.Options{...})` — the zero value matches the reference
defaults (Zen Grid on, row count on, comma delimiter, compact, quoted keys):

| Option | Effect |
|---|---|
| `NoZenGrid` | Disable Zen Grid; emit standard JSON. |
| `NoRowCount` | Emit `[: ...]` instead of `[N: ...]`. |
| `UnquotedKeys` | Identifier object keys without quotes. |
| `BareStrings` | Identifier string *values* unquoted in cells. |
| `ImplicitNull` | Null cells written empty instead of `null`. |
| `MultilineZen` | TOON-compatible multi-line table. |
| `Delimiter` | `DelimiterComma` (default), `DelimiterTab`, `DelimiterPipe`. |
| `Indent` | Pretty-print with N spaces (0 = compact). |

A list becomes a Zen Grid only when it has ≥2 elements that are all objects with
**scalar** cells (no nested objects/arrays, no strings containing structural
characters) and a homogeneous schema (≥70% sharing the first row's keys);
otherwise it stays a standard JSON array — exactly matching the reference's
`detect_zen_grid_candidate`.

### CLI

```
go run ./cmd/jton < data.json            # encode JSON -> Zen Grid
go run ./cmd/jton --decode < data.jton   # decode Zen Grid -> JSON
go run ./cmd/jton --bare-strings --tab < data.json
go run ./cmd/jton --hint zen_grid_rowcount
```

## Conformance

`go test ./conformance` drives the **real reference `jton`** (a Python process
wrapping the Rust core) as an oracle and asserts the Go port agrees with it:

- `dumps` output across a 26-input corpus × 16 option combinations, byte-for-byte.
- `dumps` of **20,000 random doubles** plus edge cases — Ryū float formatting is
  reproduced exactly.
- `loads` round-trips over the full **JSONTestSuite** reference vectors
  (601 fixtures): the two implementations agree on accept/reject and, when both
  accept, on the re-serialized value.

Build the reference once into a local venv (Python ≤ 3.12 for pyo3 0.20):

```sh
uv venv --python 3.12 .venv
uv pip install --python .venv maturin
( cd ../JTON && VIRTUAL_ENV="$PWD/../jton-go/.venv" ../jton-go/.venv/bin/maturin develop --release )
go test ./conformance      # auto-finds .venv; skips cleanly if absent
```

### Known divergences

Three families of inputs behave differently — all are reference bugs/quirks that
the reference's own Python suite crashes on or marks `xfail`, and the Go port's
behavior is the correct one:

- `-9223372036854775808` (i64::MIN) **panics** the reference
  (`fast_number.rs:215`, negate-overflow); Go decodes it correctly.
- Malformed objects like `{a-b: 1}` or `{"a"/* : 1` are **quirk-accepted** by the
  reference's structural-index parser (it jumps to the next indexed `:` across
  junk or inside comments); Go rejects them as the syntax errors they are.
- Deeply-nested fixtures abort the reference's recursive parser; Go returns an
  error.

## Build

Standard Go and Bazel (bzlmod, rules_go + gazelle, mirroring
`fdb-record-layer-go`) both work:

```sh
go build ./...   &&  go test ./...
bazel build //... && bazel test //...          # unit tests run; conformance skips w/o python
bazel run  //:gazelle                          # regenerate BUILD files

# run conformance under Bazel against the venv:
bazel test //conformance:conformance_test \
  --test_env=JTON_ORACLE_PYTHON=$PWD/.venv/bin/python \
  --test_env=JTON_REPO_ROOT=$PWD --spawn_strategy=local
```

## Performance

The parser is a single-pass scalar parser (no SIMD — that is a Rust-specific
micro-optimization, not idiomatic in Go); the serializer is copy-free for
already-canonical trees and formats floats straight into the output buffer.
Indicative numbers (`go test -bench .`, 1,000-row × 5-column table):

| Op | time/op | allocs/op |
|---|---|---|
| Marshal (→ Zen Grid) | ~0.37 ms | ~3.8k |
| Parse (Zen Grid) | ~0.74 ms | ~13k |

## Citation

If JTON is useful to you, cite the original work and star the upstream project —
not this port:

```bibtex
@misc{nandakishore2026jton,
  title  = {JTON: A Token-Efficient JSON Superset with Zen Grid Tabular
            Encoding for Large Language Models},
  author = {Nandakishore, Gowthamkumar},
  year   = {2026},
  eprint = {2604.05865},
  archivePrefix = {arXiv},
  url    = {https://github.com/gowthamkumar-nandakishore/JTON}
}
```

## License

The JTON format, paper, and reference implementation are © Gowthamkumar
Nandakishore, released under the MIT License. This Go port is an independent,
derivative re-implementation that claims no originality over the format and
follows the same MIT License. All design credit is the author's.
