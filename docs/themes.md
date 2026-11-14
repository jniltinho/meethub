# Themes

MeetHub carries over the theme system of [ad-manager](https://github.com/jniltinho/ad-manager): a **role contract**
of CSS custom properties in `frontend/src/style.css`, one CSS file per theme family in `frontend/src/themes/`, and
themed toasts, alerts and dialogs instead of the browser's native UI.

Corners are subtly rounded and the same in every theme: `--radius-sm` (4 px, small marks), `--radius-md` (6 px,
controls) and `--radius-lg` (8 px, containers) in `style.css`; presence dots and counters are round. A theme file
never declares a radius (a unit test rejects it).

Contrast: every theme meets WCAG AA; `meethub-dark`, `meethub-light`, `high-contrast`, `crystal`, `crystal-dark` and
`classic-blue` meet AAA (7:1 for text). In the MeetHub themes the primary fill is `#3d41cf` (white text at 7.35:1)
and links and mentions use the lighter `#b3b8ff` on dark: no single indigo reaches 7:1 both under white text and as
text on navy.

## Catalog

22 themes: `meethub-dark` (the default, from the mockup) and `meethub-light` in `themes/meethub.css`, plus the
ad-manager catalog (`default`, `admanager`, `breeze`/`breeze-dark`, `violet`/`violet-dark`, `aurora`/`aurora-dark`,
`crystal`/`crystal-dark`, `cone`, `classic-blue`, `classic-gray`, `graphite`, `grafana`, `rabbitmq`, `matrix`,
`winamp`, `pink-panther`, `high-contrast`).

The catalog lives in three places kept equal by tests: `internal/theme/theme.go` (the source of truth, served at
`GET /api/v1/themes`), `frontend/src/themes/catalog.ts` (the picker, with preview swatches) and the
`[data-theme='id']` rules of the CSS files (`frontend/tests/unit/themes.test.ts`).

## How a theme is chosen

1. The logged-in user's preference (`PATCH /api/v1/me`, saved on the server, follows the user to other browsers).
2. Otherwise the theme remembered by this browser while logged in (`localStorage['meethub.theme']`, cleared on logout).
3. Otherwise the organization default, rendered by the server into `index.html` (`data-theme`, `data-default-theme`);
   when it is `meethub-dark` and the browser prefers light, `meethub-light`.

`public/theme-boot.js` (an external file: the CSP forbids inline scripts) applies this before the first paint.

## Adding a theme

1. Add the id, label and scheme to `internal/theme/theme.go` and `frontend/src/themes/catalog.ts` (with a swatch).
2. Create `frontend/src/themes/<family>.css` with `:root[data-theme='<id>'] { … }` redefining only roles and recipe
   parameters (never component rules), and import it from `style.css`.
3. Run the browser tests (`make test-e2e`): every role must resolve to a color, every recipe of
   `themes/matrix.json` (plus MeetHub's own) must meet WCAG AA (AAA for the themes listed in `aaa`), the controls
   and containers must have the shared radii, and the gallery screenshot needs a new baseline
   (`E2E_ARGS="tests/e2e/01-setup-login.spec.ts tests/e2e/03-feedback.spec.ts --update-snapshots=all" make test-e2e E2E_DBS=sqlite`).

## MeetHub roles

MeetHub adds roles for the chat layout (rail, sidebar, side panel, messages, presence, meeting cards) and the feedback
UI (the info toast, inline alerts). Each defaults to a value **derived** from base roles (`var()` or `color-mix()`), so
every theme styles them without changes; a theme overrides one only when the derived value is wrong or fails contrast.

## Role contract

| Role | From | Default |
| --- | --- | --- |
| `--color-cn` | ad-manager | `#113058` |
| `--color-cn-dark` | ad-manager | `#0e172d` |
| `--color-cn-light` | ad-manager | `#1d4a7e` |
| `--color-cn-accent` | ad-manager | `#f58322` |
| `--color-cn-accent-light` | ad-manager | `#f79a4c` |
| `--color-cn-accent-strong` | ad-manager | `#fe6101` |
| `--color-cn-muted` | ad-manager | `#69788b` |
| `--color-cn-soft` | ad-manager | `#8b9aad` |
| `--color-cn-bg` | ad-manager | `#d9e6f3` |
| `--color-cn-bg-light` | ad-manager | `#e7ecef` |
| `--color-cn-bg-soft` | ad-manager | `#eaf0f6` |
| `--color-cn-border` | ad-manager | `#b9c9dc` |
| `--color-cn-ok` | ad-manager | `#2fa14f` |
| `--color-cn-err` | ad-manager | `#c62828` |
| `--color-cn-warn` | ad-manager | `#ef6c00` |
| `--color-fg` | ad-manager | `oklch(20.8% 0.042 265.755)` |
| `--color-on-page` | ad-manager | `var(--color-fg)` |
| `--color-fg-strong` | ad-manager | `var(--color-cn)` |
| `--color-fg-muted` | ad-manager | `var(--color-cn-muted)` |
| `--color-fg-subtle` | ad-manager | `var(--color-cn-soft)` |
| `--color-link` | ad-manager | `var(--color-cn)` |
| `--color-link-hover` | ad-manager | `var(--color-cn-accent-strong)` |
| `--color-page` | ad-manager | `var(--color-cn-bg)` |
| `--color-page-dots` | ad-manager | `rgba(17, 48, 88, 0.35)` |
| `--color-surface` | ad-manager | `#fff` |
| `--color-surface-2` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-surface-3` | ad-manager | `var(--color-cn-bg-soft)` |
| `--color-row-alt` | ad-manager | `var(--color-cn-bg)` |
| `--color-row-hover` | ad-manager | `var(--color-cn-bg-soft)` |
| `--color-line` | ad-manager | `var(--color-cn-border)` |
| `--color-line-soft` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-field` | ad-manager | `#fff` |
| `--color-on-field` | ad-manager | `oklch(27.9% 0.041 260.031)` |
| `--color-field-line` | ad-manager | `#aab3c2` |
| `--color-field-focus` | ad-manager | `var(--color-cn)` |
| `--color-check-line` | ad-manager | `#8b9aad` |
| `--color-autofill` | ad-manager | `#333` |
| `--color-action` | ad-manager | `var(--color-cn)` |
| `--color-action-top` | ad-manager | `var(--color-cn-light)` |
| `--color-action-line` | ad-manager | `var(--color-cn-dark)` |
| `--color-on-action` | ad-manager | `#fff` |
| `--color-action-2` | ad-manager | `var(--color-cn-bg-soft)` |
| `--color-action-2-top` | ad-manager | `#fff` |
| `--color-action-2-line` | ad-manager | `var(--color-cn-border)` |
| `--color-action-2-hover-line` | ad-manager | `var(--color-cn)` |
| `--color-on-action-2` | ad-manager | `var(--color-cn)` |
| `--color-danger` | ad-manager | `var(--color-cn-err)` |
| `--color-danger-top` | ad-manager | `#d84343` |
| `--color-danger-line` | ad-manager | `oklch(39.6% 0.141 25.723)` |
| `--color-on-danger` | ad-manager | `#fff` |
| `--color-brand-action` | ad-manager | `var(--color-cn-accent)` |
| `--color-brand-action-top` | ad-manager | `var(--color-cn-accent-light)` |
| `--color-brand-action-hover` | ad-manager | `var(--color-cn-accent-strong)` |
| `--color-on-brand-action` | ad-manager | `#fff` |
| `--color-accent` | ad-manager | `var(--color-cn-accent)` |
| `--color-accent-strong` | ad-manager | `var(--color-cn-accent-strong)` |
| `--color-selection` | ad-manager | `var(--color-cn)` |
| `--color-on-selection` | ad-manager | `#fff` |
| `--color-focus` | ad-manager | `var(--color-cn-accent)` |
| `--color-ok` | ad-manager | `var(--color-cn-ok)` |
| `--color-err` | ad-manager | `var(--color-cn-err)` |
| `--color-warn` | ad-manager | `var(--color-cn-warn)` |
| `--color-ok-fill` | ad-manager | `var(--color-cn-ok)` |
| `--color-err-fill` | ad-manager | `var(--color-cn-err)` |
| `--color-warn-fill` | ad-manager | `var(--color-cn-warn)` |
| `--color-on-ok-fill` | ad-manager | `#fff` |
| `--color-on-err-fill` | ad-manager | `#fff` |
| `--color-on-warn-fill` | ad-manager | `#fff` |
| `--color-on-neutral-fill` | ad-manager | `#fff` |
| `--color-on-strong-fill` | ad-manager | `#fff` |
| `--color-on-toast` | ad-manager | `#fff` |
| `--color-on-toast-warn` | ad-manager | `var(--color-on-toast)` |
| `--color-err-soft` | ad-manager | `#f6dcdc` |
| `--color-on-err-soft` | ad-manager | `#8a2020` |
| `--color-neutral-fill` | ad-manager | `var(--color-cn-soft)` |
| `--color-strong-fill` | ad-manager | `var(--color-cn)` |
| `--color-ok-soft` | ad-manager | `#d9ecd9` |
| `--color-on-ok-soft` | ad-manager | `#1f5c2a` |
| `--color-warn-soft` | ad-manager | `#fef3d7` |
| `--color-on-warn-soft` | ad-manager | `#8a5a00` |
| `--color-toast-ok` | ad-manager | `#2e7d32` |
| `--color-toast-err` | ad-manager | `#c62828` |
| `--color-toast-warn` | ad-manager | `#ef6c00` |
| `--color-header-top` | ad-manager | `#fff` |
| `--color-header` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-on-header` | ad-manager | `var(--color-cn)` |
| `--color-on-header-muted` | ad-manager | `var(--color-cn-muted)` |
| `--color-on-header-subtle` | ad-manager | `var(--color-cn-soft)` |
| `--color-header-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-header-link` | ad-manager | `var(--color-link)` |
| `--color-header-link-hover` | ad-manager | `var(--color-link-hover)` |
| `--color-tabs-top` | ad-manager | `var(--color-cn-dark)` |
| `--color-tabs` | ad-manager | `#0a1222` |
| `--color-tab` | ad-manager | `transparent` |
| `--color-on-tab` | ad-manager | `#ccc` |
| `--color-tab-hover` | ad-manager | `var(--color-cn-light)` |
| `--color-on-tab-hover` | ad-manager | `#fff` |
| `--color-tab-divider` | ad-manager | `transparent` |
| `--color-on-tab-aux` | ad-manager | `#ccc` |
| `--color-on-tab-aux-hover` | ad-manager | `#fff` |
| `--color-tab-on-top` | ad-manager | `var(--color-cn-light)` |
| `--color-tab-on` | ad-manager | `var(--color-cn)` |
| `--color-on-tab-on` | ad-manager | `#fff` |
| `--color-tab-on-icon` | ad-manager | `var(--color-cn-accent)` |
| `--color-footer-top` | ad-manager | `var(--color-cn)` |
| `--color-footer` | ad-manager | `var(--color-cn-dark)` |
| `--color-footer-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-on-footer` | ad-manager | `#c9d4e3` |
| `--color-footer-strong` | ad-manager | `var(--color-cn-accent)` |
| `--color-nav-on` | ad-manager | `var(--color-cn)` |
| `--color-on-nav-on` | ad-manager | `#fff` |
| `--color-nav-on-line` | ad-manager | `var(--color-cn-border)` |
| `--color-card-head-top` | ad-manager | `#fff` |
| `--color-card-head` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-card-head-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-on-card-head` | ad-manager | `var(--color-cn)` |
| `--color-table-head-top` | ad-manager | `var(--color-cn-light)` |
| `--color-table-head` | ad-manager | `var(--color-cn)` |
| `--color-on-table-head` | ad-manager | `#fff` |
| `--color-dialog-line` | ad-manager | `var(--color-cn)` |
| `--color-on-dialog` | ad-manager | `#000` |
| `--color-dialog-head-top` | ad-manager | `var(--color-cn)` |
| `--color-dialog-head` | ad-manager | `var(--color-cn-dark)` |
| `--color-dialog-head-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-on-dialog-head` | ad-manager | `#fff` |
| `--color-backdrop` | ad-manager | `rgba(14, 23, 45, 0.55)` |
| `--color-pop-line` | ad-manager | `var(--color-cn)` |
| `--color-tooltip` | ad-manager | `var(--color-cn-dark)` |
| `--color-on-tooltip` | ad-manager | `#fff` |
| `--color-tooltip-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-on-pop` | ad-manager | `#1e293b` |
| `--color-on-selection-muted` | ad-manager | `rgba(255, 255, 255, 0.75)` |
| `--color-card-hover-line` | ad-manager | `var(--color-cn)` |
| `--color-subtab-line` | ad-manager | `var(--color-cn)` |
| `--color-subtab` | ad-manager | `transparent` |
| `--color-on-subtab` | ad-manager | `var(--color-fg-strong)` |
| `--color-subtab-hover` | ad-manager | `var(--color-row-hover)` |
| `--color-subtab-on` | ad-manager | `var(--color-selection)` |
| `--color-on-subtab-on` | ad-manager | `var(--color-on-selection)` |
| `--color-cardtabs` | ad-manager | `var(--color-surface-2)` |
| `--color-cardtabs-line` | ad-manager | `var(--color-line)` |
| `--color-cardtab` | ad-manager | `transparent` |
| `--color-on-cardtab` | ad-manager | `var(--color-fg-muted)` |
| `--color-cardtab-hover` | ad-manager | `transparent` |
| `--color-cardtab-on` | ad-manager | `var(--color-surface)` |
| `--color-on-cardtab-on` | ad-manager | `var(--color-fg-strong)` |
| `--color-cardtab-bar` | ad-manager | `var(--color-accent)` |
| `--color-chart-1` | ad-manager | `var(--color-cn)` |
| `--color-decor-ok` | ad-manager | `var(--color-cn-ok)` |
| `--color-decor-err` | ad-manager | `var(--color-cn-err)` |
| `--color-decor-warn` | ad-manager | `var(--color-cn-warn)` |
| `--color-decor-muted` | ad-manager | `var(--color-cn-muted)` |
| `--color-decor-strong` | ad-manager | `var(--color-cn)` |
| `--color-scroll-thumb` | ad-manager | `var(--color-cn-soft)` |
| `--color-scroll-track` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-login-page` | ad-manager | `var(--color-cn-bg-light)` |
| `--color-login-box` | ad-manager | `var(--color-cn)` |
| `--color-login-box-line` | ad-manager | `var(--color-cn-dark)` |
| `--color-login-box-bar` | ad-manager | `var(--color-cn-accent)` |
| `--color-on-login-box` | ad-manager | `#fff` |
| `--color-on-login-box-strong` | ad-manager | `rgba(255, 255, 255, 0.9)` |
| `--color-on-login-box-muted` | ad-manager | `rgba(255, 255, 255, 0.6)` |
| `--color-on-login-box-faint` | ad-manager | `rgba(255, 255, 255, 0.55)` |
| `--color-login-title` | ad-manager | `var(--color-cn-accent)` |
| `--color-login-field-line` | ad-manager | `var(--color-cn-border)` |
| `--color-login-focus` | ad-manager | `rgba(245, 131, 34, 0.45)` |
| `--color-logo-plate` | ad-manager | `transparent` |
| `--color-rail` | MeetHub | `var(--color-tabs)` |
| `--color-on-rail` | MeetHub | `var(--color-on-tab)` |
| `--color-rail-on` | MeetHub | `var(--color-tab-on)` |
| `--color-on-rail-on` | MeetHub | `var(--color-on-tab-on)` |
| `--color-sidebar` | MeetHub | `var(--color-surface-2)` |
| `--color-on-sidebar` | MeetHub | `var(--color-fg)` |
| `--color-on-sidebar-muted` | MeetHub | `var(--color-fg-muted)` |
| `--color-sidebar-hover` | MeetHub | `var(--color-row-hover)` |
| `--color-sidebar-on` | MeetHub | `var(--color-selection)` |
| `--color-on-sidebar-on` | MeetHub | `var(--color-on-selection)` |
| `--color-panel` | MeetHub | `var(--color-surface)` |
| `--color-message-hover` | MeetHub | `var(--color-row-hover)` |
| `--color-mention` | MeetHub | `color-mix(in oklab, var(--color-warn-fill) 18%, var(--color-surface))` |
| `--color-unread` | MeetHub | `var(--color-err-fill)` |
| `--color-on-unread` | MeetHub | `var(--color-on-err-fill)` |
| `--color-presence-online` | MeetHub | `var(--color-ok-fill)` |
| `--color-presence-away` | MeetHub | `var(--color-warn-fill)` |
| `--color-presence-offline` | MeetHub | `var(--color-neutral-fill)` |
| `--color-meeting-card` | MeetHub | `var(--color-surface-2)` |
| `--color-meeting-card-bar` | MeetHub | `var(--color-action)` |
| `--color-toast-info` | MeetHub | `var(--color-action)` |
| `--color-on-toast-info` | MeetHub | `var(--color-on-action)` |
| `--color-info-soft` | MeetHub | `color-mix(in oklab, var(--color-selection) 14%, var(--color-surface))` |
| `--color-on-info-soft` | MeetHub | `var(--color-fg-strong)` |
| `--color-alert-line-info` | MeetHub | `var(--color-selection)` |
| `--color-alert-line-ok` | MeetHub | `var(--color-ok)` |
| `--color-alert-line-warn` | MeetHub | `var(--color-warn)` |
| `--color-alert-line-err` | MeetHub | `var(--color-err)` |
