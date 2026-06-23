package jton

import (
	"encoding/base64"
	"math/big"
	"strconv"
)

// Streaming writer. This is the low-level encoding surface that generated code
// and external codecs (for example protojton) use to emit JTON directly from
// their own data structures, with no value tree. It reuses the exact framing,
// indentation, and cell formatting of the value-tree encoder, so streamed output
// is identical to Marshaling the equivalent value.
//
// Container methods manage separators and indentation:
//
//	w.BeginObject(); w.Field("a"); w.Int(1); w.Field("b"); w.String("x"); w.EndObject()
//	w.BeginArray(); w.Int(1); w.Int(2); w.EndArray()
//	w.Table(headers, n, func(c *Cells, i int) error { ... })  // Zen Grid
//
// Field must be followed by exactly one value (or a Begin*/Table). In an array,
// each value/Begin*/Table is one element.

type wframe struct {
	kind byte // 'o' object, 'a' array
	n    int  // elements written so far
}

// Writer accumulates JTON output.
type Writer struct {
	e     *encoder
	stack []wframe
}

// NewWriter returns a Writer that encodes with the given options.
func NewWriter(opts Options) *Writer { return NewWriterSize(opts, 256) }

// NewWriterSize returns a Writer with the output buffer pre-sized to sizeHint
// bytes. A good hint (e.g. an estimate of the final size) avoids reallocations
// and the associated copies on large output.
func NewWriterSize(opts Options, sizeHint int) *Writer {
	if sizeHint < 64 {
		sizeHint = 64
	}
	return &Writer{e: &encoder{opts: opts, buf: make([]byte, 0, sizeHint)}}
}

// Bytes returns the encoded output accumulated so far.
func (w *Writer) Bytes() []byte { return w.e.buf }

func (w *Writer) depth() int { return len(w.stack) }

// beforeValue handles the separator and indentation before an array element or a
// top-level value. Object field values are positioned by Field instead.
func (w *Writer) beforeValue() {
	if len(w.stack) == 0 {
		return
	}
	top := &w.stack[len(w.stack)-1]
	if top.kind == 'a' {
		if top.n > 0 {
			w.e.buf = append(w.e.buf, ',')
		}
		if w.e.opts.Indent > 0 {
			w.e.buf = append(w.e.buf, '\n')
			w.e.writeIndent(len(w.stack) * w.e.opts.Indent)
		}
		top.n++
	}
}

// BeginObject opens an object.
func (w *Writer) BeginObject() {
	w.beforeValue()
	w.stack = append(w.stack, wframe{kind: 'o'})
	w.e.buf = append(w.e.buf, '{')
}

// Field writes an object key. The next call writes its value.
func (w *Writer) Field(key string) {
	top := &w.stack[len(w.stack)-1]
	if top.n > 0 {
		w.e.buf = append(w.e.buf, ',')
	}
	if w.e.opts.Indent > 0 {
		w.e.buf = append(w.e.buf, '\n')
		w.e.writeIndent(len(w.stack) * w.e.opts.Indent)
	}
	top.n++
	w.e.writeKey(key)
	w.e.buf = append(w.e.buf, ':')
	if w.e.opts.Indent > 0 {
		w.e.buf = append(w.e.buf, ' ')
	}
}

// EndObject closes the current object.
func (w *Writer) EndObject() {
	top := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	if w.e.opts.Indent > 0 && top.n > 0 {
		w.e.buf = append(w.e.buf, '\n')
		w.e.writeIndent(len(w.stack) * w.e.opts.Indent)
	}
	w.e.buf = append(w.e.buf, '}')
}

// BeginArray opens an array.
func (w *Writer) BeginArray() {
	w.beforeValue()
	w.stack = append(w.stack, wframe{kind: 'a'})
	w.e.buf = append(w.e.buf, '[')
}

// EndArray closes the current array.
func (w *Writer) EndArray() {
	top := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	if w.e.opts.Indent > 0 && top.n > 0 {
		w.e.buf = append(w.e.buf, '\n')
		w.e.writeIndent(len(w.stack) * w.e.opts.Indent)
	}
	w.e.buf = append(w.e.buf, ']')
}

// Value writes an arbitrary Go value (the same input Marshal accepts).
func (w *Writer) Value(v any) error {
	if w.depth() == 0 {
		if done, err := w.e.tryStructTable(v); err != nil {
			return err
		} else if done {
			return nil
		}
	}
	w.beforeValue()
	nv, _, err := normalize(v)
	if err != nil {
		return err
	}
	return w.e.encode(nv, w.depth())
}

