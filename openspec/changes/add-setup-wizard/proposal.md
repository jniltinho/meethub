# Proposal

## Why

The first-run page is one form (token, organization, account). It says nothing about whether the server it is
setting up is ready — the address people will open, Redis, meetings, `ffmpeg`, e-mail — and leaves meetings,
recordings and AI summaries to be found later in files and in Administration. ad-manager's installer
(`frontend/src/pages/Setup.vue` there) shows the shape wanted: numbered steps, a diagnosis of the server, a hint
under every field, a review before anything is done and a progress list at the end.

## What Changes

- The setup page becomes a **wizard** with a stepper: **Access** (the token, with the command that prints it and
  a Copy button) → **This server** (a diagnosis with "Check again") → **Organization and account** → **Meetings
  and recordings** → **AI summaries** → **Review** → **Finish** (progress, then next steps).
- **Diagnosis**: database, Redis, file storage, the public address against the one in the browser, HTTPS,
  meetings (LiveKit), recordings (`ffmpeg`), e-mail, the key for stored secrets, the AI service and whether the
  settings page can apply changes — each ok, note, warning or problem with how to fix it. Available before setup
  only with the setup token.
- The organization and the owner are created at the third step, exactly as today. The **optional steps run as
  that owner through the existing Administration APIs** (recordings and AI settings, the AI key, the connection
  test, the summaries setting): nothing new can be configured without an account.
- Where the settings page cannot apply changes (containers without the applier), the review shows the exact
  lines for `config.toml` and `.env` with Copy buttons instead.
- Every field has a hint; optional steps can be skipped and are found again in Administration → Settings. The
  wizard can be reopened by an owner at `/setup` to finish the optional steps.
- Docs: the install guides describe the wizard step by step.

Depends on `edit-ai-and-recording-settings` (the AI key and settings from the page).

## Capabilities

### New Capabilities

- `setup-wizard`: the first-run wizard — its steps, the server diagnosis, the optional feature steps and how
  they are applied or handed over as lines to copy.

### Modified Capabilities

None: the setup token, the atomic initialization and the bootstrap read keep their requirements.

## Impact

- Code: `frontend/src/views/SetupView.vue` (rewritten as the wizard, with its validation rules in a testable
  module), a `diagnosis` service reusing the ready checks, `meethub meetings check` and the recording probe,
  `POST /api/v1/auth/setup/diagnosis` (setup token) and `GET /api/v1/admin/diagnosis` (`manage_system`), the i18n
  catalogs, `01-setup` and a new e2e spec.
- Docs: `docs/install-linux.md`, `docs/install-binary.md`, `docs/deployment.md`, `README.md`, `docs/api.md`.
- No new dependency, no migration.
