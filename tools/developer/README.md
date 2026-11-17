# Development commands

Run from the repository root with Python 3 and the pinned Go toolchain (Go 1.26.4).
`dev.py` uses the existing verifier and distribution producer/consumer. It does
not install, authenticate, sign, upload or publish. Only explicit development
preview/integration modes start their own GUI or isolated fixture processes.

## Offline interactive GUI preview

```bash
.local/bin/dev preview
.local/bin/dev preview --check
```

The first command serves the existing Flutter offline demo at
`http://127.0.0.1:7357`; `--port` changes the loopback port. `--flutter` selects
the pinned SDK executable (default `.local/bin/flutter`). The version must
match `gui/.flutter-version`; dependencies use `--enforce-lockfile`.
`--check` runs analysis, the offline demo/layout tests, and its Web build.
Both modes use local CanvasKit resources rather than a rendering CDN.
CanvasKit cannot discover desktop fonts. The helper temporarily copies a
Chinese-capable host font into an ignored, owned `gui/web/assets/preview-font-*`
directory and loads it only in the demo. Linux selects it through Fontconfig;
Windows uses Microsoft YaHei. `--font /path/to/font.ttf` overrides the choice.
The temporary source copy is removed on exit; the checked Web build retains its
copy for local viewing. Native product bundles do not contain this preview font.
Invoking Flutter's demo target directly without this define may fetch fallback
fonts from the network.

Widget capture modes require `--dart-define=SYSTEM_FONT_MANIFEST=/absolute/path.json`.
The JSON maps platform font family names to lists of installed font file paths;
it supplies real fonts to the test runner, whose system font discovery is disabled.
Ordinary widget tests still run without this capture-only input.

The demo reuses production pages with fictional in-memory clients and settings.
It does not launch the daemon, read production credentials, contact the school,
or register PATH/autostart. Browser refresh resets the demo. Press `r` for hot
reload, `R` for hot restart, and `q` or Ctrl+C to stop the foreground server;
refresh the browser if it has not picked up an update. A Web hot restart produced
a `disposed EngineFlutterView` assertion during acceptance; its cause is not yet
established. Refresh recovered the page, and ordinary hot reload plus subsequent
interaction produced no new errors. Prefer `r` and refresh after `R`.
Native tray, notifications,
window controls and announcement hosting still require desktop verification.

```bash
python3 tools/developer/dev.py verify --scope go --go-checks native
python3 tools/developer/dev.py verify --scope go --go-checks cross
python3 tools/developer/dev.py verify --scope all
python3 tools/developer/dev.py benchmark --scope go --go-checks full --runs 3 --cache cold-warm
```

Use `--go /absolute/path/to/go` when the pinned Go is not on PATH. Normal runs
preserve Go's configured/default compile cache; do not allocate a new cache per
task. Tests still use `-count=1`, which reruns tests without discarding compiled
objects. `--jobs 2` bounds independent cross-compilation commands (1–4 supported);
Go retains its own compiler scheduling. All existing cross targets are checked.
Native tests and vet retain their order. For a small code change, use ordinary
`go test -count=1 ./affected/package/...` rather than requiring a full matrix.

Verification, benchmark and core packaging invocations write a unique report and complete logs under
`.project/evidence/build-efficiency/` by default (`--reports` overrides it).
Reports contain source commit, dirty status, byte fingerprint, tool/cache/host
metadata, command exits and timings. Failed checks return nonzero; source changes
during measurement invalidate the result. `cold-warm` uses one temporary empty
Go compile cache for the first run and reuses it for subsequent runs, then removes
it. Module/download and OS caches are not cleared. Compare repeated runs with the
same flags and uncontended host; the first existing-cache run has unknown warmth.

## Incremental Windows GUI and distribution

Run from Windows or WSL with Windows PowerShell interop:

```bash
python3 tools/developer/dev.py windows
python3 tools/developer/dev.py windows --check-gui
python3 tools/developer/dev.py windows --package --windows-python 'C:\Python314\python.exe' --iscc 'C:\Tools\Inno Setup\ISCC.exe'
```

The last command's Python and Inno paths are examples; supply installed native
executables. Defaults are `D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat`,
`C:\Program Files\Go\bin\go.exe`, and workspace
`D:\Tools\Sidravia\build-workspace`. Override with `--flutter`, `--windows-go`,
`--workspace`, and `--version`. No tool is downloaded or installed automatically.

