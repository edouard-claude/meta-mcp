// Package app holds the use cases of the server: the login flow and one file
// per MCP tool. It depends on the domain ports only, never on HTTP, SQL or
// the Graph API.
package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

// newTenantID returns a random UUID v4, the primary key of a tenant.
func newTenantID() string {
	var b [16]byte
	// crypto/rand.Read never fails on the platforms this binary targets; it
	// panics internally rather than returning a short read.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}

// newSecret returns 32 bytes of randomness, base64url encoded. It backs the
// reconnection state, which must be unguessable.
func newSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
