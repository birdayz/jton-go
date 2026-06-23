package protojton

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"

	"github.com/birdayz/jton-go"
)

// Conversion helpers from the jton value tree into Go/proto scalar types. The
// generated UnmarshalJTON methods call these; they accept the value types Parse
// produces: int64, *big.Int, float64, bool, string, *jton.Object, []any.

// AsObject asserts v is a JTON object.
func AsObject(v any) (*jton.Object, error) {
	o, ok := v.(*jton.Object)
	if !ok {
		return nil, fmt.Errorf("protojton: expected object, got %T", v)
	}
	return o, nil
}

// AsArray asserts v is a JTON array.
func AsArray(v any) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("protojton: expected array, got %T", v)
	}
	return a, nil
}

// AsBool asserts v is a boolean.
func AsBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("protojton: expected bool, got %T", v)
	}
	return b, nil
}

// AsString asserts v is a string.
func AsString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("protojton: expected string, got %T", v)
	}
	return s, nil
}

// AsBytes decodes a base64 string into bytes.
func AsBytes(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("protojton: expected base64 string, got %T", v)
	}
	return base64.StdEncoding.DecodeString(s)
}

func asInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case float64:
		if n != math.Trunc(n) {
			return 0, fmt.Errorf("protojton: %v is not an integer", n)
		}
		return int64(n), nil
	case *big.Int:
		if !n.IsInt64() {
			return 0, fmt.Errorf("protojton: %v overflows int64", n)
		}
		return n.Int64(), nil
	default:
		return 0, fmt.Errorf("protojton: expected integer, got %T", v)
	}
}

func asUint64(v any) (uint64, error) {
	switch n := v.(type) {
	case int64:
		if n < 0 {
			return 0, fmt.Errorf("protojton: %d cannot be unsigned", n)
		}
		return uint64(n), nil
	case float64:
		if n < 0 || n != math.Trunc(n) {
			return 0, fmt.Errorf("protojton: %v is not a non-negative integer", n)
		}
		return uint64(n), nil
	case *big.Int:
		if !n.IsUint64() {
			return 0, fmt.Errorf("protojton: %v overflows uint64", n)
		}
		return n.Uint64(), nil
	default:
		return 0, fmt.Errorf("protojton: expected integer, got %T", v)
	}
}

// AsInt32 / AsInt64 / AsUint32 / AsUint64 / AsFloat32 / AsFloat64 convert a JTON
// number to the named proto scalar type, range-checking where applicable.

func AsInt32(v any) (int32, error) {
	n, err := asInt64(v)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("protojton: %d overflows int32", n)
	}
	return int32(n), nil
}

func AsInt64(v any) (int64, error) { return asInt64(v) }

func AsUint32(v any) (uint32, error) {
	n, err := asUint64(v)
	if err != nil {
		return 0, err
	}
	if n > math.MaxUint32 {
		return 0, fmt.Errorf("protojton: %d overflows uint32", n)
	}
	return uint32(n), nil
}

func AsUint64(v any) (uint64, error) { return asUint64(v) }

func AsFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case *big.Int:
		f, _ := new(big.Float).SetInt(n).Float64()
		return f, nil
	default:
		return 0, fmt.Errorf("protojton: expected number, got %T", v)
	}
}

func AsFloat32(v any) (float32, error) {
	f, err := AsFloat64(v)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

// AsEnum resolves a JTON value to an enum number: a string is looked up in
// valueMap (the generated <Enum>_value), a number is used directly.
func AsEnum(v any, valueMap map[string]int32) (int32, error) {
	switch x := v.(type) {
	case string:
		n, ok := valueMap[x]
		if !ok {
			return 0, fmt.Errorf("protojton: unknown enum value %q", x)
		}
		return n, nil
	default:
		return AsInt32(v)
	}
}
