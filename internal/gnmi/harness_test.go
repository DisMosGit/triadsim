package gnmi

import (
	"context"
	"strings"
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// testServer is one running gNMI server over a seeded store.
type testServer struct {
	server *Server
	store  *store.Memory
	bus    *event.Bus
	client gnmi.GNMIClient
}

// newTestServer seeds a DefaultDevice into a Memory store and serves gNMI on an
// ephemeral port. A false withBus leaves the server without an event bus, which
// disables STREAM subscriptions.
func newTestServer(t *testing.T, withBus bool) *testServer {
	t.Helper()

	ctx := context.Background()
	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	st.SetStateFilter(r.IsState)
	require.NoError(t, r.Seed(ctx, store.Running))
	require.NoError(t, st.Rollback(ctx))

	var bus *event.Bus
	if withBus {
		bus = event.New(event.DefaultBuffer, clock.RealClock{})
		t.Cleanup(bus.Close)
	}

	server := New(r, st, bus, Options{Addr: "127.0.0.1:0", Clock: clock.RealClock{}})
	require.NoError(t, server.Listen())
	t.Cleanup(func() { _ = server.Close() })

	runCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() { _ = server.Serve(runCtx) }()

	conn, err := grpc.NewClient(server.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return &testServer{
		server: server,
		store:  st,
		bus:    bus,
		client: gnmi.NewGNMIClient(conn),
	}
}

// elem builds a gNMI path element without keys.
func elem(name string) *gnmi.PathElem { return &gnmi.PathElem{Name: name} }

// keyed builds a gNMI list-element path element.
func keyed(name, key, value string) *gnmi.PathElem {
	return &gnmi.PathElem{Name: name, Key: map[string]string{key: value}}
}

// path builds a gNMI path from elements.
func path(elems ...*gnmi.PathElem) *gnmi.Path { return &gnmi.Path{Elem: elems} }

// updateWith returns the update whose relative path spells elements, or nil.
func updateWith(notification *gnmi.Notification, elements ...string) *gnmi.Update {
	wanted := strings.Join(elements, "/")
	for _, update := range notification.GetUpdate() {
		if relativeString(update.GetPath()) == wanted {
			return update
		}
	}
	return nil
}

// relativeString renders a gNMI path as router-style text.
func relativeString(path *gnmi.Path) string {
	parts := make([]string, 0, len(path.GetElem()))
	for _, elem := range path.GetElem() {
		if value, ok := elem.GetKey()["name"]; ok {
			parts = append(parts, elem.GetName()+"[name="+value+"]")
			continue
		}
		parts = append(parts, elem.GetName())
	}
	return strings.Join(parts, "/")
}
