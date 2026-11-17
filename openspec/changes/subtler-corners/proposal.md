# Proposal

## Why

The owner finds the interface too round: the 6 px controls and 8 px containers read as soft. The corners should
stay rounded, but only just.

## What Changes

- The three shared corner radii become **2 px** (small marks), **3 px** (controls) and **4 px** (containers),
  in every theme and every page. Presence dots, counters and round pictures stay round.
- Nothing else changes: a theme still declares no radius.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ui-themes`: the values of the shared corner radii in the theme contract.

## Impact

- `frontend/src/style.css` (the three tokens), `frontend/tests/e2e/02-themes.spec.ts` (the values it measures),
  `docs/themes.md`, rule 7 of `AGENTS.md`. No API, no migration.
