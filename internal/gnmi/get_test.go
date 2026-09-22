package gnmi

import (
	"context"
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCapabilitiesReportsModelsAndEncodings(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Capabilities(context.Background(), &gnmi.CapabilityRequest{})
	require.NoError(t, err)

	var names []string
	for _, model := range response.GetSupportedModels() {
		names = append(names, model.GetName())
	}
	assert.Equal(t, []string{"sim-device", "sim-radio-link", "sim-l2-switching", "sim-sync"}, names)
	assert.ElementsMatch(t, []gnmi.Encoding{
		gnmi.Encoding_JSON,
		gnmi.Encoding_JSON_IETF,
		gnmi.Encoding_PROTO,
	}, response.GetSupportedEncodings())
}

func TestGetLeafJSONIETF(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path:     []*gnmi.Path{path(elem("ptp"), elem("clock"), elem("state"))},
		Type:     gnmi.GetRequest_STATE,
		Encoding: gnmi.Encoding_JSON_IETF,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	notification := response.GetNotification()[0]
	require.Len(t, notification.GetUpdate(), 1)
	assert.Equal(t, "ptp/clock/state", relativeString(notification.GetPrefix()))
	assert.NotZero(t, notification.GetTimestamp())

	update := notification.GetUpdate()[0]
	assert.Empty(t, update.GetPath().GetElem(), "a leaf notification carries an empty relative path")
	assert.JSONEq(t, `"locked"`, string(update.GetVal().GetJsonIetfVal()))
}

func TestGetSubtreeReturnsOneUpdatePerLeaf(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path:     []*gnmi.Path{path(elem("ptp"), elem("clock"))},
		Type:     gnmi.GetRequest_ALL,
		Encoding: gnmi.Encoding_PROTO,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	notification := response.GetNotification()[0]
	assert.Equal(t, "ptp/clock", relativeString(notification.GetPrefix()))

	state := updateWith(notification, "state")
	require.NotNil(t, state, "the subtree must carry the state leaf")
	assert.Equal(t, "locked", state.GetVal().GetStringVal())

	domain := updateWith(notification, "domain")
	require.NotNil(t, domain, "the subtree must carry the domain leaf")
	assert.Equal(t, uint64(24), domain.GetVal().GetUintVal())
}

func TestGetConfigExcludesStateLeaves(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path: []*gnmi.Path{path(elem("ptp"), elem("clock"))},
		Type: gnmi.GetRequest_CONFIG,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	notification := response.GetNotification()[0]
	assert.Nil(t, updateWith(notification, "state"), "config data must not carry read-only leaves")
	assert.NotNil(t, updateWith(notification, "domain"))
}

func TestGetRootCoversEveryTopLevelNode(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Type: gnmi.GetRequest_ALL,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	notification := response.GetNotification()[0]
	assert.NotNil(t, updateWith(notification, "system-info", "device-id"))
	assert.NotNil(t, updateWith(notification, "ptp", "clock", "state"))
	assert.NotNil(t, updateWith(notification,
		"interfaces", "interface[name=radio0]", "radio-link", "tx-power"))
}

func TestGetListEntryKeepsItsKey(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path: []*gnmi.Path{path(
			elem("interfaces"),
			keyed("interface", "name", "radio0"),
			elem("radio-link"),
		)},
		Type: gnmi.GetRequest_ALL,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	assert.Equal(t, "interfaces/interface[name=radio0]/radio-link",
		relativeString(response.GetNotification()[0].GetPrefix()))
}

func TestGetUnknownPathIsNotFound(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path: []*gnmi.Path{path(elem("no-such-container"))},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGetEmptyLeafPathIsNotAnError(t *testing.T) {
	ts := newTestServer(t, false)

	// A VLAN the datastore does not hold: the path is valid, the data is not.
	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path: []*gnmi.Path{path(elem("vlans"), keyed("vlan", "id", "4090"))},
		Type: gnmi.GetRequest_ALL,
	})
	require.NoError(t, err)
	assert.Empty(t, response.GetNotification())
}

func TestGetRejectsUnsupportedEncoding(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Path:     []*gnmi.Path{path(elem("ptp"), elem("clock"))},
		Encoding: gnmi.Encoding_BYTES,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetAppliesTheRequestPrefix(t *testing.T) {
	ts := newTestServer(t, false)

	response, err := ts.client.Get(context.Background(), &gnmi.GetRequest{
		Prefix:   path(elem("interfaces"), keyed("interface", "name", "radio0")),
		Path:     []*gnmi.Path{path(elem("radio-link"), elem("tx-power"))},
		Encoding: gnmi.Encoding_JSON_IETF,
	})
	require.NoError(t, err)

	require.Len(t, response.GetNotification(), 1)
	notification := response.GetNotification()[0]
	assert.Equal(t, "interfaces/interface[name=radio0]/radio-link/tx-power",
		relativeString(notification.GetPrefix()))
	require.Len(t, notification.GetUpdate(), 1)
	assert.JSONEq(t, `20`, string(notification.GetUpdate()[0].GetVal().GetJsonIetfVal()))
}
