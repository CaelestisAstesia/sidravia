#!/usr/bin/env python3
"""Small, repeatable verification, measurement and distribution entry point."""
from __future__ import annotations

import argparse
import contextlib
import datetime
import json
import hashlib
import shutil
import os
import platform
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import verify_repository as verify

ROOT = Path(__file__).resolve().parents[2]


def capture(argv, env):
    return subprocess.check_output(argv, cwd=ROOT, env=env, text=True).strip()


def source_fingerprint(env):
    """Fingerprint visible source bytes, including dirty and untracked files."""
    paths = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT, env=env).split(b"\0")
    digest = hashlib.sha256()
    for name in sorted(set(filter(None, paths))):
        path = ROOT / os.fsdecode(name)
        digest.update(name + b"\0")
        if path.is_symlink():
            digest.update(b"symlink\0" + os.fsencode(os.readlink(path)))
        elif path.is_file():
            digest.update(b"file\0" + hashlib.sha256(path.read_bytes()).digest())
        else:
            digest.update(b"missing\0")
    return digest.hexdigest()


def same_source(before, after):
    return all(before[key] == after[key] for key in ("commit", "status", "source_fingerprint"))


def metadata(go, env):
    status = capture(["git", "status", "--porcelain", "--untracked-files=all"], env)
    return {
        "commit": capture(["git", "rev-parse", "HEAD"], env),
        "dirty": bool(status), "status": status,
        "source_fingerprint": source_fingerprint(env),
        "host": platform.platform(), "python": sys.version,
        "go": capture([go, "version"], env),
        "go_env": json.loads(capture([go, "env", "-json", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "GOFLAGS", "GOMAXPROCS"], env)),
        "cpu_count": os.cpu_count(),
    }


def verify_once(args, env, directory, label):
    started = time.monotonic()
    results = []
    with (directory / f"{label}.log").open("w", encoding="utf-8") as log:
        with contextlib.redirect_stdout(log), contextlib.redirect_stderr(log):
            with tempfile.TemporaryDirectory(prefix="sidravia-build-") as build:
                if args.scope in ("all", "go"):
                    results.extend(verify.run_go_checks(ROOT, args.go, verify.get_gofmt_path(args.go), Path(build), env, args.go_checks, args.jobs))
                if args.scope in ("all", "python"):
                    results.extend(verify.run_python_checks(ROOT, env))
    result = {"label": label, "duration_seconds": time.monotonic() - started,
              "passed": all(r.exit_code == 0 for r in results),
              "stages": [{"argv": r.argv, "exit_code": r.exit_code, "duration_seconds": r.duration_seconds} for r in results],
              "log": str(directory / f"{label}.log")}
    print(f"{label}: {'PASS' if result['passed'] else 'FAIL'} {result['duration_seconds']:.3f}s")
    return result


def package_commands(args, env):
    info = metadata(args.go, env)
    if info["dirty"]:
        raise ValueError("package requires a clean tracked and untracked source tree")
    if platform.system() != "Linux" or platform.machine().lower() not in ("amd64", "x86_64"):
        raise ValueError("package requires native Linux amd64; use windows --package for Windows GUI provenance")
    target = getattr(args, "target", "linux-amd64")
    if target not in ("linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"):
        raise ValueError("unsupported core target")
    output = Path(args.output).resolve()
    if output.exists() or not output.parent.is_dir():
        raise ValueError("output must not exist and its parent must exist")
    producer = [args.go, "run", "-mod=readonly", "./tools/build", "--version", args.version, "--build-id", info["commit"], "--artifact", "distribution", "--target", target, "--output", str(output), "--go", args.go]
    if getattr(args, "compression", "gzip") == "xz":
        if not target.startswith("linux-"):
            raise ValueError("xz compression requires a Linux target")
        producer += ["--compression", "xz"]
    consumer = [sys.executable, "tools/developer/verify_distribution.py", "--directory", str(output), "--target", target, "--version", args.version, "--build-id", info["commit"], "--go", args.go]
    if target == "linux-amd64":
        consumer.append("--smoke")
    return info, [producer, consumer]


