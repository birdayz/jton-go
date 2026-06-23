package jton

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// This file implements the direct reflection bridge between Go values and the
// canonical JTON value tree (nil, bool, string, int64, *big.Int, float64,
// *Object, []any), used by Marshal for non-native types and by Unmarshal for
// non-*any targets. It does not route through encoding/json; struct field plans
// are reflected once per type and cached.

var (
	jsonMarshalerType   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	bigIntType          = reflect.TypeOf((*big.Int)(nil))
)

// ── encode: Go value -> canonical tree ─────────────────────────────────────

func reflectEncode(rv reflect.Value) (any, error) {
	if !rv.IsValid() {
		return nil, nil
	}
	t := rv.Type()

	// Honor Marshaler interfaces first, like encoding/json, so types such as
	// time.Time keep their custom representation.
	if t != bigIntType {
		if m, ok := marshalerFor(rv, jsonMarshalerType); ok {
			b, err := m.Interface().(json.Marshaler).MarshalJSON()
			if err != nil {
				return nil, err
			}
			return Parse(b)
		}
		if m, ok := marshalerFor(rv, textMarshalerType); ok {
			b, err := m.Interface().(encoding.TextMarshaler).MarshalText()
			if err != nil {
				return nil, err
			}
			return string(b), nil
		}
	}

	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
		return reflectEncode(rv.Elem())
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := rv.Int()
		if n >= 0 && n < int64(len(smallInt)) {
			return smallInt[n], nil
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uintToCanonical(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	case reflect.String:
		return rv.String(), nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return base64.StdEncoding.EncodeToString(rv.Bytes()), nil
		}
		if rv.IsNil() {
			return nil, nil
		}
		return encodeList(rv)
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(b), rv)
			return base64.StdEncoding.EncodeToString(b), nil
		}
		return encodeList(rv)
	case reflect.Map:
		if rv.IsNil() {
			return nil, nil
		}
		return encodeMap(rv)
	case reflect.Struct:
		return encodeStruct(rv)
	default:
		return nil, fmt.Errorf("jton: unsupported type %s", t)
	}
}

func marshalerFor(rv reflect.Value, iface reflect.Type) (reflect.Value, bool) {
	t := rv.Type()
	if t.Implements(iface) {
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			return reflect.Value{}, false
		}
		return rv, true
	}
	if rv.CanAddr() && reflect.PointerTo(t).Implements(iface) {
		return rv.Addr(), true
	}
	return reflect.Value{}, false
}

func encodeList(rv reflect.Value) (any, error) {
	n := rv.Len()
	out := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := reflectEncode(rv.Index(i))
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func encodeMap(rv reflect.Value) (any, error) {
	keys := rv.MapKeys()
	type kv struct {
		s string
		v reflect.Value
	}
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		ks, err := mapKeyString(k)
		if err != nil {
			return nil, err
		}
		pairs[i] = kv{ks, k}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].s < pairs[j].s })
	o := newObjectCap(len(pairs))
	for _, p := range pairs {
		v, err := reflectEncode(rv.MapIndex(p.v))
		if err != nil {
			return nil, err
		}
		o.Set(p.s, v) // Set, not pushUnique: distinct map keys can share a string form
	}
	return o, nil
}

func mapKeyString(k reflect.Value) (string, error) {
	if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
		b, err := tm.MarshalText()
		return string(b), err
	}
	switch k.Kind() {
	case reflect.String:
		return k.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	default:
		return "", fmt.Errorf("jton: unsupported map key type %s", k.Type())
	}
}

func encodeStruct(rv reflect.Value) (any, error) {
	fields := cachedFields(rv.Type())
	o := newObjectCap(len(fields))
	for i := range fields {
		f := &fields[i]
		fv := fieldByIndex(rv, f.index)
		if !fv.IsValid() {
			continue
		}
		if f.omitEmpty && isEmptyValue(fv) {
			continue
		}
		v, err := reflectEncode(fv)
		if err != nil {
			return nil, err
		}
		o.pushUnique(f.name, v) // field names are resolved unique
	}
	return o, nil
}

