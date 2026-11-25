"""Linux desktop build, distribution and acceptance commands."""
from __future__ import annotations

import json
import os
import platform
import re
import shutil
import subprocess
from pathlib import Path

from verification_commands import resolve_flutter_sdk


def producer_directory(output):
    return output.with_name(output.name + ".producer")


def read_portable_receipt(archive):
    return json.loads((producer_directory(archive.parent) / "receipt.json").read_text())


def portable_guide(root, commit):
    guide = (root / "docs/linux-portable.md").read_text()
    # The archive contains this page. Supplemental links point to the exact
    # published source; in-page recovery steps remain readable offline.
    origin = f"https://github.com/CaelestisAstesia/sidravia/blob/{commit}/docs/"
    return re.sub(r"\]\(((?:[a-z0-9-]+)\.md(?:#[^ )]+)?)\)", lambda match: "](" + origin + match[1] + ")", guide)


def run(root, args, env, *, metadata, same_source):
    """Build/run only the explicit development target; never installs a product."""
    if platform.system() != "Linux":
        raise ValueError("linux desktop development requires native Linux")
    if args.check_gui and (args.verify_package or args.announcement_integration or args.rpm or args.rpm_core or args.verify_rpm_upgrade):
        raise ValueError("--check-gui must run separately from package or announcement native acceptance")
    source_before = metadata(args.go, env)
    if args.rpm or args.rpm_core or args.verify_rpm_upgrade:
        if source_before['dirty']:
            raise ValueError('RPM candidates require clean source')
        import linux_rpm
        current_source = lambda: metadata(args.go, env)
        if args.rpm or args.rpm_core:
            if args.rpm_core and not args.version:
                raise ValueError('--rpm-core requires --version')
            return linux_rpm.build(root, Path(args.rpm or args.rpm_core).resolve(), args.output, args.rpm_release, env, source_before, current_source,
                                   core=bool(args.rpm_core), version=args.version, go=args.go)
        return linux_rpm.accept(root, [Path(p).resolve() for p in args.verify_rpm_upgrade], env, source_before,
                                go=args.go, flutter=args.flutter)
    flutter, sdk = resolve_flutter_sdk(args.flutter, root / "gui/.flutter-version", cwd=root, env=env)
    flutter = str(flutter)
    env = dict(env, PATH=str(Path(sdk["flutterRoot"]) / "bin") + os.pathsep + env.get("PATH", ""))
    if args.package:
        if source_before["dirty"]:
            raise ValueError("package requires a clean tracked and untracked source tree")
        if args.integration or platform.machine().lower() not in ("amd64", "x86_64"):
            raise ValueError("GUI package requires native Linux AMD64 and a separate integration run")
        if not args.version or not args.output:
            raise ValueError("linux --package requires --version and --output")
        output = Path(args.output).resolve()
        if output.exists() or producer_directory(output).exists() or not output.parent.is_dir():
            raise ValueError("output and its producer evidence directory must not exist; parent must exist")
    if args.verify_package:
        if args.package or args.integration:
            raise ValueError("--verify-package is a separate acceptance run")
        archive = Path(args.verify_package).resolve()
        receipt = read_portable_receipt(archive)
        if source_before["dirty"] or receipt["buildId"] != source_before["commit"] or receipt["source"]["source_fingerprint"] != source_before["source_fingerprint"]:
            raise ValueError("package acceptance requires its exact clean producer source")
        expected = receipt["artifactHashes"].get(archive.name)
        if expected is None:
            raise ValueError("package not named in the verified producer receipt")
        import linux_desktop_fixture
        return linux_desktop_fixture.run(root, flutter, env, source_before, lambda: metadata(args.go, env), archive,
                                         expected_sha256=expected)
    if args.announcement_integration:
        if args.package or args.integration or args.verify_package:
            raise ValueError("--announcement-integration requires a separate acceptance run")
        import linux_announcement_fixture
        return linux_announcement_fixture.run(root,env,source_before,lambda:metadata(args.go,env))
    if args.package:
        # The distribution producer owns the version-bound core. Do not build
        # development siblings only to discard them from the package input.
        env = dict(env, SIDRAVIA_CORE_DIRECTORY="")
    else:
        core = root / ".local" / "linux-desktop-core"
        core.mkdir(parents=True, exist_ok=True)
        env = dict(env, SIDRAVIA_CORE_DIRECTORY=str(core))
        for name, package in (("sidraviactl", "sidraviactl"), ("sidraviad", "sidraviad")):
            subprocess.run([args.go, "build", "-mod=readonly", "-trimpath", "-o", str(core / name), "./cmd/" + package], cwd=root, env=env, check=True)
    def flutter_run(argv):
        subprocess.run([flutter, *argv], cwd=root / "gui", env=env, check=True)
    flutter_run(["pub", "get", "--enforce-lockfile"])
    if args.check_gui:
        native = root / ".local" / "linux-native-policies"
        for argv in (["cmake", "-S", str(root / "gui/test/native"), "-B", str(native), "-G", "Ninja", "-DCMAKE_BUILD_TYPE=Release"],
                     ["cmake", "--build", str(native)], ["ctest", "--test-dir", str(native), "--output-on-failure"]):
            subprocess.run(argv, cwd=root, env=env, check=True)
        flutter_run(["analyze", "--no-pub"])
        flutter_run(["test", "--no-pub"])
    if args.integration or args.business_integration:
        # Fixture setup and real-chain ownership checks live in the dedicated
        # helper; no production start command is part of this entry point.
        if args.business_integration:
            env = dict(env, SIDRAVIA_GUI_BUSINESS_FIXTURE="1")
        import linux_desktop_fixture
        return linux_desktop_fixture.run(root, flutter, env, source_before, lambda: metadata(args.go, env))
    build = ["build", "linux", "--release", "--no-pub"]
    if args.package:
        build += ["--build-name", args.version]
    flutter_run(build)
    if args.package:
        # Exclude developer copies of core/profiles. The producer owns those.
        bundle = root / "gui/build/linux/x64/release/bundle"
        native_assets = (bundle / 'data/flutter_assets/NativeAssetsManifest.json').read_text()
        if 'jni' in native_assets.lower() or (bundle / 'lib/libdartjni.so').exists():
            raise ValueError('Linux JNI native assets require an explicit runtime integration; no developer JDK payload is accepted')
        evidence = producer_directory(output)
        evidence.mkdir(mode=0o700)
        stage = evidence / "gui"
        stage.mkdir()
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
        (stage / "data/linux-portable.md").write_text(portable_guide(root, source_before["commit"]))
        licenses = stage / "data/licenses"
        licenses.mkdir()
        shutil.copy2(Path(sdk["flutterRoot"]) / "bin/cache/pkg/sky_engine/LICENSE", licenses / "FLUTTER-ENGINE-LICENSE")
        shutil.copytree(root / "gui/linux/protocols", licenses / "wayland-protocols")
        if not same_source(source_before, metadata(args.go, env)):
            raise ValueError("source changed during GUI build")
        producer = [args.go, "run", "-mod=readonly", "./tools/build", "--artifact", "distribution", "--target", "linux-amd64", "--version", args.version, "--build-id", source_before["commit"], "--go", args.go, "--gui-directory", str(stage), "--output", str(output)]
        if getattr(args, "compression", "gzip") == "xz":
            producer += ["--compression", "xz"]
        subprocess.run(producer, cwd=root, env=env, check=True)
        import verify_distribution
        receipt = verify_distribution.verify(output, root, "linux-amd64", args.version, source_before["commit"], stage, args.go, smoke=True)
        if not same_source(source_before, metadata(args.go, env)):
            raise ValueError("source changed during packaging; candidate not accepted")
        receipt.update(source=source_before, source_unchanged=True, flutter=sdk["frameworkVersion"], guiBuildName=args.version)
        (evidence / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(f"Verified Linux GUI portable: {output}; producer input and receipt: {evidence}")
        return 0
    print("Development bundle: gui/build/linux/x64/release/bundle (not installed or started)")
    return 0
