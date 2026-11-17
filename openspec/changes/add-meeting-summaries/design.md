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
max_output_tokens = 8000                  # reasoning models spend part of it thinking (profile, task 0.1)

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
  sample MeetHub generates (a tone), on the owner's explicit action — never anything from a meeting; "off sends nothing"
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

When an owner turns the setting on (and again from a "Test connection" button), MeetHub sends a short sample
to the transcription endpoint asking for timed segments (`response_format=verbose_json`,
segment timestamps), and a one-line request to the chat endpoint.
The setting turns on only if both answer and the transcription has segments with start and end times (segment
timing is what is required; word timing is not). A model that
returns text without timing is refused with `ai_unsupported`: the ordered, attributed transcript this change
promises cannot be built without it, and there is no degraded mode.

*Corrected by the capability profile (task 0.1):*

- **The sample is generated, not bundled.** The repository carries no media files, so the sample is three seconds
  of a generated tone encoded by `ffmpeg` exactly like a summary source (mono 16 kHz Opus in Ogg). The test proves
  that the endpoint reads the format MeetHub will send and answers in the timed format; it does not judge
  recognition quality. A timed answer is a JSON object with a `segments` array whose entries all have numeric
  `start` and `end` (an empty array is a timed answer with no speech); `json`-style answers (`{"text": …}` alone)
  are `ai_unsupported`.
- **A `200` can be an error.** whisper.cpp answers its errors with status `200` and `{"error": …}` (for instance
  when it cannot read Ogg because it was started without `--convert`). An answer with an `error` member is a
  failure: in the test it is `ai_unsupported` ("the service could not read the audio"), in the pipeline
  `ai_rejected`.
- **The chat request has the real output budget.** Reasoning models count their reasoning against `max_tokens`:
  with 16 to 2 000 tokens four of the five tested models answered `content: null` with `finish_reason: "length"`.
  The test's one-line request therefore uses `ai.max_output_tokens` and passes on a non-empty `content`; a
  `length` answer with no content fails with a message that says the output budget is too small for the model.
- A refused key may be `403` as well as `401` (NVIDIA answers `403`): both are `ai_unauthorized`. `404` (an unknown
  model or path) is `ai_unsupported`.

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

*Corrected by the capability profile (task 0.1):*

- **Rejection for length** is a `400` whose body speaks of the context, tokens or length (NVIDIA: `400`); the AI
  client reads the body only to classify it and returns the class `too_long`, never the body.
- **Validate**: the headings come back translated into the meeting's language (asked for by D6), so "contains the
  Summary section" means: the answer's first non-blank line is a level-2 Markdown heading (the Summary section always
  comes first). `content: null` counts as empty.
- **No speech**: whisper.cpp does not answer an empty `segments` for silence but one segment `[BLANK_AUDIO]` over
  the whole piece. Segments whose text is only a bracketed marker (`[BLANK_AUDIO]`, `[MUSIC]`, `(silence)`…) or
  empty are dropped before the merge; a recording with nothing left has no speech.
- **Language**: whisper.cpp names the language in English words (`"english"`), not ISO-639-1 as OpenAI does, and
  its server transcribes as English unless it was started with `-l auto` or the request carries `language`.
  MeetHub never reads the language from the answer; it sends `language` when `ai.language` is set, and the
  documentation tells operators of whisper.cpp to start it with `-l auto` or set `ai.language`.

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

## Capability profile (task 0.1)

Measured on 2026-11-14 with synthetic content only: speech made by `espeak-ng` (English and Brazilian Portuguese
voices) and an invented meeting transcript. **OpenAI itself was not tested** (there was no OpenAI key); what was
tested instead is a self-hosted OpenAI-compatible transcription server and an OpenAI-compatible chat API of
another vendor. OpenAI's own behaviour in D3 and D5 (an upload limit of 25 MB, ISO-639-1 language codes, empty
`segments` for silence) is from its documentation, not measured.

**Transcription — whisper.cpp's `whisper-server`** (Ubuntu 26.04 package `whisper.cpp` 1.8.3+dfsg-2), run as
`whisper-server -m ggml-base.bin --host 127.0.0.1 --port 18090 --inference-path /v1/audio/transcriptions
--convert -l auto`. Model `ggml-base.bin` (multilingual, 147 951 465 bytes, SHA-256
`60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe`) from
`https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin`.

