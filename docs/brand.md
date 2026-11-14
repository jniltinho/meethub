# MeetHub visual identity

The mark is an open geometric cube with three separated faces. Its shared
structure suggests connected workspaces. The SVG uses a 64 × 64 coordinate grid;
the flat faces and open seams preserve its silhouette at small sizes.

## Assets and colors

- `mark.svg`: full-color mark on a transparent background.
- `mark-mono.svg`: one-color mark using `currentColor`. Inline the SVG to inherit
  CSS text color; an SVG loaded through an image element does not inherit it.
- `logo-dark.svg`: horizontal lockup with light text for dark backgrounds.
- `logo-light.svg`: horizontal lockup with navy text for light backgrounds.
- `favicon.svg`: the mark alone on a transparent background, for either browser-tab theme.

The wordmark is outlined from the project's bundled Inter SemiBold font. SVG
lockups contain paths and require no font downloads.

| Role | Hex |
| --- | --- |
| Primary indigo / left face | `#5b5fef` |
| Violet / top face | `#797cff` |
| Deep indigo / right face | `#3e42bb` |
| Dark navy tile of the home-screen icons | `#0f1322` |
| Interface background (`meethub-dark`) | `#0c1020` |
| Interface panel | `#151b31` |
| Interface text | `#f2f4fb` |
| Interface primary fill (white text at 7.35:1) | `#3d41cf` |
| Interface accent text (links, mentions) | `#b3b8ff` |
| Navy wordmark | `#12152a` |

## Placement

Keep at least one quarter of the mark's nominal height clear around marks and
lockups (16 units at the original 64-unit height). The lockup's built-in gap
between mark and text is intentional. Use a minimum mark size of 16 × 16 CSS
pixels and a minimum lockup height of 32 CSS pixels. Prefer 24 pixels or larger
for standalone marks outside browser tabs.

Do preserve proportions, face colors, spacing, and the supplied wordmark paths.
Use the monochrome mark when only one ink or color is available, choosing a color
with clear contrast against its background. Do not stretch, rotate, add shadows,
replace the wordmark font, close the seams, or place the mark over busy imagery.
Do not use the light-text lockup on light backgrounds or the navy-text lockup on
dark backgrounds.

## Regenerate raster assets

From the repository root, with Docker running:

```sh
./scripts/brand/render.sh
```

The script builds an Alpine image with librsvg and ImageMagick (the first build
requires network access), then renders as your host UID and GID. No host SVG tools
are needed. Repeated runs replace the same output files without root ownership.
The PNG lockups have transparent backgrounds and a height of 128 pixels.

Outputs are `frontend/public/favicon.ico` (16, 32, and 48 pixels),
`apple-touch-icon.png` (180), `icon-192.png`, `icon-512.png`, and
`icon-maskable-512.png`, plus `frontend/src/assets/brand/logo-dark.png` and
`logo-light.png`. The maskable icon uses a light monochrome mark on solid
`#5b5fef`; its artwork stays within the central 60% square and the central 80%
diameter safe circle. Keep that padding when preparing additional maskable sizes.
The manifest lists these generated files; generate them before publishing.

The rendering script is supplied without running it as part of asset creation.
