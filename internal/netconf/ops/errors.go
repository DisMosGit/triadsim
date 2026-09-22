package ops

import "fmt"

// NETCONF error-type values (RFC 6241 §7.5.3).
const (
	// TypeRPC marks protocol-message errors such as a malformed message.
	TypeRPC = "rpc"
	// TypeProtocol marks operation errors such as an invalid value.
	TypeProtocol = "protocol"
	// TypeApplication marks failures of the device itself, such as a store
	// write.
	TypeApplication = "application"
)

// NETCONF error-tag values used by the simulator (RFC 6241 §7.5.3).
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

// SeverityError is the only error severity the simulator reports.
const SeverityError = "error"

// Error is one NETCONF <rpc-error>. Operations return it, directly or wrapped,
// and the RPC layer renders it into the reply. errors.As finds it through any
// wrapping, so callers can always recover the protocol tag.
type Error struct {
	// Type is the error-type: TypeRPC, TypeProtocol or TypeApplication.
	Type string
	// Tag is the error-tag, for example TagInvalidValue.
	Tag string
	// Severity is the error-severity, always SeverityError today.
	Severity string
	// Message is the error-message shown to the client.
	Message string
	// Path is the optional error-path of the offending node.
	Path string
}

// Error implements error.
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Tag
}

// newError builds an error with the standard severity.
func newError(errorType, tag, format string, args ...any) *Error {
	return &Error{
		Type:     errorType,
		Tag:      tag,
		Severity: SeverityError,
		Message:  fmt.Sprintf(format, args...),
	}
}

// newPathError builds an error that also names the offending model path, which
// the reply renders as error-path.
func newPathError(errorType, tag, path, format string, args ...any) *Error {
	err := newError(errorType, tag, format, args...)
	err.Path = path
	return err
}

// Malformed reports a message the server could not parse.
func Malformed(format string, args ...any) *Error {
	return newError(TypeRPC, TagMalformedMessage, format, args...)
}

// TooBig reports a message that exceeded the size limit.
func TooBig(format string, args ...any) *Error {
	return newError(TypeRPC, TagTooBig, format, args...)
}

// MissingAttribute reports a required attribute, for example message-id.
func MissingAttribute(name string) *Error {
	return newError(TypeRPC, TagMissingAttribute, "missing attribute %s", name)
}

// MissingElement reports a required element, for example <target>.
func MissingElement(what string) *Error {
	return newError(TypeProtocol, TagMissingElement, "missing element: %s", what)
}

// UnknownElement reports an element that does not exist in the data model.
func UnknownElement(name string) *Error {
	return newError(TypeProtocol, TagUnknownElement, "unknown element: %s", name)
}

// InvalidValue reports a value the model rejects.
func InvalidValue(format string, args ...any) *Error {
	return newError(TypeProtocol, TagInvalidValue, format, args...)
}

// AccessDenied reports a write to a read-only node.
func AccessDenied(path string) *Error {
	return newPathError(TypeProtocol, TagAccessDenied, path, "access denied: %s is read-only", path)
}

// Denied reports an operation the caller may not perform, for example a
// confirmed commit while another session owns the confirmed commit in
// progress. Unlike AccessDenied it names no model path.
func Denied(format string, args ...any) *Error {
	return newError(TypeProtocol, TagAccessDenied, format, args...)
}

// DataExists reports an element that already exists.
func DataExists(path string) *Error {
	return newPathError(TypeProtocol, TagDataExists, path, "data already exists: %s", path)
}

// DataMissing reports an element that does not exist.
func DataMissing(path string) *Error {
	return newPathError(TypeProtocol, TagDataMissing, path, "data missing: %s", path)
}

// NotSupported reports an operation or option the simulator does not implement.
func NotSupported(format string, args ...any) *Error {
	return newError(TypeProtocol, TagOperationNotSupported, format, args...)
}

// Failed wraps a device failure, for example a store write error.
func Failed(err error) *Error {
	return newError(TypeApplication, TagOperationFailed, "%v", err)
}
