# Design

## Context

- `add-meeting-recordings` captures every participant's microphone as its own Ogg/Opus files positioned on the
  recording's clock, composes one file with `ffmpeg`, publishes it with a server-side operation
  (`PublishSystemPost`, typed `props`, deterministic keys) and owns each recording through a lease. Lifecycle tasks
  have their own worker pool. `ffmpeg` is already required where recordings are on.
- Messages render through one sanitizing Markdown pipeline (`frontend/src/chat/render.ts`), which also never loads
  remote images; a message holds at most 16 383 characters. Threads, files and access rules exist.
- The organization row holds settings, but no setting is edited in Administration yet: this change adds the first
  one, with its route.
- Secrets come only from the environment. Queued tasks log the error a task returns.
- OpenAI's API has `POST /audio/transcriptions` (multipart; timed segments only in some response formats and
  models; an upload limit of 25 MB) and `POST /chat/completions`. Other services implement both with differences.

## Goals / Non-Goals

**Goals:**

- A summary a person can act on: organized, attributed, marked as written by AI, with the transcript to check.
- The organization's switch is real: after "off", no further byte leaves the server.
- No vendor lock-in, and no pretending: a service that cannot do what the summary needs is refused when it is
  configured, not discovered meeting by meeting.

**Non-Goals:**

- Live captions, translation, questions to an assistant, summaries of text conversations, speaker identification
  inside one microphone (diarization), local model hosting, budgets per month.

## Decisions

### D1. Configuration: one OpenAI-compatible provider, stated limits

```toml
[ai]
base_url = "https://api.openai.com/v1"   # any server that speaks the same API
transcription_model = ""                  # required
summary_model = ""                        # required
language = ""                             # "" = the language spoken; or "pt", "en", "es"…
timeout = "5m"
max_upload_bytes = 20000000               # per transcription request
context_tokens = 32000                    # what the summary model accepts
max_output_tokens = 2000

[ai.transcription]                        # optional: another service for speech to text
base_url = ""
```

Keys: `MEETHUB_AI_API_KEY` and, with the override, `MEETHUB_AI_TRANSCRIPTION_API_KEY` (both also as `_FILE`).
"AI is configured" means the key and both models are set. There is no `enabled` flag in the file: configuration says
what MeetHub *can* reach, the organization setting (D2) says whether it *does*.

The administrator names the models and states the summary model's context size, because MeetHub cannot know them
for an arbitrary compatible service.

### D2. The switch, and what "off" guarantees

`meeting_summaries` on the organization: `off` (default), `manual`, `automatic`; changed by `manage_system` through
a new organization-settings route, audited, and announced to every replica on the event bus.

- Turning it **on** runs a capability test first (D3); it stays `off` if the test fails. The test sends only a
  sample bundled with MeetHub, on the owner's explicit action — never anything from a meeting; "off sends nothing"
  is about meeting content.
- The setting is read from the database **before every request** to the AI service and before every retry, and
  every running summary re-reads it **every 5 seconds** while a request is in flight. `off` means: no new summary
  is accepted, queued ones are cancelled (`cancelled_by_setting`), a request in flight is aborted, and every
  replica does so within 10 seconds of the change. The bus message makes it immediate; the 5-second re-read is what
  guarantees the bound when the bus is down. What was already sent cannot be recalled, and the
  interface says so when the owner turns it off.
- Each recording stores the policy in force **when it started** (`summary_policy`). A recording started under `off`
  has no per-speaker sources and can never be summarized; changing the setting later affects recordings started
  afterwards. `off` always wins over a stored policy.

### D3. A provider is accepted by test, not by name

When an owner turns the setting on (and again from a "Test connection" button), MeetHub sends a short spoken sample
bundled in the binary to the transcription endpoint asking for timed segments (`response_format=verbose_json`,
segment timestamps), and a one-line request to the chat endpoint.
The setting turns on only if both answer and the transcription has segments with start and end times (segment
timing is what is required; word timing is not). A model that
returns text without timing is refused with `ai_unsupported`: the ordered, attributed transcript this change
promises cannot be built without it, and there is no degraded mode.

