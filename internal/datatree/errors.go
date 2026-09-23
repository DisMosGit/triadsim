package datatree

import (
	"errors"
	"fmt"
)

// Error types and tags follow RFC 6241 §7.5.3, the vocabulary both management
// planes map from: NETCONF renders an <rpc-error> from them and RESTCONF maps
// the tag onto an HTTP status code.
const (
	// TypeRPC marks protocol-message errors such as a malformed message.
	TypeRPC = "rpc"
	// TypeProtocol marks operation errors such as an invalid value.
	TypeProtocol = "protocol"
	// TypeApplication marks failures of the device itself, such as a store
	// write.
	TypeApplication = "application"
)

// Operation error tags.
const (
	TagMalformedMessage      = "malformed-message"
	TagTooBig                = "too-big"
	TagMissingAttribute      = "missing-attribute"
	TagMissingElement        = "missing-element"
	TagUnknownElement        = "unknown-element"
	TagInvalidValue          = "invalid-value"
	TagAccessDenied          = "access-denied"
	TagDataExists            = "data-exists"
	TagDataMissing           = "data-missing"
	TagOperationNotSupported = "operation-not-supported"
	TagOperationFailed       = "operation-failed"
)

// Error is one failed data-tree operation. errors.As finds it through any
// wrapping, so a plane can always recover the protocol tag.
type Error struct {
	// Type is the error-type: TypeRPC, TypeProtocol or TypeApplication.
	Type string
	// Tag is the error-tag, for example TagInvalidValue.
	Tag string
	// Path is the optional path of the offending node.
	Path string
	// Message is the human-readable error message.
	Message string
	// Validation marks a failure of the whole proposed snapshot rather than of
	// one node. RESTCONF answers those with 422 Unprocessable Entity, while a
	// per-leaf invalid-value is a 400.
	Validation bool
}

// Error implements error.
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Tag
}

// newError builds an error with the standard message formatting.
func newError(errorType, tag, path, format string, args ...any) *Error {
	return &Error{
		Type:    errorType,
		Tag:     tag,
		Path:    path,
		Message: fmt.Sprintf(format, args...),
	}
}

// UnknownElement reports an element that does not exist in the data model.
func UnknownElement(name string) *Error {
	return newError(TypeProtocol, TagUnknownElement, "", "unknown element: %s", name)
}

// Malformed reports a message the server could not parse.
func Malformed(format string, args ...any) *Error {
	return newError(TypeRPC, TagMalformedMessage, "", format, args...)
}

// TooBig reports a message that exceeded the size limit.
func TooBig(format string, args ...any) *Error {
	return newError(TypeRPC, TagTooBig, "", format, args...)
}

// MissingAttribute reports a required attribute, for example message-id.
// RFC 6241 §4.1 reports it as error-type rpc.
func MissingAttribute(name string) *Error {
	return newError(TypeRPC, TagMissingAttribute, "", "missing attribute %s", name)
}

// Denied reports an operation the caller may not perform, for example a
// confirmed commit while another session owns the confirmed commit in
// progress. Unlike AccessDenied it names no model path.
func Denied(format string, args ...any) *Error {
	return newError(TypeProtocol, TagAccessDenied, "", format, args...)
}

// InvalidValue reports a value the model rejects.
func InvalidValue(format string, args ...any) *Error {
	return newError(TypeProtocol, TagInvalidValue, "", format, args...)
}

// InvalidValueAt reports a value the model rejects at a known path.
func InvalidValueAt(path, format string, args ...any) *Error {
	return newError(TypeProtocol, TagInvalidValue, path, format, args...)
}

// AccessDenied reports a write to a read-only node.
func AccessDenied(path string) *Error {
	return newError(TypeProtocol, TagAccessDenied, path, "access denied: %s is read-only", path)
}

// DataExists reports an element that already exists.
func DataExists(path string) *Error {
	return newError(TypeProtocol, TagDataExists, path, "data already exists: %s", path)
}

// DataMissing reports an element that does not exist.
func DataMissing(path string) *Error {
	return newError(TypeProtocol, TagDataMissing, path, "data missing: %s", path)
}

// NotSupported reports an operation or option that is not implemented.
func NotSupported(format string, args ...any) *Error {
	return newError(TypeProtocol, TagOperationNotSupported, "", format, args...)
}

// MissingElement reports a required element, for example the key leaf of a list
// entry.
func MissingElement(what string) *Error {
	return newError(TypeProtocol, TagMissingElement, "", "missing element: %s", what)
}

// Failed wraps a device failure, for example a store write error.
func Failed(err error) *Error {
	return newError(TypeApplication, TagOperationFailed, "", "%v", err)
}

// ValidationError reports a configuration the model rejected. It carries the
// invalid-value tag so NETCONF reports it as invalid-value, and the Validation
// flag so RESTCONF can answer 422.
func ValidationError(err error) *Error {
	value := newError(TypeProtocol, TagInvalidValue, "", "%v", err)
	value.Validation = true
	return value
}

// AsError extracts a *Error from err, reporting whether err carries one.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
