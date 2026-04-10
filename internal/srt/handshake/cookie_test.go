package handshake

import (
	"net"
	"testing"
)

// TestCookieGenerateNonZero verifies that generated cookies are non-zero.
// A zero cookie would be indistinguishable from "no cookie" in the protocol.
func TestCookieGenerateNonZero(t *testing.T) {
	gen := NewCookieGenerator()
	addr := &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345}

	cookie := gen.Generate(addr)
	if cookie == 0 {
		t.Error("Generate returned zero cookie; expected non-zero")
	}
}

// TestCookieSameAddressSameBucket verifies that the same address produces
// the same cookie within the same time bucket (deterministic behavior).
func TestCookieSameAddressSameBucket(t *testing.T) {
	gen := NewCookieGenerator()
	addr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}

	cookie1 := gen.Generate(addr)
	cookie2 := gen.Generate(addr)

	if cookie1 != cookie2 {
		t.Errorf("same address produced different cookies: %d vs %d", cookie1, cookie2)
	}
}

// TestCookieDifferentAddresses verifies that different remote addresses
// produce different cookies, preventing address spoofing attacks.
func TestCookieDifferentAddresses(t *testing.T) {
	gen := NewCookieGenerator()
	addr1 := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}
	addr2 := &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 5000}
	addr3 := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5001}

	cookie1 := gen.Generate(addr1)
	cookie2 := gen.Generate(addr2)
	cookie3 := gen.Generate(addr3)

	// Different IPs should give different cookies.
	if cookie1 == cookie2 {
		t.Errorf("different IPs produced same cookie: %d", cookie1)
	}

	// Different ports should give different cookies.
	if cookie1 == cookie3 {
		t.Errorf("different ports produced same cookie: %d", cookie1)
	}
}

// TestCookieValidateCurrentBucket verifies that a freshly generated cookie
// passes validation (current time bucket check).
func TestCookieValidateCurrentBucket(t *testing.T) {
	gen := NewCookieGenerator()
	addr := &net.UDPAddr{IP: net.ParseIP("172.16.0.5"), Port: 8000}

	cookie := gen.Generate(addr)
	if !gen.Validate(cookie, addr) {
		t.Error("Validate failed for freshly generated cookie")
	}
}

// TestCookieValidateWrongAddress verifies that a cookie generated for one
// address fails validation when checked against a different address.
func TestCookieValidateWrongAddress(t *testing.T) {
	gen := NewCookieGenerator()
	addr1 := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}
	addr2 := &net.UDPAddr{IP: net.ParseIP("10.0.0.99"), Port: 5000}

	cookie := gen.Generate(addr1)

	// The cookie should NOT validate against a different address.
	if gen.Validate(cookie, addr2) {
		t.Error("Validate succeeded for wrong address; expected failure")
	}
}

// TestCookieValidateWrongValue verifies that an arbitrary cookie value
// fails validation even for the correct address.
func TestCookieValidateWrongValue(t *testing.T) {
	gen := NewCookieGenerator()
	addr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}

	// Try validating a made-up cookie value.
	if gen.Validate(0xDEADBEEF, addr) {
		t.Error("Validate succeeded for arbitrary cookie value; expected failure")
	}
}

// TestCookieDifferentGenerators verifies that two generators with different
// secrets produce different cookies for the same address, ensuring the
// secret is actually used in the computation.
func TestCookieDifferentGenerators(t *testing.T) {
	gen1 := NewCookieGenerator()
	gen2 := NewCookieGenerator()
	addr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}

	cookie1 := gen1.Generate(addr)
	cookie2 := gen2.Generate(addr)

	// Different secrets should produce different cookies (with high probability).
	// There's a 1/2^32 chance of a false positive, which is negligible.
	if cookie1 == cookie2 {
		t.Errorf("different generators produced same cookie: %d (extremely unlikely)", cookie1)
	}

	// A cookie from gen1 should not validate with gen2.
	if gen2.Validate(cookie1, addr) {
		t.Error("cookie from gen1 validated with gen2; expected failure")
	}
}