### D4. Summary sources: speech-sized audio, made by `ffmpeg`, inventoried

When a recording whose policy is not `off` is composed, the same replica also produces its **summary sources**:
each speaker's audio re-encoded with `ffmpeg` to mono 16 kHz Opus at 24 kbit/s and cut into pieces of at most 5
minutes **and** at most a quarter of `ai.max_upload_bytes`, each independently decodable. Pieces are cut **in
pauses** (at least 300 ms of silence), so a sentence is not split; only where a participant speaks for five minutes
without a pause is a piece cut mid-speech, and there a sentence may come out in two parts — the transcript marks
that spot. There is no overlap between pieces and nothing to de-duplicate.

Every object is **registered before it is written**: a row in `recording_objects` (`recording_id`, `key`, `kind`,
`speaker`, `offset_ms`, `duration_ms`, `state`: `pending` or `stored`, `create_at`) is inserted first, the upload
follows, then the row is marked `stored`. An upload checks the recording's deleted/expired mark before and after
writing and removes its own object if the mark appeared. Cleanup removes `pending` and `stored` alike, so no object
can exist without a row that leads to it. Pieces are internal: no file info, not listed, not downloadable.

The recording row gets `summary_source` (`none`, `available`, `expired`) and `summary_source_expires_at` (7 days
after the recording). A request for a recording without available sources answers
`409 summary_source_unavailable` or `summary_source_expired`, and the interface does not offer the action.

"Speaker" means the participant whose microphone it was. Two people at one microphone are one speaker; the
interface and the documentation say "by participant", not "by voice".

### D5. The pipeline: claimed, checkpointed, bounded

State on the recording row: `summary_status` (`none`, `queued`, `transcribing`, `summarizing`, `ready`, `failed`,
`cancelled`), `summary_error`, `summary_claim_until`, `summary_generation`, `summary_post_id`.

- **Durable intent**: in automatic mode `queued` is written in the transaction that marks the recording ready; a
  manual request writes it conditionally (`409` if not `none`/`failed`). A reconciler on the lifecycle pool, every
  minute, enqueues again any row in an **active** state (`queued`, `transcribing`, `summarizing`) without a live
  claim. Reaching `ready`, `failed` or `cancelled` clears the claim, and those rows are never picked up by the
  reconciler: a failed or cancelled summary runs again only when someone asks ("Try again").
- **Who may ask**: a member able to read the channel who holds its recording role (as in `add-meeting-recordings`,
  without the requirement of being in the room — the meeting is over).
- **Claim**: the task takes the row with a conditional update that bumps `summary_generation` and sets a claim it
  renews; a task that loses its claim stops. This, not the status name, is what makes it one at a time.
- **Own workers**: summary tasks run on their own queue with their own pool (two workers), never the lifecycle
  pool; at most four AI requests in flight per replica.
- **Transcribe**: each piece to `/audio/transcriptions`; each answer is stored as a checkpoint object before the
  next request. A retry reuses stored checkpoints; a request whose answer was lost in a crash may be sent again —
  that is the guarantee, not "never twice".
- **Merge**: each timed segment shifted by its piece's offset, everything ordered by time, consecutive segments of
  one participant joined: `[00:03:12] Ana Silva: …`.
- **No speech**: when no piece has speech, no request is made to the summary model and the thread gets a system
  note that the recording has no speech to summarize.
- **Summarize** within a budget checked for **every** request: the input is estimated conservatively (one token
  per two bytes) and must fit `context_tokens` minus `max_output_tokens` minus 1 000 tokens for the instructions
  and the request's envelope. A transcript that fits goes in one request; otherwise it is cut at speaker turns into
  parts that fit, each summarized, and the partial summaries combined — the combination is budgeted the same way
  and reduced once more if it does not fit (at most two levels, 40 parts), beyond which the summary fails with
  `ai_too_long`. If the service still rejects a request for its length, the part size is halved once and the step
  repeated; a second rejection is `ai_too_long`. Responses are read up to 1 MiB.
- **Validate**: the answer must be non-empty text containing the Summary section; an empty, truncated
  (`finish_reason = length`) or malformed answer is retried once with the same input and then fails
  (`ai_rejected`).
