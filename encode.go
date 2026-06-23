package jton

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"unsafe"
)

// Delimiter selects the separator used between Zen Grid header fields and row
// values. The zero value is comma (", "), the reference default.
type Delimiter int

const (
	DelimiterComma Delimiter = iota // ", "
	DelimiterTab                    // "\t"
	DelimiterPipe                   // " | "
)

func (d Delimiter) sep() string {
	switch d {
	case DelimiterTab:
		return "\t"
	case DelimiterPipe:
		return " | "
	default:
		return ", "
	}
}

// Options controls serialization. The zero value reproduces the reference
// defaults: Zen Grid enabled, row-count prefix enabled, comma delimiter,
// compact (no indent), quoted keys.
type Options struct {
	// NoZenGrid disables Zen Grid tabular encoding (reference zen_grid=False).
	NoZenGrid bool
	// UnquotedKeys writes identifier-like object keys without quotes.
	UnquotedKeys bool
	// Indent, when > 0, pretty-prints with that many spaces per level. 0 means
	// compact output.
	Indent int
	// BareStrings writes identifier-like string values without quotes in Zen
	// Grid cells.
	BareStrings bool
	// ImplicitNull writes null Zen Grid cells as empty instead of the literal
	// null.
	ImplicitNull bool
	// NoRowCount omits the [N: ...] row-count prefix (reference row_count=False).
	NoRowCount bool
	// MultilineZen emits the TOON-compatible multi-line Zen Grid format.
	MultilineZen bool
	// Delimiter selects the Zen Grid field separator.
	Delimiter Delimiter
}

const maxDepth = 256

type encoder struct {
	buf  []byte
	opts Options
}

// Marshal serializes v to JTON using the default options (Zen Grid enabled).
func Marshal(v any) ([]byte, error) { return MarshalOptions(v, Options{}) }

// MarshalOptions serializes v to JTON with the given options.
func MarshalOptions(v any, opts Options) ([]byte, error) {
	nv, _, err := normalize(v)
	if err != nil {
		return nil, err
	}
	e := &encoder{opts: opts, buf: make([]byte, 0, 256)}
	if err := e.encode(nv, 0); err != nil {
		return nil, err
	}
	return e.buf, nil
}

// ── normalization to the canonical value tree ──────────────────────────────

// normalize converts an arbitrary Go value into the canonical tree the encoder
// understands: nil, bool, string, int64, *big.Int, float64, *Object, []any. It
// returns changed=false (and the original value, no copy) when the input is
// already canonical, so dumping parsed data or []*Object trees is allocation
// free in the normalize pass. Native containers (map[string]any, []any,
// *Object) are converted structurally so float/int types survive; foreign types
// (structs, typed slices/maps) go through encoding/json, adopting its
// conventions.
func normalize(v any) (any, bool, error) {
	switch t := v.(type) {
	case nil:
		return nil, false, nil
	case bool:
		return t, false, nil
	case string:
		return t, false, nil
	case float64:
		return t, false, nil
	case int64:
		return t, false, nil
	case *big.Int:
		return t, false, nil
	case float32:
		return float64(t), true, nil
	case int:
		return int64(t), true, nil
	case int8:
		return int64(t), true, nil
	case int16:
		return int64(t), true, nil
	case int32:
		return int64(t), true, nil
	case uint8:
		return int64(t), true, nil
	case uint16:
		return int64(t), true, nil
	case uint32:
		return int64(t), true, nil
	case uint:
		return uintToCanonical(uint64(t)), true, nil
	case uint64:
		return uintToCanonical(t), true, nil
	case uintptr:
		return uintToCanonical(uint64(t)), true, nil
	case json.Number:
		return numberStringToCanonical(string(t)), true, nil
	case *Object:
		var no *Object
		for i := 0; i < t.Len(); i++ {
			k, val := t.At(i)
			nval, ch, err := normalize(val)
			if err != nil {
				return nil, false, err
			}
			if no != nil {
				no.Set(k, nval)
				continue
			}
			if ch {
				no = &Object{}
				for j := 0; j < i; j++ {
					kk, vv := t.At(j)
					no.Set(kk, vv)
				}
				no.Set(k, nval)
			}
		}
		if no == nil {
			return t, false, nil
		}
		return no, true, nil
	case []any:
		var out []any
		for i := range t {
			nval, ch, err := normalize(t[i])
			if err != nil {
				return nil, false, err
			}
			if out != nil {
				out[i] = nval
				continue
			}
			if ch {
				out = make([]any, len(t))
				copy(out[:i], t[:i])
				out[i] = nval
			}
		}
		if out == nil {
			return t, false, nil
		}
		return out, true, nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := &Object{}
		for _, k := range keys {
			nv, _, err := normalize(t[k])
			if err != nil {
				return nil, false, err
			}
			o.Set(k, nv)
		}
		return o, true, nil
	default:
		nv, err := normalizeForeign(v)
		return nv, true, err
	}
}

