import { describe, expect, it } from 'vitest'
import { durationNanos } from '@/views/settings'

describe('durationNanos', () => {
  it('reads Go durations as the server does', () => {
    expect(durationNanos('30m')).toBe(30 * 6e10)
    expect(durationNanos('168h')).toBe(168 * 3.6e12)
    expect(durationNanos('1h30m')).toBe(5.4e12)
    expect(durationNanos('1.5h')).toBe(5.4e12)
    expect(durationNanos('500ms')).toBe(5e8)
  })
  it('refuses what ParseDuration refuses', () => {
    for (const s of ['', '7d', '10', 'h', '1 h', '-1h', '1h 30m']) expect(durationNanos(s)).toBeNull()
  })
})
