// durationNanos parses a Go duration ("30m", "1h30m", "168h") as the server does; null when it is not one.
const units: Record<string, number> = { ns: 1, us: 1e3, µs: 1e3, μs: 1e3, ms: 1e6, s: 1e9, m: 6e10, h: 3.6e12 }

export function durationNanos(s: string): number | null {
  if (!/^(\d+(\.\d+)?(ns|us|µs|μs|ms|s|m|h))+$/.test(s)) return null
  let total = 0
  for (const m of s.matchAll(/(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g)) total += Number(m[1]) * units[m[2]!]!
  return total
}
