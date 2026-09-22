package netconf

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update rewrites the golden transcripts. Regenerate them with
//
//	go test ./internal/netconf -update
//
// and review the diff; the files are never edited by hand. (go test ./... -update
// fails in packages that do not declare the flag.)
var update = flag.Bool("update", false, "rewrite testdata/netconf/*.golden.xml")

// goldenDir holds the recorded NETCONF sessions, relative to this package.
var goldenDir = filepath.Join("..", "..", "testdata", "netconf")

// transcript is an in-memory NETCONF stream: reads come from the recorded client
// messages, writes collect the server's answers.
type transcript struct {
	reader *bytes.Reader
	writer bytes.Buffer
}

// Read implements io.Reader.
func (t *transcript) Read(p []byte) (int, error) { return t.reader.Read(p) }

// Write implements io.Writer.
func (t *transcript) Write(p []byte) (int, error) { return t.writer.Write(p) }

// playSession runs one recorded session through the real session code and
// returns what the server wrote.
func playSession(t *testing.T, input []byte) []byte {
	t.Helper()

	r, st := newTestStore(t)
	server := New(r, st, nil, Options{Addr: "127.0.0.1:0"})

	stream := &transcript{reader: bytes.NewReader(input)}
	sess := &session{server: server, id: server.nextSessionID(), stream: stream}
	sess.run(context.Background())

	return stream.writer.Bytes()
}

// TestGoldenTranscripts compares the server's answers with the recorded golden
// files: the hello exchange, edit-config, commit and get-config in one, the
// get-config filter and error cases in the other, and the confirmed-commit
// dialog in the third.
func TestGoldenTranscripts(t *testing.T) {
	for _, name := range []string{"edit-config", "get-config", "confirmed-commit"} {
		t.Run(name, func(t *testing.T) {
			input := readGolden(t, name+".xml")
			got := playSession(t, input)

			if *update {
				writeGolden(t, name+".golden.xml", got)
				return
			}

			want := readGolden(t, name+".golden.xml")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// readGolden reads one file of goldenDir.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(goldenDir, name))
	require.NoError(t, err, "read %s", name)
	return data
}

// writeGolden rewrites one golden file during -update.
func writeGolden(t *testing.T, name string, data []byte) {
	t.Helper()

	path := filepath.Join(goldenDir, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	t.Logf("updated %s", path)
}