The helper accepts only a new empty directory or its own marked workspace and
holds an exclusive lock. It synchronizes tracked source by SHA256, retains
unchanged file timestamps, removes only previously owned stale source files, and
keeps Flutter/CMake and Go caches on the native Windows filesystem. Interrupted
syncs retain ownership of partly copied files for the next run. Source must have
no untracked, nonignored files; build-only runs may be dirty and say so.

Every GUI request executes locked dependency resolution and incremental Release
compilation; there is no routine `flutter clean`, fresh source copy, or skip based
on EXE existence. `--check-gui` additionally runs analyze and GUI tests, stopping
on failure. Plain build-only success is not test acceptance. The package path
requires a clean commit, rechecks staged source bytes after compilation, invokes
`tools/build`, then `verify_distribution.py --smoke`. It also produces and verifies
a separate Windows core portable archive without GUI or an installer. The
report records `distribution` (GUI portable + setup) and `core_distribution`. Smoke invokes only allowed
help commands, never the installer or GUI. Reports/logs/artifacts are in the
workspace's unique `runs/` directory. Each stage streams stdout and stderr to
separate files. The helper bounds stage execution to 900 seconds and output
drain to 5 seconds; direct PowerShell callers can override `-StageTimeoutSeconds`.
On failure it saves the report, then exits. A Windows job contains this dedicated
PowerShell process and its descendants, so exit also terminates its owned tools.
Invoke the helper as a separate process (`-File`), never dot-source it.

Successful native compilation does not prove
GUI interaction, installation, notification delivery or campus behavior.

## Core distributions

Distribution archives use `-gui-portable` or `-core-portable` before `.zip`/`.tar.gz`, matching updater asset selection. Windows GUI and core archives therefore have distinct public asset names.
ZIP and gzip archives use the standard library's best compression level; their
formats, content and extraction requirements are unchanged.

