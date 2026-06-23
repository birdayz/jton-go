package jton

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// SyntaxError describes a JTON/JSON parse failure. It mirrors the reference
// implementation, which raises ValueError on any malformed input.
type SyntaxError struct {
	Msg    string
	Offset int // byte offset into the input where the error was detected
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("jton: %s (at byte offset %d)", e.Msg, e.Offset)
}

type parser struct {
	data    []byte
	pos     int
	depth   int
	scratch []byte // reusable buffer for string unescaping
}

// maxParseDepth bounds container nesting so pathological inputs return an error
// instead of overflowing the stack. It is set well above any realistic document.
const maxParseDepth = 10000

func (p *parser) errf(offset int, format string, args ...any) error {
	return &SyntaxError{Msg: fmt.Sprintf(format, args...), Offset: offset}
}

// Parse decodes JTON (a strict superset of JSON, including Zen Grid tables,
// comments, unquoted keys, and the Infinity/-Infinity/NaN literals) into a Go
// value tree composed of: nil, bool, string, int64, *big.Int, float64,
// *Object, and []any. Numbers without a fraction or exponent decode to int64
// (or *big.Int when they overflow int64); all others decode to float64.
func Parse(data []byte) (any, error) {
	p := &parser{data: data}
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, p.errf(p.pos, "unexpected end of input")
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos < len(p.data) {
		return nil, p.errf(p.pos, "trailing content after document")
	}
	return v, nil
}

// ── whitespace, comments, BOM ──────────────────────────────────────────────

func isAsciiSpace(c byte) bool {
	// Matches Rust u8::is_ascii_whitespace: space, tab, LF, FF, CR.
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == 0x0C
}

// skipWS skips insignificant whitespace, // line comments, /* */ block
// comments, and a leading UTF-8 BOM (only at offset 0). An unterminated block
// comment consumes to end of input (matching the reference, which does not
// error on it).
func (p *parser) skipWS() {
	in := p.data
	for p.pos < len(in) {
		c := in[p.pos]
		if p.pos == 0 && c == 0xEF && len(in) >= 3 && in[1] == 0xBB && in[2] == 0xBF {
			p.pos += 3
			continue
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			p.pos++
		case '/':
			if p.pos+1 >= len(in) {
				return
			}
			switch in[p.pos+1] {
			case '/':
				p.pos += 2
				for p.pos < len(in) {
					cc := in[p.pos]
					if cc == '\n' {
						p.pos++
						break
					}
					if cc == '\r' {
						p.pos++
						if p.pos < len(in) && in[p.pos] == '\n' {
							p.pos++
						}
						break
					}
					p.pos++
				}
			case '*':
				p.pos += 2
				terminated := false
				for p.pos+1 < len(in) {
					if in[p.pos] == '*' && in[p.pos+1] == '/' {
						p.pos += 2
						terminated = true
						break
					}
					p.pos++
				}
				if !terminated {
					p.pos = len(in)
				}
			default:
				return
			}
		default:
			return
		}
	}
}

// skipSpacesOnly skips only ASCII spaces, used inside Zen Grid rows so tab and
// pipe delimiters are not consumed as whitespace.
func (p *parser) skipSpacesOnly() {
	for p.pos < len(p.data) && p.data[p.pos] == ' ' {
		p.pos++
	}
}

// ── value dispatch ─────────────────────────────────────────────────────────

func (p *parser) parseValue() (any, error) {
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, p.errf(p.pos, "unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		p.depth++
		if p.depth > maxParseDepth {
			return nil, p.errf(p.pos, "input too deeply nested")
		}
		v, err := p.parseObject()
		p.depth--
		return v, err
	case c == '[':
		p.depth++
		if p.depth > maxParseDepth {
			return nil, p.errf(p.pos, "input too deeply nested")
		}
		v, err := p.parseArrayOrGrid()
		p.depth--
		return v, err
	case c == '"':
		return p.parseString()
	case c == 't' || c == 'f' || c == 'n':
		return p.parseLiteral()
	case c == '-' || (c >= '0' && c <= '9') || c == 'I' || c == 'N':
		return p.parseNumber()
	default:
		return nil, p.errf(p.pos, "unexpected character %q", c)
	}
}