def source_manifest(env, require_clean=False):
    commit = capture(["git", "rev-parse", "HEAD"], env)
    status = capture(["git", "status", "--porcelain", "--untracked-files=all"], env)
    untracked = capture(["git", "ls-files", "--others", "--exclude-standard"], env)
    if untracked:
        raise ValueError("manifest requires untracked files to be tracked or ignored first")
    if require_clean and status:
        raise ValueError("package requires a clean source tree")
    paths = capture(["git", "ls-files", "-z"], env).split("\0")
    files = []
    for name in filter(None, paths):
        path = ROOT / name
        if path.is_symlink():
            raise ValueError(f"source manifest does not support symlink: {name}")
        if path.is_file():
            files.append({"path": name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    after_commit = capture(["git", "rev-parse", "HEAD"], env)
    after_status = capture(["git", "status", "--porcelain", "--untracked-files=all"], env)
    if after_commit != commit or after_status != status:
        raise ValueError("source changed while creating manifest; retry with a stable source tree")
    return {"schema": 1, "source_commit": commit, "dirty": bool(status), "files": files}


def windows_command(args, env):
    manifest = source_manifest(env, args.package)
    directory = Path(args.reports).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix="windows-", dir=directory))
    manifest_path = run / "source-manifest.json"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    native = platform.system() == "Windows"
    def windows_path(path):
        return str(path) if native else capture(["wslpath", "-w", str(path)], env)
    powershell = shutil.which("powershell.exe") if native else "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
    if not powershell or not Path(powershell).is_file():
        raise ValueError("Windows build requires native Windows or WSL with PowerShell interop")
    command = [powershell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", windows_path(ROOT / "tools/developer/build_windows.ps1"), "-SourceRoot", windows_path(ROOT), "-ManifestPath", windows_path(manifest_path), "-Workspace", args.workspace, "-Flutter", args.flutter, "-Go", args.windows_go, "-Version", args.version]
    if args.windows_python:
        command += ["-Python", args.windows_python]
    elif native:
        command += ["-Python", sys.executable]
    if args.iscc:
        command += ["-ISCC", args.iscc]
    if args.package:
        if not native and not args.windows_python:
            raise ValueError("--package from WSL requires --windows-python with a native Windows Python path")
        command.append("-Package")
    if args.check_gui:
        command.append("-CheckGUI")
    # The helper owns native stage timings and logs in its persistent workspace.
    return subprocess.run(command, cwd=ROOT, env=env, check=False).returncode


def linux_command(args, env):
    import linux_development
    from verification_commands import worktree_lock

    # RPM producers wrap existing inputs without writing Flutter state.
    lock = contextlib.nullcontext() if args.rpm or args.rpm_core else worktree_lock(ROOT)
    with lock:
        return linux_development.run(ROOT, args, env, metadata=metadata, same_source=same_source)


@contextlib.contextmanager
def preview_font_asset(font=None):
    """Stage a host font for CanvasKit only; native products never bundle it."""
    if font is None:
        if shutil.which("fc-match"):
            font = subprocess.check_output(
                ["fc-match", "-f", "%{file}", ":lang=zh-cn"], text=True,
            ).strip()
        elif platform.system() == "Windows":
            font = Path(os.environ.get("WINDIR", "C:/Windows")) / "Fonts/msyh.ttc"
    if not font or not Path(font).is_file():
        raise ValueError("web preview requires --font pointing to a local Chinese-capable font")
    assets = ROOT / "gui/web/assets"
    assets.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="preview-font-", dir=assets) as temporary:
        directory = Path(temporary)
        shutil.copy2(font, directory / "system-font.ttf")
        yield directory.name + "/system-font.ttf"


