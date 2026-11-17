import { t } from '@/i18n'

// durationNanos parses a Go duration ("30m", "1h30m", "168h") as the server does; null when it is not one.
const units: Record<string, number> = { ns: 1, us: 1e3, µs: 1e3, μs: 1e3, ms: 1e6, s: 1e9, m: 6e10, h: 3.6e12 }

export function durationNanos(s: string): number | null {
  if (!/^(\d+(\.\d+)?(ns|us|µs|μs|ms|s|m|h))+$/.test(s)) return null
  let total = 0
  for (const m of s.matchAll(/(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g)) total += Number(m[1]) * units[m[2]!]!
  return total
}

// label is a setting's name on the settings page and in the setup wizard
export function label(key: string): string {
  return (
    {
      'files.max_size_bytes': t('Largest file (bytes)'),
      'files.max_per_post': t('Files per message'),
      'session.idle_timeout': t('Logged out after inactivity'),
      'session.absolute_lifetime': t('Longest session'),
      'oidc.name': t('Single sign-on button label'),
      'log.level': t('Log level'),
      'log.format': t('Log format'),
      'recordings.enabled': t('Meeting recordings'),
      'recordings.mode': t('What is recorded'),
      'recordings.video_format': t('Video file format'),
      'recordings.start': t('Who may record'),
      'recordings.max_minutes': t('Longest recording (minutes)'),
      'recordings.max_bytes': t('Largest recording (bytes)'),
      'recordings.max_concurrent': t('Recordings at the same time'),
      'recordings.retention_days': t('Days recordings are kept'),
      'recordings.ffmpeg_path': t('ffmpeg program'),
      'recordings.dir': t('Capture directory'),
      'ai.base_url': t('AI service address'),
      'ai.transcription_model': t('Transcription model'),
      'ai.summary_model': t('Summary model'),
      'ai.language': t('Language of the meetings'),
      'ai.timeout': t('Request timeout'),
      'ai.max_upload_bytes': t('Largest audio per request (bytes)'),
      'ai.context_tokens': t('Context size (tokens)'),
      'ai.max_output_tokens': t('Largest summary (tokens)'),
      'ai.transcription.base_url': t('Transcription service address'),
    }[key] ?? key
  )
}

// hint is one line on what the setting is and a typical value
export function hint(key: string): string {
  return (
    {
      'files.max_size_bytes': t('The largest file someone may attach.'),
      'files.max_per_post': t('How many files one message may carry.'),
      'session.idle_timeout': t('Someone inactive for this long is logged out, e.g. 168h.'),
      'session.absolute_lifetime': t('A session ends after this long even when in use, e.g. 720h.'),
      'oidc.name': t('The login button reads "Sign in with" and this name.'),
      'log.level': t('debug shows the most, error the least.'),
      'log.format': t('json for log collectors, text for reading in a terminal.'),
      'recordings.enabled': t('Lets meetings be recorded. Needs meetings (LiveKit) and a working ffmpeg.'),
      'recordings.mode': t('audio: the voices only. video: the shared screen and the cameras too.'),
      'recordings.video_format': t('webm works with every ffmpeg; mp4 needs one with libx264.'),
      'recordings.start': t('admins: channel administrators. members: any member of the channel.'),
      'recordings.max_minutes': t('A recording stops by itself after this long.'),
      'recordings.max_bytes': t('A recording stops by itself at this size.'),
      'recordings.max_concurrent': t('Meetings recorded at the same time on this server.'),
      'recordings.retention_days': t('0 keeps recordings until someone deletes them; older ones are deleted for good.'),
      'recordings.ffmpeg_path': t('A name found in PATH or a full path. The server tests it when it starts.'),
      'recordings.dir': t('Where a recording is captured before it is posted; empty uses the data directory.'),
      'ai.base_url': t(
        'An OpenAI-compatible API, e.g. https://api.openai.com/v1. Meeting audio and transcripts go there.',
      ),
      'ai.transcription_model': t('It must return timed segments, e.g. whisper-1 (gpt-4o-transcribe does not).'),
      'ai.summary_model': t('The model that writes the summary, e.g. gpt-4.1-mini.'),
      'ai.language': t('Empty: the language spoken in each meeting. Or a two-letter code: pt, en, es.'),
      'ai.timeout': t('How long one request to the AI service may take, e.g. 5m.'),
      'ai.max_upload_bytes': t('The largest audio the transcription service takes in one request.'),
      'ai.context_tokens': t('How many tokens the summary model accepts in one request.'),
      'ai.max_output_tokens': t('The most a summary may take; reasoning models count their reasoning here too.'),
      'ai.transcription.base_url': t('Only when speech to text is another service; it then needs its own key.'),
    }[key] ?? ''
  )
}