func (p *parser) parseLiteral() (any, error) {
	in := p.data
	if p.pos+4 <= len(in) {
		s := in[p.pos : p.pos+4]
		if string(s) == "true" {
			p.pos += 4
			return true, nil
		}
		if string(s) == "null" {
			p.pos += 4
			return nil, nil
		}
	}
	if p.pos+5 <= len(in) && string(in[p.pos:p.pos+5]) == "false" {
		p.pos += 5
		return false, nil
	}
	return nil, p.errf(p.pos, "invalid literal")
}

// ── numbers ────────────────────────────────────────────────────────────────

func isNumberByte(c byte) bool {
	switch c {
	case '-', '+', '.', 'e', 'E', 'I', 'N', 'a', 'f', 'n', 'i', 't', 'y':
		return true
	}
	return c >= '0' && c <= '9'
}

func equalsCI(b []byte, kw string) bool {
	if len(b) != len(kw) {
		return false
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != kw[i] {
			return false
		}
	}
	return true
}

func (p *parser) parseNumber() (any, error) {
	start := p.pos
	for p.pos < len(p.data) && isNumberByte(p.data[p.pos]) {
		p.pos++
	}
	b := p.data[start:p.pos]
	return p.numberFromToken(b, start)
}

// numberFromToken validates and converts a number token exactly as the
// reference (fast_number.rs): rejects '+', leading zeros, trailing '.', and
// trailing garbage; accepts case-insensitive nan/inf/infinity with optional
// sign; routes integers to int64/big.Int and reals to float64.
func (p *parser) numberFromToken(b []byte, off int) (any, error) {
	if len(b) == 0 {
		return nil, p.errf(off, "empty number")
	}
	if b[0] == '+' {
		return nil, p.errf(off, "invalid number")
	}
	pos := 0
	neg := b[0] == '-'
	if neg {
		pos = 1
		if pos >= len(b) {
			return nil, p.errf(off, "invalid number")
		}
	}
	lit := b[pos:]
	if equalsCI(lit, "nan") {
		return math.NaN(), nil
	}
	if equalsCI(lit, "inf") || equalsCI(lit, "infinity") {
		if neg {
			return math.Inf(-1), nil
		}
		return math.Inf(1), nil
	}
	if !(b[pos] >= '0' && b[pos] <= '9') {
		return nil, p.errf(off, "invalid number")
	}
	// No leading zeros (00, 01, -01); 0, 0.x, 0ex are fine.
	if b[pos] == '0' && pos+1 < len(b) && b[pos+1] >= '0' && b[pos+1] <= '9' {
		return nil, p.errf(off, "invalid number: leading zeros not allowed")
	}

	i := pos
	hasDot, hasExp, hasFrac := false, false, false
	for i < len(b) {
		c := b[i]
		if c >= '0' && c <= '9' {
			i++
		} else if c == '.' {
			hasDot = true
			i++
			break
		} else if c == 'e' || c == 'E' {
			hasExp = true
			i++
			break
		} else {
			break
		}
	}
	if hasDot || hasExp {
		for i < len(b) {
			c := b[i]
			switch {
			case c >= '0' && c <= '9':
				if hasDot && !hasExp {
					hasFrac = true
				}
				i++
			case (c == 'e' || c == 'E') && hasDot && !hasExp:
				if !hasFrac {
					return nil, p.errf(off, "invalid number: decimal point must be followed by digits")
				}
				hasExp = true
				i++
				if i < len(b) && (b[i] == '+' || b[i] == '-') {
					i++
				}
			case (c == '+' || c == '-') && hasExp:
				i++
			default:
				i = len(b) + 1 // sentinel: trailing garbage
			}
			if i > len(b) {
				break
			}
		}
	}
	if i > len(b) {
		return nil, p.errf(off, "invalid number")
	}
	if hasDot && !hasFrac {
		return nil, p.errf(off, "invalid number: decimal point must be followed by digits")
	}
	if i < len(b) {
		return nil, p.errf(off, "invalid number")
	}

	if !hasDot && !hasExp {
		// Manual int64 accumulation avoids a string allocation per integer; the
		// token is already validated, so only an overflow needs the slow path.
		if v, ok := parseInt64(b, neg); ok {
			if v >= 0 && v < int64(len(smallInt)) {
				return smallInt[v], nil // interned: no boxing allocation
			}
			return v, nil
		}
		bi, ok := new(big.Int).SetString(string(b), 10)
		if !ok {
			return nil, p.errf(off, "invalid integer")
		}
		return bi, nil
	}
	// Alias the token bytes as a string (read-only) to skip the copy.
	v, err := strconv.ParseFloat(unsafe.String(unsafe.SliceData(b), len(b)), 64)
	if err != nil {
		// Out-of-range magnitudes are not errors: like the reference's
		// lexical-core path, overflow yields ±Inf and underflow yields 0.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return v, nil
		}
		return nil, p.errf(off, "invalid float")
	}
	return v, nil
}

