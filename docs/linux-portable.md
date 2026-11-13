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

Settings can enable GUI autostart after login to a desktop session. This uses
an XDG Autostart desktop entry under `$XDG_CONFIG_HOME/autostart` (absolute XDG
path) or `$HOME/.config/autostart`. It starts this exact executable with
`--start-hidden`; without an available tray host the main window remains
accessible. It is not a system boot service and does not authenticate before
login. Automatic authentication is a separate explicit preference.

Disable autostart before moving or deleting an enabled portable copy, then
re-enable it from the new location. A conflicting entry from another location
is reported and preserved; the application never silently takes ownership.
Move the whole directory only after exiting the GUI and its daemon. For an
upgrade, keep a separate extracted copy and transfer only your private data
after both instances exit; never extract an archive over a running application.

Flutter and dependency notices ship in `FLUTTER-LICENSE` and
`data/flutter_assets/NOTICES.Z` and `data/licenses/`. System libraries retain their RPM licenses.
This build has not been accepted on other distributions, desktop environments,
architectures, X11, or a real Windows installation. Notification delivery and a
fresh login autostart require desktop session acceptance separately.
