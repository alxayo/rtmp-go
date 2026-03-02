package conn

// SessionState represents where a connection is in the RTMP session lifecycle.
// Each connection progresses through these states as commands are exchanged:
//
//	Uninitialized → Connected → StreamCreated → Publishing or Playing
//
// The client drives transitions by sending connect, createStream, publish/play commands.
type SessionState uint8

const (
	SessionStateUninitialized SessionState = iota // Initial state before any commands
	SessionStateConnected                         // After successful "connect" command
	SessionStateStreamCreated                     // After "createStream" allocates a stream ID
	SessionStatePublishing                        // After "publish" command (sending media)
	SessionStatePlaying                           // After "play" command (receiving media)
)

// Session holds per-connection RTMP session metadata established during the
// command exchange phase. It tracks the application name, stream identifiers,
// and the current lifecycle state.
type Session struct {
	app            string // application name from connect command (e.g. "live")
	tcUrl          string // target URL from connect (e.g. "rtmp://host/live")
	flashVer       string // client's Flash version string
	objectEncoding uint8  // AMF encoding version (0=AMF0, must be 0 for this server)

	transactionID uint32 // incrementing counter for request-response matching
	streamID      uint32 // message stream ID allocated by createStream (typically 1)
	streamKey     string // full stream key: "app/streamName" (e.g. "live/mystream")

	state SessionState // current lifecycle state
}

// NewSession creates a new Session in Uninitialized state.
func NewSession() *Session {
	return &Session{transactionID: 1, state: SessionStateUninitialized}
}

// SetConnectInfo sets fields derived from the "connect" command and
// moves the session into Connected state.
func (s *Session) SetConnectInfo(app, tcUrl, flashVer string, objectEncoding uint8) {
	s.app = app
	s.tcUrl = tcUrl
	s.flashVer = flashVer
	s.objectEncoding = objectEncoding
	if s.state == SessionStateUninitialized {
		s.state = SessionStateConnected
	}
}

// NextTransactionID increments and returns the next transaction id.
// Starts from 1 so the first call returns 2. This mirrors common RTMP
// client behavior (FFmpeg/OBS) where the connect command uses an id
// of 1 and responses increment from there.
func (s *Session) NextTransactionID() uint32 {
	s.transactionID++
	return s.transactionID
}

// AllocateStreamID allocates (or increments) the message stream ID.
// Typical RTMP sessions only allocate a single stream (id 1). We keep
// the counter logic simple to allow future multi-stream support.
// Returns the allocated stream id.
func (s *Session) AllocateStreamID() uint32 {
	if s.streamID == 0 {
		s.streamID = 1
	} else {
		s.streamID++
	}
	if s.state == SessionStateConnected {
		s.state = SessionStateStreamCreated
	}
	return s.streamID
}

// SetStreamKey composes and stores the fully-qualified stream key
// using the application name and provided streamName. Returns the
// constructed key. The higher-level publish/play handlers will set
// the appropriate final state (Publishing or Playing); we only set
// Publishing as a neutral placeholder if not already set.
func (s *Session) SetStreamKey(app, streamName string) string {
	// Prefer explicit app param (may match s.app); do not override if empty.
	if app != "" {
		s.app = app
	}
	s.streamKey = s.app + "/" + streamName
	// If stream was created but role not yet specified, mark as Publishing placeholder.
	if s.state == SessionStateStreamCreated {
		s.state = SessionStatePublishing
	}
	return s.streamKey
}

// Accessor methods (read-only) ------------------------------------------------

func (s *Session) App() string           { return s.app }
func (s *Session) TcUrl() string         { return s.tcUrl }
func (s *Session) FlashVer() string      { return s.flashVer }
func (s *Session) ObjectEncoding() uint8 { return s.objectEncoding }
func (s *Session) TransactionID() uint32 { return s.transactionID }
func (s *Session) StreamID() uint32      { return s.streamID }
func (s *Session) StreamKey() string     { return s.streamKey }
func (s *Session) State() SessionState   { return s.state }
