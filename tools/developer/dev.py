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
    if target not in ("linux-amd64", "linux-arm64", "windows-arm64"):
        raise ValueError("unsupported core target; use windows --package for Windows AMD64 GUI provenance")
    output = Path(args.output).resolve()
    if output.exists() or not output.parent.is_dir():
        raise ValueError("output must not exist and its parent must exist")
    producer = [args.go, "run", "-mod=readonly", "./tools/build", "--version", args.version, "--build-id", info["commit"], "--artifact", "distribution", "--target", target, "--output", str(output), "--go", args.go]
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


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest="command", required=True)
    for command in ("verify", "benchmark", "package", "manifest", "windows"):
        sub = subs.add_parser(command)
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
        if command == "windows":
            sub.add_argument("--workspace", default=r"D:\Tools\Sidravia\build-workspace")
            sub.add_argument("--flutter", default=r"D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat")
            sub.add_argument("--windows-go", default=r"C:\Program Files\Go\bin\go.exe")
            sub.add_argument("--windows-python")
            sub.add_argument("--iscc")
            sub.add_argument("--version", default="0.3.0-alpha.1")
            sub.add_argument("--package", action="store_true")
            sub.add_argument("--check-gui", action="store_true")
        if command == "package":
            sub.add_argument("--target", choices=("linux-amd64", "linux-arm64", "windows-arm64"), default="linux-amd64")
            sub.add_argument("--version", required=True)
            sub.add_argument("--output", required=True)
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    try:
        if args.command == "manifest":
            manifest = source_manifest(env, args.require_clean)
            with Path(args.output).open("x", encoding="utf-8") as output:
                json.dump(manifest, output, indent=2)
                output.write("\n")
            return 0
        if args.command == "windows":
            return windows_command(args, env)
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
        print(f"dev: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
