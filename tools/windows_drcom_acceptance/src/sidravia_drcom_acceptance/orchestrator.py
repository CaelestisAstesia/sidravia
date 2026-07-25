from __future__ import annotations

import getpass
import json
import re
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Protocol

from .artifacts import SensitiveArtifacts
from .processes import CommandRunner


class RunFailed(RuntimeError):
    """A safe, redacted acceptance-run failure."""


class ContractUnavailable(RunFailed):
    """The production CLI has not implemented the acceptance contract."""


_SAFE_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z")
_EXPECTED_CAPABILITIES = {
    "stdin_credentials",
    "application_transcript_v1",
    "transport_observation_v1",
    "session_snapshot_v1",
    "idempotent_cleanup",
}


@dataclass(frozen=True, slots=True)
class RunRequest:
    local_app_data: Path
    run_id: str
    broker_script: Path
    dumpcap_path: Path
    interface_id: str
    duration_seconds: int = 60
    terminal_timeout_seconds: int = 120
    require_keepalive: bool = False


@dataclass(frozen=True, slots=True)
class SafeRunSummary:
    run_id: str
    session_id: str
    passed: bool
    checks: tuple[str, ...]


@dataclass(slots=True)
class RunDependencies:
    preflight: Any
    artifacts: Any
    capture: Any
    session: Any
    tshark: Any
    reporter: Any


class _Runner(Protocol):
    def run(
        self,
        argv: tuple[str, ...],
        *,
        input_text: str | None = None,
        timeout: float = 10.0,
    ) -> Any: ...


def _validate_request(request: RunRequest) -> None:
    if not _SAFE_ID.fullmatch(request.run_id):
        raise ValueError("unsafe acceptance run ID")
    if not 1 <= request.duration_seconds <= 900:
        raise ValueError("capture duration must be between 1 and 900 seconds")
    if not 1 <= request.terminal_timeout_seconds <= 900:
        raise ValueError("terminal timeout must be between 1 and 900 seconds")
    if (
        not request.interface_id
        or len(request.interface_id) > 512
        or any(ord(character) < 32 for character in request.interface_id)
    ):
        raise ValueError("unsafe capture interface identifier")


def write_capture_manifest(
    artifacts: SensitiveArtifacts, request: RunRequest
) -> dict[str, object]:
    """Write the broker's exact, credential-free input schema."""

    _validate_request(request)
    root = artifacts.root.resolve()
    manifest: dict[str, object] = {
        "schema_version": 1,
        "run_root": str(root),
        "dumpcap_path": str(request.dumpcap_path.resolve()),
        "interface_id": request.interface_id,
        "bpf": "udp port 61440",
        "pcap_path": str(artifacts.capture.resolve()),
        "ready_path": str(artifacts.ready.resolve()),
        "stop_path": str(artifacts.stop_signal.resolve()),
        "duration_seconds": request.duration_seconds,
    }
    artifacts.manifest.write_text(
        json.dumps(manifest, ensure_ascii=False, separators=(",", ":")),
        encoding="utf-8",
    )
    return manifest


def build_broker_command(broker_script: Path, manifest_path: Path) -> tuple[str, ...]:
    script = broker_script.resolve()
    manifest = manifest_path.resolve()
    if not script.is_absolute() or not manifest.is_absolute():
        raise ValueError("broker and manifest paths must be absolute")
    return (
        "powershell.exe",
        "-NoProfile",
        "-NonInteractive",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        str(script),
        "-ManifestPath",
        str(manifest),
    )


