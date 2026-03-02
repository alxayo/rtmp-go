package client

// Minimal RTMP test client (Task T052)
// ------------------------------------
// Scope: Provide a tiny RTMP client used only by integration / unit tests to
// drive the server implementation. It purposefully implements only the pieces
// required by current tests and specification phases:
//   * TCP dial + simple handshake (handshake.ClientHandshake)
//   * Send connect + createStream command messages
//   * Publish mode: send publish command + raw audio/video RTMP messages
//   * Play mode: send play command and read incoming audio/video messages
//     (parsing limited to chunk reassembly; higher‑level parsing lives in
//     media layer packages already implemented for the server).
//   * CLI compatibility hook (RunCLI) used by a future small main in
//     cmd/rtmp-client – kept here so tests can exercise without duplication.
//
// Non‑Goals (for now): full error command responses, bandwidth / control
// messages, extended timestamp edge cases, retransmission, AMF3.
//
// Design Notes:
//   * A single goroutine is used for reading when in Play mode; Write calls
//     are synchronous and not concurrency‑safe (mirrors internal server
//     patterns where one writeLoop owns the connection).
//   * Command messages share a fixed Chunk Stream ID (3) with MessageStreamID
//     0 until a stream ID (currently we assume stream ID 1) is needed for
//     publish / play flows. For test purposes we optimistically use 1 – the
//     server side createStream implementation also allocates 1 first.
//   * Transaction IDs start at 1 and increment per command that expects a
//     response (connect=1, createStream=2). Publish/play use 0 per common RTMP
//     practice.
//
// Simplifications are documented inline so future tasks can extend safely.

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// DialTimeout used for TCP connections.
const DialTimeout = 5 * time.Second

// Default outbound chunk size – matches server control burst (T025) value (4096)
// but we start with 128 until the server potentially issues Set Chunk Size.
const defaultChunkSize = 128

// Chunk stream IDs per RTMP convention.
const (
	commandCSID = 3 // commands (connect, createStream, publish, play)
	audioCSID   = 6 // audio data
	videoCSID   = 7 // video data
)

// Client represents a minimal RTMP client for testing and relay purposes.
// It handles the full connection lifecycle: TCP dial → handshake → connect
// command → createStream → publish/play → send audio/video.
type Client struct {
	conn   net.Conn       // underlying TCP connection
	writer *chunk.Writer  // encodes outbound messages into chunks
	reader *chunk.Reader  // decodes inbound chunks into messages
	url    *url.URL       // parsed RTMP URL
	log    *slog.Logger   // structured logger with "rtmp_client" component tag

	app       string       // application name extracted from URL path (e.g. "live")
	streamKey string       // full stream key: "app/streamName" (e.g. "live/mystream")
	streamID  uint32       // message stream ID assigned by server's createStream response

	trxMu sync.Mutex      // protects trxID from concurrent access
	trxID float64         // incrementing transaction ID for request-response matching
}

// New creates a new Client (not yet connected).
func New(rawurl string) (*Client, error) {
	if !strings.HasPrefix(rawurl, "rtmp://") {
		return nil, fmt.Errorf("url must start with rtmp://")
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	// Path expected: /app/streamName
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("rtmp url must be rtmp://host/app/stream")
	}
	app := parts[0]
	stream := strings.Join(parts[1:], "/")
	c := &Client{
		url:       u,
		app:       app,
		streamKey: app + "/" + stream,
		trxID:     0,
		log:       logger.Logger().With("component", "rtmp_client"),
	}
	return c, nil
}

// nextTrx increments and returns the next transaction ID (AMF0 number semantics).
func (c *Client) nextTrx() float64 { c.trxMu.Lock(); defer c.trxMu.Unlock(); c.trxID++; return c.trxID }

// Connect performs TCP dial, RTMP simple handshake, then sends connect + createStream.
func (c *Client) Connect() error {
	if c.conn != nil {
		return nil
	}
	host := c.url.Host
	if !strings.Contains(host, ":") {
		host = host + ":1935"
	}
	d := net.Dialer{Timeout: DialTimeout}
	conn, err := d.Dial("tcp", host)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.conn = conn
	c.writer = chunk.NewWriter(conn, defaultChunkSize)
	c.reader = chunk.NewReader(conn, defaultChunkSize)

	if err := handshake.ClientHandshake(conn); err != nil {
		_ = conn.Close()
		return err
	}

	if err := c.sendConnectAndWaitResponse(); err != nil {
		return err
	}
	if err := c.sendCreateStreamAndWaitResponse(); err != nil {
		return err
	}
	return nil
}

