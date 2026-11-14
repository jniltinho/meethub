// The LiveKit side of a call (meeting-room-ui spec): the preview before joining (camera, microphone level, device
// pickers), the connection, one tile per camera and per shared screen, mute, camera, screen share, raised hand (a
// participant attribute), reactions (data messages on the "reaction" topic), reconnection and leaving. LiveKit objects
// are kept out of Vue's reactivity (markRaw): tiles are rebuilt from the room after each of its events.
import { markRaw, reactive, ref, shallowRef } from 'vue'
import {
  createLocalTracks,
  DisconnectReason,
  type LocalAudioTrack,
  type LocalTrack,
  type LocalVideoTrack,
  type Participant,
  type RemoteTrack,
  Room,
  RoomEvent,
  Track,
} from 'livekit-client'

/** the reactions anyone can send; others received are dropped */
export const REACTIONS = ['👍', '👏', '😂', '🎉', '❤️'] as const
const REACTION_MS = 3000

export interface Tile {
  key: string
  identity: string
  local: boolean
  screen: boolean
  /** the video to show; none: the person's picture */
  track?: Track
  muted: boolean
  camOff: boolean
  speaking: boolean
  hand: boolean
  reaction: string
}

export type EndReason = 'left' | 'removed' | 'elsewhere' | 'lost'
/** what the preview found: devices, the browser refused them, or there are none */
export type DeviceState = 'checking' | 'ok' | 'denied' | 'none'

export interface CallHooks {
  end: (why: EndReason) => void
  /** someone else joined (true) or left (false) after one's own join */
  peer?: (identity: string, joined: boolean) => void
  /** the connection is being restored (true) or is back (false) */
  reconnecting?: (on: boolean) => void
}

export const call = reactive({
  phase: 'idle' as 'idle' | 'joining' | 'joined',
  mic: true,
  cam: true,
  sharing: false,
  hand: false,
  reconnecting: false,
  /** chosen devices ('' = the browser's default) */
  micId: '',
  camId: '',
})
export const tiles = shallowRef<Tile[]>([])
export const preview = shallowRef<LocalTrack[]>([])
export const devices = shallowRef<{ audio: MediaDeviceInfo[]; video: MediaDeviceInfo[] }>({ audio: [], video: [] })
/** the preview microphone's level, 0 to 1 */
export const micLevel = ref(0)

let room: Room | null = null
let leaving = false
/** grow when a preview or a join is abandoned: a camera or a connection that arrives afterwards is closed at once */
let previewGen = 0
let joinGen = 0
let audioBox: HTMLElement | null = null
let hooks: CallHooks | null = null
const reactions = new Map<string, { emoji: string; timer: ReturnType<typeof setTimeout> }>()
let meter: { ctx: AudioContext; timer: ReturnType<typeof setInterval> } | null = null

/** startPreview opens the camera and microphone for the pre-join screen */
export async function startPreview(): Promise<DeviceState> {
  stopPreview()
  const g = previewGen
  let refused = false
  for (const want of [
    { audio: true, video: true },
    { audio: true, video: false },
    { audio: false, video: true },
  ]) {
    try {
      const tracks = await createLocalTracks({
        audio: want.audio && (call.micId ? { deviceId: call.micId } : true),
        video: want.video && (call.camId ? { deviceId: call.camId } : true),
      })
      if (g !== previewGen) {
        for (const t of tracks) t.stop() // the pre-join screen closed meanwhile
        return 'none'
      }
      preview.value = tracks.map((t) => markRaw(t))
      call.mic = want.audio
      call.cam = want.video
      startMeter()
      await listDevices()
      return 'ok'
    } catch (e) {
      if (g !== previewGen) return 'none' // abandoned: no more capture attempts
      refused ||= e instanceof Error && (e.name === 'NotAllowedError' || e.name === 'SecurityError')
    }
  }
  call.mic = false
  call.cam = false
  return refused ? 'denied' : 'none'
}

export function stopPreview(): void {
  previewGen++
  stopMeter()
  for (const t of preview.value) t.stop()
  preview.value = []
}

async function listDevices(): Promise<void> {
  const [audio, video] = await Promise.all([
    Room.getLocalDevices('audioinput', false).catch(() => []),
    Room.getLocalDevices('videoinput', false).catch(() => []),
  ])
  devices.value = { audio, video }
}

/** chooseDevice switches the preview to another microphone or camera, also used when joining */
export async function chooseDevice(kind: 'audio' | 'video', deviceId: string): Promise<void> {
  const keep = (id: string) => (kind === 'audio' ? (call.micId = id) : (call.camId = id))
  const previous = kind === 'audio' ? call.micId : call.camId
  const track = preview.value.find((t) => t.kind === (kind === 'audio' ? Track.Kind.Audio : Track.Kind.Video)) as
    | LocalAudioTrack
    | LocalVideoTrack
    | undefined
  if (!track) {
    keep(deviceId)
    return
  }
  try {
    await track.restartTrack({ deviceId: deviceId || undefined })
    keep(deviceId)
  } catch (e) {
    // back to the device that worked (restartTrack stopped it before failing)
    await track.restartTrack({ deviceId: previous || undefined }).catch(() => undefined)
    preview.value = [...preview.value]
    throw e
  }
  if (kind === 'audio') startMeter()
  preview.value = [...preview.value]
}

