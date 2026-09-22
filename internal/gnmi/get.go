package gnmi

import (
	"context"
	"strings"

	"github.com/openconfig/gnmi/proto/gnmi"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Get returns the requested paths of the running datastore as a notification
// per path, with one Update per leaf. A path that holds no data contributes no
// notification rather than an error, which is the gNMI convention.
func (s *service) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	prefix, err := routerPath(req.GetPrefix())
	if err != nil {
		return nil, err
	}
	encoding, err := encodingFor(req.GetEncoding())
	if err != nil {
		return nil, err
	}
	content := contentFor(req.GetType())

	paths := req.GetPath()
	if len(paths) == 0 {
		paths = []*gnmi.Path{{}}
	}

	response := &gnmi.GetResponse{}
	for _, path := range paths {
		target, err := joinPrefix(prefix, path)
		if err != nil {
			return nil, err
		}
		if err := checkPath(s.server.router, target); err != nil {
			return nil, err
		}

		notification, err := s.notification(ctx, target, content, encoding)
		if err != nil {
			return nil, err
		}
		if notification == nil {
			continue
		}
		response.Notification = append(response.Notification, notification)
	}
	return response, nil
}

// notification reads target from ds and renders it as one gNMI notification
// whose prefix is target and whose updates carry the leaves below it. It
// returns (nil, nil) when the node holds no data.
func (s *service) notification(ctx context.Context, target string, content datatree.Content, encoding gnmi.Encoding) (*gnmi.Notification, error) {
	node, err := datatree.Read(ctx, s.server.router, runningDatastore, datatree.ReadOptions{
		Prefix:  target,
		Content: content,
	})
	if err != nil {
		return nil, statusError(err)
	}
	if node == nil {
		return nil, nil
	}

	updates, err := s.leafUpdates(node, target, encoding)
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}

	prefix, err := gnmiPath(target)
	if err != nil {
		return nil, statusError(err)
	}
	return &gnmi.Notification{
		Timestamp: s.server.clock.Now().UnixNano(),
		Prefix:    prefix,
		Update:    updates,
	}, nil
}

// leafUpdates flattens a resolved node into one update per leaf, with paths
// relative to base.
func (s *service) leafUpdates(node *datatree.Node, base string, encoding gnmi.Encoding) ([]*gnmi.Update, error) {
	var updates []*gnmi.Update

	var walk func(current *datatree.Node) error
	walk = func(current *datatree.Node) error {
		if current.Kind == router.KindLeaf {
			value, err := typedValue(current, encoding)
			if err != nil {
				return err
			}
			path, err := gnmiPath(relativePath(current.Path, base))
			if err != nil {
				return statusError(err)
			}
			updates = append(updates, &gnmi.Update{Path: path, Val: value})
			return nil
		}
		for _, child := range current.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(node); err != nil {
		return nil, err
	}
	return updates, nil
}

// relativePath strips the base prefix from a canonical path, returning the path
// of the leaf relative to the notification prefix.
func relativePath(path, base string) string {
	switch {
	case base == "":
		return path
	case path == base:
		return ""
	default:
		return strings.TrimPrefix(strings.TrimPrefix(path, base), "/")
	}
}

// runningDatastore is the datastore gNMI reads and writes.
const runningDatastore = store.Running
