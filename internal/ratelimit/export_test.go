package ratelimit

// MemoryKeys reports how many keys the in-memory login limiter holds.
func MemoryKeys(l *Logins) int { return l.memory.size() }
