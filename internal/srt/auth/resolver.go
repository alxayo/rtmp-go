// Package srtauth provides pluggable passphrase resolution for SRT
// encryption. When the SRT listener is configured with a resolver,
// each incoming connection's Stream ID is used to look up the correct
// passphrase during the handshake.
//
// Two built-in resolvers are provided:
//
//   - [StaticResolver]: returns the same passphrase for every stream
//     (backward-compatible with the single -srt-passphrase flag)
//   - [FileResolver]: loads per-stream passphrases from a JSON file,
//     supports live reload via [FileResolver.Reload] (triggered by SIGHUP)
//
// Custom resolvers can be implemented by satisfying the [PassphraseResolver]
// interface (e.g., for database-backed or API-driven passphrase lookup).
//
// # How It Works
//
// During the SRT handshake Conclusion phase, the listener parses the
// Stream ID extension and calls the resolver to look up the passphrase
// for that stream. The passphrase is then used to derive the KEK and
// unwrap the client's Stream Encrypting Key (SEK).
//
// # Passphrase Constraints
//
// Per the SRT specification and libsrt implementation, passphrases must
// be 10–79 characters long. These constraints are enforced at load time
// (for FileResolver) and at construction time (for StaticResolver).
// See [ValidatePassphrase] for the validation function.
package srtauth

import "errors"

// PassphraseResolver looks up the encryption passphrase for a given
// SRT stream key. Implementations must be safe for concurrent use.
type PassphraseResolver interface {
	// ResolvePassphrase returns the passphrase for the given stream key.
	// The stream key is the normalized resource from the SRT Stream ID
	// (e.g., "live/mystream"), not the raw Stream ID string.
	//
	// Returns the passphrase on success, or an error if the stream is
	// unknown or the connection should be rejected.
	ResolvePassphrase(streamKey string) (string, error)

	// EncryptionRequired returns true if this resolver requires
	// encryption for connections. Used for logging and early validation.
	EncryptionRequired() bool
}

// Sentinel errors returned by resolvers. Use errors.Is() to check for
// these in calling code, since they may be wrapped with additional context
// (e.g., the stream key that was not found).
var (
	// ErrStreamNotFound is returned by [FileResolver.ResolvePassphrase]
	// when the requested stream key has no entry in the passphrase map.
	// Callers should treat this as a rejected connection — the client
	// is requesting a stream the server does not recognize.
	ErrStreamNotFound = errors.New("srt auth: stream not found")

	// ErrPassphraseRequired is returned when the resolver requires
	// encryption but the client did not provide a Stream ID, so there
	// is no stream key to resolve a passphrase for.
	ErrPassphraseRequired = errors.New("srt auth: passphrase required but stream ID missing")
)

// ValidatePassphrase checks that a passphrase meets SRT spec constraints
// (10–79 characters). These bounds come from the libsrt implementation:
// the minimum prevents trivially weak passphrases, and the maximum
// prevents buffer issues in the PBKDF2 key derivation step.
//
// Returns nil if the passphrase is valid, or an error describing
// which constraint was violated.
func ValidatePassphrase(passphrase string) error {
	// SRT spec minimum: 10 characters. Passphrases shorter than this
	// are rejected by libsrt during KMREQ processing.
	if len(passphrase) < 10 {
		return errors.New("srt passphrase too short: minimum 10 characters")
	}
	// SRT spec maximum: 79 characters. libsrt uses a fixed 80-byte
	// buffer (null-terminated), so the usable limit is 79.
	if len(passphrase) > 79 {
		return errors.New("srt passphrase too long: maximum 79 characters")
	}
	return nil
}
