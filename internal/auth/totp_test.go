package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B (SHA-1), the last 6 of its 8 digits: HOTP truncates modulo 10^digits.
func TestTOTPVectors(t *testing.T) {
	t.Parallel()
	key := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		code string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"},
	} {
		if got := totpCode(key, v.unix/totpPeriod); got != v.code {
			t.Errorf("T=%d: %s, want %s", v.unix, got, v.code)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	t.Parallel()
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(1111111111, 0)
	step := now.Unix() / totpPeriod
	tests := []struct {
		name     string
		code     string
		lastStep int64
		ok       bool
	}{
		{"current", "050471", 0, true},
		{"previous step", TOTPCodeAt(secret, now.Add(-30*time.Second)), 0, true},
		{"next step", TOTPCodeAt(secret, now.Add(30*time.Second)), 0, true},
		{"two steps old", TOTPCodeAt(secret, now.Add(-60*time.Second)), 0, false},
		{"replayed", "050471", step, false},
		{"wrong", "000000", 0, false},
		{"short", "05047", 0, false},
	}
	for _, tt := range tests {
		if _, ok := checkTOTP(secret, tt.code, tt.lastStep, now); ok != tt.ok {
			t.Errorf("%s: ok=%v", tt.name, ok)
		}
	}
}

func TestSeal(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := seal(key, "user-a", "JBSWY3DPEHPK3PXP")
	if err != nil || strings.Contains(sealed, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("seal: %q %v", sealed, err)
	}
	if got, err := unseal(key, "user-a", sealed); err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if again, _ := seal(key, "user-a", "JBSWY3DPEHPK3PXP"); again == sealed {
		t.Fatal("the nonce is not random")
	}
	tampered := []byte(sealed)
	tampered[len(tampered)/2] ^= 1
	for name, c := range map[string]func() (string, error){
		"wrong key":     func() (string, error) { return unseal(bytes.Repeat([]byte{8}, 32), "user-a", sealed) },
		"another user":  func() (string, error) { return unseal(key, "user-b", sealed) },
		"tampered data": func() (string, error) { return unseal(key, "user-a", string(tampered)) },
		"not base64":    func() (string, error) { return unseal(key, "user-a", "%%%") },
	} {
		if _, err := c(); err == nil {
			t.Errorf("%s: opened", name)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 200 {
		c, err := newRecoveryCode()
		if err != nil || len(c) != 11 || c[5] != '-' || len(normalizeRecovery(c)) != recoveryLen {
			t.Fatalf("code %q %v", c, err)
		}
		if seen[c] {
			t.Fatalf("repeated %q", c)
		}
		seen[c] = true
	}
	if normalizeRecovery(" ABCDE-fghjk ") != "abcdefghjk" {
		t.Fatal("normalization")
	}
}

func TestOTPAuthURL(t *testing.T) {
	t.Parallel()
	u := otpauthURL("Acme & Co", "ana@acme.io", "ABC")
	if !strings.HasPrefix(u, "otpauth://totp/Acme%20&%20Co:ana@acme.io?") || !strings.Contains(u, "issuer=Acme%20%26%20Co") ||
		!strings.Contains(u, "secret=ABC") {
		t.Fatalf("uri %q", u)
	}
}
