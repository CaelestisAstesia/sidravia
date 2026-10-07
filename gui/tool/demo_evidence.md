# R3: shared interactive offline Demo and equivalent home responsiveness

Baseline: `codex/gui-platform-boundary@49520cf25078bb60cd53fd3f4bacae29b05b77f7`.
This slice is locally committed for review; no push or merge. Aesthetic approval,
automatic validation and Windows interaction acceptance remain separate.

## Implementation

- `lib/dev/preview_main.dart` and the existing fixed 398×642 screenshot test are
  unchanged. Their HomeFixture remains a fixed-data screenshot input.
- `lib/dev/interactive_demo_main.dart` composes the actual SidraviaShell. Its routes
  instantiate the actual HomePage/HomeView/ConfigurationPage/SettingsPage/AdvancedPage
  and existing announcement sheet. It displays a persistent yellow offline label and
  the fake client's explicit simulated operation feedback. No preview-only page exists.
- `lib/dev/offline_demo_client.dart` implements the existing typed interface in memory,
  supplies a fake bootstrap and fake announcement fetcher, and uses MemoryAnnouncementStore.
  It never constructs a real process launcher, socket, HTTP fetcher, SharedPreferences
  instance or credential store. Password text is never retained; only a simulated boolean
  is stored. No real account, network or authentication is used. Production main,
  GuiController and capabilities are unchanged.
- Home is centered and capped at620 including padding; wide padding16/36/32 is determined
  from the full available content viewport before applying that cap. 758 content pixels
  correspond to760 HTML frame pixels. Other widths retain12/24/26. Context text is capped
  at310; 92 is a minimum, not a fixed height. The page fills a tall viewport but its children
  stay in natural flow. Short windows scroll; no whole-page scale or pinned announcement.
- Other pages and their existing unsupported/disabled controls were reused without edits.
  Disabled appearance, auto-reconnect, delete and diagnostic-export controls are not
  represented as implemented features.

## Automatic evidence

All final commands completed successfully unless separately listed under attempts.
Run Flutter commands from the repository `gui/` directory.

```sh
dart format lib/features/home/home_page.dart lib/dev/interactive_demo_main.dart lib/dev/offline_demo_client.dart test/offline_demo_test.dart test/home_responsive_test.dart
flutter analyze
flutter test test/offline_demo_test.dart test/home_responsive_test.dart test/home_visual_test.dart --reporter expanded
flutter test --dart-define=HOME_RESPONSIVE_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R3/evidence test/home_responsive_test.dart --reporter expanded
flutter test --reporter compact
```

- Analyzer: no issues. Focused tests:20 passed. Responsive candidate tests:12 passed.
  Final full suite:129 passed. No thresholds relaxed, assertions deleted, skipped tests
  introduced or approved golden overwritten.
- Production Shell route clicks cover home→configuration→save/back,
  home→settings→configuration→back→settings→home, home→details→diagnostics→back.
  Configuration update and auto-login switch assert actual fake-client operations.
- Home disconnect/start clicks use the actual GuiController and assert typed-client
  calls, retained session state and text feedback. Announcement click opens the actual
  BottomSheet, asserts body and read state. Existing operation-error/update tests remain.
-69 continuous width/height steps from320 to1000 validate620 page cap, fixed44 mark and
  absence of overflow. Intermediate518×602,757×642,759×642,918×732, narrow298×542,
  short398×280 and long strings are also exercised. Long context grows beyond92 and
  status beyond258, and a short viewport can scroll to and click the announcement.

## Rendered measurement

Original `Sidravia_Final.html` SHA256 is unchanged:
`53b0f460a6aba89ae1397747b89294a4f6055c9dde2d118055c2b34c54a4e813`.
The reference browser is Chromium153 via Playwright1.63.0, DPR1, light/connected/active.
1280×1000 viewport is used for prescribed sizes; the extra large probe uses1600×1000.
Flutter widget captures use DPR1/textscale1, loaded HarmonyOS Sans Regular/Medium/Bold,
MaterialIcons and existing local Segoe UI Symbol. No fonts were swapped in product code.

| HTML frame | Measured content | Page width | Page padding top/horizontal/bottom |
|---|---|---|---|
| reference400×690 |398×642|398|12/24/26|
| compact400×620 |398×572|398|12/24/26|
| tall400×780 |398×732|398|12/24/26|
| wide760×590 |758×542|620|16/36/32|

The two1px borders and46px reserved top area are excluded from the content capture.
For the extra requested1000px HTML frame, the original preview-board max-width920
compresses it to920; measured content918 is compared with Flutter918, not stretched
into998. Standalone Flutter widths through1000 remain covered by the continuous test.

Default candidate pixel data is exactly identical to the49520cf candidate: **zero
changed channels**. This protects the existing result; it is not new golden approval.
In all four prescribed sizes, header/status/mark/actions/divider/notice coordinates and
sizes match HTML. Context top differs0.359px from fractional HTML line boxes. Wide page
height554 vs viewport542 naturally scrolls12px; short viewport scrolls264px. The long
case grows to833px in Flutter vs837.531px in HTML, a4.531px text-metric difference.
Reference uses Microsoft YaHei UI/Segoe UI while Flutter retains HarmonyOS Sans; font
and icon raster differences remain visible in overlays and are not claimed resolved.
Each of11 cases includes reference-window/content, Flutter content, overlay, absolute
channel difference, both measurement JSONs and geometry-comparison.md. No image scaling.

