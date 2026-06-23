// Package protojton marshals protobuf messages to JTON and back, reusing the
// core jton value tree and serializer so all the over-arching format logic
// (Zen Grid detection, framing, float formatting, options) lives in one place.
//
// It lives in a separate Go module so that the protobuf dependency does not
// reach code that only needs the pure jton library.
//
// # Mapping
//
// A message becomes a JTON object keyed by field name (proto name by default,
// JSON name with UseJSONNames). Scalars map to their JTON equivalents; bytes to
// base64; enums to their value name (or number with EnumsAsInts); nested
// messages to objects; repeated fields to arrays (a repeated all-scalar message
// becomes a Zen Grid); maps to objects with stringified keys. By default every
// field is emitted (including zero values) so repeated messages keep a stable,
// homogeneous schema that the Zen Grid can compress; set OmitDefaults to drop
// unset fields.
package protojton

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"

	"github.com/birdayz/jton-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// MarshalOptions configures marshaling, following the google.golang.org/protobuf
// idiom of an options struct with a method.
type MarshalOptions struct {
	// JTON is forwarded to the core serializer (Zen Grid, delimiter, indent, …).
	JTON jton.Options
	// UseJSONNames keys objects by the field's JSON name (camelCase) instead of
	// the proto field name.
	UseJSONNames bool
	// EnumsAsInts emits enum values as numbers instead of their value names.
	EnumsAsInts bool
	// OmitDefaults drops fields holding their zero value (proto3 implicit
	// presence). Off by default so repeated messages stay homogeneous for the
	// Zen Grid.
	OmitDefaults bool
}

// UnmarshalOptions configures unmarshaling.
type UnmarshalOptions struct {
	// DiscardUnknown ignores object keys that do not match a field instead of
	// returning an error.
	DiscardUnknown bool
}

// Marshal returns the JTON encoding of m using default options.
func Marshal(m proto.Message) ([]byte, error) { return MarshalOptions{}.Marshal(m) }

// Marshal returns the JTON encoding of m.
func (o MarshalOptions) Marshal(m proto.Message) ([]byte, error) {
	if m == nil {
		return jton.MarshalOptions(nil, o.JTON)
	}
	return jton.MarshalOptions(messageToValue(m.ProtoReflect(), o), o.JTON)
}

// MarshalList encodes a slice of messages as a JTON array; an all-scalar,
// homogeneous set becomes a Zen Grid.
func MarshalList[M proto.Message](msgs []M, opts MarshalOptions) ([]byte, error) {
	arr := make([]any, len(msgs))
	for i, m := range msgs {
		arr[i] = messageToValue(m.ProtoReflect(), opts)
	}
	return jton.MarshalOptions(arr, opts.JTON)
}

// Unmarshal parses JTON data into m using default options.
func Unmarshal(data []byte, m proto.Message) error { return UnmarshalOptions{}.Unmarshal(data, m) }

// Unmarshal parses JTON data into m.
func (o UnmarshalOptions) Unmarshal(data []byte, m proto.Message) error {
	v, err := jton.Parse(data)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	return valueToMessage(v, m.ProtoReflect(), o)
}

// UnmarshalList parses a JTON array into messages produced by newMsg.
func UnmarshalList(data []byte, newMsg func() proto.Message, opts UnmarshalOptions) ([]proto.Message, error) {
	v, err := jton.Parse(data)
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("protojton: expected a JTON array, got %T", v)
	}
	out := make([]proto.Message, len(arr))
	for i, e := range arr {
		m := newMsg()
		if err := valueToMessage(e, m.ProtoReflect(), opts); err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

// ── encode: proto -> jton value tree ───────────────────────────────────────

func messageToValue(m protoreflect.Message, opts MarshalOptions) any {
	if !m.IsValid() {
		return nil
	}
	fields := m.Descriptor().Fields()
	o := jton.NewObjectCap(fields.Len())
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !m.Has(fd) {
			// Omit unset presence-tracked fields (optional, oneof members, message
			// fields) so their unset state round-trips. Implicit-presence proto3
			// scalars are emitted at their zero value (unless OmitDefaults) so a
			// repeated message keeps a homogeneous schema for the Zen Grid.
			if opts.OmitDefaults || fd.HasPresence() {
				continue
			}
		}
		o.Set(fieldName(fd, opts), fieldToValue(m, fd, opts))
	}
	return o
}

func fieldName(fd protoreflect.FieldDescriptor, opts MarshalOptions) string {
	if opts.UseJSONNames {
		return fd.JSONName()
	}
	return string(fd.Name())
}

