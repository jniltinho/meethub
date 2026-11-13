// The chat's WebSocket client (design D9): reconnects with backoff (1 s → 30 s, jitter), detects lost events through
// `seq` gaps, and asks for a resynchronization through the API after `resync`, a gap, a reconnection and every 60
// seconds while the page is visible. The socket is not a durable queue: the API is the source of truth.
import type { ServerEvent } from '@/chat/types'

export type SocketState = 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface SocketOptions {
  url?: string
  /** a new event, in order */
  onEvent: (ev: ServerEvent) => void
  /** events may have been missed: reload from the API */
  onResync: (reason: 'resync' | 'gap' | 'reconnect' | 'interval') => void
  onState?: (state: SocketState) => void
  /** test seams */
  createSocket?: (url: string) => WebSocket
  random?: () => number
  resyncEvery?: number
  isVisible?: () => boolean
}

interface Pending {
  resolve: (data: unknown) => void
  reject: (err: Error) => void
  timer: ReturnType<typeof setTimeout>
}

export const BACKOFF_MIN = 1000
export const BACKOFF_MAX = 30000
const ACTION_TIMEOUT = 10000

export class ChatSocket {
  private ws: WebSocket | null = null
  private state: SocketState = 'closed'
  private stopped = true
  private attempt = 0
  private lastSeq = -1
  private everOpened = false
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private resyncTimer: ReturnType<typeof setInterval> | null = null
  private actionSeq = 0
  private pending = new Map<number, Pending>()

  constructor(private readonly opts: SocketOptions) {}

  get status(): SocketState {
    return this.state
  }

  start(): void {
    if (!this.stopped) return
    this.stopped = false
    this.open()
    const every = this.opts.resyncEvery ?? 60000
    this.resyncTimer = setInterval(() => {
      const visible = this.opts.isVisible ? this.opts.isVisible() : document.visibilityState === 'visible'
      if (this.state === 'open' && visible) this.opts.onResync('interval')
    }, every)
  }

  /** stop closes the socket for good (logout, session end) and fails pending actions. */
  stop(): void {
    this.stopped = true
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    if (this.resyncTimer) clearInterval(this.resyncTimer)
    this.reconnectTimer = this.resyncTimer = null
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null
      ws.close(1000)
    }
    this.failPending('closed')
    this.setState('closed')
  }

  /** send an action and wait for its response data (rejects on FAIL, timeout or disconnect). */
  send(action: string, data?: unknown): Promise<unknown> {
    if (!this.ws || this.state !== 'open') return Promise.reject(new Error('offline'))
    const seq = ++this.actionSeq
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(seq)
        reject(new Error('timeout'))
      }, ACTION_TIMEOUT)
      this.pending.set(seq, { resolve, reject, timer })
      this.ws!.send(JSON.stringify({ action, seq, data: data ?? {} }))
    })
  }

  /** backoff returns the delay before reconnect attempt n (1-based): exponential with full jitter in its upper half. */
  backoff(n: number): number {
    const base = Math.min(BACKOFF_MAX, BACKOFF_MIN * 2 ** (n - 1))
    const r = this.opts.random ? this.opts.random() : Math.random()
    return Math.round(base / 2 + (base / 2) * r)
  }

  private url(): string {
    if (this.opts.url) return this.opts.url
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}/api/v1/websocket`
  }

  private open(): void {
    this.setState(this.everOpened ? 'reconnecting' : 'connecting')
    const ws = this.opts.createSocket ? this.opts.createSocket(this.url()) : new WebSocket(this.url())
    this.ws = ws
    ws.onopen = () => {
      this.attempt = 0
      this.lastSeq = -1
    }
    ws.onmessage = (m: MessageEvent) => this.receive(String(m.data))
    ws.onclose = () => {
      if (this.ws !== ws) return
      this.ws = null
      this.failPending('disconnected')
      if (this.stopped) return
      this.setState('reconnecting')
      this.attempt++
      this.reconnectTimer = setTimeout(() => this.open(), this.backoff(this.attempt))
    }
    ws.onerror = () => {
      /* onclose follows */
    }
  }

  private receive(raw: string): void {
    let msg: Record<string, unknown>
    try {
      msg = JSON.parse(raw) as Record<string, unknown>
    } catch {
      return
    }
    if (typeof msg.seq_reply === 'number') {
      const p = this.pending.get(msg.seq_reply)
      if (!p) return
      this.pending.delete(msg.seq_reply)
      clearTimeout(p.timer)
      if (msg.status === 'OK') p.resolve(msg.data)
      else p.reject(new Error(String(msg.error ?? 'failed')))
      return
    }
    const ev = msg as unknown as ServerEvent
    if (typeof ev.event !== 'string' || typeof ev.seq !== 'number') return
    if (ev.event === 'hello') {
      this.lastSeq = ev.seq
      this.setState('open')
      if (this.everOpened) this.opts.onResync('reconnect')
      this.everOpened = true
      return
    }
    const gap = this.lastSeq >= 0 && ev.seq !== this.lastSeq + 1
    this.lastSeq = ev.seq
    if (ev.event === 'resync') {
      this.opts.onResync('resync')
      return
    }
    this.opts.onEvent(ev)
    if (gap) this.opts.onResync('gap') // an event was lost: the API fills the hole
  }

  private failPending(reason: string): void {
    for (const p of this.pending.values()) {
      clearTimeout(p.timer)
      p.reject(new Error(reason))
    }
    this.pending.clear()
  }

  private setState(s: SocketState): void {
    if (this.state === s) return
    this.state = s
    this.opts.onState?.(s)
  }
}
