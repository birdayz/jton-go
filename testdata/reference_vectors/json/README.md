# JSON Reference Data

The files in this directory are vendored from
<https://github.com/ibireme/yyjson/tree/master/test/data/json> and keep the
same upstream content but with JTON-specific folder names:

- `parsing/` ← `test_parsing`
- `transform/` ← `test_transform`
- `checker/` ← `test_checker`
- `roundtrip/` ← `test_roundtrip`
- `encoding/` ← `test_encoding`
- `extensions/` ← `test_yyjson`

### parsing/
Source: <https://github.com/nst/JSONTestSuite>

The filename prefix describes the expectation for RFC 8259 compliant parsers:

- `y_` – content must be accepted.
- `n_` – content must be rejected.
- `i_` – implementation defined; parsers may accept or reject.

### transform/
Source: <https://github.com/nst/JSONTestSuite>

Files that contain unusual code points, deep nesting, or other constructs that
stress‐test parser implementations.

### checker/
Source: <http://www.json.org/JSON_checker/>

Classic “pass*.json” and “fail*.json” fixtures for verifying strict JSON
parsers. The upstream suite excludes `fail01.json` and `fail18.json` because
their requirements changed in RFC 7159.

### roundtrip/
Source: <https://github.com/miloyip/nativejson-benchmark>

Condensed JSON documents that should roundtrip through parse → stringify
without data loss. Upstream added several extra samples to extend coverage.

### encoding/
Source: <https://github.com/miloyip/nativejson-benchmark>

Variants of the same JSON encoded as UTF-8/16/32 with and without BOM. RFC
8259 mandates UTF-8 without BOM; the other encodings are provided for
compatibility experiments.

### extensions/
Original name: `test_yyjson`. These are feature probes used by yyjson to verify
extended syntax. File names use tags such as `(fail)`, `(comment)`, `(comma)`,
`(inf)`, `(nan)`, `(ext_num)`, etc. to describe the expected behavior. See the
upstream README for the complete legend.
