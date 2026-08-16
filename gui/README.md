# Sidravia GUI

Sidravia `0.2.0` test-line Flutter visual MVP.

Windows is the current delivery target. The MVP presents Home, Configuration
and Settings with a shared responsive navigation model. It bootstraps a
desktop-owned daemon through the sibling `sidraviactl.exe`, uses typed local
WebSocket IPC, and exposes one unambiguous Configuration/Session flow. Tray
presence and explicit exit are implemented; startup and PATH integration are
not part of this slice.

The application bundles and uses HarmonyOS Sans. Its license is included at
`assets/fonts/LICENSE.txt` and is available in Settings from Flutter's standard
license page.

## Desktop presence

The Windows runner keeps one per-user GUI presence. A second launch restores
and foregrounds the existing window instead of starting a second Flutter
engine or desktop daemon. Title-bar close, Alt+F4 and system-menu Close hide
the window to the tray; the tray `打开 Sidravia` restores it and the tray
`退出并断开` performs the only ordinary full exit. That exit submits the
existing typed `daemon.stop` with a bounded wait before destroying the window,
and falls back to the desktop owner watcher if the stop or transport fails.
The tray icon is recreated after an Explorer restart. Only the first entry into
an actionable blocked state and the first recovery from it produce short
process-local notifications; all other states, polling revisions and dismissed
notifications stay silent. Desktop UI, tray or notification failure never
changes or blocks authentication, polling or announcements.

Notifications use `Shell_NotifyIcon` balloon presentation for this unpackaged
prototype; final identity, toasts and packaging remain GP-03 decisions.

The generated Android runner preserves a future shared-client path; it is not
an Android build or support claim.

## Announcement feed

The GUI can read versioned announcements from a fixed static HTTPS JSON feed.
The request goes directly from the Flutter process over HTTPS; it never passes
through the daemon or local IPC, and it carries no account, token, cookie or
network state. The endpoint is compile-time only:

```sh
flutter run -d windows --dart-define=SIDRAVIA_ANNOUNCEMENT_FEED_URL=https://notices.example.edu/feed.json
```

An empty or invalid value (anything but an absolute `https` URL without
user-info or fragment) disables the feature entirely: no request is made and
no announcement surface is shown. Without a configured endpoint the GUI simply
does not enable announcements. No production endpoint is hosted yet; the
domain, hosting and content publication are a separate external delivery gate.

The feed is a strict schema-1 document (at most 20 items, at most 65536
uncompressed response bytes):

```json
{
  "schema": 1,
  "generatedAt": "2026-08-18T08:00:00Z",
  "items": [
    {
      "id": "maintenance-2026-08",
      "revision": 1,
      "level": "maintenance",
      "title": "校园网维护",
      "body": "8 月 20 日 00:00 至 02:00 期间认证可能中断。",
      "publishedAt": "2026-08-18T08:00:00Z",
      "startsAt": "2026-08-18T08:00:00Z",
      "expiresAt": "2026-08-20T18:00:00Z",
      "actionLabel": "查看详情",
      "actionUrl": "https://notices.example.edu/maintenance-2026-08"
    }
  ]
}
```

`level` is one of `info`, `maintenance` or `critical`. Items are shown only
while `startsAt <= now < expiresAt`. An `actionUrl`, when present, must be an
HTTPS URL on the exact same origin as the feed endpoint and opens only in the
system external browser.

The last successful feed, its validators, and the most recent 128 read and
dismissed keys are kept as non-critical local preferences. Fetch, decode,
cache or link failures never change, block or degrade authentication: the GUI
keeps showing the unexpired cache or simply no announcements.

```sh
flutter analyze
flutter test
```

## Visual development

Run the production GUI in debug mode for real-window hot reload:

```sh
flutter run -d windows
```

Run the isolated visual catalog when no daemon or packaged sibling executables
should be involved:

```sh
flutter run -d windows -t lib/dev/preview_main.dart
```

The catalog uses fictional in-memory snapshots and lets a developer switch
page and state while editing. Saving a Dart file hot reloads the running
window. It never launches `sidraviactl`, opens IPC or changes user data.

Flutter 3.47 also provides the experimental browser Widget Previewer:

```sh
flutter widget-preview start
```

Use Flutter Inspector from DevTools while either debug app is running to select
widgets, inspect constraints and padding, and visualize Row/Column layout.