func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for i, x := range index {
		if i > 0 {
			if v.Kind() == reflect.Pointer {
				if v.IsNil() {
					return reflect.Value{}
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v
}

// ── struct field plans (cached) ────────────────────────────────────────────

type fieldInfo struct {
	name      string
	index     []int
	omitEmpty bool
}

var fieldCache sync.Map // reflect.Type -> []fieldInfo

func cachedFields(t reflect.Type) []fieldInfo {
	if c, ok := fieldCache.Load(t); ok {
		return c.([]fieldInfo)
	}
	f := computeFields(t)
	fieldCache.Store(t, f)
	return f
}

// computeFields resolves a struct's encodable fields with json-tag semantics and
// breadth-first embedded-field promotion (shallower depth wins; a name tied at
// the same depth is dropped).
func computeFields(t reflect.Type) []fieldInfo {
	type candidate struct {
		fieldInfo
		depth  int
		tagged bool
	}
	var out []candidate
	seen := map[reflect.Type]bool{}
	type queued struct {
		t     reflect.Type
		index []int
		depth int
	}
	queue := []queued{{t, nil, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur.t] {
			continue
		}
		seen[cur.t] = true
		for i := 0; i < cur.t.NumField(); i++ {
			sf := cur.t.Field(i)
			tag := sf.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, opts := parseTag(tag)
			tagged := name != ""
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			idx := append(append([]int(nil), cur.index...), i)

			ft := sf.Type
			if sf.Anonymous && !tagged {
				et := ft
				if et.Kind() == reflect.Pointer {
					et = et.Elem()
				}
				if et.Kind() == reflect.Struct {
					queue = append(queue, queued{et, idx, cur.depth + 1})
					continue
				}
				if !sf.IsExported() {
					continue
				}
			}
			if !sf.IsExported() {
				continue
			}
			if name == "" {
				name = sf.Name
			}
			out = append(out, candidate{
				fieldInfo: fieldInfo{name: name, index: idx, omitEmpty: opts.contains("omitempty")},
				depth:     cur.depth,
				tagged:    tagged,
			})
		}
	}

	// Resolve name collisions: shallowest depth wins; a tagged field beats an
	// untagged one at the same depth; otherwise a tie is dropped.
	byName := map[string][]candidate{}
	order := []string{}
	for _, c := range out {
		if _, ok := byName[c.name]; !ok {
			order = append(order, c.name)
		}
		byName[c.name] = append(byName[c.name], c)
	}
	var fields []fieldInfo
	for _, name := range order {
		cs := byName[name]
		best := cs[0]
		tie := false
		for _, c := range cs[1:] {
			switch {
			case c.depth < best.depth:
				best, tie = c, false
			case c.depth > best.depth:
			case c.tagged && !best.tagged:
				best, tie = c, false
			case best.tagged && !c.tagged:
			default:
				tie = true
			}
		}
		if !tie {
			fields = append(fields, best.fieldInfo)
		}
	}
	// Stable output in declaration/promotion order.
	sort.SliceStable(fields, func(i, j int) bool {
		return lessIndex(fields[i].index, fields[j].index)
	})
	return fields
}

func lessIndex(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

type tagOptions string

func parseTag(tag string) (string, tagOptions) {
	name, opts, _ := strings.Cut(tag, ",")
	return name, tagOptions(opts)
}

func (o tagOptions) contains(opt string) bool {
	s := string(o)
	for s != "" {
		var cur string
		cur, s, _ = strings.Cut(s, ",")
		if cur == opt {
			return true
		}
	}
	return false
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

// ── decode: canonical tree -> Go value ─────────────────────────────────────

func reflectDecode(val any, rv reflect.Value) error {
	// Allocate through pointers and honor Unmarshaler interfaces.
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		if u, ok := unmarshalerFor(rv); ok {
			return invokeUnmarshaler(u, val)
		}
		return reflectDecode(val, rv.Elem())
	}
	if rv.CanAddr() {
		if u, ok := unmarshalerFor(rv.Addr()); ok {
			return invokeUnmarshaler(u, val)
		}
	}

	if val == nil {
		rv.Set(reflect.Zero(rv.Type()))
		return nil
	}

	switch rv.Kind() {
	case reflect.Interface:
		if rv.NumMethod() == 0 {
			rv.Set(reflect.ValueOf(val))
			return nil
		}
		return fmt.Errorf("jton: cannot decode into non-empty interface %s", rv.Type())
	case reflect.Bool:
		b, ok := val.(bool)
		if !ok {
			return typeErr(val, rv)
		}
		rv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt64(val)
		if err != nil {
			return err
		}
		if rv.OverflowInt(n) {
			return fmt.Errorf("jton: %d overflows %s", n, rv.Type())
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := toUint64(val)
		if err != nil {
			return err
		}
		if rv.OverflowUint(n) {
			return fmt.Errorf("jton: %d overflows %s", n, rv.Type())
		}
		rv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := toFloat64(val)
		if err != nil {
			return err
		}
		rv.SetFloat(f)
	case reflect.String:
		s, ok := val.(string)
		if !ok {
			return typeErr(val, rv)
		}
		rv.SetString(s)
	case reflect.Slice:
		return decodeSlice(val, rv)
	case reflect.Array:
		return decodeArray(val, rv)
	case reflect.Map:
		return decodeMap(val, rv)
	case reflect.Struct:
		return decodeStruct(val, rv)
	default:
		return fmt.Errorf("jton: cannot decode into %s", rv.Type())
	}
	return nil
}

func unmarshalerFor(ptr reflect.Value) (reflect.Value, bool) {
	t := ptr.Type()
	if t.Implements(jsonUnmarshalerType) || t.Implements(textUnmarshalerType) {
		return ptr, true
	}
	return reflect.Value{}, false
}

func invokeUnmarshaler(ptr reflect.Value, val any) error {
	if u, ok := ptr.Interface().(json.Unmarshaler); ok {
		js, err := MarshalOptions(val, Options{NoZenGrid: true})
		if err != nil {
			return err
		}
		return u.UnmarshalJSON(js)
	}
	if u, ok := ptr.Interface().(encoding.TextUnmarshaler); ok {
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("jton: TextUnmarshaler %s requires a string", ptr.Type())
		}
		return u.UnmarshalText([]byte(s))
	}
	return fmt.Errorf("jton: %s is not an unmarshaler", ptr.Type())
}

func decodeSlice(val any, rv reflect.Value) error {
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		s, ok := val.(string)
		if !ok {
			return typeErr(val, rv)
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return fmt.Errorf("jton: invalid base64 for %s: %w", rv.Type(), err)
		}
		rv.SetBytes(b)
		return nil
	}
	arr, ok := val.([]any)
	if !ok {
		return typeErr(val, rv)
	}
	out := reflect.MakeSlice(rv.Type(), len(arr), len(arr))
	for i, e := range arr {
		if err := reflectDecode(e, out.Index(i)); err != nil {
			return err
		}
	}
	rv.Set(out)
	return nil
}

func decodeArray(val any, rv reflect.Value) error {
	arr, ok := val.([]any)
	if !ok {
		return typeErr(val, rv)
	}
	n := rv.Len()
	for i := 0; i < n && i < len(arr); i++ {
		if err := reflectDecode(arr[i], rv.Index(i)); err != nil {
			return err
		}
	}
	for i := len(arr); i < n; i++ {
		rv.Index(i).Set(reflect.Zero(rv.Type().Elem()))
	}
	return nil
}

func decodeMap(val any, rv reflect.Value) error {
	o, ok := val.(*Object)
	if !ok {
		return typeErr(val, rv)
	}
	mt := rv.Type()
	if rv.IsNil() {
		rv.Set(reflect.MakeMapWithSize(mt, o.Len()))
	}
	kt := mt.Key()
	for i := 0; i < o.Len(); i++ {
		ks, mv := o.At(i)
		kv, err := stringToMapKey(ks, kt)
		if err != nil {
			return err
		}
		ev := reflect.New(mt.Elem()).Elem()
		if err := reflectDecode(mv, ev); err != nil {
			return err
		}
		rv.SetMapIndex(kv, ev)
	}
	return nil
}

func stringToMapKey(s string, kt reflect.Type) (reflect.Value, error) {
	if reflect.PointerTo(kt).Implements(textUnmarshalerType) {
		kp := reflect.New(kt)
		if err := kp.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
			return reflect.Value{}, err
		}
		return kp.Elem(), nil
	}
	switch kt.Kind() {
	case reflect.String:
		return reflect.ValueOf(s).Convert(kt), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(n).Convert(kt), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(n).Convert(kt), nil
	default:
		return reflect.Value{}, fmt.Errorf("jton: unsupported map key type %s", kt)
	}
}

func decodeStruct(val any, rv reflect.Value) error {
	o, ok := val.(*Object)
	if !ok {
		return typeErr(val, rv)
	}
	fields := cachedFields(rv.Type())
	for i := range fields {
		f := &fields[i]
		fv, present := o.Get(f.name)
		if !present {
			continue
		}
		target := fieldByIndexAlloc(rv, f.index)
		if err := reflectDecode(fv, target); err != nil {
			return err
		}
	}
	return nil
}

func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}

// ── numeric coercion ───────────────────────────────────────────────────────

func toInt64(val any) (int64, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("jton: %v has a fractional part, cannot be an integer", v)
		}
		return int64(v), nil
	case *big.Int:
		if !v.IsInt64() {
			return 0, fmt.Errorf("jton: %v overflows int64", v)
		}
		return v.Int64(), nil
	default:
		return 0, fmt.Errorf("jton: cannot use %T as integer", val)
	}
}

func toUint64(val any) (uint64, error) {
	switch v := val.(type) {
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("jton: %d cannot be unsigned", v)
		}
		return uint64(v), nil
	case float64:
		if v < 0 || v != math.Trunc(v) {
			return 0, fmt.Errorf("jton: %v is not a non-negative integer", v)
		}
		return uint64(v), nil
	case *big.Int:
		if !v.IsUint64() {
			return 0, fmt.Errorf("jton: %v overflows uint64", v)
		}
		return v.Uint64(), nil
	default:
		return 0, fmt.Errorf("jton: cannot use %T as unsigned integer", val)
	}
}

func toFloat64(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case *big.Int:
		f := new(big.Float).SetInt(v)
		r, _ := f.Float64()
		return r, nil
	default:
		return 0, fmt.Errorf("jton: cannot use %T as float", val)
	}
}

func typeErr(val any, rv reflect.Value) error {
	return fmt.Errorf("jton: cannot decode %T into %s", val, rv.Type())
}