From repository root, using only isolated temporary Node dependencies:

```sh
NODE_PATH=/tmp/sidravia-visual-tools/node_modules \
FONTCONFIG_FILE=/tmp/sidravia-visual-tools/fonts.conf \
LD_LIBRARY_PATH=/tmp/sidravia-visual-tools/libs/usr/lib/x86_64-linux-gnu \
node gui/tool/capture_home_responsive.cjs /tmp/sidravia-reference-r2/Sidravia_Final.html /mnt/d/Downloads/Sidravia-Offline-Demo-R3/evidence
```

## Demo, build and recording

An isolated source copy is prepared by `python3 gui/tool/prepare_demo_build.py <new-directory>`.
It copies only GUI source, excluding tests/archive/cache, and records source SHA256 in
DEMO-SOURCE-MANIFEST.json. Native/web copies used final identical HomeView and demo source.

Native build: Windows Flutter3.47.0 at `D:\Tools\Flutter\3.47.0\flutter`, from
`D:\code\sidravia-demo-r3\gui`, via CMD:

```bat
D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart
```

Final native build passed in31s. The portable GUI-only Demo is delivered separately;
no sidraviactl/sidraviad or config/runtime/credential data is included. Double-click
`打开离线 Demo.cmd` in the desktop Demo folder. For source debug use
`flutter run -d windows -t lib/dev/interactive_demo_main.dart` from a Windows GUI
checkout; this debug command was not run. Production `lib/main.dart` is not replaced.

For recording only, web platform files were generated in a separate temporary copy:

```sh
flutter create --platforms=web --project-name=sidravia_gui .
flutter build web --release --no-web-resources-cdn -t lib/dev/interactive_demo_main.dart
```

Web build passed. The build reports an unused CupertinoIcons family warning; Material
icons and all displayed controls were inspected. No dependency was added to silence it.
Flutter's browser font fallback initially requested exact Noto font files for existing
Material buttons.18 engine-requested resources(539612bytes) were downloaded during setup
and cached in the temporary web build, then fontFallbackBaseUrl pointed to localhost.
Existing Segoe UI Symbol was registered only in that temporary manifest. Neither those
resources nor the Windows system font enter repository/Windows package. Runtime then
performed zero external requests; no font substitution aliases were used.

```sh
NODE_PATH=/tmp/sidravia-visual-tools/node_modules \
FONTCONFIG_FILE=/tmp/sidravia-visual-tools/fonts.conf \
LD_LIBRARY_PATH=/tmp/sidravia-visual-tools/libs/usr/lib/x86_64-linux-gnu \
node gui/tool/record_offline_demo.cjs /mnt/d/code/sidravia-demo-r3-web/gui/build/web /mnt/d/Downloads/Sidravia-Offline-Demo-R3/recording-complete
```

Recording is the actual compiled Flutter app with memory injection, not a static image
sequence or a separately implemented HTML UI. It lasts29.44s,1100×900,25fps.19 actual
operation nodes include config save, settings/details/diagnostics, announcement, simulated
disconnect/reconnect, six continuous viewport changes(24 intermediate steps each) and
short-window scrolling. recording.json shows no page errors and no blocked external
requests. Initial/config/modal/action/resize screenshots and actual decoded video frames
at12s and25s were personally inspected. The video margins are capture canvas; UI is not
scaled to fill them. This is WSL Chromium viewport evidence, **not native Windows window
dragging or button acceptance**. Native Windows interactive execution remains unverified.

## Attempts and limitations

- Windows Computer Use failed initialization and after reset with sandboxCwd not a local
  file URI; no alternate PowerShell UI automation was used.
- First CMD launch used a WSL UNC cwd and could not find pubspec; corrected to the isolated
  Windows project directory. Source did not change because of this environment error.
- Odd browser frame positions made locator screenshots one pixel wider; the whole
  reference frame was moved to integer origin before clipping, without scaling or CSS
  content changes. Extra large reference was measured against its actual920 container.
- Initial title center property altered1919 glyph channels despite same rectangle.
  A recorded Recovery restored the original alignment; final guard returns zero.
- Browser fill did not update Flutter's editable state; actual keyboard typing is used
  in the final recording, and saved demo-student02 is visible.
- Temporary font file was read-only when reused; registration now avoids overwriting
  existing assets. Initial font network requests were blocked; complete recording
  uses the exact cached fallback resources and no external request.
- Playwright's stripped ffmpeg lacks fps filter. Timestamp seeking with `-ss12/-ss25`
  successfully decoded actual video frames instead. No final video was modified.
- Automatic/final builds passed. Native Windows interaction, live daemon, campus, dark,
  seven-state visual reconstruction and redesign of other pages were not run/accepted.

Protected scope diff against49520cf is empty: Go, IPC contract/client, GuiController,
capabilities, bootstrap, credentials, announcement controller, production main, global
styles, existing other pages, Windows runner/title/tray, fixed preview and old candidates.
Only home responsive presentation, dev-only injection/entry, tests and tools/report change.
