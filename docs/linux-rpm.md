# Linux RPM candidate

Initial target: Fedora 45 x86_64, KDE/Wayland. This unsigned upstream binary
package is not a Fedora repository submission or a universal Linux build.
Flutter/AOT runtime stays private in `/usr/lib64/sidravia`; GTK, WebKitGTK 4.1,
Ayatana AppIndicator and the other ELF dependencies come from system RPMs.
Flutter engine, Dart package and bundled font notices ship with the application.

The package owns `/usr/bin/sidraviactl`, `sidraviad`, `sidraviaupdater` and
`sidravia_gui`, plus its system desktop entry and hicolor icon. PATH discovery
is managed by RPM; the GUI does not create redundant `~/.local/bin` links.
The updater refuses archive replacement of an RPM installation. Use the system
package manager for upgrades. No system service or autostart is enabled by
installation, upgrade or removal. Maintenance scripts stop only processes from
this exact RPM installation, without reading account configuration or user homes.

Upgrade and removal automatically request graceful GUI/session/daemon exit.
While the real RPM transaction lock is held, new GUI/CTL/daemon/updater starts
from this installation are rejected. The short-lived guard observes that lock
and the package-manager host pidfd, so a persistent Discover/PackageKit backend
does not leave the gate locked after its transaction. No permanent service is
installed and no process is automatically relaunched after upgrade.

An unresponsive application or unsaved configuration draft blocks maintenance
with a bounded error. Save your edits and retry in that case. The scripts never
use SIGKILL. Independent portable copies and other installation roots are not
stopped. The unpublished pre-coordination prototype has no startup gate or
system-exit handler: exit that legacy installation once, and do not launch it
during its first upgrade. A running legacy installation is refused rather than
terminated and risking its unsaved work. Subsequent coordinated RPM versions
support the automatic live lifecycle described above.

Configuration uses the existing XDG
Sidravia paths; RPM does not migrate data from a portable copy or remove any
user data. For isolated namespaces, configuration/preferences/runtime/logs use
`$XDG_CONFIG_HOME/Sidravia/namespaces/<namespace>` (fallback `~/.config`).
Default configuration/preferences use `$XDG_CONFIG_HOME/Sidravia`, while the
production daemon runtime/logs use `$XDG_CACHE_HOME/Sidravia`.

Optional GUI or background-only XDG login autostart is still an explicit user
setting. Newly registered RPM entries use exact `TryExec` paths: entries survive
upgrades at stable executable paths, and are automatically ignored after removal.
They need not be manually disabled before uninstalling. Reinstalling makes the
retained explicit user choice available again. RPM never enumerates or edits
user home directories. Exact older entries are recognized and migrated when
explicitly setting autostart again; prototype entries without TryExec need that
one-time refresh. No real account has been registered by the candidate tests.
This is desktop login autostart, not pre-login boot authentication.

Local development/acceptance uses `tools/developer/dev.py linux --rpm` and
`--verify-rpm-upgrade`; see its README. Acceptance installs real RPMs into a
private root/RPM database cloned from the current Fedora dependency inventory,
then mounts the installed payload over read-only host runtime libraries in an
unprivileged sandbox. Dependency checks remain enabled. This tests transactions
and installed behavior on this host; it is not a clean Fedora dependency solve,
fresh login, campus authentication, other distribution or public release test.
The initial upgrade test is RPM Release 1 to 2 of the same product/source,
not a cross-version user-data migration test. Unsigned-candidate verification
disables signature acceptance explicitly while retaining all digest checks.
Host RPM plugins (including SELinux labeling and shutdown inhibition) are
disabled inside the user namespace; ordinary desktop/icon file triggers run
and their cache outputs are checked. Host SELinux installation is not accepted.