def preview_command(args, env):
    """Run only the in-memory Flutter demo on a loopback web server."""
    flutter = str(Path(args.flutter).resolve())
    sdk = json.loads(subprocess.check_output([flutter, "--version", "--machine"], cwd=ROOT, env=env, text=True))
    if sdk["frameworkVersion"] != (ROOT / "gui" / ".flutter-version").read_text().strip():
        raise ValueError("Flutter SDK does not match .flutter-version")
    env = dict(env, PATH=str(Path(sdk["flutterRoot"]) / "bin") + os.pathsep + env.get("PATH", ""))

    def run(argv):
        subprocess.run([flutter, *argv], cwd=ROOT / "gui", env=env, check=True)

    run(["pub", "get", "--enforce-lockfile"])
    target = "lib/dev/interactive_demo_main.dart"
    with preview_font_asset(args.font) as font:
        font_define = "--dart-define=SIDRAVIA_DEMO_SYSTEM_FONT=" + font
        if args.check:
            run(["analyze", "--no-pub"])
            run(["test", "--no-pub", "test/offline_demo_test.dart", "test/r51_adaptation_test.dart"])
            run(["build", "web", "--no-pub", "--no-web-resources-cdn", "--target", target, font_define])
        else:
            print(f"Offline GUI preview: http://127.0.0.1:{args.port} (fictional in-memory data only)", flush=True)
            run(["run", "--no-pub", "-d", "web-server", "--web-hostname", "127.0.0.1", "--web-port", str(args.port), "--no-web-resources-cdn", "--target", target, font_define])
    return 0


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest="command", required=True)
    for command in ("verify", "benchmark", "package", "manifest", "windows", "linux", "preview", "vm", "check", "release", "visual", "report"):
        sub = subs.add_parser(command)
        if command == "report":
            sub.add_argument("path", help="existing stage report JSON or its directory")
            sub.add_argument("--kind", choices=('stage', 'linux-desktop', 'linux-announcement', 'linux-rpm', 'windows-field'), default='stage', help="explicit existing receipt format")
            sub.add_argument("--native", action='append', default=[], metavar='KIND=PATH', help="attach an explicitly selected native receipt; repeatable")
            sub.add_argument("--stage", help="show one exact stage, including successful-stage logs")
            sub.add_argument("--tail-bytes", type=int, choices=range(0, 16385), default=2000, metavar="BYTES")
            sub.add_argument("--format", choices=("text", "json"), default="text")
        if command == "release":
            sub.add_argument("mode", choices=("prepare", "verify", "download"))
            sub.add_argument("--request", required=True, help="public release artifact inventory JSON")
            sub.add_argument("--public-key", required=True, help="base64 Ed25519 public key file; never a private seed")
            sub.add_argument("--output", help="new unsigned manifest file for prepare")
            sub.add_argument("--manifest", help="exact existing manifest for verify")
            sub.add_argument("--signature", help="existing detached signature for verify")
            sub.add_argument("--format", choices=("text", "json"), default="text")
            sub.add_argument("--stage-timeout", type=int, choices=range(1, 3601), default=900, metavar='SECONDS')
        if command == "visual":
            sub.add_argument("mode", choices=("capture", "compare"))
            sub.add_argument("--output", required=True, help="new owned output directory; never overwrite a baseline")
            sub.add_argument("--font-manifest", help="explicit font family/file JSON for capture")
            sub.add_argument("--request", help="optional capture matrix/fixed-time JSON")
            sub.add_argument("--flutter", default=".local/bin/flutter")
            sub.add_argument("--stage-timeout", type=int, choices=range(1, 3601), default=900, metavar="SECONDS")
            sub.add_argument("--baseline")
            sub.add_argument("--candidate")
            sub.add_argument("--format", choices=("text", "json"), default="text")
        if command == "check":
            sub.add_argument("--recommend", action="store_true", help="only recommend a check plan from Git changes; never execute")
            sub.add_argument("--base", help="exact comparison ref for --recommend; default HEAD, including local changes")
            sub.add_argument("--scope", action="append", choices=("go", "python", "gui"), help="repeat to combine scopes; default go+python")
            sub.add_argument("--go-checks", choices=("native", "cross", "full"), default="native")
            sub.add_argument("--jobs", type=int, choices=range(1, 5), default=2)
            sub.add_argument("--go-package", action="append", default=[])
            sub.add_argument("--python-test", action="append", default=[], help="filename pattern in tools/developer/tests")
            sub.add_argument("--gui-test", action="append", default=[], help="test/*.dart path relative to gui")
            sub.add_argument("--flutter", default=".local/bin/flutter")
            sub.add_argument("--preview-build", action="store_true")
            sub.add_argument("--linux-build", action="store_true")
            sub.add_argument("--stage-timeout", type=int, choices=range(1, 3601), default=900, metavar="SECONDS")
            sub.add_argument("--plan", action="store_true", help="print the JSON command plan without executing it")
            sub.add_argument("--format", choices=("text", "json"), default="text")
        if command == "vm":
            sub.add_argument("--request", required=True, help="schema-1 JSON command sequence")
            sub.add_argument("--uri", default="qemu:///session")
            sub.add_argument("--domain", default="windows11-sidravia")
            sub.add_argument("--native-update-fixtures", action="store_true", help="isolated Windows update acceptance with scoped breakaway and process cleanup")
            sub.add_argument("--guest-root", required=True, help="dedicated owned directory below the test user's profile")
            sub.add_argument("--guest-user", required=True, help="existing logged-in Windows user (no password)")
            sub.add_argument("--windows-python", required=True)
            sub.add_argument("--timeout", type=int, choices=range(1, 21601), default=1800, metavar="SECONDS")
            sub.add_argument("--format", choices=("text", "json"), default="text")
        if command == "preview":
            sub.add_argument("--flutter", default=".local/bin/flutter")
            sub.add_argument("--font", help="local Chinese-capable font for the web preview only; defaults to the host font")
            sub.add_argument("--port", type=int, choices=range(1024, 65536), default=7357, metavar="PORT")
            sub.add_argument("--check", action="store_true", help="analyze, test the offline demo and compile its web build without starting a server")
        sub.add_argument("--go", default="go")
        sub.add_argument("--reports", default=str(ROOT / ".project" / "evidence" / "build-efficiency"))
        if command in ("verify", "benchmark"):
            sub.add_argument("--scope", choices=("go", "python", "all"), default="all")
            sub.add_argument("--go-checks", choices=("native", "cross", "full"), default="full")
            sub.add_argument("--jobs", type=int, choices=range(1, 5), default=2)
        if command == "benchmark":
            sub.add_argument("--runs", type=int, choices=range(2, 21), default=3)
            sub.add_argument("--cache", choices=("existing", "cold-warm"), default="existing", help="cold-warm uses one disposable empty Go compile cache; module and OS caches remain warm")
        if command == "manifest":
            sub.add_argument("--output", required=True)
            sub.add_argument("--require-clean", action="store_true")
        if command == "linux":
            mode = sub.add_mutually_exclusive_group()
            mode.add_argument("--package", action="store_true")
            mode.add_argument("--verify-package", help="owned native acceptance of a verified portable tar.gz")
            mode.add_argument("--rpm", metavar="PORTABLE", help="build RPM from the exact clean-source verified GUI portable")
            mode.add_argument("--rpm-core", metavar="PORTABLE", help="build core-only RPM from an exact clean-source verified core portable; requires --version")
            mode.add_argument("--verify-rpm-upgrade", nargs=2, metavar=("OLD_RPM", "NEW_RPM"), help="isolated RPM install/upgrade/erase and installed GUI acceptance")
            sub.add_argument("--rpm-release", default="1", help="positive RPM packaging revision")
            sub.add_argument("--version")
            sub.add_argument("--output")
            sub.add_argument("--flutter", default=".local/bin/flutter")
            sub.add_argument("--check-gui", action="store_true")
            mode.add_argument("--integration", action="store_true")
            mode.add_argument("--business-integration", action="store_true")
            mode.add_argument("--announcement-integration", action="store_true")
        if command == "windows":
            sub.add_argument("--workspace", default=r"D:\Tools\Sidravia\build-workspace")
            sub.add_argument("--flutter", default=r"D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat")
            sub.add_argument("--windows-go", default=r"C:\Program Files\Go\bin\go.exe")
            sub.add_argument("--windows-python")
            sub.add_argument("--iscc")
            sub.add_argument("--version", default="0.5.0-alpha.1")
            sub.add_argument("--package", action="store_true")
            sub.add_argument("--check-gui", action="store_true")
        if command in ("package", "linux"):
            sub.add_argument("--compression", choices=("gzip", "xz"), default="gzip")
        if command == "package":
            sub.add_argument("--target", choices=("linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"), default="linux-amd64")
            sub.add_argument("--version", required=True)
            sub.add_argument("--output", required=True)
    return parser.parse_args(argv)


