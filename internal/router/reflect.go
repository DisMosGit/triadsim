package router

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// node is a resolved model position: the reflect value plus whether the field
// is read-only (config:"false").
type node struct {
	value    reflect.Value
	readOnly bool
}

// derefValue follows interfaces and pointers. It reports false for a nil
// pointer, which callers treat as "node not found".
func derefValue(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	return v, true
}

// matchField finds the struct field whose path tag matches the longest prefix
// of segs. A tag may contain several segments (interfaces/interface): the
// earlier parts are implicit containers and the last part is the field.
func matchField(v reflect.Value, segs []Segment) (reflect.Value, reflect.StructField, int, bool) {
	t := v.Type()
	bestLen := 0
	var bestValue reflect.Value
	var bestField reflect.StructField

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, ok := field.Tag.Lookup("path")
		if !ok {
			continue
		}
		parts := strings.Split(tag, "/")
		if len(parts) > len(segs) {
			continue
		}
		matches := true
		for j, part := range parts {
			if part != segs[j].Name {
				matches = false
				break
			}
		}
		if matches && len(parts) > bestLen {
			bestLen = len(parts)
			bestValue = v.Field(i)
			bestField = field
		}
	}

	if bestLen == 0 {
		return reflect.Value{}, reflect.StructField{}, 0, false
	}
	return bestValue, bestField, bestLen, true
}

// keyFieldIndex returns the index of the field tagged key:"true" on a list
// element type, or -1.
func keyFieldIndex(t reflect.Type) int {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("key") == "true" {
			return i
		}
	}
	return -1
}

// keyTagName returns the predicate name of a list element key: the first
// segment of the key field's path tag.
func keyTagName(t reflect.Type) string {
	index := keyFieldIndex(t)
	if index < 0 {
		return ""
	}
	return strings.Split(t.Field(index).Tag.Get("path"), "/")[0]
}

// selectElement picks the list element whose key field equals seg.Value.
//
// The store, not the boot template, is authoritative for which instances
// exist, so an element the template does not have is synthesized from the
// element type when the list is tagged creatable:"true" (VLANs, MAC entries,
// LLDP neighbours). Reads and writes still go through the store, so a
// synthesized element only supplies the node's type and access; a closed list
// keeps reporting ErrNotFound, which is how interfaces and the modulation
// profiles stay fixed.
func selectElement(slice reflect.Value, seg Segment, creatable bool) (reflect.Value, error) {
	elemType := slice.Type().Elem()
	for elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}
	if elemType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%w: %s is not a list of structs", ErrInvalidPath, seg.Name)
	}

	keyIndex := keyFieldIndex(elemType)
	if keyIndex < 0 {
		return reflect.Value{}, fmt.Errorf("%w: list %s has no key field", ErrInvalidPath, seg.Name)
	}
	if name := keyTagName(elemType); name != seg.Key {
		return reflect.Value{}, fmt.Errorf("%w: list %s has no key %q", ErrInvalidPath, seg.Name, seg.Key)
	}

	for i := 0; i < slice.Len(); i++ {
		element := slice.Index(i)
		value, ok := derefValue(element)
		if !ok {
			continue
		}
		key, ok := derefValue(value.Field(keyIndex))
		if !ok {
			continue
		}
		if scalarString(key) == seg.Value {
			return element, nil
		}
	}

	if !creatable {
		return reflect.Value{}, fmt.Errorf("%w: %s[%s=%s]", ErrNotFound, seg.Name, seg.Key, seg.Value)
	}
	return synthesizeElement(elemType), nil
}

// synthesizeElement returns a detached addressable zero element of a list.
func synthesizeElement(elemType reflect.Type) reflect.Value {
	if elemType.Kind() == reflect.Pointer {
		return reflect.New(elemType.Elem())
	}
	return reflect.New(elemType).Elem()
}

// resolve walks segs from root and returns the addressed node.
func resolve(root reflect.Value, segs []Segment) (node, error) {
	value, ok := derefValue(root)
	if !ok {
		return node{}, fmt.Errorf("%w: nil root", ErrNotFound)
	}
	current := node{value: value}

	for i := 0; i < len(segs); {
		value, ok := derefValue(current.value)
		if !ok {
			return node{}, fmt.Errorf("%w: %s", ErrNotFound, segs[i].Name)
		}

		switch value.Kind() {
		case reflect.Struct:
			fieldValue, field, consumed, found := matchField(value, segs[i:])
			if !found {
				return node{}, fmt.Errorf("%w: %s", ErrNotFound, segs[i].Name)
			}
			last := segs[i+consumed-1]
			i += consumed

			readOnly := current.readOnly || field.Tag.Get("config") == "false"
			fieldValue, ok := derefValue(fieldValue)
			if !ok {
				return node{}, fmt.Errorf("%w: %s", ErrNotFound, last.Name)
			}

			if fieldValue.Kind() == reflect.Slice {
				if last.Key == "" {
					if i == len(segs) {
						return node{value: fieldValue, readOnly: readOnly}, nil
					}
					return node{}, fmt.Errorf("%w: list %s requires a key predicate", ErrInvalidPath, last.Name)
				}
				element, err := selectElement(fieldValue, last, field.Tag.Get("creatable") == "true")
				if err != nil {
					return node{}, err
				}
				current = node{value: element, readOnly: readOnly}
				continue
			}
			current = node{value: fieldValue, readOnly: readOnly}

		case reflect.Slice:
			return node{}, fmt.Errorf("%w: list %s requires a key predicate", ErrInvalidPath, segs[i].Name)

		default:
			return node{}, fmt.Errorf("%w: %s is a %s, not a container", ErrNotFound, segs[i].Name, value.Kind())
		}
	}
	return current, nil
}