func (c *Client) sendConnect() error {
	trx := c.nextTrx()
	cmdObj := map[string]interface{}{
		"app":            c.app,
		"type":           "nonprivate",
		"tcUrl":          c.url.String(),
		"fpad":           false,
		"capabilities":   15.0,
		"audioCodecs":    0.0,
		"videoCodecs":    0.0,
		"videoFunction":  1.0,
		"flashVer":       "LNX 9,0,124,2",
		"swfUrl":         "",
		"objectEncoding": 0.0,
	}
	payload, err := amf.EncodeAll("connect", trx, cmdObj)
	if err != nil {
		return err
	}
	msg := &chunk.Message{CSID: commandCSID, TypeID: rpc.CommandMessageAMF0TypeIDForTest(), MessageStreamID: 0, MessageLength: uint32(len(payload)), Payload: payload}
	return c.writer.WriteMessage(msg)
}

func (c *Client) sendCreateStream() error {
	trx := c.nextTrx()
	payload, err := amf.EncodeAll("createStream", trx, nil)
	if err != nil {
		return err
	}
	msg := &chunk.Message{CSID: commandCSID, TypeID: rpc.CommandMessageAMF0TypeIDForTest(), MessageStreamID: 0, MessageLength: uint32(len(payload)), Payload: payload}
	if err := c.writer.WriteMessage(msg); err != nil {
		return err
	}
	// Assume streamID 1 for subsequent publish/play (typical first allocation)
	c.streamID = 1
	return nil
}

func (c *Client) sendConnectAndWaitResponse() error {
	c.log.Debug("sending connect command")
	if err := c.sendConnect(); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}

	c.log.Debug("waiting for connect response")
	_, err := c.waitForCommandResponse("connect")
	if err != nil {
		return fmt.Errorf("connect response: %w", err)
	}
	c.log.Info("connect completed")
	return nil
}

func (c *Client) sendCreateStreamAndWaitResponse() error {
	c.log.Debug("sending createStream command")
	if err := c.sendCreateStream(); err != nil {
		return fmt.Errorf("send createStream: %w", err)
	}

	c.log.Debug("waiting for createStream response")
	args, err := c.waitForCommandResponse("createStream")
	if err != nil {
		return fmt.Errorf("createStream response: %w", err)
	}
	// Extract stream ID from response (args[3])
	if len(args) >= 4 {
		if streamID, ok := args[3].(float64); ok {
			c.streamID = uint32(streamID)
		}
	}
	c.log.Info("createStream completed", "stream_id", c.streamID)
	return nil
}

// waitForCommandResponse reads messages until a _result or _error response is
// received for the given command name. Returns the decoded AMF values on success.
func (c *Client) waitForCommandResponse(cmdName string) ([]interface{}, error) {
	for {
		msg, err := c.reader.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read message: %w", err)
		}

		if msg.TypeID != rpc.CommandMessageAMF0TypeIDForTest() {
			continue
		}

		args, err := amf.DecodeAll(msg.Payload)
		if err != nil {
			continue // skip malformed messages
		}
		if len(args) < 1 {
			continue
		}

		name, ok := args[0].(string)
		if !ok {
			continue
		}

		switch name {
		case "_result":
			c.log.Debug("received _result", "cmd", cmdName)
			return args, nil
		case "_error":
			c.log.Debug("received _error", "cmd", cmdName)
			return nil, fmt.Errorf("%s command failed", cmdName)
		}
	}
}

