# Linux GUI portable

Initial supported host: Fedora 45 x86_64, KDE Plasma on Wayland. This archive
is relocatable; it is not a statically linked or universal Linux application.
GTK 3, GLib, libstdc++, glibc, Wayland, Ayatana AppIndicator GTK3 and
WebKitGTK 4.1 remain system dependencies. Fedora runtime packages:

```
sudo dnf install gtk3 libayatana-appindicator-gtk3 webkit2gtk4.1
```

Extract the entire archive into a writable directory and run `./sidravia_gui`.
Keep `lib/` and `data/` beside the executable. Run `ldd ./sidravia_gui` to inspect
missing libraries. No account configuration or autostart is registered by
extracting or opening the application. Runtime data stays beside this portable
copy: `config/`, `runtime/`, `logs/`, and `config/gui-settings.json`. Isolated
`SIDRAVIA_NAMESPACE` runs use `namespaces/<namespace>/` instead. Do not share a
writable portable copy between untrusted accounts.

Settings supports GUI or background-only autostart after login to a desktop
session, using one XDG Autostart entry per namespace. Its location is
`$XDG_CONFIG_HOME/autostart` (absolute XDG path), or `$HOME/.config/autostart`.
GUI mode starts the exact GUI with `--start-hidden`; without a tray host the
main window remains accessible. Background mode starts the same package's
`sidraviactl daemon start --log-level info`, without a GUI or tray. It is not a
system boot service and does not authenticate before login. Automatic
authentication is a separate explicit preference.

Changing or disabling autostart only changes future launches. It does not stop
or adopt an already running daemon. Use this package's `sidraviactl daemon stop`
to stop background mode before opening the GUI; desktop/headless ownership
conflicts are reported rather than silently taking over.

The user PATH switch publishes exact symlinks for `sidraviactl`, `sidraviad`,
`sidraviaupdater` and `sidravia_gui` in `$HOME/.local/bin`. It never replaces
unrelated files or links, and disabling it removes only this package's links.
It does not rewrite shell startup files or the environment of existing processes.
Fedora's default bash configuration adds this directory for a new shell; if your
shell/session omits it, the settings message explicitly asks you to add it to
PATH or log in again. A same-name command earlier in PATH is also reported.
PATH registration is user-global command discovery. Runtime namespace still
comes from the calling process's `SIDRAVIA_NAMESPACE`; command links do not
freeze the GUI's namespace. An incomplete registration is reported and can be
removed before retrying.

Disable autostart and user PATH registration before moving or deleting an
enabled portable copy, then re-enable them from the new location. A conflicting entry from another location
is reported and preserved; the application never silently takes ownership.
Move the whole directory only after exiting the GUI and its daemon. For an
upgrade, keep a separate extracted copy and transfer only your private data
after both instances exit; never extract an archive over a running application.

Flutter and dependency notices ship in `FLUTTER-LICENSE` and
`data/flutter_assets/NOTICES.Z` and `data/licenses/`. System libraries retain their RPM licenses.
This build has not been accepted on other distributions, desktop environments,
architectures, X11, or a real Windows installation. Notification delivery and a
fresh login autostart require desktop session acceptance separately.

Linux GUI candidates use the release asset suffix `-linux-amd64-gui-portable.tar.gz`.
The updater recognizes this package kind automatically in a GUI portable layout.
A missing GUI release does not fall back to a core-only asset. Check, download and
integrity verification are supported; automatic installation remains unavailable.
