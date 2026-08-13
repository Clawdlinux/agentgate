/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"crypto/hmac"
	"crypto/sha256"
)

// purposeSignerV1 domain-separates the signer's storage key from the
// vault's token-encryption key, even though both derive from the same
// master secret.
const purposeSignerV1 = "agentgate.signer.v1"

// DerivePurposeKey returns a 32-byte AES-256 key derived from masterKey for
// the given purpose via HMAC-SHA256. This is a single-subkey KDF: HMAC-SHA256
// is a well-analyzed pseudorandom function, and using the purpose string as
// the MAC input over a high-entropy master key gives clean domain
// separation without adding a new dependency for one extraction.
func DerivePurposeKey(masterKey []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, masterKey)
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}
