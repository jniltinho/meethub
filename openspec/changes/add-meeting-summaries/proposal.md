# Meeting summaries written by an AI model

## Why

A recording keeps a meeting, but nobody wants to listen to an hour to learn what was decided. People who missed the
meeting, and the ones who were there a week later, need the short version: what was discussed, what was decided, who
does what. A language model writes that well from a transcript, and `add-meeting-recordings` already captures each
person's voice separately, so the transcript can say who said what without guessing.

This must be the organization's choice: the audio leaves the server for whichever AI service the administrator
configures. So it is off by default, switched on and off without a restart, and visible to everyone in the room.

## What Changes

- An `[ai]` configuration section for an **OpenAI-compatible API**: base URL (default `https://api.openai.com/v1`),
  the model that transcribes audio, the model that writes the summary, and the key — only from the environment
  (`MEETHUB_AI_API_KEY`). Any service that speaks that API works (OpenAI, a gateway, a local server).
- An organization setting, **Meeting summaries**: `off` (default), `manual` or `automatic`, changed by an owner in
  Administration without restarting. With no AI configured the setting is unavailable. Turning it on first tests
  the service; turning it off cancels queued summaries and stops running ones within seconds, on every replica —
  what was already sent cannot be recalled, and the interface says so.
- For a finished recording MeetHub transcribes each participant's audio (`/audio/transcriptions`, with timed
  segments), merges the pieces on the meeting's timeline with the participants' names, and asks the model
  (`/chat/completions`) for an organized
  summary: overview, decisions, action items with their owners, topics with who said what, open questions — in the
  language spoken in the meeting.
- The summary is posted as a reply in the thread of the recording's message, marked as written by AI, with the full
  transcript attached as a text file. It follows the channel's access like any message.
- `automatic` summarizes every recording when it is ready; `manual` adds a "Summarize" action to a recording, for
  the people who may record.
- Everyone is told, in both modes, before and while it happens: with `automatic`, that the meeting will be
  summarized by AI; with `manual`, that an AI summary may be written from the recording.
- A recording keeps the policy in force when it started: one made while summaries were off can never be summarized.
- Failures (the service refuses, is down, takes too long) are retried and then shown on the recording with a
  "Try again" action; nothing about the meeting's content goes to the logs.
- Enabling, disabling and requesting summaries are audited.

Assumptions recorded here because they shape the scope (change them before implementation if they are wrong):

- **One provider, one key**, for both transcription and summary, because that is what was asked. An optional
  `[ai.transcription]` override (base URL and `MEETHUB_AI_TRANSCRIPTION_API_KEY`) lets the two come from different
  services without changing anything else.
- **No built-in model names.** The administrator names both models; model names change faster than releases and
  differ between services.
- **Summaries need a recording.** There is no live transcription and no summary of an unrecorded meeting.
- **Timed transcription is required.** A service or model that returns text without timing is refused when the
  setting is turned on; there is no degraded transcript.
- **Attribution is by participant** (whose microphone it was), not by voice.
- **Recording retention removes the transcript and keeps the summary text**, as the minutes of the meeting.
- **Participants' display names are sent** with the transcript to the summary model — the summary is useless
  without them. MeetHub adds no e-mails, user ids or channel names; what people said, and the names they chose, go
  as they are.

## Capabilities

### New Capabilities

- `meeting-summaries`: when a summary is written, how (transcription per speaker, the summary's shape and
  language), where it goes, failure and retry, what is sent to the AI service.

### Modified Capabilities

- `configuration`: the `ai` settings and their validation.
- `organizations`: the Meeting summaries setting.
- `meeting-room-ui`: the notice that an AI summary will be written.
- `chat-ui`: the summary in the recording's thread, the "Summarize" and "Try again" actions.
- `background-jobs`: the summary task.
- `audit-log`: the setting's changes and summary requests.

## Impact

- **Depends on** `add-meeting-recordings` (the per-speaker audio captures, the recording message).
- **Server**: a new `internal/ai` package (a small HTTP client for the two endpoints — no SDK) and
  `internal/summaries` (the pipeline and its states); a migration adding the summary's state to
  `meeting_recordings` and `meeting_summaries` to the organization settings; one Asynq task; routes
  `POST /api/v1/recordings/{id}/summary` and `GET /api/v1/recordings/{id}/summary`.
- **Recordings**: at composition, each participant's audio is also re-encoded for speech and cut into small pieces
  kept in the file storage (not as channel files, listed in an inventory table) until the summary is written, the
  recording is deleted, or seven days pass.
- **Workers**: summary tasks get their own queue and worker pool; an organization-settings route is added (the
  first setting edited in Administration).
- **Dependencies**: none new; the API is plain HTTPS with JSON and multipart.
- **Privacy and cost**: meeting audio and the transcript are sent to the configured service, which may charge per
  minute and per token; `docs/security.md` says exactly what is sent, and the setting and its notices exist so that
  nobody is summarized unknowingly.
- **Web app**: the setting in Administration, the notices, the summary's rendering, strings in three languages.
