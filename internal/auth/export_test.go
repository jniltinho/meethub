package auth

import "time"

// TOTPCodeAt is the code an authenticator app shows for secret at t.
func TOTPCodeAt(secret string, t time.Time) string {
	key, _ := b32.DecodeString(secret)
	return totpCode(key, t.Unix()/totpPeriod)
}