// smallInt interns boxed int64 values 0..255, the common case for ids, counts,
// and flags, so decoding them does not allocate a fresh interface box.
var smallInt = func() [256]any {
	var a [256]any
	for i := range a {
		a[i] = int64(i)
	}
	return a
}()

// parseInt64 parses a validated integer token (optional leading '-', then
// digits) into an int64, reporting ok=false on overflow.
func parseInt64(b []byte, neg bool) (int64, bool) {
	i := 0
	if neg {
		i = 1
	}
	const cutoff = uint64(1) << 63
	var u uint64
	for ; i < len(b); i++ {
		d := uint64(b[i] - '0')
		if u > (math.MaxUint64-d)/10 {
			return 0, false
		}
		u = u*10 + d
	}
	if neg {
		if u > cutoff {
			return 0, false
		}
		return -int64(u), true
	}
	if u >= cutoff {
		return 0, false
	}
	return int64(u), true
}

// ── strings ────────────────────────────────────────────────────────────────

func (p *parser) parseString() (string, error) {
	in := p.data
	p.pos++ // opening quote
	start := p.pos
	// Fast path: AVX2-scan to the first '"' or '\'.
	p.pos += scanStringBody(in[p.pos:])
	if p.pos >= len(in) {
		return "", p.errf(p.pos, "unterminated string")
	}
	if in[p.pos] == '"' {
		s := string(in[start:p.pos])
		p.pos++
		return s, nil
	}
	// Slow path: escapes present. Copy what we have and decode.
	buf := p.scratch[:0]
	buf = append(buf, in[start:p.pos]...)
	for p.pos < len(in) {
		c := in[p.pos]
		switch {
		case c == '"':
			p.pos++
			p.scratch = buf
			return string(buf), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(in) {
				return "", p.errf(p.pos, "unterminated string")
			}
			e := in[p.pos]
			switch e {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case '/':
				buf = append(buf, '/')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'u':
				escPos := p.pos - 1
				p.pos++
				r, err := p.readHex4()
				if err != nil {
					return "", err
				}
				if utf16.IsSurrogate(rune(r)) {
					if r >= 0xD800 && r <= 0xDBFF &&
						p.pos+1 < len(in) && in[p.pos] == '\\' && in[p.pos+1] == 'u' {
						p.pos += 2
						low, err := p.readHex4()
						if err != nil {
							return "", err
						}
						if dec := utf16.DecodeRune(rune(r), rune(low)); dec != utf8.RuneError {
							buf = utf8.AppendRune(buf, dec)
							continue
						}
					}
					return "", p.errf(escPos, "invalid surrogate pair")
				}
				buf = utf8.AppendRune(buf, rune(r))
				continue
			default:
				return "", p.errf(p.pos, "invalid escape %q", e)
			}
			p.pos++
		default:
			// Bulk-copy the clean run up to the next '"' or '\'.
			r := scanStringBody(in[p.pos:])
			buf = append(buf, in[p.pos:p.pos+r]...)
			p.pos += r
		}
	}
	return "", p.errf(p.pos, "unterminated string")
}