func fieldToValue(m protoreflect.Message, fd protoreflect.FieldDescriptor, opts MarshalOptions) any {
	switch {
	case fd.IsList():
		if !m.Has(fd) {
			return []any{}
		}
		list := m.Get(fd).List()
		out := make([]any, list.Len())
		isMsg := fd.Message() != nil
		for i := 0; i < list.Len(); i++ {
			if isMsg {
				out[i] = messageToValue(list.Get(i).Message(), opts)
			} else {
				out[i] = singleToValue(list.Get(i), fd, opts)
			}
		}
		return out
	case fd.IsMap():
		mp := m.Get(fd).Map()
		return mapToValue(mp, fd, opts)
	case fd.Message() != nil:
		if !m.Has(fd) {
			return nil
		}
		return messageToValue(m.Get(fd).Message(), opts)
	default:
		return singleToValue(m.Get(fd), fd, opts)
	}
}

func mapToValue(mp protoreflect.Map, fd protoreflect.FieldDescriptor, opts MarshalOptions) any {
	valFD := fd.MapValue()
	type kv struct {
		k string
		v protoreflect.Value
	}
	pairs := make([]kv, 0, mp.Len())
	mp.Range(func(mk protoreflect.MapKey, mv protoreflect.Value) bool {
		pairs = append(pairs, kv{mapKeyString(mk), mv})
		return true
	})
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	o := jton.NewObjectCap(len(pairs))
	for _, p := range pairs {
		if valFD.Message() != nil {
			o.Set(p.k, messageToValue(p.v.Message(), opts))
		} else {
			o.Set(p.k, singleToValue(p.v, valFD, opts))
		}
	}
	return o
}