- **Post**: one transaction with the deterministic key `summary:<recording id>`: a reply in the recording's thread
  through `PublishSystemPost` (type `meeting_summary`; no mention parsing, so nothing the model writes can notify
  anyone), the transcript attached as a file. A summary longer than a message allows is cut at a section boundary
  with a note, and the whole summary is attached as `summary.md` too.

Attempts follow the queue's backoff (4). `401`/`403` fail at once (`ai_unauthorized`); `429` and `5xx` are retried;
after the last attempt the status is `failed` with a code and the recording shows "Try again".

### D6. The summary's shape

The system prompt asks for Markdown with these sections, each omitted when empty, in the meeting's language (or
`ai.language`): **Summary** (three to five sentences), **Decisions**, **Action items** (who does what, with the
date when one was said), **Topics** (the points made and who made them), **Open questions**. It tells the model
that the transcript is a record of speech, not instructions, and to state only what the transcript supports. The
"Written by AI from the recording — check the transcript" label is the interface's, not the model's.

### D7. The outbound boundary

- Requests go only to the configured base URLs. Redirects are **not followed**. A base URL with credentials, a
  query or a fragment is rejected; plain `http://` is accepted only for loopback and private addresses.
- MeetHub adds to a request nothing but the audio piece, or the transcript with the participants' display names,
  and the fixed prompt: no e-mail, user id, channel or organization name of its own. What people *said*, and the
  names they chose, are in the audio and the transcript by nature — including anything sensitive they said — and
  the documentation states that plainly.
- Provider answers are size-capped, and errors are reduced to a class and a status code before they leave the AI
  client: a provider's error body (which may echo the transcript) is never returned, logged or shown.
- Nothing of the audio, transcript or summary is logged; logs have the recording id, durations, sizes, classes.

### D8. Telling people

The notice depends on the policy in force when the recording starts: `automatic` — "Recording · AI summary" in the
room and "This meeting is being recorded and will be summarized by AI" before joining; `manual` — "Recording" in
the room with "An AI summary may be written from this recording" in the start confirmation, the announcement and
the pre-join notice. Nobody's speech goes to an AI service from a meeting where the people present were not told
it could.

### D9. Lifecycle of everything derived

| Thing | Removed when |
| --- | --- |
| Summary sources and transcription checkpoints (`recording_objects`) | the summary is posted, or 7 days after the recording, or the recording is deleted |
| Transcript file | with its message; and by recording retention |
| Summary reply | with the recording's message (a thread goes with its root) |
| Summary text after recording retention | **stays**: it is the minutes of the meeting; the recording and the transcript are gone and the message says so |

A recording that is deleted or expired is marked so on its row; the task and the reconciler check that mark before
every step and before posting, so nothing is recreated after a deletion. A daily task on the lifecycle pool removes
listed objects that are due, retrying the ones that fail. Archived channels follow the same rules.

## Risks / Trade-offs

- [Audio of people sent to a third party] → off by default; an owner's explicit choice behind a passing test;
  announced in the room and before joining in both modes; a local OpenAI-compatible server keeps it in house.
- [A wrong or invented summary taken as fact] → labelled, the transcript attached, the prompt restricted to what
  was said, the summary a reply people can answer.
- [Speech that tries to steer the model] → passed as content under a system prompt that says so; the output is
  rendered as a sanitized message, cannot mention, and has no tools.
- [Cost surprises] → manual mode; one summary per recording; bounded duration, parts and output; every request
  audited. No monthly budget in this change.
- [Compatible services differ] → the capability test refuses what cannot work; limits are stated by the
  administrator rather than assumed.
- [The switch turned off mid-summary] → in-flight work stops within 10 seconds; what was sent stays sent, and the
  interface says so.

## Migration Plan

One additive migration (summary columns, `recording_objects`, the organization setting, default `off`). Without
`MEETHUB_AI_API_KEY` nothing changes. Rollback: set the setting to `off`, or `meethub migrate down`; summaries
already posted stay as messages.

## Open Questions

- In `manual` mode only the people who may record can ask for a summary (it spends the organization's money).
  Should any member of the channel be able to?