func (p *parser) readHex4() (uint16, error) {
	if p.pos+4 > len(p.data) {
		return 0, p.errf(p.pos, "invalid \\u escape")
	}
	var v uint16
	for i := 0; i < 4; i++ {
		c := p.data[p.pos+i]
		var d uint16
		switch {
		case c >= '0' && c <= '9':
			d = uint16(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint16(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint16(c-'A') + 10
		default:
			return 0, p.errf(p.pos+i, "invalid hex digit in \\u escape")
		}
		v = v<<4 | d
	}
	p.pos += 4
	return v, nil
}

// ── objects ────────────────────────────────────────────────────────────────

func isKeyByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func (p *parser) parseObject() (any, error) {
	p.pos++ // '{'
	o := NewObject()
	for {
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unexpected end of input in object")
		}
		if p.data[p.pos] == '}' {
			p.pos++
			return o, nil
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errf(p.pos, "expected ':' after object key")
		}
		p.pos++
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		o.Set(key, val)
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unexpected end of input in object")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return o, nil
		default:
			return nil, p.errf(p.pos, "expected ',' or '}' in object")
		}
	}
}

func (p *parser) parseKey() (string, error) {
	p.skipWS()
	if p.pos >= len(p.data) {
		return "", p.errf(p.pos, "unexpected end of input")
	}
	c := p.data[p.pos]
	if c == '"' {
		return p.parseString()
	}
	if isKeyByte(c) {
		start := p.pos
		for p.pos < len(p.data) && isKeyByte(p.data[p.pos]) {
			p.pos++
		}
		return string(p.data[start:p.pos]), nil
	}
	return "", p.errf(p.pos, "expected object key")
}

// ── arrays and Zen Grid ────────────────────────────────────────────────────

func (p *parser) parseArrayOrGrid() (any, error) {
	p.pos++ // '['
	p.skipWS()
	if p.pos < len(p.data) {
		b := p.data[p.pos]
		if b == ':' {
			return p.parseZenGrid()
		}
		if b >= '0' && b <= '9' {
			q := p.pos + 1
			for q < len(p.data) && p.data[q] >= '0' && p.data[q] <= '9' {
				q++
			}
			for q < len(p.data) && p.data[q] == ' ' {
				q++
			}
			if q < len(p.data) && p.data[q] == ':' {
				p.pos = q
				return p.parseZenGrid()
			}
		}
	}
	return p.parseArrayBody()
}

func (p *parser) parseArrayBody() (any, error) {
	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return []any{}, nil
	}
	arr := []any{}
	for {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unexpected end of input in array")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
			p.skipWS()
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.pos++
				return arr, nil
			}
		case ']':
			p.pos++
			return arr, nil
		default:
			return nil, p.errf(p.pos, "expected ',' or ']' in array")
		}
	}
}

// parseZenGrid parses a Zen Grid table. pos must be at the ':' that follows the
// optional row-count prefix.
func (p *parser) parseZenGrid() (any, error) {
	p.pos++ // ':'
	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return []any{}, nil
	}
	headers, err := p.parseHeaders()
	if err != nil {
		return nil, err
	}
	unique := headersUnique(headers)
	rows := []any{}
	for {
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unterminated Zen Grid")
		}
		if p.data[p.pos] == ']' {
			p.pos++
			break
		}
		row, ended, err := p.parseRow(headers, unique)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		if ended {
			p.pos++ // consume the ']' parseRow stopped at
			break
		}
	}
	return rows, nil
}

func (p *parser) parseHeaders() ([]string, error) {
	headers := []string{}
	for {
		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unterminated Zen Grid header")
		}
		if p.data[p.pos] == ';' {
			p.pos++
			return headers, nil
		}
		h, err := p.parseHeaderKey()
		if err != nil {
			return nil, err
		}
		headers = append(headers, h)
		p.skipSpacesOnly()
		if p.pos >= len(p.data) {
			return nil, p.errf(p.pos, "unterminated Zen Grid header")
		}
		switch p.data[p.pos] {
		case ',', '\t', '|':
			p.pos++
		case ';':
			p.pos++
			return headers, nil
		default:
			return nil, p.errf(p.pos, "expected ',', ';' or ']' in Zen Grid header")
		}
	}
}

func (p *parser) parseHeaderKey() (string, error) {
	p.skipWS()
	if p.pos >= len(p.data) {
		return "", p.errf(p.pos, "unexpected end of input in Zen Grid header")
	}
	if p.data[p.pos] == '"' {
		return p.parseString()
	}
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ',' || c == ';' || c == ']' || c == '\t' || c == '|' {
			break
		}
		p.pos++
	}
	end := p.pos
	for end > start && isAsciiSpace(p.data[end-1]) {
		end--
	}
	return string(p.data[start:end]), nil
}

