# JTON Reference Vectors

This directory vendors the JSON and numeric compatibility suites from
[yyjson](https://github.com/ibireme/yyjson/tree/master/test/data). The files are
copied verbatim and only the folder layout has been renamed so we can refer to
Samsung-internal paths (for example `json/extensions` instead of
`test_yyjson`).

Source projects:

- [JSONTestSuite](https://github.com/nst/JSONTestSuite) – strict conformance
  vectors (`json/parsing`, `json/transform`).
- [JSON_checker](http://www.json.org/JSON_checker/) – the classic
  `pass*.json`/`fail*.json` fixtures (`json/checker`).
- [nativejson-benchmark](https://github.com/miloyip/nativejson-benchmark) –
  round-trip corpora and multi-encoding samples (`json/roundtrip`,
  `json/encoding`).
- [yyjson custom cases](https://github.com/ibireme/yyjson/tree/master/test/data)
  – extension toggles (`json/extensions`) and detailed numeric sweeps
  (`number/`).

Usage notes:

- The upstream licenses (MIT / public domain) continue to apply to every file.
- Some fixtures intentionally exceed practical resource limits
  (e.g. `n_structure_100000_opening_arrays.json`). These remain in the tree, but
  our test harness skips them to keep the suite stable.
- The numeric `.txt` files contain one JSON number per line (comments start with
  `#`).
