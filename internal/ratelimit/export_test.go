package ratelimit

import "time"

// MemoryKeys reports how many keys the in-memory login limiter holds.
func MemoryKeys(l *Logins) int { return l.memory.size() }

// AllowAt runs the memory window with a given clock (deterministic tests).
func AllowAt(w *Window, key string, now time.Time) bool {
	ok, _ := w.memory.allow(w.Prefix+key, w.Limit, w.Per, now)
	return ok
}