function startMeter(): void {
  stopMeter()
  const t = preview.value.find((x) => x.kind === Track.Kind.Audio)
  if (!t || typeof AudioContext === 'undefined') return
  const ctx = new AudioContext()
  const analyser = ctx.createAnalyser()
  analyser.fftSize = 512
  ctx.createMediaStreamSource(new MediaStream([t.mediaStreamTrack])).connect(analyser)
  // the graph only runs when it reaches the output: through a silent gain, so nothing is heard
  const silent = ctx.createGain()
  silent.gain.value = 0
  analyser.connect(silent).connect(ctx.destination)
  void ctx.resume().catch(() => undefined) // created after an await: the click's activation still allows it
  const data = new Float32Array(analyser.fftSize)
  const timer = setInterval(() => {
    analyser.getFloatTimeDomainData(data)
    let sum = 0
    for (const v of data) sum += v * v
    // decibels from -60 (silence) to 0 dBFS, falling back slowly as meters do (and as Meet's does)
    const db = 20 * Math.log10(Math.sqrt(sum / data.length) || 1e-9)
    const now = Math.min(1, Math.max(0, (db + 60) / 60))
    micLevel.value = call.mic ? Math.max(now, micLevel.value * 0.85) : 0
  }, 100)
  meter = { ctx, timer }
}

function stopMeter(): void {
  if (!meter) return
  clearInterval(meter.timer)
  void meter.ctx.close().catch(() => undefined)
  meter = null
  micLevel.value = 0
}

function refresh(): void {
  if (!room) {
    tiles.value = []
    return
  }
  const people: Participant[] = [room.localParticipant, ...room.remoteParticipants.values()]
  const screens: Tile[] = []
  const cams: Tile[] = []
  for (const p of people) {
    const local = p === room.localParticipant
    const mic = p.getTrackPublication(Track.Source.Microphone)
    const cam = p.getTrackPublication(Track.Source.Camera)
    const screen = p.getTrackPublication(Track.Source.ScreenShare)
    const video = cam?.track && !cam.isMuted ? markRaw(cam.track) : undefined
    const base = {
      identity: p.identity,
      local,
      muted: !mic || mic.isMuted,
      camOff: !video,
      speaking: p.isSpeaking,
      hand: p.attributes.hand === '1',
      reaction: reactions.get(p.identity)?.emoji ?? '',
    }
    cams.push({ ...base, key: p.identity, screen: false, track: video })
    if (screen?.track)
      screens.push({ ...base, key: p.identity + ':screen', screen: true, track: markRaw(screen.track) })
  }
  tiles.value = [...screens, ...cams]
}

function showReaction(identity: string, emoji: string): void {
  if (!(REACTIONS as readonly string[]).includes(emoji)) return
  const prev = reactions.get(identity)
  if (prev) clearTimeout(prev.timer)
  reactions.set(identity, {
    emoji,
    timer: setTimeout(() => {
      reactions.delete(identity)
      refresh()
    }, REACTION_MS),
  })
  refresh()
}

function attachAudio(track: RemoteTrack): void {
  if (track.kind !== Track.Kind.Audio || !audioBox) return
  audioBox.append(track.attach())
}

