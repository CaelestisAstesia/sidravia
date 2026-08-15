# Sidravia GUI

Sidravia `0.2.0` test-line Flutter visual MVP.

Windows is the current delivery target. The MVP presents Home, Configuration
and Settings with a shared responsive navigation model. It bootstraps a
desktop-owned daemon through the sibling `sidraviactl.exe`, uses typed local
WebSocket IPC, and exposes one unambiguous Configuration/Session flow. It does
not yet integrate with the tray or configure startup/PATH.

The application bundles and uses HarmonyOS Sans. Its license is included at
`assets/fonts/LICENSE.txt` and is available in Settings from Flutter's standard
license page.

The generated Android runner preserves a future shared-client path; it is not
an Android build or support claim.

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
