package jton

// Object is an insertion-ordered string-keyed map — the JTON/JSON object.
//
// JTON (like Python's dict and the reference implementation) preserves key
// insertion order, and the Zen Grid serializer derives its column order from
// the first row's key order. Go's built-in map has no order, so decoding and
// re-encoding through map[string]any would scramble columns and break
// round-trips. Object is the order-preserving representation produced by Parse
// and consumed by Marshal.
//
// Setting an existing key updates its value in place and keeps its original
// position, matching dict semantics.
type Object struct {
	members []member
	// index maps key -> position in members. It is built lazily: small objects
	// (the overwhelming majority) use a linear scan and never allocate a map.
	index map[string]int
}

type member struct {
	key string
	val any
}

// linearScanLimit is the size past which an Object switches from linear-scan
// key lookup to a hash index.
const linearScanLimit = 16

// NewObject returns an empty Object.
func NewObject() *Object { return &Object{} }

// NewObjectCap returns an empty Object whose backing storage is pre-sized for n
// members. Use it when the member count is known up front (e.g. building a row
// from a fixed schema) to avoid reallocations.
func NewObjectCap(n int) *Object { return &Object{members: make([]member, 0, n)} }

// pushUnique appends a key/value pair without checking for duplicates. The
// caller must guarantee the key is not already present. It is the hot path for
// building Zen Grid rows, whose columns come from a known-unique header list.
func (o *Object) pushUnique(key string, val any) {
	o.members = append(o.members, member{key, val})
	if o.index != nil {
		o.index[key] = len(o.members) - 1
	}
}

// Obj builds an Object from alternating key, value arguments. It panics if the
// number of arguments is odd or a key is not a string. It is a convenience for
// constructing literals in code and tests:
//
//	jton.Obj("id", 1, "name", "Alice")
func Obj(kv ...any) *Object {
	if len(kv)%2 != 0 {
		panic("jton.Obj: odd number of arguments")
	}
	o := &Object{}
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			panic("jton.Obj: key is not a string")
		}
		o.Set(k, kv[i+1])
	}
	return o
}

// Len returns the number of members.
func (o *Object) Len() int { return len(o.members) }

// Set inserts or updates key. An existing key keeps its position and gets the
// new value; a new key is appended.
func (o *Object) Set(key string, val any) {
	if o.index != nil {
		if i, ok := o.index[key]; ok {
			o.members[i].val = val
			return
		}
		o.index[key] = len(o.members)
		o.members = append(o.members, member{key, val})
		return
	}
	for i := range o.members {
		if o.members[i].key == key {
			o.members[i].val = val
			return
		}
	}
	o.members = append(o.members, member{key, val})
	if len(o.members) > linearScanLimit {
		o.buildIndex()
	}
}

func (o *Object) buildIndex() {
	o.index = make(map[string]int, len(o.members))
	for i := range o.members {
		// First occurrence wins as the canonical position; Set never creates
		// duplicates, so this is unambiguous.
		if _, ok := o.index[o.members[i].key]; !ok {
			o.index[o.members[i].key] = i
		}
	}
}

// setOrPush appends when unique is known up front (no duplicate-key scan),
// otherwise falls back to the deduplicating Set.
func (o *Object) setOrPush(key string, val any, unique bool) {
	if unique {
		o.pushUnique(key, val)
	} else {
		o.Set(key, val)
	}
}

// Get returns the value for key and whether it was present.
func (o *Object) Get(key string) (any, bool) {
	if o.index != nil {
		if i, ok := o.index[key]; ok {
			return o.members[i].val, true
		}
		return nil, false
	}
	for i := range o.members {
		if o.members[i].key == key {
			return o.members[i].val, true
		}
	}
	return nil, false
}

// At returns the key and value at position i (0 <= i < Len).
func (o *Object) At(i int) (string, any) {
	return o.members[i].key, o.members[i].val
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string {
	ks := make([]string, len(o.members))
	for i := range o.members {
		ks[i] = o.members[i].key
	}
	return ks
}
