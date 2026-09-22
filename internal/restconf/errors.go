package restconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/DisMosGit/triadsim/internal/datatree"
)

// error-type values of the ietf-restconf error document (RFC 8040 §7.1).
const (
	errorTypeProtocol    = "protocol"
	errorTypeApplication = "application"
)

// httpError couples an RFC 8040 error-tag with the HTTP status it maps to and,
// for 405, the methods the target does allow.
type httpError struct {
	status  int
	typ     string
	tag     string
	path    string
	message string
	allow   string
}

// Error implements error.
func (e *httpError) Error() string { return e.message }

// newHTTPError builds one error response descriptor.
func newHTTPError(status int, typ, tag, format string, args ...any) *httpError {
	return &httpError{
		status:  status,
		typ:     typ,
		tag:     tag,
		message: fmt.Sprintf(format, args...),
	}
}

// Error response constructors, one per status the server answers.
func malformedRequest(format string, args ...any) *httpError {
	return newHTTPError(http.StatusBadRequest, errorTypeProtocol, "malformed-message", format, args...)
}

func unknownElement(name string) *httpError {
	return newHTTPError(http.StatusBadRequest, errorTypeProtocol, "unknown-element", "unknown element: %s", name)
}

func invalidValue(format string, args ...any) *httpError {
	return newHTTPError(http.StatusBadRequest, errorTypeProtocol, "invalid-value", format, args...)
}

func notFound(resource string) *httpError {
	return newHTTPError(http.StatusNotFound, errorTypeProtocol, "invalid-value", "resource not found: %s", resource)
}

func methodNotAllowed(allow string) *httpError {
	value := newHTTPError(http.StatusMethodNotAllowed, errorTypeProtocol, "operation-not-supported", "method not allowed")
	value.allow = allow
	return value
}

func conflict(format string, args ...any) *httpError {
	return newHTTPError(http.StatusConflict, errorTypeProtocol, "data-exists", format, args...)
}

func unsupportedMedia(format string, args ...any) *httpError {
	return newHTTPError(http.StatusUnsupportedMediaType, errorTypeProtocol, "operation-not-supported", format, args...)
}

func notAcceptable(format string, args ...any) *httpError {
	return newHTTPError(http.StatusNotAcceptable, errorTypeProtocol, "operation-not-supported", format, args...)
}

func notImplemented(operation string) *httpError {
	return newHTTPError(http.StatusNotImplemented, errorTypeProtocol, "operation-not-supported",
		"%s is not implemented", operation)
}

func internalError(err error) *httpError {
	return newHTTPError(http.StatusInternalServerError, errorTypeApplication, "operation-failed", "%v", err)
}

// errorDocument is the ietf-restconf:errors structure.
type errorDocument struct {
	Errors errorList `json:"ietf-restconf:errors"`
}

// errorList wraps the error array of the document.
type errorList struct {
	Error []errorEntry `json:"error"`
}

// errorEntry is one error of the document.
type errorEntry struct {
	Type    string `json:"error-type"`
	Tag     string `json:"error-tag"`
	Path    string `json:"error-path,omitempty"`
	Message string `json:"error-message,omitempty"`
}

// asHTTPError converts err into an httpError, defaulting to a 500.
func asHTTPError(err error) *httpError {
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		return httpErr
	}
	return internalError(err)
}

// writeTreeError maps a data-tree error onto the HTTP status and writes it.
func (s *Server) writeTreeError(w http.ResponseWriter, r *http.Request, err *datatree.Error) {
	s.writeError(w, r, dataTreeHTTPError(err))
}

// dataTreeHTTPError maps an RFC 6241 error tag onto a RESTCONF status code
// (RFC 8040 §7.3). A rejected snapshot is the roadmap's 422; a rejected leaf
// value stays a 400.
func dataTreeHTTPError(err *datatree.Error) *httpError {
	status := http.StatusInternalServerError
	switch err.Tag {
	case datatree.TagMalformedMessage, datatree.TagUnknownElement, datatree.TagMissingElement:
		status = http.StatusBadRequest
	case datatree.TagInvalidValue:
		status = http.StatusBadRequest
		if err.Validation {
			status = http.StatusUnprocessableEntity
		}
	case datatree.TagAccessDenied:
		status = http.StatusForbidden
	case datatree.TagDataExists:
		status = http.StatusConflict
	case datatree.TagDataMissing:
		status = http.StatusNotFound
	case datatree.TagOperationNotSupported:
		status = http.StatusNotImplemented
	case datatree.TagOperationFailed:
		status = http.StatusInternalServerError
	case datatree.TagTooBig:
		status = http.StatusRequestEntityTooLarge
	}

	errorType := err.Type
	if errorType == "" {
		errorType = errorTypeProtocol
	}
	httpErr := newHTTPError(status, errorType, err.Tag, "%s", err.Message)
	httpErr.path = err.Path
	return httpErr
}

// writeError writes the RESTCONF error document, defaulting to JSON.
//
// The XML document arrives with the XML codec; this is the JSON fallback every
// error path can rely on.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := asHTTPError(err)

	if httpErr.allow != "" {
		w.Header().Set("Allow", httpErr.allow)
	}
	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(httpErr.status)

	document := errorDocument{Errors: errorList{Error: []errorEntry{{
		Type:    httpErr.typ,
		Tag:     httpErr.tag,
		Path:    httpErr.path,
		Message: httpErr.message,
	}}}}
	_ = json.NewEncoder(w).Encode(document)
}
