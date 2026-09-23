package router

import (
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
// The datastore is authoritative for which list instances exist: pruneList
// drops the template entries the map does not mention, so deleting every leaf
// of an entry removes it from the snapshot. Only a completely empty map keeps
// the whole template, which is what validating a fresh, unseeded datastore
// needs.
//
// The map is permissive by design: it rebuilds whatever the store says. The
// write path is what enforces the creatable list contract, in selectElement.
func (r *Router) deviceFromValues(values map[string]any) (*model.Device, error) {
	device := cloneDevice(r.root)
	hydrator, err := newHydrator(values)
	if err != nil {
		return nil, err
	}
	root := reflect.ValueOf(device).Elem()
	hydrator.pruneLists(root, "")

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
		if err := hydrator.assignPath(root, parsed.Segments, values[path]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return device, nil
}

// hydrator carries the datastore's list membership into the model rebuild.
type hydrator struct {
	// instances maps the model path of a list ("vlans/vlan") to the key values
	// the datastore holds for it.
	instances map[string]map[string]struct{}
	// seeded reports whether the datastore holds any leaf at all. An unseeded
	// datastore is not evidence that the configured lists are empty, so the
	// boot template keeps supplying the shape.
	seeded bool
}

// newHydrator collects the list instances the datastore holds.
func newHydrator(values map[string]any) (*hydrator, error) {
	h := &hydrator{
		instances: make(map[string]map[string]struct{}),
		seeded:    len(values) > 0,
	}
	for path := range values {
		parsed, err := Parse(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		prefix := ""
		for _, segment := range parsed.Segments {
			if prefix == "" {
				prefix = segment.Name
			} else {
				prefix += "/" + segment.Name
			}
			if segment.Key == "" {
				continue
			}
			keys := h.instances[prefix]
			if keys == nil {
				keys = make(map[string]struct{})
				h.instances[prefix] = keys
			}
			keys[segment.Value] = struct{}{}
		}
	}
	return h, nil
}

// pruneLists walks the model subtree at path and drops the boot-template
// entries of every list the datastore does not hold. It runs before the paths
// are assigned, so an instance that lives only in the template disappears even
// when no stored path passes through its list.
func (h *hydrator) pruneLists(v reflect.Value, path string) {
	if !h.seeded {
		return
	}
	v, ok := derefValue(v)
	if !ok {
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			tag, ok := t.Field(i).Tag.Lookup("path")
			if !ok {
				continue
			}
			h.pruneLists(v.Field(i), joinPath(path, tag))
		}
	case reflect.Slice:
		h.pruneList(v, path)
		for i := 0; i < v.Len(); i++ {
			element := v.Index(i)
			base, ok := derefValue(element)
			if !ok {
				continue
			}
			predicate, ok := keyPredicateOf(base)
			if !ok {
				h.pruneLists(element, path)
				continue
			}
			h.pruneLists(element, path+"["+predicate+"]")
		}
	}
}

// joinPath appends a path tag to a prefix path.
func joinPath(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + "/" + tag
}

// pruneList drops the boot-template entries of a list that the datastore does
// not hold. It is a no-op for an unseeded datastore.
func (h *hydrator) pruneList(slice reflect.Value, path string) {
	if !h.seeded {
		return
	}
	base := slice.Type().Elem()
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	if base.Kind() != reflect.Struct {
		return
	}
	keyIndex := keyFieldIndex(base)
	if keyIndex < 0 {
		return
	}

	live := h.instances[path]
	kept := reflect.MakeSlice(slice.Type(), 0, slice.Len())
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
		if _, ok := live[scalarString(key)]; ok {
			kept = reflect.Append(kept, element)
		}
	}
	slice.Set(kept)
}

// keyPredicateOf returns the "key=value" predicate of a list element struct.
func keyPredicateOf(element reflect.Value) (string, bool) {
	t := element.Type()
	index := keyFieldIndex(t)
	if index < 0 {
		return "", false
	}
	value, ok := derefValue(element.Field(index))
	if !ok {
		return "", false
	}
	return keyTagName(t) + "=" + scalarString(value), true
}

// cloneDevice deep-copies the device template structurally. A JSON
// marshal/unmarshal round-trip would do, but it is measurably slower and the
// rebuild runs on every SNMP request; the model is plain structs, slices,
// pointers and scalars, so a reflection copy is exact. Unexported fields
// (none in the model) are skipped, matching what the JSON round-trip kept.
func cloneDevice(root *model.Device) *model.Device {
	return deepCopy(reflect.ValueOf(root)).Interface().(*model.Device)
}

// deepCopy returns a detached copy of v. Scalars are immutable and returned
// as they are; containers are rebuilt element by element.
func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(deepCopy(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopy(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(deepCopy(iter.Key()), deepCopy(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if out.Field(i).CanSet() {
				out.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return out
	default:
		return v
	}
}

// assignPath walks segs from an addressable root and stores value in the leaf
// they address. Missing list entries are appended and nil pointer subtrees are
// allocated, so the whole path becomes representable in the model.
func (h *hydrator) assignPath(root reflect.Value, segs []Segment, value any) error {
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
		lastIndex := index + consumed - 1
		last := segs[lastIndex]
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
