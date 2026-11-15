# Tasks

Depends on `add-meeting-recordings` being implemented (its spike included). Dependency order: 0 → 1 → 2 → 3 → 4.
Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`) unless it says
otherwise.

## 0. Provider capability check (findings kept)

- [x] 0.1 Against OpenAI and one self-hosted OpenAI-compatible server, record what the transcription endpoint
  accepts and returns (timed segments, response formats, upload limit, language detection) and how the chat
  endpoint reports truncation; verify by writing the capability profile into `design.md` and adjusting D3 and D5 to it
  — done with whisper.cpp and NVIDIA's API in place of OpenAI, by the owner's decision (see "Capability profile")

## 1. Settings and switch

- [x] 1.1 Add the `ai` settings (base URLs, models, limits, environment-only keys, the transcription override) with
  validation, redaction and `config.toml.example`; verify table-driven config tests (key in the file, plain HTTP,
  private address, credentials in the URL, override without key)
- [x] 1.2 Add the migration (summary columns, `recording_objects`, the organization setting) and the organization
  settings route with its permission, audit event and bus announcement; verify the `organizations` and `audit-log`
  scenarios on two replicas
- [x] 1.3 Implement the capability test with the sample (generated, not bundled: design D3) and the "Test connection" action; verify against
  the fake server: passing, no timed segments, wrong key, unreachable

## 2. AI client, sources, pipeline

- [x] 2.1 Implement `internal/ai`: the two calls with timeouts, no redirects, capped responses, typed errors that
  never carry a provider body, nothing of the content in logs; verify against a fake OpenAI-compatible server
  (success, 401, 429 then success, 5xx, slow answer, a redirect, an error echoing the input, an oversized answer)
- [x] 2.2 Produce summary sources at composition (`ffmpeg`: mono 16 kHz Opus, cut in pauses by time and bytes),
  register each object before uploading it, and record source state and expiry on the recording; verify each piece
  decodes alone, size limits with loud continuous speech, cuts landing in pauses, five minutes without a pause, a
  crash between registration and upload, and a recording made under `off`
- [x] 2.3 Implement the task on its own queue and pool: claim and renewal (cleared on terminal states), the setting
  read before every request and every 5 seconds during one, cancellation from the bus, checkpoints per piece, merge
  by time, no-speech result, per-request budgets with reduction in parts and one halving on a length rejection,
  answer validation, failure codes, the reconciler limited to active states; verify the `meeting-summaries` and `background-jobs`
  scenarios with the fake server, including switch-off mid-request with the bus down, on two replicas, a cancelled
  summary after the setting returns, and periodic tasks under saturation
- [x] 2.4 Publish the summary in one conditional transaction (deterministic key, `PublishSystemPost`, transcript
  and long-summary attachments) and reconcile lost enqueues; verify posted once across a crash at each step, no
  mention notifications, a summary over the message limit
- [x] 2.5 Implement the lifecycle of derived data (removal on summary, deletion, expiry and retention; the
  deleted/expired mark checked before every step); verify the `Derived data is removed` scenarios, a deletion
  racing a retry, and an archived channel

## 3. API and interface

- [x] 3.1 Add the summary routes and the automatic trigger in the readiness transaction; verify permissions, the
  `409` codes, and the policy stored at the recording's start
- [x] 3.2 Add the setting to Administration (values, test, turning-off note), the notices of both modes in the room,
  the start confirmation and the pre-join screen, the recording's summary states and actions, and the "Written by
  AI" label; verify Playwright against the fake server and the accessibility and contrast checks
- [x] 3.3 Add the pt-BR and es strings; document the settings, the capability test, exactly what is sent (and that
  speech and names may themselves be sensitive), "by participant" attribution, costs, the notices and the lifecycle
  table in `docs/deployment.md`, `docs/security.md` and `docs/api.md`

## 4. Validation

- [ ] 4.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review of the
  implementation with its findings fixed, and CI green
- [ ] 4.2 With a real service and real recordings in Portuguese and in English (two participants, overlapping
  speech, a long silence), check attribution, order, language and sections, a retry after a forced failure, and
  switching off mid-run; record what was checked in the change before archiving

## Workflow follow-up

- Archive with `openspec archive add-meeting-summaries` after review.
