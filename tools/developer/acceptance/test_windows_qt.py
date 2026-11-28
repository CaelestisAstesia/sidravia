"""Consume two real, separately built Windows Qt releases in a disposable account."""

import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid

import pytest

from verify_distribution import matches_pe, native_arch


OPTIONS = ("--accept-licenses", "--reject-messages", "--confirm-command")


@pytest.fixture(scope="module")
def qt_release(tmp_path_factory):
    request = os.environ.get("SIDRAVIA_QT_ACCEPTANCE_REQUEST")
    if sys.platform != "win32":
        raise pytest.UsageError("Qt lifecycle acceptance needs a native Windows host")
    if not request:
        raise pytest.UsageError("set SIDRAVIA_QT_ACCEPTANCE_REQUEST to a Windows Qt release fixture")
    if os.environ.get("SIDRAVIA_IFW_ISOLATED_WINDOWS") != "1":
        raise pytest.UsageError("Qt lifecycle acceptance requires an isolated Windows account")
    path = Path(request)
    assert path.is_absolute() and path.is_file()
    data = json.loads(path.read_bytes())
    assert data["arch"] == native_arch(), "Qt lifecycle acceptance requires a native Windows host"
    for key in ("setup", "repositories", "manifest", "signature", "testProgram", "maintenanceImage"):
        path = Path(data[key])
        assert path.is_absolute() and path.exists()
        data[key] = path
    if "productCommands" in data:
        commands = Path(data["productCommands"])
        assert commands.is_absolute() and commands.is_dir()
        for name in ("sidraviactl.exe", "sidraviaupdater.exe"):
            with (commands / name).open("rb") as command:
                assert matches_pe(command.read(4096), data["arch"])
        data["productCommands"] = commands
    setup = tmp_path_factory.mktemp("verified-qt-setup") / data["setup"].name
    shutil.copyfile(data["setup"], setup)
    assert setup.stat().st_size == data["setupSize"], "offline installer size changed since verification"
    with setup.open("rb") as handle:
        assert hashlib.file_digest(handle, "sha256").hexdigest() == data["setupSHA256"], (
            "offline installer digest changed since verification"
        )
    data["setup"] = setup
    assert matches_pe(data["testProgram"].read_bytes(), data["arch"]), "native handoff test architecture mismatch"
    manifest = data["manifest"].read_bytes()
    data["assets"] = json.loads(manifest)["assets"]
    data["publisherProof"] = {
        "PublicKey": data["publicKey"],
        "Manifest": base64.b64encode(manifest).decode(),
        "Signature": base64.b64encode(data["signature"].read_bytes()).decode(),
    }
    return data


def invoke(evidence, name, program, args, env=None, *, separate_stderr=False):
    result = subprocess.run([program, *args], cwd=evidence, env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE if separate_stderr else subprocess.STDOUT,
                            timeout=65)
    log = evidence / f"{name}.log"
    log.write_bytes(result.stdout)
    if separate_stderr:
        (evidence / f"{name}.stderr.log").write_bytes(result.stderr)
    return result.returncode, log


def require_success(result):
    code, log = result
    assert code == 0, log.read_text(encoding="utf-8", errors="replace")


def purge(evidence, label, maintenance, env=None):
    require_success(invoke(evidence, label, maintenance, [*OPTIONS, "purge"], env))
    # IFW removes the running maintenance executable in a detached cleanup process.
    deadline = time.monotonic() + 10
    while maintenance.exists() and time.monotonic() < deadline:
        time.sleep(.2)
    assert not maintenance.exists(), f"Qt did not remove its maintenance executable: {maintenance}"


def autostart_mode(evidence, label, manager, env):
    result = invoke(evidence, label, manager, ["status"], env)
    require_success(result)
    return json.loads(result[1].read_bytes())["autostart"]



def wait_for_runtime(path, process):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if process is not None:
            assert process.poll() is None, "GUI exited before starting its daemon"
        try:
            return json.loads(path.read_bytes())
        except (FileNotFoundError, json.JSONDecodeError):
            time.sleep(.15)
    pytest.fail("daemon runtime record did not appear")


