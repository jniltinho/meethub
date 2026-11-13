# Spec Delta

## Purpose

Defines MeetHub's languages: English everywhere by default (code, API, CLI, logs and web app), with Portuguese
(Brazil) and Spanish translations for the web app.

## ADDED Requirements

### Requirement: English is the default language
The server, API messages, CLI output, logs and the web app SHALL be in English by default; English SHALL be used
whenever no other language has been chosen.

#### Scenario: First visit
- **GIVEN** a visitor who never chose a language, with a browser set to Portuguese
- **WHEN** the login page loads
- **THEN** it is shown in English

#### Scenario: CLI output
- **WHEN** any `meethub` command prints a message or an error
- **THEN** the text is in English regardless of the environment's locale

### Requirement: Web app translations
The web app SHALL offer English (`en`), Portuguese — Brazil (`pt-BR`) and Spanish (`es`), with English source strings
in code and one translation catalog per other language.

#### Scenario: Missing translation falls back
- **GIVEN** a string with no `es` entry
- **WHEN** the UI is in Spanish
- **THEN** the English text is shown

#### Scenario: Catalog completeness
- **WHEN** the frontend i18n test runs
- **THEN** it fails if any string used in the SPA has no `pt-BR` or no `es` entry

### Requirement: Language selection
Users SHALL choose the language in Settings (saved in their preferences on the server) and on the login page (kept in
the browser), and the change SHALL apply without reloading.

#### Scenario: Language follows the user
- **GIVEN** a user who chose `es` in Settings
- **WHEN** they log in from another browser
- **THEN** the app is shown in Spanish after login

#### Scenario: Unsupported language
- **WHEN** a client saves the language preference `fr`
- **THEN** the response is 400 with code `invalid_language`

### Requirement: API errors translated by code
API error responses SHALL carry a stable machine `code` and an English `message`; the SPA SHALL show the translation
of the `code` in the active language, falling back to the English `message` for unknown codes.

#### Scenario: Known code in Portuguese
- **GIVEN** the UI is in `pt-BR`
- **WHEN** login returns `invalid_credentials`
- **THEN** the alert shows the Portuguese text for `invalid_credentials`

### Requirement: Localized dates and numbers
Dates, times and numbers in the SPA SHALL be formatted with the active language's conventions and the user's time
zone.

#### Scenario: Date in pt-BR
- **WHEN** the audit log is shown in `pt-BR`
- **THEN** dates use the `dd/mm/yyyy` order and 24-hour times
