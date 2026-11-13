package auth

// MemoryKeys reports how many keys the in-memory limiter holds.
func MemoryKeys(l *Limiter) int { return l.memory.size() }
