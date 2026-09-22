package netconf

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// framingMode is one of the two NETCONF over SSH framing mechanisms (RFC 6242).
type framingMode uint8

const (
	// framingEOM is the end-of-message framing of RFC 6242 §4.1.
	framingEOM framingMode = iota
	// framingChunked is the chunked framing of RFC 6242 §4.2.
	framingChunked
)

// String names the framing mode for logs and tests.
func (m framingMode) String() string {
	if m == framingChunked {
		return "chunked"
	}
	return "eom"
}

// endOfMessage terminates a message in end-of-message framing.
const endOfMessage = "]]>]]>"

// maxMessageSize bounds one NETCONF message, so a malformed or hostile peer
// cannot make the server allocate without limit.
const maxMessageSize = 16 << 20

// Framing errors.
var (
	// errMalformedFrame means the peer sent bytes that do not follow the
	// negotiated framing grammar.
	errMalformedFrame = errors.New("netconf: malformed frame")
	// errMessageTooBig means one message exceeded maxMessageSize.
	errMessageTooBig = errors.New("netconf: message too big")
)

// messageReader reads NETCONF messages from a stream using one framing mode.
// The mode can change once, after the hello exchange (RFC 6242 §4.2).
type messageReader struct {
	reader *bufio.Reader
	mode   framingMode
}

// newMessageReader returns a reader in the given framing mode. The caller keeps
// the bufio.Reader, so detectFraming can sniff it before the first message.
func newMessageReader(r *bufio.Reader, mode framingMode) *messageReader {
	return &messageReader{reader: r, mode: mode}
}

// SetMode switches the framing mode for subsequent messages.
func (r *messageReader) SetMode(mode framingMode) { r.mode = mode }

// ReadMessage reads one message, without its framing, and returns it. The
// returned slice is a fresh copy.
func (r *messageReader) ReadMessage() ([]byte, error) {
	if r.mode == framingChunked {
		return r.readChunked()
	}
	return r.readEOM()
}

// readEOM reads until the end-of-message delimiter.
func (r *messageReader) readEOM() ([]byte, error) {
	var message []byte
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		message = append(message, b)
		if len(message) > maxMessageSize {
			return nil, errMessageTooBig
		}
		if bytes.HasSuffix(message, []byte(endOfMessage)) {
			return message[:len(message)-len(endOfMessage)], nil
		}
	}
}

// readChunked reads a sequence of chunks terminated by end-of-chunks:
//
//	chunk         = "\n" "#" chunk-size "\n" chunk-data
//	end-of-chunks = "\n" "##" "\n"
//
// chunk-size is hexadecimal, as RFC 6242 requires.
func (r *messageReader) readChunked() ([]byte, error) {
	var message []byte
	for {
		if err := r.expectChunkStart(); err != nil {
			return nil, err
		}
		size, end, err := r.readChunkSize()
		if err != nil {
			return nil, err
		}
		if end {
			return message, nil
		}
		if size > maxMessageSize-len(message) {
			return nil, errMessageTooBig
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(r.reader, chunk); err != nil {
			return nil, err
		}
		message = append(message, chunk...)
	}
}

// expectChunkStart consumes the LF that precedes every chunk and the '#' that
// opens it. A leading LF is optional, so peers that only put one before each
// chunk after the first still interoperate.
func (r *messageReader) expectChunkStart() error {
	b, err := r.reader.ReadByte()
	if err != nil {
		return err
	}
	if b == '\n' {
		if b, err = r.reader.ReadByte(); err != nil {
			return err
		}
	}
	if b != '#' {
		return fmt.Errorf("%w: expected '#', got %q", errMalformedFrame, b)
	}
	return nil
}

// readChunkSize reads the hexadecimal size that follows '#' and the closing LF.
// It reports true for the '##' end-of-chunks marker.
func (r *messageReader) readChunkSize() (int, bool, error) {
	first, err := r.reader.ReadByte()
	if err != nil {
		return 0, false, err
	}
	if first == '#' {
		b, err := r.reader.ReadByte()
		if err != nil {
			return 0, false, err
		}
		if b != '\n' {
			return 0, false, fmt.Errorf("%w: malformed end-of-chunks", errMalformedFrame)
		}
		return 0, true, nil
	}

	digits := []byte{first}
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return 0, false, err
		}
		if b == '\n' {
			break
		}
		digits = append(digits, b)
		if len(digits) > 8 {
			return 0, false, fmt.Errorf("%w: chunk size out of range", errMalformedFrame)
		}
	}

	size, err := strconv.ParseUint(string(digits), 16, 32)
	if err != nil {
		return 0, false, fmt.Errorf("%w: chunk size %q is not hexadecimal", errMalformedFrame, digits)
	}
	return int(size), false, nil
}

// detectFraming sniffs the framing of the first message on a stream: chunked
// messages start with an optional LF and '#', end-of-message messages start
// with '<'. RFC 6242 has both hellos use end-of-message framing, but accepting
// a chunked hello keeps clients that switch earlier interoperable.
func detectFraming(reader *bufio.Reader) (framingMode, error) {
	first, err := reader.Peek(1)
	if err != nil {
		return framingEOM, err
	}
	if first[0] == '#' {
		return framingChunked, nil
	}
	if first[0] == '\n' {
		two, err := reader.Peek(2)
		if err != nil {
			return framingEOM, err
		}
		if two[1] == '#' {
			return framingChunked, nil
		}
	}
	return framingEOM, nil
}

// messageWriter writes NETCONF messages to a stream using one framing mode.
//
// One writer serves the whole session: the RPC loop writes <rpc-reply>
// documents while the notification dispatcher writes <notification> documents
// from another goroutine, so the mutex keeps every message framed by exactly one
// Write call and never interleaved with another.
type messageWriter struct {
	mu     sync.Mutex
	writer io.Writer
	mode   framingMode
}

// newMessageWriter returns a writer in the given framing mode.
func newMessageWriter(w io.Writer, mode framingMode) *messageWriter {
	return &messageWriter{writer: w, mode: mode}
}

// SetMode switches the framing mode for subsequent messages.
func (w *messageWriter) SetMode(mode framingMode) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.mode = mode
}

// WriteMessage writes one message with its framing. Each message is written
// with one Write call; concurrent writers are serialized.
func (w *messageWriter) WriteMessage(message []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.mode == framingEOM {
		framed := make([]byte, 0, len(message)+len(endOfMessage))
		framed = append(framed, message...)
		framed = append(framed, endOfMessage...)
		_, err := w.writer.Write(framed)
		return err
	}

	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "\n#%x\n", len(message))
	buffer.Write(message)
	buffer.WriteString("\n##\n")
	_, err := w.writer.Write(buffer.Bytes())
	return err
}
