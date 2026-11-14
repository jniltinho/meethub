// The LiveKit side of a call (meeting-room-ui spec): the camera preview before joining, the connection, one tile per
// camera and per shared screen, mute, camera, screen share, raised hand (a participant attribute), reactions (data
// messages on the "reaction" topic) and leaving. LiveKit objects are kept out of Vue's reactivity (markRaw): tiles
// are rebuilt from the room after each of its events.
import { markRaw, reactive, shallowRef } from 'vue'
import {
  createLocalTracks,
  DisconnectReason,
  type LocalTrack,
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
  speaking: boolean
  hand: boolean
  reaction: string
}

export type EndReason = 'left' | 'removed' | 'elsewhere' | 'lost'

export const call = reactive({
  phase: 'idle' as 'idle' | 'joining' | 'joined',
  mic: true,
  cam: true,
  sharing: false,
  hand: false,
})
export const tiles = shallowRef<Tile[]>([])
export const preview = shallowRef<LocalTrack[]>([])

let room: Room | null = null
let leaving = false
let audioBox: HTMLElement | null = null
let onEnd: ((why: EndReason) => void) | null = null
const reactions = new Map<string, { emoji: string; timer: ReturnType<typeof setTimeout> }>()

/** startPreview opens the camera and microphone for the pre-join screen; false when the browser has none or refuses */
export async function startPreview(): Promise<boolean> {
  stopPreview()
  for (const want of [
    { audio: true, video: true },
    { audio: true, video: false },
    { audio: false, video: true },
  ]) {
    try {
      preview.value = (await createLocalTracks(want)).map((t) => markRaw(t))
      call.mic = want.audio
      call.cam = want.video
      return true
    } catch {
      /* try with fewer devices */
    }
  }
  call.mic = false
  call.cam = false
  return false
}

export function stopPreview(): void {
  for (const t of preview.value) t.stop()
  preview.value = []
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
    const base = {
      identity: p.identity,
      local,
      muted: !mic || mic.isMuted,
      speaking: p.isSpeaking,
      hand: p.attributes.hand === '1',
      reaction: reactions.get(p.identity)?.emoji ?? '',
    }
    cams.push({
      ...base,
      key: p.identity,
      screen: false,
      track: cam?.track && !cam.isMuted ? markRaw(cam.track) : undefined,
    })
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
export async function join(url: string, token: string, box: HTMLElement, end: (why: EndReason) => void): Promise<void> {
  const mic = call.mic
  const cam = call.cam
  stopPreview()
  audioBox = box
  onEnd = end
  leaving = false
  call.phase = 'joining'
  const r = markRaw(new Room({ adaptiveStream: true, dynacast: true }))
  room = r
  for (const ev of [
    RoomEvent.ParticipantConnected,
    RoomEvent.ParticipantDisconnected,
    RoomEvent.TrackPublished,
    RoomEvent.TrackUnpublished,
    RoomEvent.TrackMuted,
    RoomEvent.TrackUnmuted,
    RoomEvent.LocalTrackPublished,
    RoomEvent.LocalTrackUnpublished,
    RoomEvent.ActiveSpeakersChanged,
    RoomEvent.ParticipantAttributesChanged,
  ])
    r.on(ev, refresh)
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
  })
  r.on(RoomEvent.DataReceived, (payload, participant, _kind, topic) => {
    if (topic === 'reaction' && participant) showReaction(participant.identity, new TextDecoder().decode(payload))
  })
  r.on(RoomEvent.Disconnected, (reason) => {
    const why: EndReason = leaving
      ? 'left'
      : reason === DisconnectReason.PARTICIPANT_REMOVED || reason === DisconnectReason.ROOM_DELETED
        ? 'removed'
        : reason === DisconnectReason.DUPLICATE_IDENTITY
          ? 'elsewhere'
          : 'lost'
    cleanup()
    onEnd?.(why)
  })
  try {
    await r.connect(url, token)
  } catch (e) {
    cleanup()
    if (leaving) return // the user left while connecting: not an error
    throw e
  }
  call.phase = 'joined'
  call.mic = await r.localParticipant.setMicrophoneEnabled(mic).then(
    () => mic,
    () => false,
  )
  call.cam = await r.localParticipant.setCameraEnabled(cam).then(
    () => cam,
    () => false,
  )
  refresh()
}

function cleanup(): void {
  room = null
  for (const r of reactions.values()) clearTimeout(r.timer)
  reactions.clear()
  if (audioBox) audioBox.replaceChildren()
  Object.assign(call, { phase: 'idle', sharing: false, hand: false })
  tiles.value = []
}

/** leave disconnects (stopping the camera, microphone and screen share); the end callback says 'left' */
export async function leave(): Promise<void> {
  stopPreview()
  if (!room) return
  leaving = true
  await room.disconnect()
}

export async function setMic(on: boolean): Promise<void> {
  if (!room) {
    for (const t of preview.value) if (t.kind === Track.Kind.Audio) await (on ? t.unmute() : t.mute())
    call.mic = on
    return
  }
  await room.localParticipant.setMicrophoneEnabled(on)
  call.mic = on
}

export async function setCam(on: boolean): Promise<void> {
  if (!room) {
    for (const t of preview.value) if (t.kind === Track.Kind.Video) await (on ? t.unmute() : t.mute())
    call.cam = on
    preview.value = [...preview.value]
    return
  }
  await room.localParticipant.setCameraEnabled(on)
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