def wait_for_desktop_window(evidence, label, process):
    powershell = Path(os.environ["SystemRoot"]) / "System32/WindowsPowerShell/v1.0/powershell.exe"
    command = (
        f"$p = Get-Process -Id {process.pid}; $deadline = [DateTime]::UtcNow.AddSeconds(20); "
        "do { $p.Refresh(); if (!$p.HasExited -and $p.MainWindowHandle -ne 0) { "
        "Write-Output $p.MainWindowHandle; exit 0 }; Start-Sleep -Milliseconds 150 "
        "} while (!$p.HasExited -and [DateTime]::UtcNow -lt $deadline); "
        "Write-Error 'Sidravia did not show its main window'; exit 1"
    )
    require_success(invoke(evidence, f"{label}-window", powershell,
                           ["-NoProfile", "-NonInteractive", "-Command", command]))


@contextlib.contextmanager
def running_sidravia(target, profile, namespace, env, evidence, label):
    runtime = namespace / "runtime.json"
    assert not runtime.exists()
    ctl = target / "sidraviactl.exe"
    gui = None
    if profile == "gui":
        with (evidence / f"{label}-gui.log").open("wb") as log:
            gui = subprocess.Popen([target / "Sidravia.exe", "--skip-auto-login", "--start-visible"],
                                   cwd=target, env=env, stdin=subprocess.DEVNULL,
                                   stdout=log, stderr=subprocess.STDOUT)
    else:
        require_success(invoke(evidence, f"{label}-start", ctl, ["daemon", "start"], env))
    try:
        info = wait_for_runtime(runtime, gui)
        if gui is not None:
            wait_for_desktop_window(evidence, label, gui)
        yield info, gui
    finally:
        if gui is not None and gui.poll() is None:
            gui.terminate()
            gui.wait(timeout=15)
        elif gui is None and runtime.exists():
            require_success(invoke(evidence, f"{label}-stop", ctl, ["daemon", "stop"], env))
        deadline = time.monotonic() + 15
        while runtime.exists() and time.monotonic() < deadline:
            time.sleep(.15)
        assert not runtime.exists(), "the fixture daemon did not release its runtime record"


def confirmed_outcome(cache, version):
    path = cache / "qt-outcome.json"
    deadline = time.monotonic() + 45
    status = {}
    while time.monotonic() < deadline:
        status = json.loads(path.read_bytes())
        if status["state"] != "pending":
            break
        time.sleep(.2)
    assert status == {"targetVersion": version, "state": "completed"}, (
        cache / "qt-maintenance.log"
    ).read_text(encoding="utf-8", errors="replace")


def apply_through_product(evidence, ctl, asset, env, runtime, daemon_pid):
    checked = invoke(evidence, "product-check", ctl, ["update", "check"], env, separate_stderr=True)
    require_success(checked)
    check = json.loads(checked[1].read_bytes())["result"]["check"]
    assert check["state"] == "available"
    candidate = check["candidate"]
    assert (candidate["name"], candidate["sha256"], candidate["canApply"]) == (
        asset["name"], asset["sha256"], True
    )
    selection = ["--candidate", candidate["id"]]
    download = invoke(evidence, "product-background-download", ctl, ["update", "download", *selection],
                      env, separate_stderr=True)
    require_success(download)
    downloaded = Path(json.loads(download[1].read_bytes())["result"]["downloaded"])
    assert downloaded.name == asset["name"]
    with downloaded.open("rb") as package:
        assert hashlib.file_digest(package, "sha256").hexdigest() == asset["sha256"]
    assert json.loads(runtime.read_bytes())["pid"] == daemon_pid
    cache = downloaded.parent.parent
    try:
        apply = invoke(evidence, "product-apply", ctl, ["update", "apply", *selection, "--confirm"],
                       env, separate_stderr=True)
        require_success(apply)
        assert json.loads(apply[1].read_bytes())["code"] == "handed_over"
    except Exception:
        copy_maintenance_evidence(cache, evidence)
        raise
    return cache


def copy_maintenance_evidence(cache, evidence):
    primary = sys.exception()
    try:
        for name in ("qt-outcome.json", "qt-maintenance.log"):
            path = cache / name
            if path.is_file():
                shutil.copyfile(path, evidence / name)
    except OSError as error:
        if primary is None:
            raise
        primary.add_note(f"Cannot copy Qt maintenance evidence: {error}")


def has_user_path(directory):
    import winreg

    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, "Environment") as key:
            value, _ = winreg.QueryValueEx(key, "Path")
    except FileNotFoundError:
        return False
    expected = os.path.normcase(os.path.normpath(directory))
    return any(os.path.normcase(os.path.normpath(os.path.expandvars(entry.strip('"')))) == expected
               for entry in value.split(";") if entry)


