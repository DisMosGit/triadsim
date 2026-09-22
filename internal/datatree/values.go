package datatree

import (
	"fmt"
	"strconv"

	"github.com/DisMosGit/triadsim/internal/router"
)

// ParseLeaf converts character data into the Go value the store holds for a
// leaf. The leaf kind comes from the router schema, so a value that does not
// fit its model type is rejected before it reaches the store.
func ParseLeaf(kind router.LeafKind, text string) (any, error) {
	switch kind {
	case router.LeafBool:
		value, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("expected true or false, got %q", text)
		}
		return value, nil

	case router.LeafInt:
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected an integer, got %q", text)
		}
		return int(value), nil

	case router.LeafUint8:
		value, err := strconv.ParseUint(text, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("expected an integer 0..255, got %q", text)
		}
		return uint8(value), nil

	case router.LeafUint16:
		value, err := strconv.ParseUint(text, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("expected an integer 0..65535, got %q", text)
		}
		return uint16(value), nil

	case router.LeafUint32:
		value, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("expected an integer 0..4294967295, got %q", text)
		}
		return uint32(value), nil

	case router.LeafUint64:
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected an unsigned integer, got %q", text)
		}
		return value, nil

	case router.LeafFloat64:
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("expected a number, got %q", text)
		}
		return value, nil

	case router.LeafString:
		return text, nil

	default:
		return nil, fmt.Errorf("unsupported leaf kind %q", kind)
	}
}

// FormatLeaf renders a store value as XML character data.
func FormatLeaf(value any) string {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return v
	default:
		return fmt.Sprint(value)
	}
}

// ContentMatches reports whether a filter's text selects a stored leaf value.
// The text is parsed with the leaf's kind, so "20" and "20.0" both match the
// same float leaf.
func ContentMatches(kind router.LeafKind, text string, value any) bool {
	parsed, err := ParseLeaf(kind, text)
	if err != nil {
		return false
	}
	return parsed == value
}
