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


def linux_command(args, env):
    """Build/run only the explicit development target; never installs a product."""
    if platform.system() != "Linux":
        raise ValueError("linux desktop development requires native Linux")
    if args.check_gui and (args.verify_package or args.announcement_integration or args.rpm or args.verify_rpm_upgrade):
        raise ValueError("--check-gui must run separately from package or announcement native acceptance")
    flutter = str(Path(args.flutter).resolve())
    sdk = json.loads(subprocess.check_output([flutter, "--version", "--machine"], cwd=ROOT, env=env, text=True))
    if sdk["frameworkVersion"] != (ROOT / "gui" / ".flutter-version").read_text().strip():
        raise ValueError("Flutter SDK does not match .flutter-version")
    env = dict(env, PATH=str(Path(sdk["flutterRoot"]) / "bin") + os.pathsep + env.get("PATH", ""))
    source_before = metadata(args.go, env)
    if args.rpm or args.verify_rpm_upgrade:
        if source_before['dirty']:
            raise ValueError('RPM candidates require clean source')
        import linux_rpm
        current_source = lambda: metadata(args.go, env)
        if args.rpm:
            return linux_rpm.build(ROOT, Path(args.rpm).resolve(), args.output, args.rpm_release, env, source_before, current_source)
        return linux_rpm.accept(ROOT, [Path(p).resolve() for p in args.verify_rpm_upgrade], env, source_before, current_source)
    if args.package:
        if source_before["dirty"]:
            raise ValueError("package requires a clean tracked and untracked source tree")
        if args.integration or platform.machine().lower() not in ("amd64", "x86_64"):
            raise ValueError("GUI package requires native Linux AMD64 and a separate integration run")
        if not args.version or not args.output:
            raise ValueError("linux --package requires --version and --output")
        output = Path(args.output).resolve()
        if output.exists() or not output.parent.is_dir():
            raise ValueError("output must not exist and its parent must exist")
    if args.verify_package:
        if args.package or args.integration:
            raise ValueError("--verify-package is a separate acceptance run")
        archive = Path(args.verify_package).resolve()
        receipt = json.loads((ROOT / ".local/linux-portable-package-result.json").read_text())
        if source_before["dirty"] or receipt["buildId"] != source_before["commit"] or receipt["source"]["source_fingerprint"] != source_before["source_fingerprint"]:
            raise ValueError("package acceptance requires its exact clean producer source")
        if receipt["artifactHashes"].get(archive.name) != hashlib.sha256(archive.read_bytes()).hexdigest():
            raise ValueError("package does not match the verified producer receipt")
        import linux_desktop_fixture
        return linux_desktop_fixture.run(ROOT, flutter, env, source_before, lambda: metadata(args.go, env), archive)
    if args.announcement_integration:
        if args.package or args.integration or args.verify_package:
            raise ValueError("--announcement-integration requires a separate acceptance run")
        import linux_announcement_fixture
        return linux_announcement_fixture.run(ROOT,env,source_before,lambda:metadata(args.go,env))
    core = ROOT / ".local" / "linux-desktop-core"
    core.mkdir(parents=True, exist_ok=True)
    env = dict(env, SIDRAVIA_CORE_DIRECTORY=str(core))
    for name, package in (("sidraviactl", "sidravia"), ("sidraviad", "sidraviad")):
        subprocess.run([args.go, "build", "-mod=readonly", "-trimpath", "-o", str(core / name), "./cmd/" + package], cwd=ROOT, env=env, check=True)
    def flutter_run(argv):
        subprocess.run([flutter, *argv], cwd=ROOT / "gui", env=env, check=True)
    flutter_run(["pub", "get", "--enforce-lockfile"])
    if args.check_gui:
        native = ROOT / ".local" / "linux-native-policies"
        for argv in (["cmake", "-S", str(ROOT / "gui/test/native"), "-B", str(native), "-G", "Ninja", "-DCMAKE_BUILD_TYPE=Release"],
                     ["cmake", "--build", str(native)], ["ctest", "--test-dir", str(native), "--output-on-failure"]):
            subprocess.run(argv, cwd=ROOT, env=env, check=True)
        flutter_run(["analyze", "--no-pub"])
        flutter_run(["test", "--no-pub"])
    if args.integration or args.business_integration:
        # Fixture setup and real-chain ownership checks live in the dedicated
        # helper; no production start command is part of this entry point.
        if args.business_integration:
            env = dict(env, SIDRAVIA_GUI_BUSINESS_FIXTURE="1")
        import linux_desktop_fixture
        return linux_desktop_fixture.run(ROOT, flutter, env, source_before, lambda: metadata(args.go, env))
    build = ["build", "linux", "--release", "--no-pub"]
    if args.package:
        build += ["--build-name", args.version]
    flutter_run(build)
    if args.package:
        # Exclude developer copies of core/profiles. The producer owns those.
        bundle = ROOT / "gui/build/linux/x64/release/bundle"
        native_assets = (bundle / 'data/flutter_assets/NativeAssetsManifest.json').read_text()
        if 'jni' in native_assets.lower() or (bundle / 'lib/libdartjni.so').exists():
            raise ValueError('Linux JNI native assets require an explicit runtime integration; no developer JDK payload is accepted')
        with tempfile.TemporaryDirectory(prefix="linux-gui-package-", dir=ROOT / ".local") as temporary:
            stage = Path(temporary)
            for name in ("sidravia_gui", "data", "lib"):
                origin = bundle / name
                if origin.is_dir():
                    shutil.copytree(origin, stage / name, symlinks=True)
                else:
                    shutil.copy2(origin, stage / name)
            # Flutter 3.47's Linux tool_backend omits BuildName when invoking
            # assemble; Dart's FLUTTER_BUILD_NAME is correct but version.json
            # falls back to pubspec. Stamp this packaging-owned data resource.
            version_file = stage / "data/flutter_assets/version.json"
            gui_version = json.loads(version_file.read_text())
            gui_version.update(version=args.version, build_number="1")
            version_file.write_text(json.dumps(gui_version, separators=(",", ":")))
            shutil.copy2(Path(sdk["flutterRoot"]) / "LICENSE", stage / "FLUTTER-LICENSE")
            shutil.copy2(ROOT / "docs/linux-portable.md", stage / "data/linux-portable.md")
            licenses = stage / "data/licenses"
            licenses.mkdir()
            shutil.copy2(Path(sdk["flutterRoot"]) / "bin/cache/pkg/sky_engine/LICENSE", licenses / "FLUTTER-ENGINE-LICENSE")
            shutil.copytree(ROOT / "gui/linux/protocols", licenses / "wayland-protocols")
            if not same_source(source_before, metadata(args.go, env)):
                raise ValueError("source changed during GUI build")
            producer = [args.go, "run", "-mod=readonly", "./tools/build", "--artifact", "distribution", "--target", "linux-amd64", "--version", args.version, "--build-id", source_before["commit"], "--go", args.go, "--gui-directory", str(stage), "--output", str(output)]
            subprocess.run(producer, cwd=ROOT, env=env, check=True)
            import verify_distribution
            receipt = verify_distribution.verify(output, ROOT, "linux-amd64", args.version, source_before["commit"], stage, args.go, smoke=True)
            if not same_source(source_before, metadata(args.go, env)):
                raise ValueError("source changed during packaging; candidate not accepted")
            receipt.update(source=source_before, source_unchanged=True, flutter=sdk["frameworkVersion"], guiBuildName=args.version)
            (ROOT / ".local/linux-portable-package-result.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print("Verified Linux GUI portable: " + str(output))
        return 0
    print("Development bundle: gui/build/linux/x64/release/bundle (not installed or started)")
    return 0


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
    for command in ("verify", "benchmark", "package", "manifest", "windows", "linux", "preview", "vm", "check", "visual"):
        sub = subs.add_parser(command)
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
            sub.add_argument("--version", default="0.4.0-alpha.1")
            sub.add_argument("--package", action="store_true")
            sub.add_argument("--check-gui", action="store_true")
        if command == "package":
            sub.add_argument("--target", choices=("linux-amd64", "linux-arm64", "windows-arm64"), default="linux-amd64")
            sub.add_argument("--version", required=True)
            sub.add_argument("--output", required=True)
    return parser.parse_args(argv)


def main(argv=None):
    import validation_run
    validation_run.configure_output()
    args = parse_args(argv)
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    try:
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
            return verification_commands.run(ROOT, args, identity)
        if args.command == "vm":
            import windows_vm
            return windows_vm.run(ROOT, args, source_manifest(env), lambda: source_manifest(env))
        if args.command == "preview":
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
