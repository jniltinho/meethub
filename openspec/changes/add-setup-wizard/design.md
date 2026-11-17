# Design

## Context

`SetupView.vue` is a 79-line form posting to `POST /api/v1/auth/setup` with the token. ad-manager's wizard was
read for its shape (stepper `<ol>` with `aria-current="step"`, a diagnosis step with "Check again", hints, a
review in plain sentences with "Show the commands", a progress list, validation rules in a pure module with unit
tests). Its mechanism — a root agent running an installer — does not carry over: MeetHub is already installed
when the page opens, and its settings are changed by the settings applier or by hand.

## Goals / Non-Goals

**Goals:**
- A first run that tells the operator what is wrong with the server before they invite anyone.
- Meetings, recordings and AI summaries reachable from the first run, with hints.
- No new way to change configuration without an authenticated owner.

**Non-Goals:**
- The wizard does not install LiveKit, Redis, `ffmpeg` or a certificate, and does not edit files itself.
- No wizard for SMTP, single sign-on or the database: their settings stay on the server; the diagnosis points at
  the documentation.
- The CLI setup (`meethub user create --role owner`) is unchanged.

## Decisions

### D1. The account first, then everything through Administration

Steps 4 and 5 run after the owner exists and is signed in. They call the routes of `add-admin-settings` and
`edit-ai-and-recording-settings`; the wizard is another face of the settings page, not another writer. This keeps
the pre-account surface at two routes: the existing setup, and the diagnosis.

*Alternative rejected:* collecting everything and applying at the end with the setup token (ad-manager's order)
would need a second, unauthenticated path into the configuration.

### D2. The diagnosis

One service (`internal/diagnosis`) returns a list of `{id, level, detail}` built from what exists: the ready checks (database,
Redis, storage), the configuration (public URL, TLS, environment, SMTP host, secret key, AI configured), the
LiveKit check of `meethub meetings check` (bounded to 5 seconds), the recording probe's last result and the
applier's presence. The SPA has the text and the fix of each `id` and `level`, in the three languages, naming the
setting or variable to change; `detail` is only the kind of database or file storage. Before setup it is
`POST /api/v1/auth/setup/diagnosis` with the token in the body (checked on every call, in constant time; the
token is 256 random bits, so no rate limit is added); afterwards `GET /api/v1/admin/diagnosis` for owners. A
browser at another address than `http.public_url` is refused by the same-origin check before any of this
(`403 csrf_failed`): the token step turns that answer into the message that names `public_url`.

Levels: a **problem** (database, Redis, storage) blocks Continue; a **warning** (plain HTTP outside this
machine, LiveKit configured but not answering, no key for stored secrets) and a **note** (an optional part not set
up, development mode) do not.

### D3. Applying

With an applier: one `PUT /admin/settings` with every chosen key, then the page's existing pending/keep flow
drawn as the Finish step's progress list. Without one: the review renders the same keys as TOML lines and the
secrets as `NAME=value` placeholders (never a real key) to copy. The AI key and the summaries setting need no
file and are stored either way.

### D4. Interface

One view with the steps as sections and its rules in `setupRules.ts` (unit-tested): the steps, what blocks them,
which step can be reopened, the password's repetition, the AI address and the lines for `config.toml`. Required
fields use the themed native validation the old form used. House components and role tokens only, all of the login box's
own roles (its text, muted text, line and the login button's color), which the contrast matrix already holds to
AA in every theme and to AAA in the themes listed in `aaa`: no theme gains a token. The stepper is a row of
numbered counters joined by a line — reached ones filled, with a check when finished; each is a button
(`aria-current="step"` on the open one, disabled when it cannot be reopened) named "3. Organization and account";
on a phone only the open step keeps its name, with "Step 3 of 7" under the row. Each step has a heading with its
icon, one friendly line under it, hints under the fields, grouped boxes, and Back on the left with the main
action on the right; the optional steps have "Skip this step". Field labels and hints inside the login box take
the box's muted role: the page's muted gray failed AA over the dark boxes of four themes (the login page too).

## Risks / Trade-offs

- A longer first run → the optional steps are one click to skip, and the account exists after step 3.
- The restart during Finish drops the owner's WebSocket and page requests → the existing settings page already
  survives it by polling; the wizard reuses that.
- The diagnosis before setup tells a token holder about the server's parts → no more than the operator who read
  the token from the server already knows; no secret or value from the environment is included.