// Publish sends a publish command for the stream name implied by the RTMP URL.
func (c *Client) Publish() error {
	if c.conn == nil {
		return errors.New("client not connected")
	}
	name := strings.TrimPrefix(c.streamKey, c.app+"/")
	c.log.Debug("sending publish command", "stream", name)
	payload, err := amf.EncodeAll("publish", float64(0), nil, name, "live")
	if err != nil {
		return err
	}
	msg := &chunk.Message{CSID: commandCSID, TypeID: rpc.CommandMessageAMF0TypeIDForTest(), MessageStreamID: c.streamID, MessageLength: uint32(len(payload)), Payload: payload}
	if err := c.writer.WriteMessage(msg); err != nil {
		return err
	}
	c.log.Info("publish command sent", "stream", name)
	return nil
}

// Play sends a play command for the stream name.
func (c *Client) Play() error {
	if c.conn == nil {
		return errors.New("client not connected")
	}
	name := strings.TrimPrefix(c.streamKey, c.app+"/")
	// Standard play argument pattern: name, start=-2 (live), duration=-1 (all), reset=false
	payload, err := amf.EncodeAll("play", float64(0), nil, name, float64(-2), float64(-1), false)
	if err != nil {
		return err
	}
	msg := &chunk.Message{CSID: commandCSID, TypeID: rpc.CommandMessageAMF0TypeIDForTest(), MessageStreamID: c.streamID, MessageLength: uint32(len(payload)), Payload: payload}
	return c.writer.WriteMessage(msg)
}

// SendAudio sends a raw audio message (TypeID=8) with caller-provided payload.
func (c *Client) SendAudio(ts uint32, data []byte) error {
	if c.conn == nil {
		return errors.New("client not connected")
	}
	if c.writer == nil {
		return errors.New("writer not initialized")
	}
	if len(data) == 0 {
		return errors.New("empty audio payload")
	}

	msg := &chunk.Message{
		CSID:            audioCSID,
		TypeID:          8,
		MessageStreamID: c.streamID,
		Timestamp:       ts,
		MessageLength:   uint32(len(data)),
		Payload:         data,
	}

	if err := c.writer.WriteMessage(msg); err != nil {
		return fmt.Errorf("write audio message: %w", err)
	}

	return nil
}

// SendVideo sends a raw video message (TypeID=9) with caller-provided payload.
func (c *Client) SendVideo(ts uint32, data []byte) error {
	if c.conn == nil {
		return errors.New("client not connected")
	}
	if c.writer == nil {
		return errors.New("writer not initialized")
	}
	if len(data) == 0 {
		return errors.New("empty video payload")
	}

	msg := &chunk.Message{
		CSID:            videoCSID,
		TypeID:          9,
		MessageStreamID: c.streamID,
		Timestamp:       ts,
		MessageLength:   uint32(len(data)),
		Payload:         data,
	}

	if err := c.writer.WriteMessage(msg); err != nil {
		return fmt.Errorf("write video message: %w", err)
	}

	return nil
}

// Close terminates the underlying TCP connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	c.writer = nil
	return err
}

// RunCLI executes a simplified publish / play action based on args.
// Usage examples (from task requirements):
//
//	rtmp-client publish rtmp://host/app/stream file.flv
//
// For now we only implement the connect + publish handshake; file muxing
// is out of current scope – we simulate by sending a single dummy audio tag.
func RunCLI(args []string, stdout io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(stdout, "usage: rtmp-client <publish|play> rtmp://host/app/stream [file]")
		return 2
	}
	mode := args[0]
	url := args[1]
	c, err := New(url)
	if err != nil {
		fmt.Fprintln(stdout, "error:", err)
		return 1
	}
	if err := c.Connect(); err != nil {
		fmt.Fprintln(stdout, "connect error:", err)
		return 1
	}
	switch mode {
	case "publish":
		if err := c.Publish(); err != nil {
			fmt.Fprintln(stdout, "publish error:", err)
			return 1
		}
		// send one dummy audio packet (AAC sequence header-ish)
		_ = c.SendAudio(0, []byte{0xAF, 0x00})
		fmt.Fprintln(stdout, "published", c.streamKey)
	case "play":
		if err := c.Play(); err != nil {
			fmt.Fprintln(stdout, "play error:", err)
			return 1
		}
		fmt.Fprintln(stdout, "play requested", c.streamKey)
	default:
		fmt.Fprintln(stdout, "unknown mode", mode)
		return 2
	}
	_ = c.Close()
	return 0
}
