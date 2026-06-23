package jton

import (
	"encoding/base64"
	"reflect"
	"strconv"
	"sync"
)

// Direct struct-table fast path for Marshal.
//
// The library's headline case is []Struct -> Zen Grid. Routing that through the
// canonical value tree boxes every cell and allocates an Object per row. This
// path streams a []Struct straight to the output buffer with no tree and no
// boxing, reusing the shared frame (writeZenFrame) and cell primitives.
//
// It only fires when its output is provably identical to the tree path, and
// falls back to the tree otherwise. The marshal_reflect_test cross-check asserts
// byte-equality against the tree path over random structs and every option.

type tableField struct {
	index   int
	kind    reflect.Kind // scalar kind (deref'd if pointer)
	ptr     bool         // field is a pointer to a scalar
	isBytes bool         // field is []byte
}

type tablePlan struct {
	headers []string
	fields  []tableField
}

var tablePlanCache sync.Map // reflect.Type -> *tablePlan (typed nil = ineligible)

// scalarTablePlan returns the column plan for a struct whose every exported
// field is a scalar (or pointer-to-scalar, or []byte), or nil if the struct is
// not eligible for the direct table path (embedded fields, omitempty, or any
// non-scalar field send it to the tree path instead).
func scalarTablePlan(t reflect.Type) *tablePlan {
	if c, ok := tablePlanCache.Load(t); ok {
		return c.(*tablePlan)
	}
	p := computeScalarTablePlan(t)
	tablePlanCache.Store(t, p)
	return p
}

func computeScalarTablePlan(t reflect.Type) *tablePlan {
	var p tablePlan
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous { // embedding: defer to the tree path
			return nil
		}
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts := parseTag(tag)
		if opts.contains("omitempty") { // would vary the key set per row
			return nil
		}
		if name == "" {
			name = sf.Name
		}
		ft := sf.Type
		tf := tableField{index: i}
		ck := ft.Kind()
		if ck == reflect.Pointer {
			tf.ptr = true
			ck = ft.Elem().Kind()
		}
		switch {
		case ck == reflect.Bool, isIntKind(ck), isUintKind(ck), isFloatKind(ck), ck == reflect.String:
			tf.kind = ck
		case !tf.ptr && ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Uint8:
			tf.isBytes = true
		default:
			return nil // non-scalar field
		}
		p.headers = append(p.headers, name)
		p.fields = append(p.fields, tf)
	}
	if len(p.fields) == 0 {
		return nil
	}
	return &p
}

func isIntKind(k reflect.Kind) bool {
	return k == reflect.Int || k == reflect.Int8 || k == reflect.Int16 || k == reflect.Int32 || k == reflect.Int64
}
func isUintKind(k reflect.Kind) bool {
	return k == reflect.Uint || k == reflect.Uint8 || k == reflect.Uint16 || k == reflect.Uint32 || k == reflect.Uint64 || k == reflect.Uintptr
}
func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

// tryStructTable handles v directly if it is a Zen-Grid-eligible slice/array of
// value structs. It returns done=true only after writing the full table; if v is
// not eligible it returns done=false with the buffer untouched, so the caller
// can fall back to the tree path.
func (e *encoder) tryStructTable(v any) (bool, error) {
	if e.opts.NoZenGrid {
		return false, nil
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return false, nil
	}
	k := rv.Kind()
	if k != reflect.Slice && k != reflect.Array {
		return false, nil
	}
	if rv.Type().Elem().Kind() != reflect.Struct {
		return false, nil
	}
	n := rv.Len()
	if n < 2 {
		return false, nil
	}
	plan := scalarTablePlan(rv.Type().Elem())
	if plan == nil {
		return false, nil
	}
	if !e.structTableEligible(rv, plan, n) {
		return false, nil
	}
	err := e.writeZenFrame(n, plan.headers, 0, func(i int) error {
		e.writeStructRow(rv.Index(i), plan)
		return nil
	})
	return true, err
}

// structTableEligible mirrors detectZenGrid's sampling: only the first
// min(n,10) rows are checked, and the sole runtime condition for a typed scalar
// table is that sampled string cells contain no structural characters.
func (e *encoder) structTableEligible(rv reflect.Value, plan *tablePlan, n int) bool {
	sample := n
	if sample > 10 {
		sample = 10
	}
	for i := 0; i < sample; i++ {
		sv := rv.Index(i)
		for _, f := range plan.fields {
			if f.kind != reflect.String || f.isBytes {
				continue
			}
			fv := sv.Field(f.index)
			if f.ptr {
				if fv.IsNil() {
					continue
				}
				fv = fv.Elem()
			}
			if !isSafeZenString(fv.String()) {
				return false
			}
		}
	}
	return true
}

func (e *encoder) writeStructRow(sv reflect.Value, plan *tablePlan) {
	sep := e.opts.Delimiter.sep()
	for i, f := range plan.fields {
		if i > 0 {
			e.buf = append(e.buf, sep...)
		}
		fv := sv.Field(f.index)
		if f.ptr {
			if fv.IsNil() {
				if !e.opts.ImplicitNull {
					e.buf = append(e.buf, "null"...)
				}
				continue
			}
			fv = fv.Elem()
		}
		if f.isBytes {
			e.writeCellString(base64.StdEncoding.EncodeToString(fv.Bytes()))
			continue
		}
		switch {
		case f.kind == reflect.Bool:
			if fv.Bool() {
				e.buf = append(e.buf, "true"...)
			} else {
				e.buf = append(e.buf, "false"...)
			}
		case isIntKind(f.kind):
			e.buf = strconv.AppendInt(e.buf, fv.Int(), 10)
		case isUintKind(f.kind):
			e.buf = strconv.AppendUint(e.buf, fv.Uint(), 10)
		case isFloatKind(f.kind):
			e.buf = appendFloat(e.buf, fv.Float())
		case f.kind == reflect.String:
			e.writeCellString(fv.String())
		}
	}
}

// writeCellString writes a Zen Grid string cell, applying bare_strings exactly
// as writeZenRow does.
func (e *encoder) writeCellString(s string) {
	if e.opts.BareStrings && isValidIdentifier(s) {
		e.buf = append(e.buf, s...)
	} else {
		e.writeQuoted(s)
	}
}

// marshalViaTree always uses the canonical value-tree path. It is the oracle the
// direct fast path is cross-checked against in tests.
func marshalViaTree(v any, opts Options) ([]byte, error) {
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