/** join connects to the room, then turns on the devices the pre-join screen left on */
export async function join(url: string, token: string, box: HTMLElement, h: CallHooks): Promise<void> {
  const mic = call.mic
  const cam = call.cam
  const g = joinGen
  audioBox = box
  hooks = h
  leaving = false
  call.phase = 'joining'
  const r = markRaw(new Room({ adaptiveStream: true, dynacast: true }))
  room = r
  for (const ev of [
    RoomEvent.TrackPublished,
    RoomEvent.TrackUnpublished,
    RoomEvent.TrackMuted,
    RoomEvent.TrackUnmuted,
    RoomEvent.LocalTrackPublished,
    RoomEvent.ActiveSpeakersChanged,
    RoomEvent.ParticipantAttributesChanged,
  ])
    r.on(ev, refresh)
  r.on(RoomEvent.ParticipantConnected, (p) => {
    refresh()
    if (call.phase === 'joined') hooks?.peer?.(p.identity, true)
  })
  r.on(RoomEvent.ParticipantDisconnected, (p) => {
    refresh()
    if (call.phase === 'joined') hooks?.peer?.(p.identity, false)
  })
  r.on(RoomEvent.TrackSubscribed, (track) => {
    attachAudio(track)
    refresh()
  })
  r.on(RoomEvent.TrackUnsubscribed, (track) => {
    for (const el of track.detach()) el.remove()
    refresh()
  })
  r.on(RoomEvent.LocalTrackUnpublished, (pub) => {
    if (pub.source === Track.Source.ScreenShare) call.sharing = false
    refresh()
  })
  r.on(RoomEvent.Reconnecting, () => {
    call.reconnecting = true
    hooks?.reconnecting?.(true)
  })
  r.on(RoomEvent.Reconnected, () => {
    call.reconnecting = false
    hooks?.reconnecting?.(false)
    refresh()
  })
  r.on(RoomEvent.DataReceived, (payload, participant, _kind, topic) => {
    if (topic === 'reaction' && participant) showReaction(participant.identity, new TextDecoder().decode(payload))
  })
  let connected = false
  r.on(RoomEvent.Disconnected, (reason) => {
    if (!connected) return // a failed connection: join() rejects and the pre-join screen says why
    const why: EndReason = leaving
      ? 'left'
      : reason === DisconnectReason.PARTICIPANT_REMOVED || reason === DisconnectReason.ROOM_DELETED
        ? 'removed'
        : reason === DisconnectReason.DUPLICATE_IDENTITY
          ? 'elsewhere'
          : 'lost'
    const end = hooks?.end
    cleanup()
    end?.(why)
  })
  try {
    await r.connect(url, token)
    connected = true
  } catch (e) {
    cleanup()
    if (leaving) return // the user left while connecting: not an error
    throw e
  }
  if (g !== joinGen) {
    leaving = true // abandoned while connecting
    await r.disconnect()
    return
  }
  stopPreview() // the preview stays until the room is reached: a failed join keeps the pre-join screen as it was
  // each device opens only while the join still stands; one that arrives after an abandon is stopped at once
  const abandoned = () => {
    if (g === joinGen) return false
    for (const p of r.localParticipant.trackPublications.values()) p.track?.stop()
    return true
  }
  const micOn = await r.localParticipant
    .setMicrophoneEnabled(mic, call.micId ? { deviceId: call.micId } : undefined)
    .then(
      () => mic,
      () => false,
    )
  if (abandoned()) return
  call.mic = micOn
  const camOn = await r.localParticipant.setCameraEnabled(cam, call.camId ? { deviceId: call.camId } : undefined).then(
    () => cam,
    () => false,
  )
  if (abandoned()) return
  call.cam = camOn
  call.phase = 'joined'
  refresh()
}

function cleanup(): void {
  room = null
  hooks = null
  for (const r of reactions.values()) clearTimeout(r.timer)
  reactions.clear()
  if (audioBox) audioBox.replaceChildren()
  Object.assign(call, { phase: 'idle', sharing: false, hand: false, reconnecting: false })
  tiles.value = []
}

/** abort abandons whatever is in progress: the preview, a pending join and the call */
export function abort(): void {
  joinGen++
  void leave()
}

/** leave disconnects (stopping the camera, microphone and screen share); the end hook says 'left' */
export async function leave(): Promise<void> {
  stopPreview()
  if (!room) return
  leaving = true
  await room.disconnect()
}

export async function setMic(on: boolean): Promise<void> {
  if (!room || call.phase !== 'joined') {
    for (const t of preview.value) if (t.kind === Track.Kind.Audio) await (on ? t.unmute() : t.mute())
    call.mic = on
    return
  }
  await room.localParticipant.setMicrophoneEnabled(on, call.micId ? { deviceId: call.micId } : undefined)
  call.mic = on
}

export async function setCam(on: boolean): Promise<void> {
  if (!room || call.phase !== 'joined') {
    for (const t of preview.value) if (t.kind === Track.Kind.Video) await (on ? t.unmute() : t.mute())
    call.cam = on
    preview.value = [...preview.value]
    return
  }
  await room.localParticipant.setCameraEnabled(on, call.camId ? { deviceId: call.camId } : undefined)
  call.cam = on
}

export async function setSharing(on: boolean): Promise<void> {
  if (!room) return
  await room.localParticipant.setScreenShareEnabled(on, { audio: false })
  call.sharing = on
}

export async function setHand(up: boolean): Promise<void> {
  if (!room) return
  await room.localParticipant.setAttributes({ hand: up ? '1' : '' })
  call.hand = up
}

export async function react(emoji: string): Promise<void> {
  if (!room) return
  await room.localParticipant.publishData(new TextEncoder().encode(emoji), { reliable: true, topic: 'reaction' })
  showReaction(room.localParticipant.identity, emoji)
}

/** the preview's camera track, if on */
export function previewVideo(): LocalTrack | undefined {
  return call.cam ? preview.value.find((t) => t.kind === Track.Kind.Video) : undefined
}