// Null writes a null value.
func (w *Writer) Null() { w.beforeValue(); w.e.buf = append(w.e.buf, "null"...) }

// Bool writes a boolean value.
func (w *Writer) Bool(b bool) {
	w.beforeValue()
	if b {
		w.e.buf = append(w.e.buf, "true"...)
	} else {
		w.e.buf = append(w.e.buf, "false"...)
	}
}

// Int writes a signed integer value.
func (w *Writer) Int(i int64) { w.beforeValue(); w.e.buf = strconv.AppendInt(w.e.buf, i, 10) }

// Uint writes an unsigned integer value.
func (w *Writer) Uint(u uint64) { w.beforeValue(); w.e.buf = strconv.AppendUint(w.e.buf, u, 10) }

// BigInt writes an arbitrary-precision integer value.
func (w *Writer) BigInt(v *big.Int) { w.beforeValue(); w.e.buf = append(w.e.buf, v.String()...) }

// Float writes a floating-point value (NaN/Inf become the JTON literals).
func (w *Writer) Float(f float64) { w.beforeValue(); w.e.buf = appendFloat(w.e.buf, f) }

// String writes a quoted string value. (Bare strings apply only to Zen Grid
// cells; see Cells.String.)
func (w *Writer) String(s string) { w.beforeValue(); w.e.writeQuoted(s) }

// Base64 writes a byte slice as a base64 string value.
func (w *Writer) Base64(b []byte) {
	w.beforeValue()
	w.e.writeQuoted(base64.StdEncoding.EncodeToString(b))
}

// Table streams a Zen Grid with the given headers over nRows rows. writeRow is
// called once per row to emit that row's cells through Cells, which must receive
// exactly len(headers) cells in header order. All table options are honored; the
// output is byte-identical to Marshaling the equivalent slice of objects. Table
// is only valid at the top level (a Zen Grid is itself a JSON array).
func (w *Writer) Table(headers []string, nRows int, writeRow func(*Cells, int) error) error {
	w.beforeValue()
	c := &Cells{e: w.e, sep: w.e.opts.Delimiter.sep()}
	return w.e.writeZenFrame(nRows, headers, w.depth(), func(i int) error {
		c.col = 0
		return writeRow(c, i)
	})
}

// Cells writes the cells of one Zen Grid row. Each method writes one cell,
// inserting the delimiter before every cell after the first, and applies the
// same bare-string and implicit-null rules as the value-tree encoder.
type Cells struct {
	e   *encoder
	sep string
	col int
}

func (c *Cells) pre() {
	if c.col > 0 {
		c.e.buf = append(c.e.buf, c.sep...)
	}
	c.col++
}

// Null writes a null cell (empty when ImplicitNull is set).
func (c *Cells) Null() {
	c.pre()
	if !c.e.opts.ImplicitNull {
		c.e.buf = append(c.e.buf, "null"...)
	}
}

// Bool writes a boolean cell.
func (c *Cells) Bool(b bool) {
	c.pre()
	if b {
		c.e.buf = append(c.e.buf, "true"...)
	} else {
		c.e.buf = append(c.e.buf, "false"...)
	}
}

// Int writes a signed integer cell.
func (c *Cells) Int(i int64) {
	c.pre()
	c.e.buf = strconv.AppendInt(c.e.buf, i, 10)
}

// Uint writes an unsigned integer cell.
func (c *Cells) Uint(u uint64) {
	c.pre()
	c.e.buf = strconv.AppendUint(c.e.buf, u, 10)
}

// BigInt writes an arbitrary-precision integer cell.
func (c *Cells) BigInt(v *big.Int) {
	c.pre()
	c.e.buf = append(c.e.buf, v.String()...)
}

// Float writes a floating-point cell.
func (c *Cells) Float(f float64) {
	c.pre()
	c.e.buf = appendFloat(c.e.buf, f)
}

// String writes a string cell, bare when BareStrings is set and the value is an
// identifier, quoted otherwise.
func (c *Cells) String(s string) {
	c.pre()
	c.e.writeCellString(s)
}

// Bytes writes a byte slice as a base64 string cell.
func (c *Cells) Bytes(b []byte) {
	c.pre()
	c.e.writeCellString(base64.StdEncoding.EncodeToString(b))
}

// Value writes an arbitrary value as a cell (used for nested objects/arrays).
func (c *Cells) Value(v any) error {
	c.pre()
	nv, _, err := normalize(v)
	if err != nil {
		return err
	}
	return c.e.encode(nv, 1)
}
