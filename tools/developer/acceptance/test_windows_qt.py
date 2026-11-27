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


OPTIONS = ("--accept-licenses", "--reject-messages", "--confirm-command")


@pytest.fixture(scope="module")
def qt_release():
    request = os.environ.get("SIDRAVIA_QT_ACCEPTANCE_REQUEST")
    if sys.platform != "win32" or not request:
        pytest.skip("needs a disposable native Windows Qt release fixture")
    if os.environ.get("SIDRAVIA_IFW_ISOLATED_WINDOWS") != "1":
        raise pytest.UsageError("Qt lifecycle acceptance requires an isolated Windows account")
    path = Path(request)
    assert path.is_absolute() and path.is_file()
    data = json.loads(path.read_bytes())
    for key in ("setup", "repositories", "manifest", "signature", "testProgram", "maintenanceImage"):
        path = Path(data[key])
        assert path.is_absolute() and path.exists()
        data[key] = path
    manifest = data["manifest"].read_bytes()
    data["assets"] = json.loads(manifest)["assets"]
    data["publisherProof"] = {
        "PublicKey": data["publicKey"],
        "Manifest": base64.b64encode(manifest).decode(),
        "Signature": base64.b64encode(data["signature"].read_bytes()).decode(),
    }
    return data


def invoke(evidence, name, program, args, env=None):
    result = subprocess.run([program, *args], cwd=evidence, env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=65)
    log = evidence / f"{name}.log"
    log.write_bytes(result.stdout)
    return result.returncode, log


def require_success(result):
    code, log = result
    assert code == 0, log.read_text(encoding="utf-8", errors="replace")

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
        yield wait_for_runtime(runtime, gui), gui
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


@pytest.mark.parametrize("profile", ("core", "gui"))
def test_windows_qt_release_lifecycle(qt_release, profile, tmp_path):
    target = tmp_path / "installation"
    cabinet = tmp_path / "prepared"
    cabinet.mkdir()
    maint = target / "sidravia-maintenance.exe"
    ctl = target / "sidraviactl.exe"
    source_version = qt_release["sourceVersion"]
    source_build = qt_release["sourceBuildId"]
    asset = next(a for a in qt_release["assets"] if a["payloadKind"] == profile and a["arch"] == qt_release["arch"])
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
        assert (target / "Sidravia.exe").is_file() == (profile == "gui")
        assert (target / "flutter_windows.dll").is_file() == (profile == "gui")
        with maint.open("rb") as handle:
            assert hashlib.file_digest(handle, "sha256").hexdigest() == image
        shutil.copytree(target / "institution-profiles", namespace / "institution-profiles")
        settings = namespace / "acceptance-settings.json"
        settings.write_bytes(b'{"fixture":"keep settings during update and uninstall"}')
        unowned = target / "acceptance-user-file.txt"
        unowned.write_bytes(b"this file is outside the Qt component ownership")
        test_request = tmp_path / "handoff.json"
        test_request.write_text(json.dumps({
            "Installation": str(target), "SourceVersion": source_version, "SourceBuild": source_build,
            "Archive": str(qt_release["repositories"] / asset["name"]), "Cache": str(cabinet),
            "MaintenanceSHA256": image, **qt_release["publisherProof"],
            "Candidate": dict(version=target_version, name=asset["name"], size=asset["size"], sha256=asset["sha256"]),
        }), encoding="utf-8")
        env["SIDRAVIA_TEST_QT_HANDOFF"] = str(test_request)

        with running_sidravia(target, profile, namespace, env, tmp_path, "before") as (daemon, gui):
            assert (daemon["productVersion"], daemon["buildId"]) == (source_version, source_build)
            code, _ = invoke(tmp_path, "running-uninstall-refused", maint, [*OPTIONS, "purge"], env)
            assert code != 0 and ctl.is_file()
            require_success(invoke(tmp_path, "apply", qt_release["testProgram"],
                                   ["-test.run=^TestQtNativeHandoff$", "-test.v"], env))
            if gui is not None:
                assert gui.wait(timeout=15) == 0
            confirmed_outcome(cabinet, target_version)

        with running_sidravia(target, profile, namespace, env, tmp_path, "after") as (daemon, _):
            assert (daemon["productVersion"], daemon["buildId"]) == (target_version, target_build)
            assert has_user_path(target)
            assert autostart_mode(tmp_path, "autostart-after-update", manager, env) == "daemon"
            assert (target / "Sidravia.exe").is_file() == (profile == "gui")
        require_success(invoke(tmp_path, "uninstall", maint, [*OPTIONS, "purge"], env))
        assert not ctl.exists()
        assert not has_user_path(target)
        assert not (target / "Sidravia.exe").exists()
        assert settings.read_bytes() == b'{"fixture":"keep settings during update and uninstall"}'
        assert unowned.read_bytes() == b"this file is outside the Qt component ownership"
    finally:
        if maint.is_file():
            invoke(tmp_path, "cleanup", maint, [*OPTIONS, "purge"], env)
        shutil.rmtree(namespace)


def test_windows_qt_can_remove_an_install_with_a_missing_manager(qt_release, tmp_path):
    setup = qt_release["setup"]
    install_core = [*OPTIONS, "--no-default-installations", "install", "org.sidravia.core"]
    original = tmp_path / "damaged-installation"
    repaired = tmp_path / "repaired-installation"
    require_success(invoke(tmp_path, "initial-install", setup, ["--root", original, *install_core]))
    try:
        manager = original / "sidraviamgr.exe"
        manager.rename(tmp_path / "original-manager.exe")
        saved = original / "user-file.txt"
        saved.write_bytes(b"user-owned")
        maint = original / "sidravia-maintenance.exe"
        require_success(invoke(tmp_path, "remove-damaged-install", maint, [*OPTIONS, "purge"]))
        assert not (original / "sidraviactl.exe").exists()
        assert saved.read_bytes() == b"user-owned"
        require_success(invoke(tmp_path, "install-verified-offline-copy", setup, ["--root", repaired, *install_core]))
        require_success(invoke(tmp_path, "verify-recovered-core", repaired / "sidraviactl.exe", ["--help"]))
    finally:
        for directory in (original, repaired):
            maint = directory / "sidravia-maintenance.exe"
            if maint.is_file():
                require_success(invoke(tmp_path, "recover-cleanup-" + directory.name, maint, [*OPTIONS, "purge"]))
