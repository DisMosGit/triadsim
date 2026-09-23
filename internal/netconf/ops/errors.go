package ops

import "github.com/DisMosGit/triadsim/internal/datatree"

// There is exactly one error vocabulary across the management planes: the
// datatree.Error of internal/datatree, which carries the RFC 6241 §7.5.3
// error-type and error-tag values. NETCONF renders it into <rpc-error> and
// RESTCONF maps it onto HTTP statuses; nothing converts between parallel
// error types any more. The aliases and forwarding constructors below exist
// so the operations read concisely — they name the shared definitions and
// cannot drift from them.

// Error is one failed data-tree operation. Operations return it, directly or
// wrapped, and the RPC layer renders it into the reply. errors.As finds it
// through any wrapping, so callers can always recover the protocol tag.
type Error = datatree.Error

// NETCONF error-type values (RFC 6241 §7.5.3).
const (
	// TypeRPC marks protocol-message errors such as a malformed message.
	TypeRPC = datatree.TypeRPC
	// TypeProtocol marks operation errors such as an invalid value.
	TypeProtocol = datatree.TypeProtocol
	// TypeApplication marks failures of the device itself, such as a store
	// write.
	TypeApplication = datatree.TypeApplication
)

// NETCONF error-tag values used by the simulator (RFC 6241 §7.5.3).
const (
	TagMalformedMessage      = datatree.TagMalformedMessage
	TagTooBig                = datatree.TagTooBig
	TagMissingAttribute      = datatree.TagMissingAttribute
	TagMissingElement        = datatree.TagMissingElement
	TagUnknownElement        = datatree.TagUnknownElement
	TagInvalidValue          = datatree.TagInvalidValue
	TagAccessDenied          = datatree.TagAccessDenied
	TagDataExists            = datatree.TagDataExists
	TagDataMissing           = datatree.TagDataMissing
	TagOperationNotSupported = datatree.TagOperationNotSupported
	TagOperationFailed       = datatree.TagOperationFailed
)

// Malformed reports a message the server could not parse.
func Malformed(format string, args ...any) *Error { return datatree.Malformed(format, args...) }

// TooBig reports a message that exceeded the size limit.
func TooBig(format string, args ...any) *Error { return datatree.TooBig(format, args...) }

// MissingAttribute reports a required attribute, for example message-id.
func MissingAttribute(name string) *Error { return datatree.MissingAttribute(name) }

// MissingElement reports a required element, for example <target>.
func MissingElement(what string) *Error { return datatree.MissingElement(what) }

// UnknownElement reports an element that does not exist in the data model.
func UnknownElement(name string) *Error { return datatree.UnknownElement(name) }

// InvalidValue reports a value the model rejects.
func InvalidValue(format string, args ...any) *Error { return datatree.InvalidValue(format, args...) }

// AccessDenied reports a write to a read-only node.
func AccessDenied(path string) *Error { return datatree.AccessDenied(path) }

// Denied reports an operation the caller may not perform, for example a
// confirmed commit while another session owns the confirmed commit in
// progress. Unlike AccessDenied it names no model path.
func Denied(format string, args ...any) *Error { return datatree.Denied(format, args...) }

// DataExists reports an element that already exists.
func DataExists(path string) *Error { return datatree.DataExists(path) }

// DataMissing reports an element that does not exist.
func DataMissing(path string) *Error { return datatree.DataMissing(path) }

// NotSupported reports an operation or option that is not implemented.
func NotSupported(format string, args ...any) *Error {
	return datatree.NotSupported(format, args...)
}

// Failed wraps a device failure, for example a store write error.
func Failed(err error) *Error { return datatree.Failed(err) }

// fromDataTree passes a data-tree error through without losing its tag and
// reports any other failure as application/operation-failed.
func fromDataTree(err error) *Error {
	if treeErr, ok := datatree.AsError(err); ok {
		return treeErr
	}
	return datatree.Failed(err)
}