// parseRow parses one Zen Grid record. It returns the row object, whether the
// row terminated at the table's closing ']' (which it leaves unconsumed), and
// any error. Missing trailing cells become null; an extra cell beyond the
// header count is an error (matching the reference, which rejects it as
// trailing content).
func (p *parser) parseRow(headers []string, unique bool) (*Object, bool, error) {
	o := newObjectCap(len(headers))
	n := len(headers)
	col := 0
	fillNull := func(from int) {
		for j := from; j < n; j++ {
			o.setOrPush(headers[j], nil, unique)
		}
	}
	for {
		p.skipSpacesOnly()
		if p.pos >= len(p.data) {
			return nil, false, p.errf(p.pos, "unterminated Zen Grid row")
		}
		c := p.data[p.pos]
		if c == ';' {
			fillNull(col)
			p.pos++
			return o, false, nil
		}
		if c == ']' {
			fillNull(col)
			return o, true, nil
		}
		if col >= n {
			return nil, false, p.errf(p.pos, "too many values in Zen Grid row")
		}
		val, err := p.parseCellValue()
		if err != nil {
			return nil, false, err
		}
		o.setOrPush(headers[col], val, unique)
		col++
		p.skipSpacesOnly()
		if p.pos >= len(p.data) {
			return nil, false, p.errf(p.pos, "unterminated Zen Grid row")
		}
		switch p.data[p.pos] {
		case ',', '\t', '|':
			p.pos++
		case ';':
			fillNull(col)
			p.pos++
			return o, false, nil
		case ']':
			fillNull(col)
			return o, true, nil
		default:
			return nil, false, p.errf(p.pos, "expected ',', ';' or ']' in Zen Grid row")
		}
	}
}

// headersUnique reports whether all header names are distinct, so rows can be
// built with the no-dedup pushUnique fast path.
func headersUnique(h []string) bool {
	if len(h) < 2 {
		return true
	}
	if len(h) <= 8 {
		for i := 0; i < len(h); i++ {
			for j := i + 1; j < len(h); j++ {
				if h[i] == h[j] {
					return false
				}
			}
		}
		return true
	}
	seen := make(map[string]struct{}, len(h))
	for _, k := range h {
		if _, ok := seen[k]; ok {
			return false
		}
		seen[k] = struct{}{}
	}
	return true
}

// parseCellValue parses a single Zen Grid cell. A cell beginning with a JSON
// value character is parsed as a full JSON/JTON value (including nested objects,
// arrays, and tables); anything else is an unquoted string read up to the next
// delimiter, with backslash escaping the following byte. An empty cell is null.
func (p *parser) parseCellValue() (any, error) {
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, p.errf(p.pos, "unexpected end of input in Zen Grid cell")
	}
	switch c := p.data[p.pos]; {
	case c == '{' || c == '[' || c == '"' || c == 't' || c == 'f' || c == 'n' ||
		c == '-' || (c >= '0' && c <= '9') || c == 'I' || c == 'N':
		return p.parseValue()
	}
	start := p.pos
	sawEscape := false
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ',' || c == ';' || c == ']' || c == '\t' || c == '|' {
			break
		}
		if c == '\\' {
			sawEscape = true
			p.pos++
			if p.pos >= len(p.data) {
				return nil, p.errf(p.pos, "unterminated Zen Grid cell")
			}
			p.pos++
			continue
		}
		p.pos++
	}
	end := p.pos
	for end > start && isAsciiSpace(p.data[end-1]) {
		end--
	}
	if !sawEscape {
		if start == end {
			return nil, nil
		}
		return string(p.data[start:end]), nil
	}
	buf := make([]byte, 0, end-start)
	for i := start; i < end; {
		if p.data[i] == '\\' {
			i++
			if i >= end {
				break
			}
			buf = append(buf, p.data[i])
			i++
			continue
		}
		buf = append(buf, p.data[i])
		i++
	}
	return string(buf), nil
}