def assert_native_payload(target, profile, arch):
    executables = ["sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe", "sidraviaupdater.exe"]
    desktop = ("Sidravia.exe", "flutter_windows.dll")
    if profile == "gui":
        executables.extend(desktop)
    else:
        for name in desktop:
            assert not (target / name).exists(), name
    for name in executables:
        assert matches_pe((target / name).read_bytes(), arch), f"{name} must target {arch}"


@pytest.mark.parametrize("profile", ("core", "gui"))
def test_windows_qt_release_lifecycle(qt_release, profile, tmp_path):
    target = tmp_path / "installation"
    cabinet = tmp_path / "prepared"
    cabinet.mkdir()
    maint = target / "sidravia-maintenance.exe"
    ctl = target / "sidraviactl.exe"
    source_version = qt_release["sourceVersion"]
    source_build = qt_release["sourceBuildId"]
    asset, = [asset for asset in qt_release["assets"]
              if asset["kind"] == f"qt-{profile}-repository" and asset["arch"] == qt_release["arch"]]
    target_data = json.loads(qt_release["manifest"].read_bytes())
    target_version = target_data["version"]
    target_build = target_data["buildId"]
    with qt_release["maintenanceImage"].open("rb") as handle:
        image = hashlib.file_digest(handle, "sha256").hexdigest()

    install = ("install",)
    if profile == "core":
        install = ("--no-default-installations", "install", "org.sidravia.core")
    require_success(invoke(tmp_path, "install", qt_release["setup"],
                           ["--root", target, *OPTIONS, *install]))
    commands = qt_release.get("productCommands")
    if commands is not None:
        for name in ("sidraviactl.exe", "sidraviaupdater.exe"):
            shutil.copyfile(commands / name, target / name)
    namespace_id = f"qt-accept-{uuid.uuid4().hex}"
    namespace = Path(os.environ["APPDATA"]) / "Sidravia" / "namespaces" / namespace_id
    namespace.mkdir(parents=True)
    env = dict(os.environ, SIDRAVIA_NAMESPACE=namespace_id)
    try:
        assert ctl.is_file() and maint.is_file()
        manager = target / "sidraviamgr.exe"
        assert not has_user_path(target)
        require_success(invoke(tmp_path, "enable-owned-path", manager, ["path", "enable"], env))
        assert has_user_path(target)
        assert autostart_mode(tmp_path, "initial-integration", manager, env) == "disabled"
        require_success(invoke(tmp_path, "enable-owned-autostart", manager, ["autostart", "set", "daemon"], env))
        assert autostart_mode(tmp_path, "enabled-autostart", manager, env) == "daemon"
        assert_native_payload(target, profile, qt_release["arch"])
        with maint.open("rb") as handle:
            assert hashlib.file_digest(handle, "sha256").hexdigest() == image
        shutil.copytree(target / "institution-profiles", namespace / "institution-profiles")
        settings = namespace / "acceptance-settings.json"
        settings.write_bytes(b'{"fixture":"keep settings during update and uninstall"}')
        unowned = target / "acceptance-user-file.txt"
        unowned.write_bytes(b"this file is outside the Qt component ownership")
        test_request = tmp_path / "handoff.json"
        handoff = {
            "Installation": str(target), "SourceVersion": source_version, "SourceBuild": source_build,
            "Archive": str(qt_release["repositories"] / asset["name"]), "Cache": str(cabinet),
            "MaintenanceSHA256": image, **qt_release["publisherProof"],
            "Candidate": dict(version=target_version, name=asset["name"], size=asset["size"], sha256=asset["sha256"]),
        }
        env["SIDRAVIA_TEST_QT_HANDOFF"] = str(test_request)

        with running_sidravia(target, profile, namespace, env, tmp_path, "before") as (daemon, gui):
            assert (daemon["productVersion"], daemon["buildId"]) == (source_version, source_build)
            code, _ = invoke(tmp_path, "running-uninstall-refused", maint, [*OPTIONS, "purge"], env)
            assert code != 0 and ctl.is_file()
            if profile == "core":
                corrupt = tmp_path / asset["name"]
                shutil.copyfile(handoff["Archive"], corrupt)
                with corrupt.open("r+b") as archive:
                    archive.seek(-1, os.SEEK_END)
                    last = archive.read(1)
                    archive.seek(-1, os.SEEK_END)
                    archive.write(bytes([last[0] ^ 1]))
                test_request.write_text(json.dumps(dict(handoff, Archive=str(corrupt), RejectArchive=True)),
                                        encoding="utf-8")
                require_success(invoke(tmp_path, "reject-corrupt-download", qt_release["testProgram"],
                                       ["-test.run=^TestQtNativeHandoff$", "-test.v"], env))
                assert list(cabinet.iterdir()) == [], "rejected archive left a maintenance operation in the cache"
            if commands is None:
                test_request.write_text(json.dumps(handoff), encoding="utf-8")
                require_success(invoke(tmp_path, "apply", qt_release["testProgram"],
                                       ["-test.run=^TestQtNativeHandoff$", "-test.v"], env))
                outcome_cache = cabinet
            else:
                outcome_cache = apply_through_product(tmp_path, ctl, asset, env,
                                                      namespace / "runtime.json", daemon["pid"])
            if gui is not None:
                assert gui.wait(timeout=15) == 0
            try:
                confirmed_outcome(outcome_cache, target_version)
            finally:
                if commands is not None:
                    copy_maintenance_evidence(outcome_cache, tmp_path)

        with running_sidravia(target, profile, namespace, env, tmp_path, "after") as (daemon, _):
            assert (daemon["productVersion"], daemon["buildId"]) == (target_version, target_build)
            assert has_user_path(target)
            assert autostart_mode(tmp_path, "autostart-after-update", manager, env) == "daemon"
            assert_native_payload(target, profile, qt_release["arch"])
        purge(tmp_path, "uninstall", maint, env)
        assert not ctl.exists()
        assert not has_user_path(target)
        assert not (target / "Sidravia.exe").exists()
        assert settings.read_bytes() == b'{"fixture":"keep settings during update and uninstall"}'
        assert unowned.read_bytes() == b"this file is outside the Qt component ownership"
    finally:
        if maint.is_file():
            primary = sys.exception()
            try:
                purge(tmp_path, "cleanup", maint, env)
            except Exception as error:
                if primary is None:
                    raise
                primary.add_note(f"QT cleanup failed after the original error: {error}")
        shutil.rmtree(namespace)