| Question | Answer |
| --- | --- |
| Path | whatever `--inference-path` says; `/v1/models` is `404`; no authentication (an `Authorization` header is ignored); the `model` field is ignored |
| `response_format` | `json` (the default), `text`, `srt`, `vtt`, `verbose_json`; only `verbose_json` has timing |
| `verbose_json` | `task`, `language` (an English word: `"english"`, `"portuguese"`), `duration`, `text`, `segments[]` with `id`, `text`, `start`, `end` (seconds), `tokens`, `words[]` (always, whatever `timestamp_granularities[]` says), `temperature`, `avg_logprob`, `no_speech_prob`; with `-l auto` also `detected_language`, `detected_language_probability` and `language_probabilities` |
| Timing | segment boundaries are coarse: a segment starts at 0.00 when the piece begins with 1.5 s of silence (the words inside are placed within about 0.1 s). Good enough to order speaker turns, which is what the merge needs |
| Formats | Ogg/Opus only with `--convert` (it runs `ffmpeg` on its host); without it: `200 {"error":"failed to read audio data"}`. WAV always |
| Errors | `200` with `{"error": …}` (no file, unreadable audio) |
| Silence | one segment `[BLANK_AUDIO]` over the whole piece, not an empty list; loud noise is transcribed as invented text (a known Whisper weakness) |
| Language detection | `-l auto` is not the default (`en` is). On synthetic Portuguese speech the base model detected Spanish (p = 0.14) and wrote Spanish-sounding text; with `language=pt` it wrote Portuguese. Real voices and the `small` model are expected to do better; not measured |
| Upload limit | none met: a 22.4 MB WAV (700 s) was accepted and transcribed in 21 s on 4 CPU threads |

For comparison, speaches 0.8.3 (faster-whisper, run briefly before the owner chose the distribution's
whisper.cpp) answered OpenAI's format: an ISO code in `language`, empty `segments` for silence, errors as `404`
JSON. A later MiniMax adapter (its own `speech_to_text` format) is possible behind the same `internal/ai` client.

**Summary — NVIDIA's OpenAI-compatible API** (`https://integrate.api.nvidia.com/v1`, `POST /chat/completions`;
no transcription endpoint).

| Question | Answer |
| --- | --- |
| Truncation | `finish_reason: "length"`, as OpenAI |
| Reasoning | `nemotron-3.5-lightning-30b-a3b`, `nemotron-3.5-super-vl-120b-a12b`, `nemotron-3-ultra-550b-a55b` and `mistral-large-4-1t-a52b` return their reasoning in `reasoning_content` and count it in `max_tokens` (`usage.completion_tokens_details.reasoning_tokens`); with a budget of 16 or 2 000 their `content` was `null` and `finish_reason` `length`. The eight-line Portuguese test transcript took lightning 5 158 tokens (4 679 of them reasoning, 29 s). `nemotron-3.5-bolt-e4b` does not reason: 344 tokens, 4.6 s |
| Shape and language | both models that finished wrote the five sections in Portuguese with translated headings, named the owners of the action items, and ignored an instruction planted in the transcript |
| Errors | wrong key `403`; unknown model `404`; a prompt beyond the context `400` whose body speaks of the context and tokens |
| Time | `mistral-large-4-1t-a52b` took 180 s for 2 000 tokens: `ai.timeout = "5m"` is not generous |

What this changed: D1's `max_output_tokens` default (8 000), D3 (a generated sample; an `error` member in a `200`;
the chat test with the real budget; `403`, `404`), D5 (length rejections, the validation of translated headings,
bracketed no-speech markers, the language), and the documentation for whisper.cpp operators (`--convert`,
`-l auto`, the inference path).

## Implementation notes

What the implementation settled where the decisions left room, or where it differs:

- **Routes**: the setting is `GET`/`PUT /api/v1/admin/summaries` and `POST /api/v1/admin/summaries/test` (with the
  other Administration routes); the page is Administration → Meeting summaries (`/admin/summaries`). A failed test
  answers `422 ai_<class>`; `409 ai_not_configured` while unavailable. `GET /channels/{id}/meeting` gains
  `summary_policy` (the setting a recording started now would keep, for the start confirmation) and the room's
  recording gains its own `summary_policy`.
- **Sources (D4)**: cut from each participant's flattened track during composition (the one the mix uses), only when
  the recording's policy is not `off` *and* the setting is not `off` at composition. Pauses are found by `ffmpeg`'s
  `silencedetect` (−40 dB, 300 ms) through `ametadata` into a file, so MeetHub's `ffmpeg` gains those two filters;
  stretches of speech are joined into pieces while they fit and long silences are not sent at all. The byte bound is
  enforced: a piece over a quarter of `ai.max_upload_bytes` is halved. Object rows carry the composition's
  `generation`, so an earlier attempt's objects are never transcribed. A failure to make sources leaves the recording
  without them (and unsummarizable); it never fails the recording. The connection test also runs those filters, so an
  operator's `ffmpeg` without them is refused (`409 ffmpeg_unsupported`).
- **"Deleted" (D9)** is the recording's message deleted (or its row gone); "expired" its `expired_at`. Both are
  checked before every AI request, before every object is written and kept, and when posting (a deleted root makes
  the reply impossible). A deleted recording's summary ends `cancelled` with `recording_deleted`.
