package restconf

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// seedRouter returns a router over a store seeded with DefaultDevice.
func seedRouter(t *testing.T) (*router.Router, *store.Memory) {
	t.Helper()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(t.Context(), store.Running))
	require.NoError(t, st.Rollback(t.Context()))
	return r, st
}

// newTestRouter returns only the router, for path-resolution tests.
func newTestRouter(t *testing.T) *router.Router {
	t.Helper()
	r, _ := seedRouter(t)
	return r
}

// parse resolves one URL against the test router.
func parse(t *testing.T, target string) (*target, *httpError) {
	t.Helper()
	server := New(newTestRouter(t), store.NewMemory(store.Options{}), nil, Options{})
	return server.parseTarget(httptest.NewRequest(http.MethodGet, target, nil))
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		wantPath      string
		wantBasePath  string
		wantName      string
		wantKind      router.Kind
		wantKey       string
		wantValue     string
		wantDatastore store.Datastore
		wantContent   datatree.Content
		wantCollect   bool
	}{
		{
			name: "datastore root", url: "/restconf/data",
			wantPath: "", wantKind: "", wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "container", url: "/restconf/data/sim-device:system-info",
			wantPath: "system-info", wantBasePath: "", wantName: "system-info", wantKind: router.KindContainer,
			wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "leaf", url: "/restconf/data/sim-device:interfaces/interface=radio0/radio-link/tx-power",
			wantPath:     "interfaces/interface[name=radio0]/radio-link/tx-power",
			wantBasePath: "interfaces/interface[name=radio0]/radio-link", wantName: "tx-power",
			wantKind: router.KindLeaf, wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "list entry", url: "/restconf/data/sim-l2-switching:vlans/vlan=100",
			wantPath:     "vlans/vlan[id=100]",
			wantBasePath: "vlans", wantName: "vlan", wantKind: router.KindList, wantKey: "id", wantValue: "100",
			wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "list collection", url: "/restconf/data/sim-l2-switching:vlans/vlan",
			wantPath: "vlans/vlan", wantBasePath: "vlans", wantName: "vlan", wantKind: router.KindList,
			wantDatastore: store.Running, wantContent: contentAll, wantCollect: true,
		},
		{
			name: "container of a list", url: "/restconf/data/sim-l2-switching:vlans",
			wantPath: "vlans", wantBasePath: "", wantName: "vlans", wantKind: router.KindContainer,
			wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "unqualified top node", url: "/restconf/data/system-info",
			wantPath: "system-info", wantName: "system-info", wantKind: router.KindContainer,
			wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "trailing slash", url: "/restconf/data/sim-device:system-info/",
			wantPath: "system-info", wantName: "system-info", wantKind: router.KindContainer,
			wantDatastore: store.Running, wantContent: contentAll,
		},
		{
			name: "query parameters", url: "/restconf/data/sim-device:system-info?datastore=candidate&content=config",
			wantPath: "system-info", wantName: "system-info", wantKind: router.KindContainer,
			wantDatastore: store.Candidate, wantContent: contentConfig,
		},
		{
			name: "nested list key", url: "/restconf/data/sim-device:interfaces/interface=radio0/radio-link/modulation-profile=5",
			wantPath:     "interfaces/interface[name=radio0]/radio-link/modulation-profile[id=5]",
			wantBasePath: "interfaces/interface[name=radio0]/radio-link", wantName: "modulation-profile",
			wantKind: router.KindList, wantKey: "id", wantValue: "5",
			wantDatastore: store.Running, wantContent: contentAll,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(t, tt.url)
			require.Nil(t, err)

			assert.Equal(t, tt.wantPath, got.Path)
			assert.Equal(t, tt.wantBasePath, got.BasePath)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantKind, got.Node.Kind)
			assert.Equal(t, tt.wantKey, got.Segment.Key)
			assert.Equal(t, tt.wantValue, got.Segment.Value)
			assert.Equal(t, tt.wantDatastore, got.Datastore)
			assert.Equal(t, tt.wantContent, got.Content)
			assert.Equal(t, tt.wantCollect, got.Collection)
		})
	}
}

func TestParseTargetErrors(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantStatus int
		wantTag    string
	}{
		{name: "unknown node", url: "/restconf/data/sim-device:nope", wantStatus: 400, wantTag: "unknown-element"},
		{name: "unknown nested node", url: "/restconf/data/sim-device:system-info/nope", wantStatus: 400, wantTag: "unknown-element"},
		{name: "unknown list entry leaf", url: "/restconf/data/sim-l2-switching:vlans/vlan=100/nope", wantStatus: 400, wantTag: "unknown-element"},
		{name: "key on a container", url: "/restconf/data/sim-device:system-info=1", wantStatus: 400, wantTag: "malformed-message"},
		{name: "missing key in the middle", url: "/restconf/data/sim-device:interfaces/interface/name", wantStatus: 400, wantTag: "malformed-message"},
		{name: "module mismatch", url: "/restconf/data/sim-device:vlans", wantStatus: 400, wantTag: "malformed-message"},
		{name: "unknown datastore", url: "/restconf/data/sim-device:system-info?datastore=nope", wantStatus: 400, wantTag: "malformed-message"},
		{name: "unknown content", url: "/restconf/data/sim-device:system-info?content=nope", wantStatus: 400, wantTag: "malformed-message"},
		{name: "outside data", url: "/restconf/other", wantStatus: 404, wantTag: "invalid-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.url)
			require.NotNil(t, err)
			assert.Equal(t, tt.wantStatus, err.status)
			assert.Equal(t, tt.wantTag, err.tag)
		})
	}
}
