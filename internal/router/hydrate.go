package router

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/DisMosGit/triadsim/internal/model"
)

// deviceFromValues rebuilds a device from a flat path -> leaf map, the shape
// the store holds and the commit validator receives.
//
// The rebuild starts from a deep copy of the boot template — so list instances
// keep their declaration order (interface indexes, VLAN order) and leaves the
// snapshot does not mention keep their template value — and then walks the map
// against the model's path tags, creating list entries and pointer subtrees on
// demand. A snapshot that contains list instances the template never had (a
// VLAN created through RESTCONF, a MAC entry learned at runtime) therefore
// produces a complete model.
//
// The map is permissive by design: it rebuilds whatever the store says. The
// write path is what enforces the creatable list contract, in selectElement.
func (r *Router) deviceFromValues(values map[string]any) (*model.Device, error) {
	device, err := cloneDevice(r.root)
	if err != nil {
		return nil, err
	}
	root := reflect.ValueOf(device).Elem()

	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		parsed, err := Parse(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := assignPath(root, parsed.Segments, values[path]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return device, nil
}

// cloneDevice deep-copies a device through its JSON representation. The copy
// keeps every concrete type because the model carries json tags on every field.
func cloneDevice(root *model.Device) (*model.Device, error) {
	data, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("router: clone model: %w", err)
	}
	var clone model.Device
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("router: clone model: %w", err)
	}
	return &clone, nil
}

// assignPath walks segs from an addressable root and stores value in the leaf
// they address. Missing list entries are appended and nil pointer subtrees are
// allocated, so the whole path becomes representable in the model.
func assignPath(root reflect.Value, segs []Segment, value any) error {
	current := root
	index := 0

	for index < len(segs) {
		var err error
		current, err = allocValue(current)
		if err != nil {
			return err
		}
		if current.Kind() != reflect.Struct {
			return fmt.Errorf("%w: %s is not a container", ErrNotFound, segs[index].Name)
		}

		fieldValue, _, consumed, found := matchField(current, segs[index:])
		if !found {
			return fmt.Errorf("%w: %s", ErrNotFound, segs[index].Name)
		}
		last := segs[index+consumed-1]
		index += consumed

		switch fieldValue.Kind() {
		case reflect.Pointer:
			// A pointer subtree such as radio-link is allocated on demand.
			if fieldValue.IsNil() {
				fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
			}
			current = fieldValue

		case reflect.Slice:
			if last.Key == "" {
				return fmt.Errorf("%w: list %s requires a key predicate", ErrInvalidPath, last.Name)
			}
			element, err := appendOrSelect(fieldValue, last)
			if err != nil {
				return err
			}
			current = element

		default:
			current = fieldValue
		}
	}
	return assignLeaf(current, value)
}

// allocValue follows pointers and allocates a nil one so the value behind it
// can be written. The root and every field reached from it are addressable.
func allocValue(v reflect.Value) (reflect.Value, error) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !v.CanSet() {
				return reflect.Value{}, fmt.Errorf("%w: nil node is not addressable", ErrNotFound)
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v, nil
}

// appendOrSelect returns the list element keyed by seg, appending a fresh one
// when the slice does not have it yet. The returned value is the element inside
// the slice, so writes to it are part of the rebuilt model.
func appendOrSelect(slice reflect.Value, seg Segment) (reflect.Value, error) {
	elemType := slice.Type().Elem()
	pointer := elemType.Kind() == reflect.Pointer
	base := elemType
	if pointer {
		base = elemType.Elem()
	}
	if base.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%w: %s is not a list of structs", ErrInvalidPath, seg.Name)
	}

	keyIndex := keyFieldIndex(base)
	if keyIndex < 0 {
		return reflect.Value{}, fmt.Errorf("%w: list %s has no key field", ErrInvalidPath, seg.Name)
	}
	if name := keyTagName(base); name != seg.Key {
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

	var element reflect.Value
	if pointer {
		element = reflect.New(base)
	} else {
		element = reflect.New(base).Elem()
	}
	target, _ := derefValue(element)
	if err := setKey(target.Field(keyIndex), seg.Value); err != nil {
		return reflect.Value{}, fmt.Errorf("%w: %s[%s=%s]: %v",
			ErrTypeMismatch, seg.Name, seg.Key, seg.Value, err)
	}

	slice.Set(reflect.Append(slice, element))
	return slice.Index(slice.Len() - 1), nil
}

// setKey parses a list key predicate value into the key field's type.
func setKey(field reflect.Value, text string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(text)
		return nil
	case reflect.Bool:
		value, err := strconv.ParseBool(text)
		if err != nil {
			return err
		}
		field.SetBool(value)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(text, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(value)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(text, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(value)
		return nil
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(text, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(value)
		return nil
	default:
		return fmt.Errorf("unsupported key type %s", field.Type())
	}
}