func uintToCanonical(u uint64) any {
	if u <= math.MaxInt64 {
		return int64(u)
	}
	return new(big.Int).SetUint64(u)
}

func numberStringToCanonical(s string) any {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if bi, ok := new(big.Int).SetString(s, 10); ok {
		return bi
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// normalizeForeign converts structs, typed slices/maps, and pointers via a
// JSON round-trip (preserving struct field order and json tags), then parses
// the result back into the canonical tree.
func normalizeForeign(v any) (any, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// ── core encoder ───────────────────────────────────────────────────────────

func (e *encoder) encode(v any, depth int) error {
	if depth > maxDepth {
		return &SyntaxError{Msg: "object too deeply nested (max 256 levels)", Offset: 0}
	}
	switch t := v.(type) {
	case nil:
		e.buf = append(e.buf, "null"...)
	case bool:
		if t {
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
	case string:
		e.writeQuoted(t)
	case int64:
		e.buf = strconv.AppendInt(e.buf, t, 10)
	case *big.Int:
		e.buf = append(e.buf, t.String()...)
	case float64:
		e.buf = appendFloat(e.buf, t)
	case *Object:
		return e.encodeObject(t, depth)
	case []any:
		return e.encodeListOrTable(t, depth)
	default:
		nv, _, err := normalize(v)
		if err != nil {
			return err
		}
		return e.encode(nv, depth)
	}
	return nil
}

func (e *encoder) encodeObject(o *Object, depth int) error {
	e.buf = append(e.buf, '{')
	n := o.Len()
	if e.opts.Indent > 0 {
		child := depth + 1
		for i := 0; i < n; i++ {
			k, v := o.At(i)
			e.buf = append(e.buf, '\n')
			e.writeIndent(child * e.opts.Indent)
			e.writeKey(k)
			e.buf = append(e.buf, ':', ' ')
			if err := e.encode(v, child); err != nil {
				return err
			}
			if i < n-1 {
				e.buf = append(e.buf, ',')
			}
		}
		if n > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent(depth * e.opts.Indent)
		}
	} else {
		for i := 0; i < n; i++ {
			k, v := o.At(i)
			e.writeKey(k)
			e.buf = append(e.buf, ':')
			if err := e.encode(v, depth+1); err != nil {
				return err
			}
			if i < n-1 {
				e.buf = append(e.buf, ',')
			}
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) encodeListOrTable(arr []any, depth int) error {
	if len(arr) == 0 {
		e.buf = append(e.buf, '[', ']')
		return nil
	}
	if !e.opts.NoZenGrid && len(arr) >= 2 {
		if headers, ok := detectZenGrid(arr); ok {
			return e.writeZenGrid(arr, headers, depth)
		}
	}
	return e.writeArray(arr, depth)
}

func (e *encoder) writeArray(arr []any, depth int) error {
	e.buf = append(e.buf, '[')
	for i, v := range arr {
		if e.opts.Indent > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent((depth + 1) * e.opts.Indent)
		}
		if err := e.encode(v, depth+1); err != nil {
			return err
		}
		if i < len(arr)-1 {
			e.buf = append(e.buf, ',')
		}
	}
	if e.opts.Indent > 0 && len(arr) > 0 {
		e.buf = append(e.buf, '\n')
		e.writeIndent(depth * e.opts.Indent)
	}
	e.buf = append(e.buf, ']')
	return nil
}

// ── Zen Grid detection ─────────────────────────────────────────────────────

func detectZenGrid(arr []any) ([]string, bool) {
	n := len(arr)
	if n < 2 {
		return nil, false
	}
	first, ok := arr[0].(*Object)
	if !ok || first.Len() == 0 || !dictAllScalar(first) {
		return nil, false
	}
	headers := first.Keys()
	nh := len(headers)

	// Fast path: sample up to 10 rows; accept if they all have exactly the
	// header key set.
	sample := n
	if sample > 10 {
		sample = 10
	}
	allExact := true
	for i := 1; i < sample; i++ {
		d, ok := arr[i].(*Object)
		if !ok {
			return nil, false
		}
		if !dictAllScalar(d) {
			return nil, false
		}
		if d.Len() != nh {
			allExact = false
			break
		}
		for _, h := range headers {
			if _, present := d.Get(h); !present {
				allExact = false
				break
			}
		}
		if !allExact {
			break
		}
	}
	if allExact {
		return headers, true
	}

	// Slow path: up to 50 rows must reach the 70% coverage threshold.
	checkSize := n
	if checkSize > 50 {
		checkSize = 50
	}
	threshold := (checkSize*7 + 9) / 10 // ceil(checkSize * 0.7)
	matching := 0
	for i := 0; i < checkSize; i++ {
		d, ok := arr[i].(*Object)
		if !ok {
			return nil, false
		}
		if !dictAllScalar(d) {
			return nil, false
		}
		found := 0
		for _, h := range headers {
			if _, present := d.Get(h); present {
				found++
			}
		}
		if found == nh {
			matching++
		}
	}
	if matching >= threshold {
		return headers, true
	}
	return nil, false
}

func dictAllScalar(o *Object) bool {
	for i := 0; i < o.Len(); i++ {
		_, v := o.At(i)
		if !isScalarCell(v) {
			return false
		}
	}
	return true
}

func isScalarCell(v any) bool {
	switch t := v.(type) {
	case nil, bool, int64, *big.Int, float64:
		return true
	case string:
		return isSafeZenString(t)
	default:
		return false
	}
}

func isSafeZenString(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\', ',', ';', ':', '|', '[', ']', '{', '}', '\n', '\r', '\t':
			return false
		}
	}
	return true
}

// ── Zen Grid serialization ─────────────────────────────────────────────────

func (e *encoder) writeZenGrid(arr []any, headers []string, depth int) error {
	nRows := len(arr)
	sep := e.opts.Delimiter.sep()

	if e.opts.MultilineZen {
		indentSize := e.opts.Indent
		if indentSize == 0 {
			indentSize = 2
		}
		e.buf = append(e.buf, '[')
		e.buf = strconv.AppendInt(e.buf, int64(nRows), 10)
		e.buf = append(e.buf, ']', '{')
		for i, h := range headers {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			e.writeZenHeaderKey(h)
		}
		e.buf = append(e.buf, '}', ':')
		for _, row := range arr {
			e.buf = append(e.buf, '\n')
			e.writeIndent((depth + 1) * indentSize)
			if err := e.writeZenRow(row, headers, depth); err != nil {
				return err
			}
		}
		return nil
	}

	if e.opts.Indent > 0 {
		w := e.opts.Indent
		e.buf = append(e.buf, '[')
		if !e.opts.NoRowCount {
			e.buf = strconv.AppendInt(e.buf, int64(nRows), 10)
		}
		e.buf = append(e.buf, ':', '\n')
		e.writeIndent((depth + 1) * w)
		for i, h := range headers {
			if i > 0 {
				e.buf = append(e.buf, sep...)
			}
			e.writeZenHeaderKey(h)
		}
		for _, row := range arr {
			e.buf = append(e.buf, '\n')
			e.writeIndent((depth + 1) * w)
			if err := e.writeZenRow(row, headers, depth); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '\n')
		e.writeIndent(depth * w)
		e.buf = append(e.buf, ']')
		return nil
	}

	e.buf = append(e.buf, '[')
	if !e.opts.NoRowCount {
		e.buf = strconv.AppendInt(e.buf, int64(nRows), 10)
	}
	e.buf = append(e.buf, ':', ' ')
	for i, h := range headers {
		if i > 0 {
			e.buf = append(e.buf, sep...)
		}
		e.writeZenHeaderKey(h)
	}
	for _, row := range arr {
		e.buf = append(e.buf, ';', ' ')
		if err := e.writeZenRow(row, headers, depth); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ' ', ']')
	return nil
}

func (e *encoder) writeZenRow(rowVal any, headers []string, depth int) error {
	row, ok := rowVal.(*Object)
	if !ok {
		// Detection guarantees *Object rows, but stay safe.
		return e.encode(rowVal, depth+1)
	}
	sep := e.opts.Delimiter.sep()
	for i, h := range headers {
		if i > 0 {
			e.buf = append(e.buf, sep...)
		}
		v, present := row.Get(h)
		if !present || v == nil {
			if !e.opts.ImplicitNull {
				e.buf = append(e.buf, "null"...)
			}
			continue
		}
		if e.opts.BareStrings {
			if s, ok := v.(string); ok {
				if isValidIdentifier(s) {
					e.buf = append(e.buf, s...)
				} else {
					e.writeQuoted(s)
				}
				continue
			}
		}
		if err := e.encode(v, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *encoder) writeZenHeaderKey(h string) {
	if isValidIdentifier(h) {
		e.buf = append(e.buf, h...)
	} else {
		e.writeQuoted(h)
	}
}

func (e *encoder) writeKey(k string) {
	if e.opts.UnquotedKeys && isValidIdentifier(k) {
		e.buf = append(e.buf, k...)
	} else {
		e.writeQuoted(k)
	}
}

// isValidIdentifier matches the reference is_valid_identifier: first byte
// [A-Za-z_$], subsequent bytes [A-Za-z0-9_$-].
func isValidIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$' || c == '-') {
			return false
		}
	}
	return true
}

func (e *encoder) writeIndent(n int) {
	for i := 0; i < n; i++ {
		e.buf = append(e.buf, ' ')
	}
}

const hexDigits = "0123456789abcdef"

// writeQuoted writes s as a JSON string with the reference's escape set: ", \,
// \n, \r, \t, \b, \f are short-escaped; other control bytes use \u00XX; all
// other bytes (including raw UTF-8) pass through. No HTML escaping. The scan for
// the next byte needing an escape is AVX2-accelerated (escapeIndex); clean spans
// are bulk-copied.
func (e *encoder) writeQuoted(s string) {
	e.buf = append(e.buf, '"')
	if len(s) > 0 {
		// Read-only byte view of s; escapeIndex never writes through it, and the
		// spans we copy out are appended (copied) into e.buf.
		b := unsafe.Slice(unsafe.StringData(s), len(s))
		for len(b) > 0 {
			j := escapeIndex(b)
			if j > 0 {
				e.buf = append(e.buf, b[:j]...)
			}
			if j == len(b) {
				break
			}
			switch c := b[j]; c {
			case '"':
				e.buf = append(e.buf, '\\', '"')
			case '\\':
				e.buf = append(e.buf, '\\', '\\')
			case '\n':
				e.buf = append(e.buf, '\\', 'n')
			case '\r':
				e.buf = append(e.buf, '\\', 'r')
			case '\t':
				e.buf = append(e.buf, '\\', 't')
			case '\b':
				e.buf = append(e.buf, '\\', 'b')
			case '\f':
				e.buf = append(e.buf, '\\', 'f')
			default:
				e.buf = append(e.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			b = b[j+1:]
		}
	}
	e.buf = append(e.buf, '"')
}