From a clean Linux amd64 checkout, choose an absent output directory whose parent
exists (the repository's ignored `dist/` path is suitable):

```bash
python3 tools/developer/dev.py package --version 0.4.0-alpha.1 --output dist/linux-amd64
python3 tools/developer/dev.py package --target linux-arm64 --version 0.4.0-alpha.1 --output dist/linux-arm64
python3 tools/developer/dev.py package --target windows-arm64 --version 0.4.0-alpha.1 --output dist/windows-arm64
```

ARM64 packages contain core tools only (Windows includes the manager). They are
cross-compiled with CGO disabled and ARMv8.0 baseline. The consumer inspects
archive content, checksums and Go OS/architecture metadata without executing
foreign binaries; help smoke is recorded as `not_run`. Cross-compilation does
not establish ARM hardware compatibility. GUI and installers remain Windows
AMD64 only. The default Linux AMD64 package also runs native help smoke.

The full commit is passed as build-id; producer and consumer remain the existing
implementations. Windows AMD64 distribution must use `windows --package`, which builds
the GUI from synchronized source. A generic existing GUI directory is not accepted
as proof of current source. Go `trimpath`'s `embedded_identity=not_recorded`
limitation remains; source/build receipts and byte hashes are not independent
runtime identity proof.

Keep the latest useful build and evidence; do not automatically retain every log
or create new task caches. Remove only a helper-owned inactive run directory when
its result is no longer needed. Preserve the shared cache and active candidate.

## Isolated Windows update handoff contracts

The Python verification scope includes an opt-in native Inno/Go fixture. Set
`SIDRAVIA_ISCC` to the pinned Inno 7.1.0 compiler and `SIDRAVIA_INNO_TEST_ROOT`
to an existing dedicated native Windows scratch directory (WSL uses `/mnt/...`
paths). Run `python3 tools/developer/dev.py verify --scope python`. Without both
variables the native cases are reported as skipped, not as Windows evidence.

The fixture starts only synthetic executables, gives its synthetic updater a
kill-on-close job, and replaces a dummy file after the updater exits. It checks
short-host target resolution, one-time recovery plans, refusal on exit timeout,
and refusal when the parent job cannot release the installer. It does not start
Sidravia, access credentials, authenticate, register an installation, change PATH
or autostart, or establish production GUI/CTL lifecycle compatibility. Temporary
files belong to the fixture; the verifier report remains under `.project/`.

## Updater foundation

The distribution now includes `sidraviaupdater`; CTL delegates `update check`,
`update download --candidate ID`, `update status` and `update version` to the
sibling executable. Optional `--channel stable|preview` and
`--package setup|gui-portable|core-portable` select the GitHub candidate. Check
uses the Releases list and SemVer ordering; download requires the exact freshly
selected candidate and verifies size/SHA256. JSON responses use schema version 1.
A protected user cache retains one verified package per target/type/channel;
repeated checks preserve it and repeated downloads reuse it. Unexpected cache
paths or changed bytes fail closed and require cache inspection.

This is foundation work: `update apply` returns `apply_unavailable`, and candidates
report `canApply=false`. The signature verifier is tested using synthetic keys;
no publisher key is configured. GUI scheduling, unattended apply, portable replacement and signed release
integration remain pending. The Windows application now exposes fixed internal
prepare/acknowledge/commit/abort stages; CTL is a short host, and the installed
Inno path uses these stages when the target CTL advertises protocol 1. Older
targets require manual exit. Normal process entry points share Inno's current-user
maintenance gate. Preparation cancellation thaws the existing application;
cancellation after a confirmed stop only restarts unchanged source binaries,
using a cold skip-auto-login generation and an at-most-once intent dispatch.
A new user mutation revokes recovery admission. Once file replacement starts,
the installer never automatically restarts the old program on failure.
The native fixture also runs the release/command tests, real CTL-to-updater
version/disabled-apply smoke and isolated real CTL/daemon lifecycle tests. GUI
unit/widget checks and compilation are separate from actual Windows GUI and
installation/upgrade acceptance. Tests never authenticate with real credentials.

To additionally run the real Windows GUI lifecycle, set
`SIDRAVIA_GUI_TEST_DIRECTORY` to the fixed builder's current Release directory.
The fixture copies only GUI runtime files into its owned portable target, uses a
fresh namespace and no credentials, checks bounded startup refusal under the
installer mutex, and exercises prepare/cancel, normal exit and cold recovery.
This may briefly show an empty test GUI. A failure preserves the fixture and
logs; success waits for its owned processes to exit before removing files.

After producer/consumer verification, `SIDRAVIA_CANDIDATE_ARCHIVE` may name the
exact GUI/core portable ZIP to repeat the real lifecycle on its unmodified
binaries. The fixture reads the package BUILD-INFO identity and reports it in
the receipt; it never executes the setup package or changes an existing install.

## Native Linux desktop development

Use the pinned Go and Flutter SDKs. On Fedora install native RPM build/runtime
headers: `clang gtk3-devel libayatana-appindicator-gtk3-devel webkit2gtk4.1-devel wayland-devel`
(CMake, Ninja, pkg-config and C++ standard-library development files are also
required). The integration fixture also uses system Python GObject bindings
(Fedora `python3-gobject-base`) and the current Wayland/session D-Bus. Ayatana supplies the StatusNotifier tray menu; WebKitGTK supplies an
in-app, ephemeral announcement view. No custom window positioning is promised
under Wayland: the compositor controls placement and focus.

```
.local/bin/dev linux --check-gui
.local/bin/dev linux --integration
```

The first command checks shared native frame geometry and builds a development bundle with exact sibling CTL/daemon
binaries; it does not start or install it. The second runs the isolated synthetic
fixture under the current desktop session, with its own namespace and XDG roots;
it never loads production accounts or sends campus authentication traffic. Tests
may briefly show windows. Neither command registers user autostart. They do not
produce RPM/DEB/Flatpak packages or establish Windows native acceptance.

Set `SIDRAVIA_GUI_TEST_OPEN_DESKTOP=1` for the additional native browser/folder
dispatch checks; regular frame/lifecycle regression does not repeatedly open
external desktop windows.

The GTK3 runner explicitly announces client-side decoration on Wayland. Where
`xdg_toplevel_icon_manager_v1` is advertised it submits immutable icon pixels;
this does not install a desktop entry. The owned Release fixture retains a short
Wayland trace and checks the KWin client-decoration acknowledgement and icon
submission/commit; client-area screenshots alone cannot prove those boundaries.

### Linux GUI portable candidate

From a clean isolated candidate on Fedora 45 x86_64:

```sh
.local/bin/dev linux --check-gui
.local/bin/dev linux --package --version 0.4.0-alpha.1 --output .local/linux-portable-output
```

The fixed Flutter SDK builds the GUI with the requested build name. The existing
Go distribution producer rebuilds version-bound sibling tools and owns public
profiles/portable marker; the consumer verifies exact bytes and executable modes.
Source identity is checked across compilation and consumption. Flutter 3.47 Linux
forwards the compiled Dart build name but omits it from asset assembly; packaging
stamps version.json and both producer/consumer check its actual version. The local receipt
is `.local/linux-portable-package-result.json`. No installation, account autostart,
release upload or signing is performed. Runtime requirements and movement rules
are shipped in `data/linux-portable.md` from `docs/linux-portable.md`.

After producing the archive, accept that exact clean-source package on the
current KDE/Wayland desktop with:

```sh
.local/bin/dev linux --verify-package .local/linux-portable-output/sidravia-v0.4.0-alpha.1-linux-amd64-gui-portable.tar.gz
```

This checks the local producer receipt before extracting into an owned directory
with spaces. It launches only a synthetic namespace, exercises configuration and
three core restarts through production typed desktop IPC; packaged CTL uses its GUI restart contract, exits through the real tray, moves the
entire directory, then checks crashed-owner cleanup. It does not perform campus
authentication or register the current account for autostart.

### Linux announcement native acceptance

`.local/bin/dev linux --announcement-integration` builds the real GTK/WebKit
announcement adapter and runs it against an owned loopback HTTP server. It checks
redirects/anchors/script navigation, engine-marked gesture dispatch to an isolated
XDG URI handler, close/reopen generations, network/download/process failures and
the actual 30-second deadline. It does not contact school pages or modify the real
account's URI handlers. Physical mouse clicks and the real default browser remain
separate desktop acceptance. Both native URL adapters use the shared navigation
intent policy; Windows keeps its existing IUri parsing and test cases.

Linux system integration acceptance uses private HOME/XDG roots in the portable
fixture. It registers and removes command links, starts the GUI through its link,
moves the package after deregistering, launches the generated GUI/background XDG
entries, checks repeated background launch keeps its generation, and stops it
through PATH-resolved CTL. Registration changes are checked not to adopt/stop the
running owner. No real account autostart/PATH/shell profile is modified. Unit
boundary tests also cover old GUI entry migration, conflicts, quoting, missing
PATH/shadowed commands, Fedora public skeleton shell lookup, namespace isolation
and nonblocking interprocess maintenance locking.

### Local unsigned Fedora RPM (x86_64)

RPM tools are system RPMs (`rpm-build`, `desktop-file-utils`, `bubblewrap`), not
project-downloaded replacements. Build the verified clean-source GUI portable
first, then wrap that exact payload. The RPM excludes the portable marker and
uses `/usr/lib64/sidravia` plus package-owned `/usr/bin` links, desktop entry,
and icon. Reviewed Python pre/preun scriptlets coordinate only this installation;
no account scan or automatic autostart registration is performed. Initial support
and user-data/upgrade rules: [linux-rpm.md](../../docs/linux-rpm.md).

```
.local/bin/dev linux --package --version 0.4.0-alpha.1 --output .local/rpm-portable-input
.local/bin/dev linux --rpm .local/rpm-portable-input/sidravia-v0.4.0-alpha.1-linux-amd64-gui-portable.tar.gz --rpm-release 1 --output .local/rpm-r1
.local/bin/dev linux --rpm .local/rpm-portable-input/sidravia-v0.4.0-alpha.1-linux-amd64-gui-portable.tar.gz --rpm-release 2 --output .local/rpm-r2
.local/bin/dev linux --verify-rpm-upgrade .local/rpm-r1/sidravia-0.4.0~alpha.1-1.x86_64.rpm .local/rpm-r2/sidravia-0.4.0~alpha.1-2.x86_64.rpm
```

Every RPM has a sibling `.rpm.json` producer/consumer receipt binding source,
input SHA256, output SHA256, exact file digests/modes/links and dependencies.
Prereleases map to RPM `~`, sorting before the final version. No signing or
repository upload is performed. The candidate is an upstream binary package;
private Flutter runtime and font licensing prevent assuming Fedora repository
eligibility. Full bundled notices remain in the payload.

Acceptance requires user namespaces and a KDE/Wayland session. It snapshots the
host RPM SQLite inventory read-only, installs/updates/erases in a newly owned
private root, with dependency checks enabled. Installed binaries run through
Bubblewrap over read-only host libraries and private HOME/XDG/namespace. A
common private user/PID namespace and private session bus give the maintenance
root a real process view without access to host production processes. Only the
Wayland socket is shared; this fixture does not re-accept KDE tray delivery. It
checks live GUI upgrade/removal, live headless removal, refusal of an unresponsive
GUI, startup gate recovery, preserved/inactive TryExec entries and an independent
portable daemon. It also checks installed IPC/configuration/preferences/core restart, update
ownership refusal, and persistence across upgrade/removal. No host RPM install,
real autostart registration or campus request is involved. This is not a clean
Fedora dependency-solving/install-image test. Receipts/logs stay in the owned
`.local/linux-rpm-fixture-*` until reviewed and explicitly cleaned.

## Isolated account-business fixture

`.local/bin/dev linux --business-integration` uses the same pinned core/Flutter
build entry and owned namespace/XDG fixture. It runs only the account-business
native test, with fictional credentials and a UDP peer bound to loopback. The
fixture verifies login statistics and checksummed plain text through the real
Run, Session, IPC and shared GUI, reauthentication isolation and retained stopped
data. Its result and source fingerprint remain in the owned fixture directory;
no production configuration, user integration or campus authentication is used.

## Fedora → Windows VM execution and evidence

`dev.py vm` connects the existing QEMU guest agent to existing Windows commands.
It neither logs in a user nor changes VM accounts, permissions, networking or
production installations. The requested test user must already have a loaded
interactive desktop. Test processes run with its limited InteractiveToken;
QGA's SYSTEM token is used only for transport, scoped setup and task management.

```bash
.local/bin/dev vm --request tools/developer/requests/windows-native.json \
  --guest-root 'C:\Users\astesia\dv' \
  --guest-user astesia --windows-python 'C:\DevTools\Python\python.exe'
```

These are example paths matching the local test VM; adapt them to your VM once.
Keep the dedicated root short: the existing Inno fixture contains deeply nested
paths and some Windows file APIs retain MAX_PATH limits. No machine-wide long-path
policy is changed by this tool.
`--domain` defaults to `windows11-sidravia`, `--uri` to `qemu:///session`.
The sample request invokes the command-runner contracts, existing Windows builder
safety tests and opt-in native Inno/Go handoff fixture. It uses fictional data,
not a real installation. Inno/Go/Python must already exist at the specified paths;
missing tools are failures, never an implicit installation. Full GUI/campus or
real system integration acceptance is separate.

The root must be an empty/new dedicated directory below that user's profile or
already carry this tool's ownership marker. Runs are unique, source is copied
from an exact SHA256 manifest, and a local VM lock is shared across worktrees.
A small temporary task starts a dedicated kill-on-close Job wrapper. Each
Windows command gets a dedicated Python worker and nested Job so even a parent which exits first cannot
leave children between stages. Timeouts stop the sequence; final cleanup stops
only this UUID task and removes its registration. No process-name kill, password,
automatic login, HTTP server or global `safe.directory` exception is used.
Source ZIPs and failed fixtures remain under the owned run, not in user data.
Shared Go/Pub caches and the existing Windows builder workspace are retained.

Requests are UTF-8 JSON (UTF-8 BOM is accepted), schema 1:

```json
{
  "schema": 1,
  "environment": {"PYTHONUTF8": "1"},
  "stages": [
    {
      "name": "example",
      "argv": ["C:\\DevTools\\Python\\python.exe", "-c", "print('中文 空格')"],
      "cwd": ".",
      "timeout_seconds": 30
    }
  ]
}
```

`argv` is an array, not a shell string. Shells are used only when explicitly named
as the executable. VM cwd is relative to the source snapshot. Literal tokens
`{source}`, `{run}`, `{workspace}` expand in argv and environment values; other
braces are unchanged. Environment entries may also be set per stage. Do not put
credentials in requests, argv or logs. Unknown keys, duplicate stage names,
case-ambiguous environment keys and escaping cwd/source paths are rejected.
Stages have a 900-second default timeout (maximum 3600); `--timeout` bounds the VM
task (default 1800, maximum 21600). A failure leaves later stages `not_run`.

Default stdout is a short stage summary with a bounded failure excerpt.
`--format json` emits one JSON result on stdout, with progress on stderr. Complete
UTF-8 reports, exact request/source manifest and separate raw stdout/stderr logs
are under the selected `--reports` directory. Even a timeout/interruption attempts
to stop the owned task and collect the partial report/logs. Execution, evidence
collection and cleanup errors remain distinct; uncertain cleanup is a failure.
Host and guest source changes both invalidate acceptance. Snapshots carry host
source identity; their temporary Git index is not a new claimed source commit.

For a Windows GUI build, use the same request schema to invoke existing
`build_windows.ps1` with `-SourceRoot {source}`, `-ManifestPath {run}\manifest.json`
and `-Workspace {workspace}`, plus the existing pinned SDK arguments. That helper
continues to own incremental source sync, caches, compiler logs and packaging
receipts; its report location appears in the captured command log. VM execution
itself does not imply that optional GUI/package checks were requested or passed.

## Combined local checks and formatted results

Use `dev check` for daily verification. Existing `verify`, `benchmark`, platform
builders and distribution receipts remain supported; this entry composes them
with the same schema-1 stage runner and text/JSON formatter used by `vm`.

```bash
# Default: Go native + Python. Cross builds and GUI are explicit selections.
.local/bin/dev check
# A local change: rerun only selected packages / developer-tool test files.
.local/bin/dev check --scope go --go-package ./internal/update/...
.local/bin/dev check --scope python --python-test test_validation_run.py
# Shared GUI checks once, with an optional offline Web and/or Linux build.
.local/bin/dev check --scope gui --preview-build --linux-build
.local/bin/dev check --scope gui --gui-test test/offline_demo_test.dart
# Combine scopes, inspect a machine-readable plan, or request one JSON result.
.local/bin/dev check --scope go --scope python --scope gui --plan
.local/bin/dev check --scope python --format json
```

Scopes and explicit test selections are deduplicated. `--go-checks` defaults to
`native`; `cross` or `full` use the existing verifier's bounded `--jobs` scheduling.
Targeted Go packages run `go test -count=1`, vet and diff checks; they do not claim
full repository/cross verification. Selected Python filename patterns are below
`tools/developer/tests`; omitting them runs the existing Python scope, including
mock-server and acceptance-tool tests. Omitting GUI test selections runs the
complete Flutter suite. No dependency resolution, analysis or GUI test stage is
repeated merely because both build outputs are selected. The existing Linux
builder still revalidates its dependencies and owns sibling binary generation;
`--linux-build` is build evidence, not native policy/integration/RPM acceptance.
Continue using the existing explicit Linux acceptance modes for those tasks.

`--plan` writes only a UTF-8 JSON argv/cwd/timeout plan to stdout, without executing checks/builds
or creating an evidence directory (Git/source identity is still read). Execution writes exact `request.json`, atomic
`report.json` and raw per-stage stdout/stderr logs to one unique evidence folder.
Human output lists status/name/time, a bounded failure excerpt and report path.
`--format json` produces one JSON object, never compiler output mixed into JSON;
progress goes to stderr. Child commands keep their native log bytes (the tool's
own JSON is UTF-8); non-UTF-8 diagnostics use replacement characters only in the
human excerpt, preserving the original log for diagnosis.

Stages stop on failure, launch error or timeout; remaining stages are `not_run`.
The report uses `passed`, `failed`, `timed_out`, `not_run` and interruption states
rather than treating an absent command as success. Both command argv and cwd are
recorded; environment overrides are in the request, not echoed in the summary.
Source commit/status/byte fingerprint before and after prevent concurrent edits
from producing a false acceptance. A per-worktree lock excludes simultaneous
`check` runs; it does not take over a separately running preview or older tools.
Go/Flutter/Pub caches stay at their existing locations. There is no automatic
package build, installer execution, test retry, Git commit or release operation.

## Deterministic GUI capture and visual triage

Use `dev visual` for repeated captures of the production pages/controls through
the existing offline demo. It does not launch the production installation, read
accounts, authenticate or alter a running preview. The previously local 27-step
capture flow is now `gui/test/gui_capture_test.dart`; existing HTML-reference and
home-layout scripts remain available for their original purposes.

```bash
.local/bin/dev visual capture --font-manifest .local/system-fonts.json --output .project/evidence/visual/before
# Repeat on the candidate with the same inputs/environment.
.local/bin/dev visual capture --font-manifest .local/system-fonts.json --output .project/evidence/visual/after
.local/bin/dev visual compare --baseline .project/evidence/visual/before --candidate .project/evidence/visual/after --output .project/evidence/visual/review --format json
```

Pillow is required for PNG inspection/comparison (Fedora `python3-pillow`). No
library is installed automatically. Use the pinned Flutter SDK and explicit
system font files, as for existing widget captures. The font JSON maps platform
font-family aliases to absolute installed font paths, for example on Linux:

```json
{
  "Roboto": ["/usr/share/fonts/abattis-cantarell-vf-fonts/Cantarell-VF.otf"],
  "Arial": ["/absolute/path/to/NotoSansCJK-SC.ttf"]
}
```

Adapt paths/families to the selected theme. This explicitly binds Flutter widget
tests to real glyphs; it does not claim native desktop font discovery. Capture
rejects missing ordinary-family mappings or a profile without Chinese glyphs in
its actually selected families, rather than accepting Ahem/tofu. Source Code Pro
and real license entries are loaded unchanged from existing assets/SDK. Nothing
is copied into native product font resources. Font bytes and the SDK license are
checked before/after the run; input changes invalidate the capture.

The default matrix is 400×800 and 960×700, each light/dark at text scale 1 and
DPR 1: 108 actual page/control screenshots. `--request` accepts this optional
UTF-8 JSON matrix (unknown fields/duplicate frame ids are rejected):

```json
{
  "schema": 1,
  "fixedTime": "2026-01-02T12:34:56.000Z",
  "platform": "linux",
  "frames": [
    {"id": "large-light", "width": 400, "height": 1100, "scale": 1.6, "dark": false}
  ]
}
```

Each frame captures the same 27 states, including messages, announcement,
configuration/editing/confirmation, settings, licenses/search and diagnostics.
Choose relevant frames without duplicating the page implementation. Width and
height are 280..1600, text scale .8..3, at most 16 frames. The fixed timestamp
requires a timezone and is normalized to UTC. Capture verifies actual local-time
conversion is UTC; setting `TZ=UTC` alone is not assumed to configure every OS.
`platform` selects a Linux/Windows *theme in a widget test*, not native platform
acceptance. The renderer, actual host, Flutter engine, fonts, fixed time and
scenario contract are recorded, so mismatched environments cannot silently pass.

The demo freezes its existing in-memory data and display clock. Default product
and ordinary interactive-demo clocks remain live. Runtime snapshots use that
same existing presentation clock; explicit snapshot timestamp overrides remain
supported. No timestamp masks, guessed dynamic areas or pixel tolerances are
used. License entries are materialized outside widget FakeAsync without editing
their content, avoiding cross-test asset-future noise.

Capture writes a new directory containing `report.json`, exact request/font/input
receipts, stage stdout/stderr and the PNG inventory. The existing shared runner
owns deadlines and the worktree lock. GUI checks bind the fixed SDK bin/Dart PATH
from the validated SDK metadata before running dependent subprocess tests; its
precheck logs remain in the same report. No baseline is overwritten or approved
automatically. A failed, incomplete, non-UTC or source-changing run cannot be
used as a comparison input; partial files/logs remain for diagnosis.

Comparison validates receipt/image identities, compares decoded RGBA pixels,
and produces `report.json`, red difference masks and `index.html`. The HTML lists
only cases requiring review, with before/after/mask links and exact changed-pixel
counts/bounding boxes; unchanged names are collapsed. Encoding-only PNG metadata
is ignored when pixels match; rendering metadata such as gamma/ICC is not.
Alpha changes, even one-pixel text changes, size changes, added/removed cases and
changed frame inputs remain visible. Added/removed cases describe capture
coverage, not proof that a product feature was added/deleted. Keep both source
capture folders: the local HTML links to those unchanged original images.

Exit codes: `0` means all cases match; `1` means changes need human review (or an
input/execution error, distinguished by JSON status); `2` means capture inputs or
rendering environments are incompatible. Check the structured status, not only
the exit code. Neither a change nor incompatibility should trigger a blind retry
or automatic baseline update. `--format json` keeps stdout compact; complete case
inventories stay in the report. Normal output gives the summary and review index.

Widget screenshots establish this deterministic rendering contract, not native
window/tray/DPI behavior, installation, campus or release acceptance.
