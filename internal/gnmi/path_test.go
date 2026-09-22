package gnmi

import (
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRouterPath(t *testing.T) {
	tests := []struct {
		name    string
		path    *gnmi.Path
		want    string
		wantErr codes.Code
	}{
		{name: "nil path is the root", path: nil, want: ""},
		{name: "empty path is the root", path: &gnmi.Path{}, want: ""},
		{
			name: "elements",
			path: &gnmi.Path{Elem: []*gnmi.PathElem{elem("ptp"), elem("clock")}},
			want: "ptp/clock",
		},
		{
			name: "module prefix is stripped",
			path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "sim-sync:ptp"}, elem("clock")}},
			want: "ptp/clock",
		},
		{
			name: "list key becomes a predicate",
			path: &gnmi.Path{Elem: []*gnmi.PathElem{
				elem("interfaces"),
				keyed("interface", "name", "radio0"),
				elem("radio-link"),
			}},
			want: "interfaces/interface[name=radio0]/radio-link",
		},
		{
			name: "module name is an accepted origin",
			path: &gnmi.Path{Origin: "sim-sync", Elem: []*gnmi.PathElem{elem("ptp")}},
			want: "ptp",
		},
		{
			name: "namespace is an accepted origin",
			path: &gnmi.Path{Origin: "urn:sim:sync", Elem: []*gnmi.PathElem{elem("ptp")}},
			want: "ptp",
		},
		{
			name: "deprecated element list",
			path: &gnmi.Path{Element: []string{"ptp", "clock"}},
			want: "ptp/clock",
		},
		{
			name:    "unknown origin",
			path:    &gnmi.Path{Origin: "openconfig", Elem: []*gnmi.PathElem{elem("ptp")}},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "composite key",
			path: &gnmi.Path{Elem: []*gnmi.PathElem{{
				Name: "interface",
				Key:  map[string]string{"name": "radio0", "slot": "1"},
			}}},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "empty key value",
			path:    &gnmi.Path{Elem: []*gnmi.PathElem{keyed("interface", "name", "")}},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "unnamed element",
			path:    &gnmi.Path{Elem: []*gnmi.PathElem{{Name: ""}}},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "malformed deprecated path",
			path:    &gnmi.Path{Element: []string{"ptp[clock"}},
			wantErr: codes.InvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := routerPath(test.path)
			if test.wantErr != codes.OK {
				require.Error(t, err)
				assert.Equal(t, test.wantErr, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestGmniPathRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "root", path: ""},
		{name: "container", path: "ptp/clock"},
		{name: "list entry", path: "interfaces/interface[name=radio0]/radio-link"},
		{name: "leaf", path: "stp/state/ports/port[name=eth0]/state"},
		{name: "invalid", path: "ptp//clock", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := gnmiPath(test.path)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			back, err := routerPath(converted)
			require.NoError(t, err)
			assert.Equal(t, test.path, back)
		})
	}
}

func TestGmniPathCarriesListKeys(t *testing.T) {
	converted, err := gnmiPath("interfaces/interface[name=radio0]/radio-link")
	require.NoError(t, err)

	require.Len(t, converted.GetElem(), 3)
	assert.Equal(t, "interface", converted.GetElem()[1].GetName())
	assert.Equal(t, map[string]string{"name": "radio0"}, converted.GetElem()[1].GetKey())
	assert.Empty(t, converted.GetElem()[2].GetKey())
}

func TestCheckPath(t *testing.T) {
	ts := newTestServer(t, false)

	tests := []struct {
		name string
		path string
		want codes.Code
	}{
		{name: "root", path: "", want: codes.OK},
		{name: "container", path: "ptp/clock", want: codes.OK},
		{name: "leaf", path: "ptp/clock/state", want: codes.OK},
		{name: "list entry", path: "interfaces/interface[name=radio0]", want: codes.OK},
		{name: "unknown container", path: "no-such-container", want: codes.NotFound},
		{name: "unknown leaf", path: "ptp/no-such-leaf", want: codes.NotFound},
		{name: "unknown parent", path: "no-such-container/leaf", want: codes.NotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkPath(ts.server.router, test.path)
			if test.want == codes.OK {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, test.want, status.Code(err))
		})
	}
}

func TestJoinPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		rel    *gnmi.Path
		want   string
	}{
		{name: "both empty", prefix: "", rel: &gnmi.Path{}, want: ""},
		{name: "prefix only", prefix: "ptp", rel: &gnmi.Path{}, want: "ptp"},
		{name: "relative only", prefix: "", rel: path(elem("clock")), want: "clock"},
		{name: "joined", prefix: "ptp", rel: path(elem("clock")), want: "ptp/clock"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := joinPrefix(test.prefix, test.rel)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestPathsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "root overlaps everything", a: "", b: "ptp/clock", want: true},
		{name: "identical", a: "ptp/clock", b: "ptp/clock", want: true},
		{name: "parent and child", a: "ptp", b: "ptp/clock", want: true},
		{name: "child and parent", a: "ptp/clock", b: "ptp", want: true},
		{
			name: "different list entries",
			a:    "interfaces/interface[name=eth0]",
			b:    "interfaces/interface[name=eth01]",
			want: false,
		},
		{name: "unrelated subtrees", a: "ptp/clock", b: "vlans", want: false},
		{name: "unparsable side", a: "ptp//clock", b: "ptp", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, pathsOverlap(test.a, test.b))
		})
	}
}