class CaptureBrokerDriver:
    """Own exactly one normal-token broker process for one acceptance run."""

    def __init__(
        self,
        *,
        process_factory: Callable[..., Any] = subprocess.Popen,
        sleep: Callable[[float], None] = time.sleep,
        monotonic: Callable[[], float] = time.monotonic,
        ready_timeout_seconds: float = 30.0,
        stop_timeout_seconds: float = 15.0,
    ):
        self._process_factory = process_factory
        self._sleep = sleep
        self._monotonic = monotonic
        self._ready_timeout = ready_timeout_seconds
        self._stop_timeout = stop_timeout_seconds
        self._process: Any | None = None
        self._artifacts: SensitiveArtifacts | None = None

    def start(self, request: RunRequest, artifacts: SensitiveArtifacts) -> None:
        if self._process is not None:
            raise RunFailed("抓包代理已启动，拒绝启动第二个代理。")
        write_capture_manifest(artifacts, request)
        command = build_broker_command(request.broker_script, artifacts.manifest)
        self._artifacts = artifacts
        try:
            self._process = self._process_factory(
                command,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                shell=False,
                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            )
            deadline = self._monotonic() + self._ready_timeout
            while self._monotonic() <= deadline:
                if artifacts.ready.is_file():
                    try:
                        ready = json.loads(artifacts.ready.read_text(encoding="utf-8"))
                    except (OSError, json.JSONDecodeError):
                        ready = None
                    if (
                        isinstance(ready, dict)
                        and ready.get("schema_version") == 1
                        and ready.get("status") == "ready"
                        and isinstance(ready.get("dumpcap_pid"), int)
                    ):
                        return
                if self._process.poll() is not None:
                    raise RunFailed("抓包代理在 ready 前退出；输出已隐藏。")
                self._sleep(0.05)
            raise RunFailed("等待抓包代理 ready 超时。")
        except RunFailed:
            self._stop_owned_process(raise_on_nonzero=False)
            raise
        except OSError:
            self._stop_owned_process(raise_on_nonzero=False)
            raise RunFailed("无法启动抓包代理；未启动测试 Session。") from None

    def stop(self) -> None:
        if self._process is None:
            return
        signal_failed = False
        try:
            if self._artifacts is not None:
                self._artifacts.stop_signal.write_bytes(b"")
        except OSError:
            signal_failed = True
        try:
            self._stop_owned_process(raise_on_nonzero=True)
        except RunFailed:
            raise
        if signal_failed:
            raise RunFailed("抓包停止哨兵写入失败；代理进程已执行有界回收。")

    def _stop_owned_process(self, *, raise_on_nonzero: bool) -> None:
        process = self._process
        self._process = None
        self._artifacts = None
        if process is None:
            return
        try:
            returncode = process.wait(timeout=self._stop_timeout)
        except (subprocess.TimeoutExpired, TimeoutError):
            try:
                process.terminate()
            except OSError:
                pass
            try:
                returncode = process.wait(timeout=5.0)
            except (subprocess.TimeoutExpired, TimeoutError):
                try:
                    process.kill()
                    returncode = process.wait(timeout=5.0)
                except (OSError, subprocess.TimeoutExpired, TimeoutError):
                    raise RunFailed("抓包代理进程无法在限定时间内回收。") from None
        if raise_on_nonzero and returncode != 0:
            raise RunFailed("抓包代理未能正常结束；输出已隐藏。")


class SidraviaSessionDriver:
    """Fixed-argv adapter for the future Sidravia acceptance CLI contract."""

    def __init__(
        self,
        executable: Path,
        runner: _Runner | None = None,
        *,
        credential_reader: Callable[[str], str] | None = None,
    ):
        self._executable = str(executable)
        self._runner = runner or CommandRunner()
        self._credential_reader = credential_reader or getpass.getpass
        self.credential_payload_cleared = True

    def probe_contract(self) -> None:
        result = self._runner.run(
            (
                self._executable, "acceptance", "drcom", "contract",
                "--output", "json",
            ),
            timeout=10.0,
        )
        try:
            document = json.loads(result.stdout) if result.returncode == 0 else None
            capabilities = set(document["capabilities"])
            valid = (
                document["schema_version"] == 1
                and capabilities >= _EXPECTED_CAPABILITIES
            )
        except (KeyError, TypeError, ValueError):
            valid = False
        if not valid:
            raise ContractUnavailable(
                "当前 sidravia 尚未提供 Windows Dr.COM 实网验收契约；未触发 UAC。"
            )

    def create_from_stdin(
        self, request: RunRequest, artifacts: SensitiveArtifacts
    ) -> str:
        username: str | None = None
        password: str | None = None
        credential_payload: str | None = None
        self.credential_payload_cleared = False
        try:
            username = self._credential_reader("Dr.COM 用户名（隐藏输入）：")
            password = self._credential_reader("Dr.COM 密码（隐藏输入）：")
            if not username or not password:
                raise RunFailed("凭据不能为空；未创建测试 Session。")
            credential_payload = json.dumps(
                {
                    "schema_version": 1,
                    "run_id": request.run_id,
                    "username": username,
                    "password": password,
                },
                ensure_ascii=False,
                separators=(",", ":"),
            )
            result = self._runner.run(
                (
                    self._executable,
                    "acceptance",
                    "drcom",
                    "session",
                    "create",
                    "--stdin-json",
                ),
                input_text=credential_payload,
                timeout=30.0,
            )
            if result.returncode != 0:
                raise RunFailed("测试 Session 创建失败；命令输出已隐藏。")
            document = json.loads(result.stdout)
            session_id = document.get("session_id")
            if not isinstance(session_id, str) or not _SAFE_ID.fullmatch(session_id):
                raise RunFailed("测试 Session 返回了不安全的标识。")
            return session_id
        except json.JSONDecodeError:
            raise RunFailed("测试 Session 返回了无效 JSON。") from None
        finally:
            credential_payload = None
            password = None
            username = None
            self.credential_payload_cleared = True

    def start(self, session_id: str, artifacts: SensitiveArtifacts) -> None:
        self._require_safe_session_id(session_id)
        self._require_success(
            (
                self._executable,
                "acceptance",
                "drcom",
                "session",
                "start",
                "--id",
                session_id,
                "--transcript",
                str(artifacts.application_transcript),
                "--transport-observation",
                str(artifacts.transport_observation),
            ),
            "测试 Session 启动失败；命令输出已隐藏。",
        )

    def wait_terminal(self, session_id: str, timeout_seconds: int) -> None:
        self._require_safe_session_id(session_id)
        self._require_success(
            (
                self._executable,
                "acceptance",
                "drcom",
                "session",
                "wait",
                "--id",
                session_id,
                "--timeout-seconds",
                str(timeout_seconds),
                "--json",
            ),
            "等待测试 Session 终态失败或超时；命令输出已隐藏。",
            timeout=float(timeout_seconds + 10),
        )

    def snapshot(self, session_id: str, destination: Path) -> None:
        self._require_safe_session_id(session_id)
        result = self._runner.run(
            (
                self._executable,
                "acceptance",
                "drcom",
                "session",
                "snapshot",
                "--id",
                session_id,
                "--json",
            )
        )
        try:
            document = json.loads(result.stdout) if result.returncode == 0 else None
        except json.JSONDecodeError:
            document = None
        if not isinstance(document, dict):
            raise RunFailed("读取 Session Snapshot 失败；命令输出已隐藏。")
        destination.write_text(
            json.dumps(document, ensure_ascii=False, separators=(",", ":")),
            encoding="utf-8",
        )

    def stop(self, session_id: str) -> None:
        self._require_safe_session_id(session_id)
        self._require_success(
            (
                self._executable, "acceptance", "drcom", "session", "stop",
                "--id", session_id,
            ),
            "停止测试 Session 失败；命令输出已隐藏。",
        )

    def delete(self, session_id: str) -> None:
        self._require_safe_session_id(session_id)
        self._require_success(
            (
                self._executable, "acceptance", "drcom", "session", "delete",
                "--id", session_id,
            ),
            "删除测试 Session 失败；命令输出已隐藏。",
        )

    def _require_success(
        self, argv: tuple[str, ...], message: str, *, timeout: float = 30.0
    ) -> None:
        if self._runner.run(argv, timeout=timeout).returncode != 0:
            raise RunFailed(message)

    @staticmethod
    def _require_safe_session_id(session_id: str) -> None:
        if not _SAFE_ID.fullmatch(session_id):
            raise RunFailed("测试 Session 标识不安全，拒绝执行命令。")


