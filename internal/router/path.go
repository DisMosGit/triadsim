package router

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidPath is returned when a path does not follow the a/b[c=d]/e
// grammar.
var ErrInvalidPath = errors.New("router: invalid path")

// Segment is one step of a model path: a node name plus an optional list key
// predicate.
type Segment struct {
	// Name is the node name, for example "interface".
	Name string
	// Key and Value are the list key predicate, for example name=radio0. Both
	// are empty when the segment names a container or a scalar.
	Key   string
	Value string
}

// Path is a parsed model path. It is the canonical address used by the store
// and by every management plane.
type Path struct {
	Segments []Segment
}

// Parse parses a model path such as
// interfaces/interface[name=radio0]/radio-link/tx-power. Leading or trailing
// slashes, empty segments, a predicate without a key or value, and trailing
// text after a predicate are rejected with ErrInvalidPath.
func Parse(s string) (Path, error) {
	if s == "" {
		return Path{}, fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	if strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "//") {
		return Path{}, fmt.Errorf("%w: %q", ErrInvalidPath, s)
	}

	parts := strings.Split(s, "/")
	segments := make([]Segment, 0, len(parts))
	for _, part := range parts {
		segment, err := parseSegment(part)
		if err != nil {
			return Path{}, fmt.Errorf("%w: %q: %v", ErrInvalidPath, s, err)
		}
		segments = append(segments, segment)
	}
	return Path{Segments: segments}, nil
}

// parseSegment parses one slash-free path component.
func parseSegment(part string) (Segment, error) {
	open := strings.IndexByte(part, '[')
	if open < 0 {
		if part == "" {
			return Segment{}, errors.New("empty segment")
		}
		return Segment{Name: part}, nil
	}

	if !strings.HasSuffix(part, "]") || strings.Count(part, "[") != 1 || strings.Count(part, "]") != 1 {
		return Segment{}, errors.New("malformed list predicate")
	}

	name := part[:open]
	body := part[open+1 : len(part)-1]
	eq := strings.IndexByte(body, '=')
	if name == "" || body == "" || eq <= 0 || eq == len(body)-1 {
		return Segment{}, errors.New("malformed list predicate")
	}

	key := body[:eq]
	value := body[eq+1:]
	if strings.ContainsAny(key, "[]") || strings.ContainsAny(value, "[]") {
		return Segment{}, errors.New("malformed list predicate")
	}
	return Segment{Name: name, Key: key, Value: value}, nil
}

// String returns the canonical path text. It round-trips through Parse.
func (p Path) String() string {
	var b strings.Builder
	for i, segment := range p.Segments {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(segment.Name)
		if segment.Key != "" {
			b.WriteByte('[')
			b.WriteString(segment.Key)
			b.WriteByte('=')
			b.WriteString(segment.Value)
			b.WriteByte(']')
		}
	}
	return b.String()
}

// IsZero reports whether the path has no segments.
func (p Path) IsZero() bool { return len(p.Segments) == 0 }

// cloneSegments returns a deep copy of segments so a walk can attach a
// predicate without touching the caller's slice.
func cloneSegments(segments []Segment) []Segment {
	out := make([]Segment, len(segments))
	copy(out, segments)
	return out
}