func mapKeyString(k protoreflect.MapKey) string {
	switch v := k.Interface().(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func singleToValue(v protoreflect.Value, fd protoreflect.FieldDescriptor, opts MarshalOptions) any {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return int64(v.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return uintValue(v.Uint())
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return v.Float()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(v.Bytes())
	case protoreflect.EnumKind:
		num := v.Enum()
		if opts.EnumsAsInts {
			return int64(num)
		}
		if ev := fd.Enum().Values().ByNumber(num); ev != nil {
			return string(ev.Name())
		}
		return int64(num) // unknown enum value: emit the number
	default:
		return nil
	}
}

// uintValue keeps a uint64 exact: int64 when it fits, *big.Int otherwise.
func uintValue(u uint64) any {
	if u <= math.MaxInt64 {
		return int64(u)
	}
	return new(big.Int).SetUint64(u)
}

// ── decode: jton value tree -> proto ───────────────────────────────────────

func valueToMessage(v any, m protoreflect.Message, opts UnmarshalOptions) error {
	o, ok := v.(*jton.Object)
	if !ok {
		return fmt.Errorf("protojton: cannot decode %T into message %s", v, m.Descriptor().FullName())
	}
	fields := m.Descriptor().Fields()
	for i := 0; i < o.Len(); i++ {
		key, val := o.At(i)
		fd := lookupField(fields, key)
		if fd == nil {
			if opts.DiscardUnknown {
				continue
			}
			return fmt.Errorf("protojton: unknown field %q in %s", key, m.Descriptor().FullName())
		}
		if val == nil {
			continue // null leaves the field unset
		}
		if err := setField(m, fd, val, opts); err != nil {
			return err
		}
	}
	return nil
}

func lookupField(fields protoreflect.FieldDescriptors, key string) protoreflect.FieldDescriptor {
	if fd := fields.ByName(protoreflect.Name(key)); fd != nil {
		return fd
	}
	return fields.ByJSONName(key)
}

func setField(m protoreflect.Message, fd protoreflect.FieldDescriptor, val any, opts UnmarshalOptions) error {
	switch {
	case fd.IsList():
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("protojton: field %s expects an array, got %T", fd.Name(), val)
		}
		list := m.Mutable(fd).List()
		for _, e := range arr {
			if fd.Message() != nil {
				em := list.NewElement()
				if err := valueToMessage(e, em.Message(), opts); err != nil {
					return err
				}
				list.Append(em)
			} else {
				pv, err := scalarToProto(e, fd)
				if err != nil {
					return err
				}
				list.Append(pv)
			}
		}
		return nil
	case fd.IsMap():
		o, ok := val.(*jton.Object)
		if !ok {
			return fmt.Errorf("protojton: field %s expects an object, got %T", fd.Name(), val)
		}
		mp := m.Mutable(fd).Map()
		valFD := fd.MapValue()
		for i := 0; i < o.Len(); i++ {
			ks, mv := o.At(i)
			mk, err := mapKeyToProto(ks, fd.MapKey())
			if err != nil {
				return err
			}
			if valFD.Message() != nil {
				em := mp.NewValue()
				if err := valueToMessage(mv, em.Message(), opts); err != nil {
					return err
				}
				mp.Set(mk, em)
			} else {
				pv, err := scalarToProto(mv, valFD)
				if err != nil {
					return err
				}
				mp.Set(mk, pv)
			}
		}
		return nil
	case fd.Message() != nil:
		sub := m.NewField(fd)
		if err := valueToMessage(val, sub.Message(), opts); err != nil {
			return err
		}
		m.Set(fd, sub)
		return nil
	default:
		pv, err := scalarToProto(val, fd)
		if err != nil {
			return err
		}
		m.Set(fd, pv)
		return nil
	}
}

func scalarToProto(val any, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		b, ok := val.(bool)
		if !ok {
			return protoreflect.Value{}, kindErr(val, fd)
		}
		return protoreflect.ValueOfBool(b), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := toInt(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return protoreflect.Value{}, fmt.Errorf("protojton: %d overflows int32 field %s", n, fd.Name())
		}
		return protoreflect.ValueOfInt32(int32(n)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := toInt(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt64(n), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := toUint(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		if n > math.MaxUint32 {
			return protoreflect.Value{}, fmt.Errorf("protojton: %d overflows uint32 field %s", n, fd.Name())
		}
		return protoreflect.ValueOfUint32(uint32(n)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := toUint(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint64(n), nil
	case protoreflect.FloatKind:
		f, err := toFloat(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfFloat32(float32(f)), nil
	case protoreflect.DoubleKind:
		f, err := toFloat(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfFloat64(f), nil
	case protoreflect.StringKind:
		s, ok := val.(string)
		if !ok {
			return protoreflect.Value{}, kindErr(val, fd)
		}
		return protoreflect.ValueOfString(s), nil
	case protoreflect.BytesKind:
		s, ok := val.(string)
		if !ok {
			return protoreflect.Value{}, kindErr(val, fd)
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("protojton: invalid base64 for field %s: %w", fd.Name(), err)
		}
		return protoreflect.ValueOfBytes(b), nil
	case protoreflect.EnumKind:
		return enumToProto(val, fd)
	default:
		return protoreflect.Value{}, fmt.Errorf("protojton: unsupported field kind %s", fd.Kind())
	}
}

func enumToProto(val any, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	switch v := val.(type) {
	case string:
		ev := fd.Enum().Values().ByName(protoreflect.Name(v))
		if ev == nil {
			return protoreflect.Value{}, fmt.Errorf("protojton: unknown enum value %q for %s", v, fd.Name())
		}
		return protoreflect.ValueOfEnum(ev.Number()), nil
	default:
		n, err := toInt(val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(n)), nil
	}
}

func mapKeyToProto(s string, fd protoreflect.FieldDescriptor) (protoreflect.MapKey, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return protoreflect.MapKey{}, err
		}
		return protoreflect.ValueOfBool(b).MapKey(), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return protoreflect.MapKey{}, err
		}
		return protoreflect.ValueOfInt32(int32(n)).MapKey(), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return protoreflect.MapKey{}, err
		}
		return protoreflect.ValueOfInt64(n).MapKey(), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return protoreflect.MapKey{}, err
		}
		return protoreflect.ValueOfUint32(uint32(n)).MapKey(), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return protoreflect.MapKey{}, err
		}
		return protoreflect.ValueOfUint64(n).MapKey(), nil
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(s).MapKey(), nil
	default:
		return protoreflect.MapKey{}, fmt.Errorf("protojton: unsupported map key kind %s", fd.Kind())
	}
}

// ── numeric coercion from the jton value tree ──────────────────────────────

func toInt(val any) (int64, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("protojton: %v is not an integer", v)
		}
		return int64(v), nil
	case *big.Int:
		if !v.IsInt64() {
			return 0, fmt.Errorf("protojton: %v overflows int64", v)
		}
		return v.Int64(), nil
	default:
		return 0, fmt.Errorf("protojton: cannot use %T as integer", val)
	}
}

func toUint(val any) (uint64, error) {
	switch v := val.(type) {
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("protojton: %d cannot be unsigned", v)
		}
		return uint64(v), nil
	case float64:
		if v < 0 || v != math.Trunc(v) {
			return 0, fmt.Errorf("protojton: %v is not a non-negative integer", v)
		}
		return uint64(v), nil
	case *big.Int:
		if !v.IsUint64() {
			return 0, fmt.Errorf("protojton: %v overflows uint64", v)
		}
		return v.Uint64(), nil
	default:
		return 0, fmt.Errorf("protojton: cannot use %T as unsigned integer", val)
	}
}

func toFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case *big.Int:
		f, _ := new(big.Float).SetInt(v).Float64()
		return f, nil
	default:
		return 0, fmt.Errorf("protojton: cannot use %T as float", val)
	}
}

func kindErr(val any, fd protoreflect.FieldDescriptor) error {
	return fmt.Errorf("protojton: cannot decode %T into %s field %s", val, fd.Kind(), fd.Name())
}
