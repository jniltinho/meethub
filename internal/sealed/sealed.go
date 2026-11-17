// Package sealed encrypts the secrets MeetHub keeps in its database (authenticator secrets, the AI keys entered in
// Administration) under MEETHUB_SECRET_KEY.
package sealed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrSealed is a sealed secret that does not open with the key (another key, or tampered data).
var ErrSealed = errors.New("the stored secret cannot be decrypted")

// Seal encrypts plain with AES-256-GCM under key, a random nonce and bound as associated data (what the secret
// belongs to: a user id, a column); the result is base64(nonce || ciphertext).
func Seal(key []byte, bound, plain string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), []byte(bound))), nil
}

// Open decrypts what Seal made for the same key and bound; anything else is ErrSealed.
func Open(key []byte, bound, sealed string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(b) < g.NonceSize() {
		return "", ErrSealed
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(bound))
	if err != nil {
		return "", ErrSealed
	}
	return string(plain), nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return cipher.NewGCM(block)
}
