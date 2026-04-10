package handshake

// This file implements SYN cookie generation and validation for SRT.
//
// SYN cookies prevent connection-state exhaustion attacks (like SYN floods
// in TCP). Instead of storing state for every incoming handshake, the server
// computes a cookie from the client's address and a time bucket. The client
// must echo it back in the Conclusion phase, proving it can receive on its
// claimed address.
//
// Cookie = HMAC-SHA1(secret, remoteIP || remotePort || timeBucket) truncated to 32 bits.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"net"
	"sync"
	"time"
)

// cookieBucketSeconds defines the time window (in seconds) during which a
// cookie remains valid. Cookies from the current bucket and the previous
// bucket are accepted, giving a total validity window of up to 2x this value.
const cookieBucketSeconds = 30

// CookieGenerator creates and validates SYN cookies using HMAC-SHA1.
// It holds a random secret that is generated once at startup and used
// for all cookie computations during the server's lifetime.
type CookieGenerator struct {
	// mu protects the secret from concurrent reads/writes, though in
	// practice the secret is only written once during construction.
	mu sync.RWMutex

	// secret is a 32-byte random key used as the HMAC secret.
	// Generated once at startup and never changed.
	secret []byte
}

// NewCookieGenerator creates a new cookie generator with a cryptographically
// random 32-byte secret. This secret is used to compute HMAC-based cookies
// that are unforgeable without knowing the secret.
func NewCookieGenerator() *CookieGenerator {
	// Generate a random 32-byte secret for HMAC computation.
	secret := make([]byte, 32)
	rand.Read(secret)
	return &CookieGenerator{secret: secret}
}

// Generate creates a SYN cookie for the given remote UDP address.
// The cookie is derived from the client's IP, port, and the current
// time bucket, so different clients (or the same client at different
// times) will get different cookies.
func (g *CookieGenerator) Generate(addr *net.UDPAddr) uint32 {
	// Compute the current time bucket by dividing Unix time by the
	// bucket size. All requests within the same bucket get the same cookie.
	bucket := time.Now().Unix() / cookieBucketSeconds
	return g.computeCookie(addr, bucket)
}

// Validate checks if a cookie is valid for the given address.
// It checks both the current and previous time bucket to handle the case
// where the cookie was generated just before a bucket boundary and the
// validation happens just after. This gives the client up to 60 seconds
// (2 * cookieBucketSeconds) to respond.
func (g *CookieGenerator) Validate(cookie uint32, addr *net.UDPAddr) bool {
	now := time.Now().Unix()
	currentBucket := now / cookieBucketSeconds

	// Check the current time bucket first (most common case).
	if g.computeCookie(addr, currentBucket) == cookie {
		return true
	}

	// Check the previous time bucket to handle clock skew at boundaries.
	// For example, if the cookie was generated at second 29 (bucket 0)
	// and validated at second 31 (bucket 1), we still accept it.
	if g.computeCookie(addr, currentBucket-1) == cookie {
		return true
	}

	return false
}

// computeCookie creates a cookie for a specific time bucket using HMAC-SHA1.
// The input to the HMAC is: remoteIP || remotePort || timeBucket.
// The output is truncated to 32 bits (4 bytes) since SRT cookies are uint32.
func (g *CookieGenerator) computeCookie(addr *net.UDPAddr, bucket int64) uint32 {
	g.mu.RLock()
	secret := g.secret
	g.mu.RUnlock()

	// Create a new HMAC-SHA1 hasher with our secret key.
	mac := hmac.New(sha1.New, secret)

	// Feed the client's IP address into the HMAC. This ensures different
	// IPs get different cookies, preventing IP spoofing attacks.
	mac.Write(addr.IP)

	// Feed the client's port as a 2-byte big-endian value. This ensures
	// different source ports (even from the same IP) get different cookies.
	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(addr.Port))
	mac.Write(portBuf)

	// Feed the time bucket as an 8-byte big-endian value. This makes
	// cookies expire after the bucket window passes.
	bucketBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(bucketBuf, uint64(bucket))
	mac.Write(bucketBuf)

	// Compute the HMAC digest (20 bytes for SHA1) and take the first
	// 4 bytes as our 32-bit cookie value.
	sum := mac.Sum(nil)
	return binary.BigEndian.Uint32(sum[:4])
}
