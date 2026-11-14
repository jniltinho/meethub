# Tasks

## 1. Details on small screens

- [ ] 1.1 Open the details as a full-screen side panel below 1024 px when they are asked for (a state of the
  layout, closed by the panel's button, Escape and Back), with focus moved in and returned; verify Playwright at
  390 px and 820 px: Members action, menu item, close by button, Escape and Back, focus return
- [ ] 1.2 Add the close button and its strings (pt-BR, es) to the details panel, hidden at 1024 px and above;
  verify the catalog test and that desktop screenshots are unchanged
- [ ] 1.3 Run `make check`, `make test` and `make test-e2e`; verify the accessibility checks at both widths

## Workflow follow-up

- Archive with `openspec archive add-mobile-channel-details` after review.