def main(argv=None):
    import validation_run
    validation_run.configure_output()
    args = parse_args(argv)
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    try:
        if args.command == "report":
            import report_inspection
            return report_inspection.run(args)
        if args.command == "visual":
            import visual_checks
            if args.mode == "capture":
                if not args.font_manifest or args.baseline or args.candidate:
                    raise ValueError("capture requires --font-manifest, without --baseline/--candidate")
                def visual_identity():
                    return {"commit": capture(["git", "rev-parse", "HEAD"], env),
                            "status": capture(["git", "status", "--porcelain", "--untracked-files=all"], env),
                            "source_fingerprint": source_fingerprint(env)}
                return visual_checks.capture(ROOT, args, visual_identity)
            if not args.baseline or not args.candidate or args.request or args.font_manifest:
                raise ValueError("compare requires --baseline/--candidate, without capture inputs")
            return visual_checks.compare(args)
        if args.command == "check":
            import verification_commands
            def identity():
                return {"commit": capture(["git", "rev-parse", "HEAD"], env),
                        "status": capture(["git", "status", "--porcelain", "--untracked-files=all"], env),
                        "source_fingerprint": source_fingerprint(env)}
            if args.recommend:
                import check_recommendations
                return check_recommendations.run(ROOT, args, identity)
            if args.base is not None:
                raise ValueError('--base requires --recommend')
            return verification_commands.run(ROOT, args, identity)
        if args.command == "vm":
            import windows_vm
            return windows_vm.run(ROOT, args, source_manifest(env), lambda: source_manifest(env))
        if args.command == "release":
            import release_artifacts
            if args.mode == 'download':
                if not args.output or args.manifest or args.signature:
                    raise ValueError('download requires only --output; metadata names are in --request')
                import release_download
                return release_download.run(ROOT, args, lambda: metadata(args.go, env))
            if (args.mode == "prepare" and (not args.output or args.manifest or args.signature)) or (args.mode == "verify" and (args.output or not args.manifest or not args.signature)):
                raise ValueError("prepare requires only --output; verify requires --manifest and --signature")
            before = metadata(args.go, env)
            result = release_artifacts.run(args, ROOT, before)
            after = metadata(args.go, env)
            result.update(source=before, source_after=after, source_unchanged=same_source(before, after))
            if not result['source_unchanged']:
                raise ValueError("source changed during release validation")
            directory = Path(args.reports).resolve()
            directory.mkdir(parents=True, exist_ok=True)
            directory = Path(tempfile.mkdtemp(prefix="release-" + args.mode + "-", dir=directory))
            report = directory / "report.json"
            report.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
            if args.format == "json":
                print(json.dumps(dict(result, report=str(report)), ensure_ascii=False))
            else:
                print(f"Release {args.mode}: PASS; artifacts {result['assets']}; signature {result['signature']}")
                print(f"Report: {report}")
            return 0
        if args.command == "preview":
            from verification_commands import worktree_lock
            with worktree_lock(ROOT):
                return preview_command(args, env)
        if args.command == "manifest":
            manifest = source_manifest(env, args.require_clean)
            with Path(args.output).open("x", encoding="utf-8") as output:
                json.dump(manifest, output, indent=2)
                output.write("\n")
            return 0
        if args.command == "windows":
            return windows_command(args, env)
        if args.command == "linux":
            return linux_command(args, env)
        # Validate package source before creating any output, including reports.
        info, commands = package_commands(args, env) if args.command == "package" else (metadata(args.go, env), [])
        reports = Path(args.reports).resolve()
        reports.mkdir(parents=True, exist_ok=True)
        directory = Path(tempfile.mkdtemp(prefix=datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ-") + args.command + "-", dir=reports))
        report = {"metadata": info, "arguments": vars(args), "runs": []}
        started = time.monotonic()
        with contextlib.ExitStack() as stack:
            if args.command == "benchmark" and args.cache == "cold-warm":
                env["GOCACHE"] = stack.enter_context(tempfile.TemporaryDirectory(prefix="sidravia-benchmark-cache-"))
                report["cache"] = {"path": env["GOCACHE"], "removed_after_run": True, "condition": "first run empty Go compile cache; later runs reuse it; module and OS caches unchanged"}
            else:
                report["cache"] = {"path": info["go_env"]["GOCACHE"], "removed_after_run": False, "condition": "existing cache; first run warmth unknown"}
            if args.command == "package":
                for index, command in enumerate(commands):
                    result = verify.run_check(verify.Check(command), ROOT, env)
                    log = directory / f"stage-{index}.log"
                    log.write_text(result.stdout + "\n" + result.stderr, encoding="utf-8")
                    report["runs"].append({"argv": command, "passed": result.exit_code == 0, "exit_code": result.exit_code, "duration_seconds": result.duration_seconds, "log": str(log)})
                    if result.exit_code:
                        break
            else:
                count = args.runs if args.command == "benchmark" else 1
                for index in range(count):
                    report["runs"].append(verify_once(args, env, directory, f"run-{index + 1}"))
        report["duration_seconds"] = time.monotonic() - started
        report["source_after"] = metadata(args.go, env)
        report["source_unchanged"] = same_source(info, report["source_after"])
        if not report["source_unchanged"]:
            print("FAIL: source changed during run; results do not describe one stable source tree", file=sys.stderr)
        times = [run["duration_seconds"] for run in report["runs"]]
        if args.command == "benchmark":
            report["repeat_seconds"] = {"min": min(times[1:]), "median": statistics.median(times[1:]), "max": max(times[1:])}
        report["passed"] = all(run["passed"] for run in report["runs"]) and report.get("source_unchanged", True)
        (directory / "report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"Report: {directory / 'report.json'}")
        return 0 if report["passed"] else 1
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        if getattr(args, "format", "text") == "json":
            print(json.dumps({"schema": 1, "status": "failed", "error": str(error)}, ensure_ascii=False))
        else:
            print(f"dev: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
