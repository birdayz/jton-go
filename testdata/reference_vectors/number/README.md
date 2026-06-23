# Number Reference Data

Mirrors <https://github.com/ibireme/yyjson/tree/master/test/data/num>. These
fixtures are shared with yyjson and nativejson-benchmark and are now used to
stress JTON's numeric parser.

## File name conventions

Each `.txt` file contains one JSON number per line (with comments prefixed by
`#`). The base name identifies the general category:

- `int` – signed/unsigned integers
- `real`, `real_1` … `real_7` – floating-point coverage (scientific notation,
	denormals, exponent ranges, etc.)
- `hex` – hexadecimal integer forms used by JSON5 (`0x1234`).
- `literal` – special literals such as `NaN` and `Infinity`.

Optional flags in parentheses add extra expectations:

- `(big)` – values beyond 64-bit integer range.
- `(inf)` – values that overflow IEEE-754 doubles (expect ±inf).
- `(ext)` – syntax only valid when an extended-number mode is enabled
	(leading `+`, leading decimal point, hexadecimal integers, etc.).
- `(fail)` – explicitly invalid samples that every parser should reject.
