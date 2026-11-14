## ADDED Requirements

### Requirement: Elapsed meeting time
The bottom bar SHALL show, after the clock, the time since the meeting began as `mm:ss`, and as `h:mm:ss` from one
hour on, updated every second and named "Time in the meeting" for assistive technology. The meeting begins at the
join of the earliest person in the room since the user joined; the count SHALL never go back while the call is open.

#### Scenario: Joining a running meeting
- **GIVEN** Ana has been in the room for five minutes
- **WHEN** Bruno joins
- **THEN** Bruno's bar shows about `05:00` and counts on from there

#### Scenario: The first person leaves
- **GIVEN** Ana and Bruno are in a meeting Ana started ten minutes ago
- **WHEN** Ana leaves
- **THEN** Bruno's count continues from ten minutes

#### Scenario: Past one hour
- **WHEN** the meeting has run for one hour, two minutes and five seconds
- **THEN** the bar shows `1:02:05`

### Requirement: Shared screen quality
A shared screen SHALL be captured at the resolution of the shared surface, not reduced to 1080p, and published with
a bitrate of up to 5 Mbit/s at 15 frames a second with the content hint for detail, so text stays readable.

#### Scenario: A monitor above 1080p
- **WHEN** Ana shares a 2560×1440 monitor
- **THEN** the published track is 2560×1440