def test_windows_qt_can_remove_an_install_with_a_missing_manager(qt_release, tmp_path):
    setup = qt_release["setup"]
    install_core = [*OPTIONS, "--no-default-installations", "install", "org.sidravia.core"]
    original = tmp_path / "待修复"
    repaired = Path(os.environ["USERPROFILE"]) / "AppData" / "Local" / "Programs" / "Sidravia"
    assert not repaired.exists(), "the default installer path must be unused in the test account"
    require_success(invoke(tmp_path, "initial-install", setup, ["--root", original, *install_core]))
    try:
        manager = original / "sidraviamgr.exe"
        manager.rename(tmp_path / "original-manager.exe")
        saved = original / "user-file.txt"
        saved.write_bytes(b"user-owned")
        maint = original / "sidravia-maintenance.exe"
        purge(tmp_path, "remove-damaged-install", maint)
        assert not (original / "sidraviactl.exe").exists()
        assert saved.read_bytes() == b"user-owned"
        require_success(invoke(tmp_path, "install-verified-offline-copy", setup, install_core))
        require_success(invoke(tmp_path, "verify-recovered-core", repaired / "sidraviactl.exe", ["--help"]))
    finally:
        for directory in (original, repaired):
            maint = directory / "sidravia-maintenance.exe"
            if maint.is_file() and (directory / "components.xml").is_file():
                purge(tmp_path, "recover-cleanup-" + directory.name, maint)
        if repaired.is_dir() and not any(repaired.iterdir()):
            repaired.rmdir()


