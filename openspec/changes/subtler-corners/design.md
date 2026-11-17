# Design

## Context

Corners come from three tokens in `style.css` (`--radius-sm`, `--radius-md`, `--radius-lg`), applied by shape
groups; no component and no theme sets a radius of its own, and inner heads and feet use the container's radius
less its border.

## Goals / Non-Goals

**Goals:** corners that are barely rounded, the same everywhere.

**Non-Goals:** square corners (0 px stays out of the contract); per-theme shapes.

## Decisions

Halve the three values: 2, 3 and 4 px. The steps between them stay visible (a field inside a card is still a
little sharper than the card), the inner radius of a card's head stays positive (4 − 1 = 3 px), and focus rings,
which follow the element's radius, stay legible.

## Risks / Trade-offs

- Pictures cut to `--radius-sm` look almost square → accepted: avatars that must be round already are.