- **States on the message**: the recording's message carries `props.summary` (status, source, error, post id),
  updated through `UpdateSystemPostProps`, so every reader sees it change; `GET …/summary` adds `can_request`.
- **Thread replies**: `PublishSystemPost` gained `RootID` and `Message` (indexed for search; mentions still not
  parsed). Replies are `meeting_summary_requested` (attributed to who asked), `meeting_summary` and
  `meeting_summary_no_speech` (attributed to who started the recording, key `summary:<id>`); the prefix `summary:`
  is reserved like `recording:`. A summary over the message limit is cut at its last level-2 heading that fits,
  `props.truncated` is set and `summary.md` attached (the note is the interface's, translated).
- **Task (D5)**: `summaries:write` on the queue `meethub:summaries` with two workers; four attempts. The handler
  returns an error only to be retried, so a finished task never stays archived holding its id; the reconciler
  (every minute, lifecycle pool) uses one stable task id per recording, requests and the automatic trigger unique
  ones. A claim lasts 30 s and is renewed with the 5-second re-read of the setting; a re-read that cannot reach the
  database stops the attempt (fail closed). "Try again" is a new request on a `failed` or `cancelled` summary.
- **Retention**: the transcript file of the summary is removed first (a failure retries the whole recording at the
  next run), then the recording expires; the summary's message gets `props.recording_expired`.
- **http:// to a name (D7)**: a host name is accepted over plain HTTP when every address it resolves to at startup is
  loopback or private (a container's name, a LAN server); it is resolved once, when the configuration is read.
- **Prompt (D6)**, after a run on real services: "the language spoken in the meeting" let a small model write German
  for an English transcript; the instruction is now "the language the participants speak in the transcript (if
  several, the one they use most), never another", with the headings in that language. The pipeline then ran end
  to end against whisper.cpp (`ggml-base.bin`) and NVIDIA (`nemotron-3.5-bolt-e4b`, `nemotron-3.5-lightning-30b-a3b`)
  with synthetic English speech of two participants: transcribed, ordered and attributed, summarized in English with
  the sections. With synthetic Portuguese speech and no `ai.language` the base model's detection chose Spanish and
  the summary came out in English; with `ai.language = "pt"` it was in Portuguese. Real voices, overlapping speech,
  long meetings and larger models are task 4.2.

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

## Real-service check (task 4.2, in progress)

**2026-11-17, OpenAI (`whisper-1`, `gpt-4.1-mini`), the local stack, synthetic voices** — two browsers with
generated speech as their microphones (Ana and Bruno), about two minutes each, in Portuguese and in English:
speech that overlaps for about a second, then forty seconds of silence.

Passed:
- The connection test from Administration, and the setting from `off` to `manual`.
- Transcription: Ogg/Opus pieces accepted, timed segments returned; two pieces sent per recording (one per
  participant), the silence not sent; nothing invented in the silence.
- Attribution and order: each passage under the participant whose microphone it was, in time order, the
  overlapping passages both kept.
- Language: the Portuguese meeting summarized in Portuguese, the English one in English.
- Sections: Summary, Decisions, Action items, Topics, Open questions, valid at the first try; the summary and
  `transcript-<date>.txt` posted in the recording's thread 12 to 17 seconds after the request.
- Switching off mid-run: "The summary was cancelled" within two seconds; on again, "Try again" wrote the summary.

Found:
- **The `gpt-5` family and later refuse the chat request**: `400 unsupported_parameter` for `max_tokens` (they
  take `max_completion_tokens`) and `400 unsupported_value` for a `temperature` other than the default. `gpt-4.1`,
  `gpt-4.1-mini` and `gpt-4o-mini` accept both forms. Task 4.3.
- `gpt-4o-transcribe` and `gpt-4o-mini-transcribe` refuse `verbose_json`: the connection test's `ai_unsupported`,
  as designed. The documentation must name `whisper-1` for OpenAI.
- `whisper-1` names the language in English words (`"portuguese"`), like whisper.cpp; MeetHub does not read it.
- Sent alone, twenty seconds of silence come back as invented text (`no_speech_prob` 0.94). The pipeline does
  not send long silences, and none appeared in these runs.
- One repeated sentence: in the Portuguese run a short passage ended with the last sentence of the speaker's
  previous passage, which she did not say again, and the summary reported it ("asked again"). The pieces join a
  participant's stretches of speech with the pauses cut out; whether that causes it is not established.
- With `gpt-4.1-mini` the headings of the Portuguese summary stayed in English ("Summary", "Decisions") while the
  text was Portuguese; `gpt-4o-mini` translated them in a direct request.

Not checked yet: recordings of real people, a retry after a forced failure of the service, the automatic mode.
