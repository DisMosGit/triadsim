//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestGNMIGetAndSet drives the containerised simulator over gNMI: the
// capabilities announce the four models, a Get returns a leaf of the seeded
// device and a Set reaches the running datastore, which the next Get observes.
func TestGNMIGetAndSet(t *testing.T) {
	sim := startSimulator(t, newTrapReceiver(t).port)

	conn, err := grpc.NewClient(sim.gnmiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := gnmi.NewGNMIClient(conn)

	capabilities, err := client.Capabilities(ctx, &gnmi.CapabilityRequest{})
	require.NoError(t, err)
	assert.Len(t, capabilities.GetSupportedModels(), 4)

	clockPath := &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "ptp"}, {Name: "clock"}, {Name: "state"}}}
	response, err := client.Get(ctx, &gnmi.GetRequest{
		Path:     []*gnmi.Path{clockPath},
		Type:     gnmi.GetRequest_STATE,
		Encoding: gnmi.Encoding_JSON_IETF,
	})
	require.NoError(t, err)
	require.Len(t, response.GetNotification(), 1)
	require.Len(t, response.GetNotification()[0].GetUpdate(), 1)
	assert.JSONEq(t, `"locked"`, string(response.GetNotification()[0].GetUpdate()[0].GetVal().GetJsonIetfVal()))

	locationPath := &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "system-info"}, {Name: "location"}}}
	_, err = client.Set(ctx, &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: locationPath,
			Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: "container-lab"}},
		}},
	})
	require.NoError(t, err)

	response, err = client.Get(ctx, &gnmi.GetRequest{
		Path:     []*gnmi.Path{locationPath},
		Type:     gnmi.GetRequest_CONFIG,
		Encoding: gnmi.Encoding_JSON_IETF,
	})
	require.NoError(t, err)
	require.Len(t, response.GetNotification(), 1)
	require.Len(t, response.GetNotification()[0].GetUpdate(), 1)
	assert.JSONEq(t, `"container-lab"`, string(response.GetNotification()[0].GetUpdate()[0].GetVal().GetJsonIetfVal()))
}

// TestGNMISubscribeOnce subscribes to the PTP subtree for a single snapshot and
// expects the sync marker that closes it.
func TestGNMISubscribeOnce(t *testing.T) {
	sim := startSimulator(t, newTrapReceiver(t).port)

	conn, err := grpc.NewClient(sim.gnmiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := gnmi.NewGNMIClient(conn).Subscribe(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{Subscribe: &gnmi.SubscriptionList{
			Mode: gnmi.SubscriptionList_ONCE,
			Subscription: []*gnmi.Subscription{{
				Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "ptp"}}},
				Mode: gnmi.SubscriptionMode_ON_CHANGE,
			}},
		}},
	}))

	update, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, update.GetUpdate())
	assert.NotEmpty(t, update.GetUpdate().GetUpdate())

	sync, err := stream.Recv()
	require.NoError(t, err)
	assert.True(t, sync.GetSyncResponse())
}
