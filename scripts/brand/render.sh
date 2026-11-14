#!/bin/sh
# Raster tooling stays in Docker; output files belong to the invoking user.
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
image=meethub-brand-render:local

# Package installation happens at image build time, never on the host.
docker build --tag "$image" - <<'DOCKERFILE'
FROM alpine:3.22
RUN apk add --no-cache rsvg-convert imagemagick
DOCKERFILE

docker run --rm --network none \
  --user "$(id -u):$(id -g)" \
  --env HOME=/tmp \
  --mount "type=bind,src=$repo_dir,dst=/work" \
  --workdir /work \
  "$image" sh -eu -c '
    public=frontend/public
    brand=frontend/src/assets/brand
    scratch=$(mktemp -d)
    trap '\''rm -rf "$scratch"'\'' EXIT HUP INT TERM

    for size in 16 32 48; do
      rsvg-convert --width "$size" --height "$size" \
        --output "$scratch/favicon-$size.png" "$public/favicon.svg"
    done
    magick "$scratch/favicon-16.png" "$scratch/favicon-32.png" \
      "$scratch/favicon-48.png" "$scratch/favicon.ico"

    # The favicon is transparent; home-screen icons keep the padded mark on an opaque navy tile
    # (iOS paints transparency black).
    cat > "$scratch/tile.svg" <<'\''SVG'\''
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <rect width="64" height="64" fill="#0f1322"/>
  <g transform="translate(4 4) scale(.875)">
  <path fill="#797cff" d="M32 4 60 18 32 32 4 18Z"/>
  <path fill="#5b5fef" d="M4 22 28 34V60L4 48Z"/>
  <path fill="#3e42bb" d="M36 34 60 22V48L36 60Z"/>
  </g>
</svg>
SVG
    for size in 180 192 512; do
      rsvg-convert --width "$size" --height "$size" \
        --output "$scratch/icon-$size.png" "$scratch/tile.svg"
    done

    # The full mark fits inside the central 60% (within the 80% safe circle).
    cat > "$scratch/maskable.svg" <<'\''SVG'\''
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">
  <rect width="512" height="512" fill="#5b5fef"/>
  <path fill="#e8eaf3" transform="translate(102.4 102.4) scale(4.8)" d="M32 4 60 18 32 32 4 18ZM4 22 28 34V60L4 48ZM36 34 60 22V48L36 60Z"/>
</svg>
SVG
    rsvg-convert --width 512 --height 512 \
      --output "$scratch/icon-maskable-512.png" "$scratch/maskable.svg"

    for variant in dark light; do
      rsvg-convert --height 128 --output "$scratch/logo-$variant.png" \
        "$brand/logo-$variant.svg"
    done

    # Copy only completed outputs. Repeated runs replace the same files.
    cp "$scratch/favicon.ico" "$public/favicon.ico"
    cp "$scratch/icon-180.png" "$public/apple-touch-icon.png"
    for name in icon-192.png icon-512.png icon-maskable-512.png; do
      cp "$scratch/$name" "$public/$name"
    done
    for variant in dark light; do
      cp "$scratch/logo-$variant.png" "$brand/logo-$variant.png"
    done
  '
