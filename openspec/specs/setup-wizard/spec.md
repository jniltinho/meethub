# setup-wizard Specification

## Purpose
TBD - created by archiving change add-setup-wizard. Update Purpose after archive.

## Requirements

### Requirement: Setup is a wizard
While setup is required, the SPA SHALL show a wizard with a stepper — Access, This server, Organization and
account, Meetings and recordings, AI summaries, Review, Finish — with Back and Continue, finished steps
reachable again, the current step announced, focus moved to each step's heading, and a hint under every field.
It SHALL work by keyboard and screen reader, in the three languages, from phone width up, meeting AA contrast in
every theme and AAA in the themes held to it.

#### Scenario: The token step
- **WHEN** a visitor opens an uninitialized install
- **THEN** the first step asks for the setup token and shows the command that prints it for this kind of install, with a Copy button

#### Scenario: A field in error
- **WHEN** the visitor continues from the account step with an invalid e-mail
- **THEN** the step stays, the field is marked invalid with its message and takes the focus

#### Scenario: Enhanced contrast
- **WHEN** the wizard is shown in `meethub-dark` or `meethub-light`
- **THEN** every text of the step, the stepper and the hints included, meets 7:1 (4.5:1 for large text)

#### Scenario: An optional step skipped
- **WHEN** the owner changes a value in Meetings and recordings and presses "Skip this step"
- **THEN** the next step opens and the review does not list that value

#### Scenario: Going back
- **WHEN** the visitor returns from the account step to This server
- **THEN** what they typed in the account step is kept, except the password

### Requirement: Diagnosis of the server
The wizard SHALL show a diagnosis of the server's parts — its stores, its public address, meetings, recordings,
e-mail, the key for stored secrets, the AI service and the settings applier — each as ok, note, warning or
problem with how to fix it, with "Check again". Before setup it SHALL be answered only with the setup token,
and SHALL carry no secret and no setting's value beyond the kind of database and of file storage.

#### Scenario: Without the token
- **WHEN** a visitor asks for the diagnosis without the setup token
- **THEN** the answer is 403 `invalid_setup_token`

#### Scenario: The address does not match
- **GIVEN** `http.public_url` is `http://localhost:8080` and the browser opened `http://10.0.0.5:8080`
- **WHEN** the visitor continues from the token step
- **THEN** the step says the server expects another address, names `public_url` and the address in the browser, and goes no further

#### Scenario: Optional parts not set up
- **GIVEN** no LiveKit and no SMTP host
- **WHEN** the diagnosis is shown
- **THEN** meetings and e-mail are notes, not problems, and Continue is allowed

#### Scenario: A problem blocks the next step
- **GIVEN** Redis does not answer
- **WHEN** the diagnosis is shown
- **THEN** Redis is a problem, Continue is disabled and "Check again" repeats the checks

### Requirement: The account comes before the optional steps
The organization and the owner SHALL be created at the third step as the existing setup does, signing the owner
in. The steps after it SHALL change settings only through the Administration routes, with their permissions,
validation, re-authentication, confirmation and audit; the server SHALL offer no way to change a setting with
the setup token.

#### Scenario: Stopped after the account
- **WHEN** the owner closes the browser at the Meetings and recordings step
- **THEN** the install is initialized, the owner can log in, and nothing else was changed

#### Scenario: Optional steps skipped
- **WHEN** the owner presses Skip on both optional steps
- **THEN** the review says nothing will be changed and Finish shows the next steps

#### Scenario: Reopened later
- **GIVEN** an initialized install
- **WHEN** an owner opens `/setup`
- **THEN** the wizard opens at Meetings and recordings; anyone else is sent to the login page or the app

### Requirement: Applying what was chosen
On an install whose settings page can apply changes, Finish SHALL apply the chosen settings as one change —
after the review names them — show its progress through the restart, and ask for "Keep these settings". On an
install where it cannot, the review SHALL show the lines for `config.toml` and `.env` with Copy buttons, and
SHALL change nothing itself beyond what needs no file (the AI key, the summaries setting).

#### Scenario: With the applier
- **GIVEN** a package install
- **WHEN** the owner chose recordings on and finishes
- **THEN** the progress list shows the change applied and the restart, and after "Keep these settings" recordings are on

#### Scenario: Without the applier
- **GIVEN** the Compose stack without the settings override
- **WHEN** the owner chose recordings on
- **THEN** the review shows `enabled = true` under `[recordings]` to copy and says the page cannot write it here

#### Scenario: AI summaries
- **GIVEN** the key for stored secrets is set
- **WHEN** the owner enters the AI key and the two models, chooses Manual, and finishes with their password
- **THEN** Finish stores the key, applies the models, and turns summaries to Manual once the connection test passes

#### Scenario: The connection test fails
- **WHEN** the connection test at Finish answers `ai_unauthorized`
- **THEN** the step says the key was refused, summaries stay off, and the owner may go back to the review or open MeetHub
