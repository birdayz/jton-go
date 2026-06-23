package jton

import (
	"bytes"
	"encoding/json"
	"io"
)

// Loads parses a JTON/JSON string into a Go value, mirroring the reference
// jton.loads. See Parse for the value tree it returns.
func Loads(s string) (any, error) { return Parse([]byte(s)) }

// Dumps serializes v to a JTON string using the default options, mirroring the
// reference jton.dumps.
func Dumps(v any) (string, error) {
	b, err := Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DumpsOptions serializes v to a JTON string with the given options.
func DumpsOptions(v any, opts Options) (string, error) {
	b, err := MarshalOptions(v, opts)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Unmarshal parses JTON data into v. If v is *any, the canonical value tree is
// stored directly (numbers as int64/*big.Int/float64, objects as *Object).
// Otherwise the parsed tree is bridged through encoding/json into v, so structs,
// maps, and slices decode as they would with the standard library (this path
// cannot represent the Infinity/NaN literals and will error on them).
func Unmarshal(data []byte, v any) error {
	parsed, err := Parse(data)
	if err != nil {
		return err
	}
	if pv, ok := v.(*any); ok {
		*pv = parsed
		return nil
	}
	js, err := MarshalOptions(parsed, Options{NoZenGrid: true})
	if err != nil {
		return err
	}
	return json.Unmarshal(js, v)
}

// ToJSON converts a JTON document (which may use Zen Grid, comments, unquoted
// keys, or special numbers) into standard JSON. Infinity/-Infinity/NaN are
// emitted as the JavaScript-style literals (matching Python's json default),
// which strict JSON consumers will reject.
func ToJSON(data []byte) ([]byte, error) {
	v, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return MarshalOptions(v, Options{NoZenGrid: true})
}

// Decoder reads JTON values from a stream. It is a thin convenience wrapper:
// the whole input is read and parsed on Decode.
type Decoder struct{ r io.Reader }

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: r} }

// Decode reads the entire stream and unmarshals it into v.
func (d *Decoder) Decode(v any) error {
	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	return Unmarshal(data, v)
}

// Encoder writes JTON values to a stream.
type Encoder struct {
	w    io.Writer
	opts Options
}

// NewEncoder returns an Encoder writing to w with default options.
func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: w} }

// SetOptions sets the serialization options and returns the Encoder for
// chaining.
func (e *Encoder) SetOptions(opts Options) *Encoder { e.opts = opts; return e }

// Encode serializes v and writes it followed by a newline.
func (e *Encoder) Encode(v any) error {
	b, err := MarshalOptions(v, e.opts)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

// MarshalJSON makes *Object usable with encoding/json, preserving key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i := 0; i < o.Len(); i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, v := o.At(i)
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