@pytest.fixture
def foreign_startup_task(tmp_path):
    """Keep another owner's registration intact even when the name collides."""
    powershell = Path(os.environ["SystemRoot"]) / "System32/WindowsPowerShell/v1.0/powershell.exe"
    marker = f"Sidravia Qt acceptance {uuid.uuid4().hex}"
    name = "SidraviaDaemon"

    def run_script(label, script):
        script = "$ProgressPreference = 'SilentlyContinue'; " + script
        encoded = base64.b64encode(script.encode("utf-16le")).decode("ascii")
        result = invoke(tmp_path, label, powershell,
                        ["-NoProfile", "-NonInteractive", "-EncodedCommand", encoded])
        require_success(result)
        return result[1].read_bytes()

    get_task = f"Get-ScheduledTask -TaskName '{name}' -TaskPath '\\'"
    run_script("create-foreign-startup", f"""
$ErrorActionPreference = 'Stop'
if ({get_task} -ErrorAction SilentlyContinue) {{ throw 'fixture account already has a startup task' }}
$sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$action = New-ScheduledTaskAction -Execute (Join-Path $env:SystemRoot 'System32/cmd.exe') -Argument '/d /c exit'
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $sid
$principal = New-ScheduledTaskPrincipal -UserId $sid -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName '{name}' -TaskPath '\\' -Action $action -Trigger $trigger -Principal $principal -Description '{marker}' | Out-Null
""")
    try:
        export = f"Export-ScheduledTask -TaskName '{name}' -TaskPath '\\' -ErrorAction Stop"
        before = run_script("foreign-startup-before", export)
        yield lambda: run_script("foreign-startup-after", export) == before
    finally:
        run_script("cleanup-foreign-startup", f"""
$ErrorActionPreference = 'Stop'
$task = {get_task} -ErrorAction SilentlyContinue
if ($task -and $task.Description -eq '{marker}') {{
    Unregister-ScheduledTask -TaskName '{name}' -TaskPath '\\' -Confirm:$false
}}
""")


def test_windows_qt_uninstall_keeps_foreign_startup(qt_release, foreign_startup_task, tmp_path):
    target = tmp_path / "installation"
    install = [*OPTIONS, "--no-default-installations", "install", "org.sidravia.core"]
    require_success(invoke(tmp_path, "install", qt_release["setup"], ["--root", target, *install]))
    maint = target / "sidravia-maintenance.exe"
    try:
        purge(tmp_path, "uninstall-with-foreign-startup", maint)
        assert not (target / "sidraviactl.exe").exists()
        assert foreign_startup_task()
    finally:
        if maint.is_file():
            invoke(tmp_path, "cleanup", maint, [*OPTIONS, "purge"])


def test_windows_qt_status_marks_abandoned_handoff_unconfirmed(qt_release, tmp_path):
    target = tmp_path / "installation"
    install = [*OPTIONS, "--no-default-installations", "install", "org.sidravia.core"]
    require_success(invoke(tmp_path, "install", qt_release["setup"], ["--root", target, *install]))
    maint = target / "sidravia-maintenance.exe"
    cache = None
    try:
        ctl = target / "sidraviactl.exe"
        cache_parent = Path(os.environ["LOCALAPPDATA"]) / "Sidravia" / "updates"
        before = set(cache_parent.iterdir()) if cache_parent.is_dir() else set()
        result = invoke(tmp_path, "status-before-handoff", ctl, ["update", "status"])
        require_success(result)
        assert json.loads(result[1].read_bytes())["code"] == "not_checked"
        cache, = {entry for entry in cache_parent.iterdir() if entry.is_dir()} - before
        version = json.loads(qt_release["manifest"].read_bytes())["version"]
        (cache / "qt-outcome.json").write_text(
            json.dumps({"targetVersion": version, "state": "pending"}), encoding="utf-8")
        result = invoke(tmp_path, "status-after-abandonment", ctl, ["update", "status"])
        require_success(result)
        assert json.loads(result[1].read_bytes())["result"]["installation"] == {
            "targetVersion": version, "state": "unconfirmed",
        }
    finally:
        if cache is not None:
            shutil.rmtree(cache)
        require_success(invoke(tmp_path, "uninstall", maint, [*OPTIONS, "purge"]))


def test_windows_qt_check_requires_offline_repair_when_registration_is_missing(qt_release, tmp_path):
    target = tmp_path / "installation"
    install = [*OPTIONS, "--no-default-installations", "install", "org.sidravia.core"]
    require_success(invoke(tmp_path, "install", qt_release["setup"], ["--root", target, *install]))
    maintenance = target / "sidravia-maintenance.exe"
    moved = []
    try:
        for name in ("sidravia-maintenance.exe", "components.xml", "sidravia-maintenance.dat",
                     "sidravia-maintenance.ini"):
            source = target / name
            if source.exists():
                saved = tmp_path / name
                source.rename(saved)
                moved.append((saved, source))
        assert not maintenance.exists()
        code, log = invoke(tmp_path, "update-check-without-qt-registration",
                           target / "sidraviactl.exe", ["update", "check"], separate_stderr=True)
        assert code != 0
        assert json.loads(log.read_bytes())["code"] == "invalid_target"
    finally:
        for saved, source in moved:
            saved.rename(source)
        require_success(invoke(tmp_path, "uninstall", maintenance, [*OPTIONS, "purge"]))
