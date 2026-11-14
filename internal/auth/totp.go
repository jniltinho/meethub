package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238's default, the one every authenticator app supports (HMAC, not a digest)
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238 defaults: what every authenticator app expects).
const (
	totpPeriod = 30 // seconds
	totpDigits = 6
	totpSkew   = 1 // steps of tolerance either way
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns 160 random bits in base32, as authenticator apps take them.
func newTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return b32.EncodeToString(raw), nil
}

// totpCode is the RFC 4226 HOTP value of key at a time step, as 6 digits.
func totpCode(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// checkTOTP returns the time step code matches at now (within the skew) and
// later than lastStep (a code is accepted once), or false.
func checkTOTP(secret, code string, lastStep int64, now time.Time) (int64, bool) {
	key, err := b32.DecodeString(secret)
	if err != nil || len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	found := int64(0)
	for step := cur - totpSkew; step <= cur+totpSkew; step++ { // every step is computed: no early exit
		if subtle.ConstantTimeCompare([]byte(totpCode(key, step)), []byte(code)) == 1 && step > lastStep && found == 0 {
			found = step
		}
	}
	return found, found != 0
}

// otpauthURL is the key URI authenticator apps scan from the QR code.
func otpauthURL(issuer, account, secret string) string {
	v := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {strconv.Itoa(totpDigits)}, "period": {strconv.Itoa(totpPeriod)}}
	// %20, not +, for spaces: some apps show a literal +; a + of the values is already %2B
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + strings.ReplaceAll(v.Encode(), "+", "%20")
}

// Recovery codes: 10 characters from an alphabet without look-alikes, shown as XXXXX-XXXXX.
const (
	recoveryCodes    = 10
	recoveryLen      = 10
	recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // 31 symbols: ~49.5 bits a code
	// recoveryPrefix separates recovery-code hashes from password hashes (and meets the hasher's length policy).
	recoveryPrefix = "meethub-recovery-code:"
)

func newRecoveryCode() (string, error) {
	raw := make([]byte, recoveryLen)
	out := make([]byte, 0, recoveryLen)
	for len(out) < recoveryLen {
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		for _, b := range raw {
			if int(b) < 248 && len(out) < recoveryLen { // 248 = 8×31: no modulo bias
				out = append(out, recoveryAlphabet[int(b)%len(recoveryAlphabet)])
			}
		}
	}
	return string(out[:5]) + "-" + string(out[5:]), nil
}

// normalizeRecovery lower-cases a typed recovery code and drops spaces and dashes.
func normalizeRecovery(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(s)))
}

// ErrSealed is a sealed secret that does not open with the key (another key, or tampered data).
var ErrSealed = errors.New("the stored secret cannot be decrypted")

// seal encrypts plain with AES-256-GCM under key, a random nonce and userID as associated data; the result is
// base64(nonce || ciphertext).
func seal(key []byte, userID, plain string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), []byte(userID))), nil
}

func unseal(key []byte, userID, sealed string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(b) < g.NonceSize() {
		return "", ErrSealed
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(userID))
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
