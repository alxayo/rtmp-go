package srtauth

// StaticResolver returns the same passphrase for every stream.
// This preserves backward compatibility with the single -srt-passphrase
// flag — all connections share one passphrase regardless of stream key.
//
// Use this resolver when you don't need per-stream passphrases but still
// want encrypted SRT connections.
//
// StaticResolver is safe for concurrent use; the passphrase is immutable
// after construction.
type StaticResolver struct {
	// passphrase is the shared secret used for all SRT streams.
	// Set once during construction and never modified afterward.
	passphrase string
}

// NewStaticResolver creates a resolver that returns the given passphrase
// for all streams. The passphrase is validated eagerly — if it violates
// SRT constraints (10–79 characters), an error is returned immediately
// so misconfiguration is caught at server startup, not at connection time.
func NewStaticResolver(passphrase string) (*StaticResolver, error) {
	if err := ValidatePassphrase(passphrase); err != nil {
		return nil, err
	}
	return &StaticResolver{passphrase: passphrase}, nil
}

// ResolvePassphrase returns the static passphrase regardless of stream key.
// The stream key parameter is ignored — every stream gets the same passphrase.
func (r *StaticResolver) ResolvePassphrase(_ string) (string, error) {
	return r.passphrase, nil
}

// EncryptionRequired returns true — a StaticResolver always requires
// encryption because it was constructed with an explicit passphrase.
func (r *StaticResolver) EncryptionRequired() bool {
	return true
}