class AcceptanceOrchestrator:
    def __init__(self, dependencies: RunDependencies):
        self._dependencies = dependencies

    def run(self, request: RunRequest) -> SafeRunSummary:
        _validate_request(request)
        dependencies = self._dependencies
        artifacts: SensitiveArtifacts | None = None
        session_id: str | None = None
        capture_owned = False
        primary: BaseException | None = None
        summary: SafeRunSummary | None = None

        try:
            dependencies.preflight.require_ready(request)
            dependencies.session.probe_contract()
            artifacts = dependencies.artifacts.create(request)
            session_id = dependencies.session.create_from_stdin(request, artifacts)
            if not _SAFE_ID.fullmatch(session_id):
                raise RunFailed("测试 Session 标识不安全，未启动抓包。")
            capture_owned = True
            dependencies.capture.start(request, artifacts)
            dependencies.session.start(session_id, artifacts)
            dependencies.session.wait_terminal(
                session_id, request.terminal_timeout_seconds
            )
            dependencies.session.snapshot(session_id, artifacts.session_snapshot)
            dependencies.capture.stop()
            capture_owned = False
            dependencies.tshark.extract(request, artifacts)
            checks = tuple(
                dependencies.reporter.compare_and_export(
                    request, artifacts, session_id
                )
            )
            summary = SafeRunSummary(request.run_id, session_id, True, checks)
        except BaseException as error:
            primary = error

        cleanup_failures: list[str] = []
        if capture_owned:
            self._cleanup("capture_stop", dependencies.capture.stop, cleanup_failures)
        if session_id is not None:
            self._cleanup(
                "session_stop",
                lambda: dependencies.session.stop(session_id),
                cleanup_failures,
            )
            self._cleanup(
                "session_delete",
                lambda: dependencies.session.delete(session_id),
                cleanup_failures,
            )
        if artifacts is not None:
            self._cleanup(
                "processes_close", dependencies.tshark.close_processes, cleanup_failures
            )
            self._cleanup(
                "artifacts_cleanup", artifacts.cleanup, cleanup_failures
            )

        if primary is not None or cleanup_failures:
            if isinstance(primary, KeyboardInterrupt):
                reason = "验收已取消"
            elif isinstance(primary, RunFailed):
                reason = str(primary)
            elif primary is not None:
                reason = f"验收失败（{type(primary).__name__}）"
            else:
                reason = "验收主体完成，但清理失败"
            if cleanup_failures:
                reason += "；清理步骤失败：" + "、".join(cleanup_failures)
            raise RunFailed(reason) from None

        if summary is None:
            raise RunFailed("验收未产生安全摘要。")
        return summary

    @staticmethod
    def _cleanup(name: str, action: Callable[[], None], failures: list[str]) -> None:
        try:
            action()
        except BaseException:
            failures.append(name)
