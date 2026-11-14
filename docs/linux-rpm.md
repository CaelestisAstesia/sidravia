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
installation, upgrade or removal, and no transaction script stops user processes.

Exit this installation's GUI and daemon before upgrading or removing it. The
package manager does not coordinate a running multi-process application; an
in-place upgrade while it is running is not supported. A separate portable
instance is neither adopted nor stopped. Configuration uses the existing XDG
Sidravia paths; RPM does not migrate data from a portable copy or remove any
user data. For isolated namespaces, configuration/preferences/runtime/logs use
`$XDG_CONFIG_HOME/Sidravia/namespaces/<namespace>` (fallback `~/.config`).
Default configuration/preferences use `$XDG_CONFIG_HOME/Sidravia`, while the
production daemon runtime/logs use `$XDG_CACHE_HOME/Sidravia`.

Optional GUI or background-only XDG login autostart is still an explicit user
setting. Disable it before uninstalling; RPM does not enumerate or modify user
home directories. Existing entries survive upgrades at the stable executable
path. A stale entry after removal cannot start an uninstalled executable.
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
