# Development commands

Run from the repository root with Python 3 and the pinned Go toolchain (Go 1.26.4).
`dev.py` uses the existing verifier and distribution producer/consumer. It does
not install, start the GUI/daemon, authenticate, sign, upload or publish.

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

Each invocation writes a unique report and complete logs under
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

From a clean Linux amd64 checkout, choose an absent output directory whose parent
exists (the repository's ignored `dist/` path is suitable):

```bash
python3 tools/developer/dev.py package --version 0.3.0-alpha.1 --output dist/linux-amd64
python3 tools/developer/dev.py package --target linux-arm64 --version 0.3.0-alpha.1 --output dist/linux-arm64
python3 tools/developer/dev.py package --target windows-arm64 --version 0.3.0-alpha.1 --output dist/windows-arm64
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
