#!/bin/sh
# Builds the Linux packages from the static binary (Dockerfile stage package-build; make package):
#   meethub_<version>_amd64.deb   meethub-<version>-1.x86_64.rpm   meethub_<version>_linux_amd64.tar.gz   SHA256SUMS
# The names carry the version (the deb, rpm and tar conventions), so files of several releases can sit side by side.
# The three hold the same files: everything under /opt/meethub, the systemd unit and the settings applier's two
# units (meethub-settings.service and .path).
# Run from a directory holding the binary (./meethub), config.toml.example, LICENSE and NOTICE.
set -eu

version="${1:?usage: build.sh <version> <output directory>}"
out="${2:?usage: build.sh <version> <output directory>}"
here=$(cd "$(dirname "$0")" && pwd)

# v1.2.3 (and what git describe adds after it) → 1.2.3…; anything else (a commit, even one that starts with a
# digit, or "dev") → 0.0.0+<it>, older than every release; no "-" (RPM refuses it)
case "$version" in
v[0-9]*.[0-9]*.[0-9]*) v="${version#v}" ;;
*) v="0.0.0+$version" ;;
esac
v=$(printf '%s' "$v" | tr -- '-' '.')

work=$(mktemp -d)
root="$work/root"
app="$root/opt/meethub"
mkdir -p "$out"

install -D -m 0755 meethub "$app/bin/meethub"
# meeting recordings' ffmpeg (LGPL, built by the Dockerfile's ffmpeg stage) with its licences and build record
install -m 0755 ffmpeg "$app/bin/ffmpeg"
install -d "$app/licenses/ffmpeg"
install -m 0644 ffmpeg-licenses/* "$app/licenses/ffmpeg/"
install -D -m 0640 "$here/meethub.env" "$app/meethub.env"
install -m 0644 LICENSE NOTICE "$app/"
# the example configuration, with the data directory inside /opt/meethub whatever the working directory is
{
  echo '# Installed with MeetHub. Secrets go in /opt/meethub/meethub.env, never here.'
  sed -e 's|^data_dir = "\./data" |data_dir = "/opt/meethub/data"|' \
    -e 's|^ffmpeg_path = "ffmpeg"   |ffmpeg_path = "/opt/meethub/bin/ffmpeg"|' config.toml.example
} >"$app/config.toml"
grep -q '^data_dir = "/opt/meethub/data"' "$app/config.toml" || {
  echo "build.sh: config.toml.example no longer has the data_dir line this script rewrites" >&2
  exit 1
}
grep -q '^ffmpeg_path = "/opt/meethub/bin/ffmpeg"' "$app/config.toml" || {
  echo "build.sh: config.toml.example no longer has the ffmpeg_path line this script rewrites" >&2
  exit 1
}
chmod 0644 "$app/config.toml"

# ── tar.gz: the folder alone (meethub/…), with the unit beside the binary, for an install by hand ──
units="meethub.service meethub-settings.service meethub-settings.path"
for u in $units; do install -m 0644 "$here/$u" "$app/$u"; done
tar -C "$root/opt" --owner=0 --group=0 --numeric-owner -czf "$out/meethub_${v}_linux_amd64.tar.gz" meethub
for u in $units; do
  rm "$app/$u"
  install -D -m 0644 "$here/$u" "$root/usr/lib/systemd/system/$u"
done

# ── rpm ──
sed "s/@VERSION@/$v/" "$here/meethub.spec" >"$work/meethub.spec"
rpmbuild -bb --quiet --target x86_64 \
  --define "_topdir $work/rpm" --define "_stagedir $root" --define "_scripts $here" \
  --define "_rpmdir $out" --define "_rpmfilename meethub-$v-1.x86_64.rpm" \
  "$work/meethub.spec"

# ── deb ──
size=$(du -sk "$root" | cut -f1)
mkdir "$root/DEBIAN"
sed -e "s/@VERSION@/$v/" -e "s/@SIZE@/$size/" "$here/control" >"$root/DEBIAN/control"
printf '%s\n' /opt/meethub/config.toml /opt/meethub/meethub.env >"$root/DEBIAN/conffiles"
install -m 0755 "$here/preinstall.sh" "$root/DEBIAN/preinst"
install -m 0755 "$here/postinstall.sh" "$root/DEBIAN/postinst"
install -m 0755 "$here/preremove.sh" "$root/DEBIAN/prerm"
install -m 0755 "$here/postremove.sh" "$root/DEBIAN/postrm"
dpkg-deb --root-owner-group -Zxz --build "$root" "$out/meethub_${v}_amd64.deb" >/dev/null

(cd "$out" && sha256sum "meethub_${v}_amd64.deb" "meethub-$v-1.x86_64.rpm" "meethub_${v}_linux_amd64.tar.gz" >SHA256SUMS)
rm -rf "$work"
echo "packages of MeetHub $v:"
ls -l "$out"
