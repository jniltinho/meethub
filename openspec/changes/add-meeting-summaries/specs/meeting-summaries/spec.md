# Spec Delta

## Purpose

Turns a recorded meeting into a written summary with the help of an AI service the organization configures: when it
happens, what is produced, where it goes and what is sent out.

## ADDED Requirements

### Requirement: Summaries are the organization's choice
A summary SHALL be written only when AI is configured and the organization's Meeting summaries setting is `manual`
or `automatic`; with the setting `off`, or without configuration, no audio or text of any meeting SHALL be sent to
any AI service. (The connection test an owner runs sends only a sample MeetHub generates.)

#### Scenario: Off by default
- **GIVEN** an install with AI configured and the setting never changed
- **WHEN** a recording finishes
- **THEN** no request is made to the AI service and the recording has no summary

#### Scenario: Automatic
- **GIVEN** the setting is `automatic` when a recording starts
- **WHEN** that recording becomes ready
- **THEN** its summary is queued without anyone asking

### Requirement: Off stops work in progress
Setting Meeting summaries to `off` SHALL cancel queued summaries and stop running ones: within 10 seconds no
further request SHALL be made to the AI service, on any replica, and a retry SHALL NOT resume them.

#### Scenario: Switched off while transcribing
- **GIVEN** a summary has transcribed three of ten pieces
- **WHEN** an owner sets the setting to `off`
- **THEN** no further piece is sent and the summary's state is `cancelled`

#### Scenario: Switched off while queued
- **GIVEN** a summary is queued and has not started
- **WHEN** the setting becomes `off`
- **THEN** it never starts

### Requirement: The policy of a recording is the one at its start
A recording SHALL be summarized according to the setting in force when it started, except that `off` always
prevails; a recording started while the setting was `off` SHALL NOT be summarizable later.

#### Scenario: Turned on after the meeting
- **GIVEN** a recording made while the setting was `off`
- **WHEN** the setting is later `manual` and someone asks for its summary
- **THEN** the answer is `409` with code `summary_source_unavailable`

### Requirement: Requesting a summary
`POST /api/v1/recordings/{id}/summary` SHALL queue the summary of a ready recording that has summary sources, for a
caller who can read its channel and holds the channel's recording role (being in the room is not required), and
answer `409` when one is queued, running or ready, or when the sources are unavailable or expired; `GET` on the
same path SHALL return its state to the channel's readers.

#### Scenario: Manual request
- **GIVEN** a recording made under `manual` and Ana, a channel administrator of `#projects` who is not in its room
- **WHEN** she asks for its summary
- **THEN** the answer is `202` and the thread says Ana asked for an AI summary

#### Scenario: Not allowed
- **WHEN** a member without the recording role asks for a summary
- **THEN** the answer is `403` with code `forbidden`

#### Scenario: Sources expired
- **GIVEN** a recording made eight days ago that was never summarized
- **WHEN** someone asks for its summary
- **THEN** the answer is `409` with code `summary_source_expired`

### Requirement: Transcript by participant
The transcript SHALL be built from each participant's own microphone, every passage carrying its time in the meeting
and that participant's display name, in the order it was said; this requires timed transcription, and a service that
cannot give it SHALL be refused rather than used.

#### Scenario: Who said what
- **GIVEN** Ana said "we ship on Friday" at minute nine and Bruno answered "the tests are not done" right after
- **WHEN** the transcript is built
- **THEN** it has Ana's sentence under her name, then Bruno's under his, each with its time

#### Scenario: Pieces cut in pauses
- **GIVEN** a participant speaks for twelve minutes with ordinary pauses
- **WHEN** the transcript is built
- **THEN** no sentence is split or repeated where the audio was cut

### Requirement: The summary's shape
The summary SHALL be Markdown with the sections Summary, Decisions, Action items (who does what), Topics (with who
said what) and Open questions, each omitted when empty, in the language spoken in the meeting or in `ai.language`
when set; an answer that is empty, cut short or without a Summary section SHALL NOT be posted.

#### Scenario: Meeting in Portuguese
- **GIVEN** a meeting held in Portuguese and `ai.language` unset
- **WHEN** its summary is written
- **THEN** the summary is in Portuguese and has at least the Summary section

#### Scenario: Truncated answer
- **WHEN** the model's answer is cut short by its output limit
- **THEN** it is asked once more and, if cut again, the summary fails with code `ai_rejected`

#### Scenario: Nobody spoke
- **GIVEN** a recording in which nobody spoke
- **WHEN** its summary runs
- **THEN** no request goes to the summary model and the thread says there is no speech to summarize

### Requirement: The summary in the channel
The summary SHALL be posted once as a reply to the recording's message, marked as written by AI, with the
transcript attached as a text file; both SHALL follow the channel's access and deletion rules. Names in the summary
SHALL NOT notify anyone as mentions. A summary too long for one message SHALL be cut at a section with a note and
attached whole.

#### Scenario: Posted in the thread
- **WHEN** the summary of a recording of `#projects` is ready
- **THEN** the recording's thread has one reply marked "Written by AI" with the summary and `transcript-….txt`

#### Scenario: Posted once
- **GIVEN** the server stops right after posting the summary
- **WHEN** the task runs again
- **THEN** the thread still has one summary

#### Scenario: Not a member
- **WHEN** someone outside `#projects` requests the transcript's file
- **THEN** the answer is `404`

### Requirement: What MeetHub sends
Requests SHALL go only to the configured base URLs, without following redirects, and SHALL contain only the audio
piece, or the transcript with the participants' display names, and the fixed instructions; MeetHub SHALL NOT add
e-mails, user ids, channel or organization names. No audio, transcript, summary or provider error body SHALL be
written to the logs.

#### Scenario: Redirect
- **WHEN** the AI service answers a request with a redirect to another host
- **THEN** it is not followed and the step fails as `ai_unavailable`

#### Scenario: Error that echoes content
- **WHEN** the service answers an error whose body repeats part of the transcript
- **THEN** logs and the recording show only the error's class and status code

### Requirement: Failure and retry
A failed step SHALL be retried by the queue, reusing the pieces already transcribed; after the last attempt the
recording SHALL show that the summary failed, with a "Try again" action for who may record, and a wrong key SHALL
fail without retries.

#### Scenario: Service down
- **GIVEN** the AI service answers `503`
- **WHEN** the attempts are exhausted
- **THEN** the recording shows "The summary could not be written" and "Try again"

#### Scenario: Wrong key
- **WHEN** the service answers `401`
- **THEN** the summary fails at once with code `ai_unauthorized` and the key is in no log or message

### Requirement: Derived data is removed
Summary sources and transcription results SHALL be removed when the summary is posted, when the recording is
deleted, or seven days after the recording; deleting a recording's message SHALL delete its summary and transcript;
a recording's retention SHALL remove its transcript and keep its summary text. Nothing SHALL be recreated for a
recording that was deleted or expired.

#### Scenario: Recording deleted while summarizing
- **GIVEN** a summary is being written
- **WHEN** the recording's message is deleted
- **THEN** the summary is not posted and its sources and results are removed

#### Scenario: Retention
- **GIVEN** a recording with a summary reaches the retention age
- **WHEN** the daily task runs
- **THEN** the recording's file and the transcript are gone and the summary text remains
