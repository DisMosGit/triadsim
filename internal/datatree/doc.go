// Package datatree implements the protocol-neutral data tree shared by the
// NETCONF and RESTCONF management planes.
//
// The router owns the schema (path tags) and the store owns the leaf values;
// this package is the layer between them and a wire document. Read turns a
// datastore into a tree of resolved nodes, and Apply edits a datastore from a
// parsed document with the NETCONF subtree semantics
// (merge/replace/create/delete/remove) that RESTCONF reuses for
// PUT/PATCH/POST/DELETE.
//
// Neither plane renders a wire format here: NETCONF maps the tree to XML
// elements and applies a subtree filter, RESTCONF maps it to JSON or XML and
// applies HTTP status codes. Keeping the semantics in one place is what stops
// the two planes from drifting apart.
package datatree