// isLeaf reports whether v is a scalar the store can hold.
func isLeaf(v reflect.Value) bool {
	v, ok := derefValue(v)
	if !ok {
		return false
	}
	switch v.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	default:
		return false
	}
}

// scalarString renders a scalar for use as a list key.
func scalarString(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	default:
		return ""
	}
}

// leafValue returns the store-compatible value of a leaf. Named types such as
// model.QL are normalised to their underlying primitive.
func leafValue(v reflect.Value) (any, error) {
	v, ok := derefValue(v)
	if !ok {
		return nil, fmt.Errorf("%w: nil leaf", ErrTypeMismatch)
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int:
		return int(v.Int()), nil
	case reflect.Uint8:
		return uint8(v.Uint()), nil
	case reflect.Uint16:
		return uint16(v.Uint()), nil
	case reflect.Uint32:
		return uint32(v.Uint()), nil
	case reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil
	default:
		return nil, fmt.Errorf("%w: unsupported leaf kind %s", ErrTypeMismatch, v.Kind())
	}
}

// assignLeaf sets dst from value, converting between compatible numeric types
// and reporting a kind mismatch as ErrTypeMismatch and a range violation as
// ErrBadValue.
func assignLeaf(dst reflect.Value, value any) error {
	raw := reflect.ValueOf(value)
	if !raw.IsValid() {
		return fmt.Errorf("%w: nil value", ErrTypeMismatch)
	}
	if raw.Type() == dst.Type() {
		dst.Set(raw)
		return nil
	}

	switch dst.Kind() {
	case reflect.String:
		if raw.Kind() == reflect.String {
			dst.SetString(raw.String())
			return nil
		}
	case reflect.Bool:
		if raw.Kind() == reflect.Bool {
			dst.SetBool(raw.Bool())
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if x, ok := toInt64(raw); ok {
			if dst.OverflowInt(x) {
				return fmt.Errorf("%w: %v overflows %s", ErrBadValue, value, dst.Type())
			}
			dst.SetInt(x)
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if x, ok := toUint64(raw); ok {
			if dst.OverflowUint(x) {
				return fmt.Errorf("%w: %v overflows %s", ErrBadValue, value, dst.Type())
			}
			dst.SetUint(x)
			return nil
		}
		if x, ok := toInt64(raw); ok && x < 0 {
			return fmt.Errorf("%w: %v overflows %s", ErrBadValue, value, dst.Type())
		}
	case reflect.Float32, reflect.Float64:
		if x, ok := toFloat64(raw); ok {
			if dst.OverflowFloat(x) {
				return fmt.Errorf("%w: %v overflows %s", ErrBadValue, value, dst.Type())
			}
			dst.SetFloat(x)
			return nil
		}
	}
	return fmt.Errorf("%w: cannot use %T as %s", ErrTypeMismatch, value, dst.Type())
}

// coerce converts value to target and returns the store-compatible result.
func coerce(value any, target reflect.Type) (any, error) {
	tmp := reflect.New(target).Elem()
	if err := assignLeaf(tmp, value); err != nil {
		return nil, err
	}
	return leafValue(tmp)
}

func toInt64(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.Trunc(f) != f || f < math.MinInt64 || f > math.MaxInt64 {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func toUint64(v reflect.Value) (uint64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		if i < 0 {
			return 0, false
		}
		return uint64(i), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), true
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.Trunc(f) != f || f < 0 || f > math.MaxUint64 {
			return 0, false
		}
		return uint64(f), true
	default:
		return 0, false
	}
}

func toFloat64(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	default:
		return 0, false
	}
}

// walkLeaves visits every concrete leaf of the model tree in declaration
// order. List elements are addressed with their key predicate.
func walkLeaves(v reflect.Value, segs []Segment, readOnly bool, fn func(Path, reflect.Value, bool) error) error {
	v, ok := derefValue(v)
	if !ok {
		return nil
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			tag, ok := field.Tag.Lookup("path")
			if !ok {
				continue
			}
			parts := strings.Split(tag, "/")
			next := make([]Segment, 0, len(segs)+len(parts))
			next = append(next, segs...)
			for _, part := range parts {
				next = append(next, Segment{Name: part})
			}
			childReadOnly := readOnly || field.Tag.Get("config") == "false"
			if err := walkLeaves(v.Field(i), next, childReadOnly, fn); err != nil {
				return err
			}
		}
		return nil

	case reflect.Slice:
		elemType := v.Type().Elem()
		for elemType.Kind() == reflect.Pointer {
			elemType = elemType.Elem()
		}
		keyIndex := keyFieldIndex(elemType)
		keyName := keyTagName(elemType)

		for i := 0; i < v.Len(); i++ {
			element := v.Index(i)
			elemSegs := cloneSegments(segs)
			if keyIndex >= 0 && len(elemSegs) > 0 {
				value, ok := derefValue(element)
				if ok {
					key, ok := derefValue(value.Field(keyIndex))
					if ok {
						last := &elemSegs[len(elemSegs)-1]
						last.Key = keyName
						last.Value = scalarString(key)
					}
				}
			}
			if err := walkLeaves(element, elemSegs, readOnly, fn); err != nil {
				return err
			}
		}
		return nil

	default:
		return fn(Path{Segments: segs}, v, readOnly)
	}
}
