# Built by packaging/build.sh from an already staged tree (_stagedir); nothing is compiled here.
Name:           meethub
Version:        @VERSION@
Release:        1
Summary:        Self-hosted team chat, files and video meetings
License:        MIT
URL:            https://github.com/jniltinho/meethub
BuildArch:      x86_64
AutoReqProv:    no
Requires(post): shadow-utils
Recommends:     (redis or valkey)

# a static Go binary: no debuginfo package, no stripping, no build-id links
%global debug_package %{nil}
%global __os_install_post %{nil}
%global _build_id_links none

%description
MeetHub is a team chat with channels, direct messages, threads, file attachments and video meetings (through
LiveKit), served by one static binary with the web app embedded.

Everything is installed in /opt/meethub and runs as the meethub user under systemd (meethub.service). It needs a
Redis server; the database is SQLite by default, or MariaDB or PostgreSQL.

%install
cp -a %{_stagedir}/. %{buildroot}/

%files
%dir /opt/meethub
%dir /opt/meethub/bin
/opt/meethub/bin/meethub
/opt/meethub/bin/ffmpeg
/opt/meethub/licenses
%config(noreplace) /opt/meethub/config.toml
%config(noreplace) %attr(0640,root,root) /opt/meethub/meethub.env
/opt/meethub/LICENSE
/opt/meethub/NOTICE
/usr/lib/systemd/system/meethub.service
/usr/lib/systemd/system/meethub-settings.service
/usr/lib/systemd/system/meethub-settings.path

%pre -f %{_scripts}/preinstall.sh
%post -f %{_scripts}/postinstall.sh
%preun -f %{_scripts}/preremove.sh
%postun -f %{_scripts}/postremove.sh
