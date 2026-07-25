"""Local-only asyncio UDP adapter and command-line entry point."""

from __future__ import annotations

import argparse
import asyncio
import ipaddress
import json
import logging
import os
import signal
import sys
from collections.abc import Callable
from datetime import datetime
from pathlib import Path
from typing import Any

from .config import (
    ConfigurationError,
    load_application_config,
    require_fictional_credentials,
)
from .scenarios import ScenarioError, builtin_scenario, load_scenario
from .server import ServerCore
from .trace import TraceRecorder, TraceWriteError, default_trace_path
from .transcript import TRANSCRIPT_VERSION


LOGGER = logging.getLogger("drcom520d_mock_server")


class ChineseArgumentParser(argparse.ArgumentParser):
    @staticmethod
    def _localize(text: str) -> str:
        return (text
                .replace("usage:", "用法：", 1)
                .replace("\noptions:\n", "\n选项：\n")
                .replace(
                    "show this help message and exit",
                    "显示帮助并退出",
                ))

    def format_usage(self) -> str:
        return self._localize(super().format_usage())

    def format_help(self) -> str:
        return self._localize(super().format_help())

    @staticmethod
    def _localize_error(message: str) -> str:
        if message.startswith("unrecognized arguments: "):
            arguments = message.removeprefix("unrecognized arguments: ")
            return f"无法识别的参数：{arguments}"
        if message.startswith("the following arguments are required: "):
            arguments = message.removeprefix(
                "the following arguments are required: ")
            return f"缺少必需参数：{arguments}"
        if message == "one of the arguments --example --accounts is required":
            return "必须指定 --example 或 --accounts 之一"
        if (
            "not allowed with argument" in message
            and "--example" in message
            and "--accounts" in message
        ):
            return "不能同时使用 --example 和 --accounts"
        if message.startswith("argument ") and ": invalid choice: " in message:
            argument, remainder = message[len("argument "):].split(
                ": invalid choice: ", 1)
            value, choices = remainder.rsplit(" (choose from ", 1)
            return (
                f"参数 {argument} 的值无效：{value}；"
                f"可选值：{choices.removesuffix(')')}")
        if message.startswith("argument ") and ": " in message:
            argument, detail = message[len("argument "):].split(": ", 1)
            if detail.startswith("ignored explicit argument "):
                value = detail.removeprefix("ignored explicit argument ")
                detail = f"不接受显式值 {value}"
            else:
                detail = {
                    "expected one argument": "需要一个值",
                    "expected at least one argument": "至少需要一个值",
                    "expected at most one argument": "最多允许一个值",
                }.get(detail, detail)
            return f"参数 {argument}：{detail}"
        return message

    def error(self, message: str) -> None:
        self.print_usage(sys.stderr)
        self.exit(2, f"{self.prog}: 错误：{self._localize_error(message)}\n")


class JsonEventFormatter(logging.Formatter):
    """Emit one safe, structured JSON object for every local event."""

    def format(self, record: logging.LogRecord) -> str:
        return json.dumps({
            "level": record.levelname,
            "event": record.getMessage(),
        }, ensure_ascii=False)


class DrcomDatagramProtocol(asyncio.DatagramProtocol):
    """Thin transport adapter around the independently tested server core."""

    def __init__(
        self,
        core: ServerCore,
        *,
        on_fatal: Callable[[BaseException], None] | None = None,
        on_logout_complete: Callable[[], None] | None = None,
    ):
        self.core = core
        self.on_fatal = on_fatal
        self.on_logout_complete = on_logout_complete
        self.transport: asyncio.DatagramTransport | None = None
        self.local_endpoint: tuple[str, int] | None = None
        self._pending_tasks: set[asyncio.Task[None]] = set()

    def connection_made(self, transport: asyncio.BaseTransport) -> None:
        self.transport = transport  # type: ignore[assignment]
        sockname = transport.get_extra_info("sockname")
        self.local_endpoint = (str(sockname[0]), int(sockname[1]))

    def datagram_received(self, data: bytes, addr: tuple[str, int]) -> None:
        task = asyncio.create_task(self._respond(data, addr))
        self._pending_tasks.add(task)
        task.add_done_callback(self._task_done)

    async def _respond(self, data: bytes, addr: tuple[str, int]) -> None:
        response = await self.core.handle_datagram(
            data, addr, local_endpoint=self.local_endpoint)
        if response is not None and self.transport is not None:
            self.transport.sendto(response, addr)
            if (
                data.startswith(b"\x06\x01")
                and response == b"\x04\0\0\0"
                and self.on_logout_complete is not None
            ):
                self.on_logout_complete()

    def _task_done(self, task: asyncio.Task[None]) -> None:
        self._pending_tasks.discard(task)
        if task.cancelled():
            return
        error = task.exception()
        if error is None:
            return
        if isinstance(error, TraceWriteError) and self.on_fatal is not None:
            self.on_fatal(error)
            return
        LOGGER.error("udp_handler_failed")

    def connection_lost(self, exc: Exception | None) -> None:
        self.transport = None
        self.local_endpoint = None
        for task in tuple(self._pending_tasks):
            task.cancel()

    def error_received(self, exc: Exception) -> None:
        LOGGER.warning("udp_error=%s", type(exc).__name__)


def build_argument_parser() -> argparse.ArgumentParser:
    parser = ChineseArgumentParser(
        description="本地 Dr.COM 5.2.0(D) 模拟 UDP 服务器。",
        allow_abbrev=False,
    )
    accounts = parser.add_mutually_exclusive_group(required=True)
    accounts.add_argument(
        "--example",
        action="store_true",
        help="使用 examples/accounts.json 中的虚构示例账户",
    )
    accounts.add_argument(
        "--accounts",
        type=Path,
        help="严格 JSON 账户配置文件",
    )
    parser.add_argument("--listen-host", default="127.0.0.1",
                        help="UDP 绑定地址（默认：127.0.0.1）")
    parser.add_argument("--port", type=_port, default=61440,
                        help="UDP 端口 0..65535；0 自动选择空闲端口（默认：61440）")
    parser.add_argument("--scenario", default="normal",
                        choices=("normal", "busy-then-success", "flaky-login",
                                 "keepalive-timeout", "session-expired",
                                 "malformed-response"),
                        help="内置确定性故障场景")
    parser.add_argument("--scenario-file", type=Path,
                        help="严格 JSON 场景文件；覆盖 --scenario")
    parser.add_argument(
        "--trace-file",
        type=Path,
        help="完整追踪 JSONL 文件；省略时自动写入 traces/时间戳.jsonl",
    )
    parser.add_argument(
        "--ready-file",
        type=Path,
        help="绑定成功后原子写入实际地址和端口的 ready JSON",
    )
    parser.add_argument(
        "--exit-after-logout",
        action="store_true",
        help="成功发送 Logout ACK 后自动停止服务器",
    )
    parser.add_argument("--log-level", default="INFO",
                        choices=("DEBUG", "INFO", "WARNING", "ERROR"),
                        help="安全事件日志级别")
    parser.add_argument("--json-events", action="store_true",
                        help="以 JSON Lines 输出安全事件日志")
    return parser


def example_accounts_path() -> Path:
    return Path(__file__).resolve().parents[2] / "examples" / "accounts.json"


def select_accounts_path(args: argparse.Namespace) -> Path:
    return example_accounts_path() if args.example else args.accounts


def select_trace_path(
    args: argparse.Namespace,
    *,
    now: datetime | None = None,
) -> Path:
    return args.trace_file or default_trace_path(now)


def build_ready_payload(
    *,
    host: str,
    port: int,
    trace_file: Path,
    scenario: str,
    status: str,
    pid: int | None = None,
    stop_reason: str | None = None,
) -> dict[str, object]:
    payload: dict[str, object] = {
        "schema_version": 1,
        "transcript_version": TRANSCRIPT_VERSION,
        "status": status,
        "host": host,
        "port": port,
        "pid": os.getpid() if pid is None else pid,
        "trace_file": str(trace_file.resolve()),
        "scenario": scenario,
    }
    if stop_reason is not None:
        payload["stop_reason"] = stop_reason
    return payload


def write_ready_file(path: Path, payload: dict[str, object]) -> None:
    resolved_path = path
    temporary: Path | None = None
    primary_error: OSError | None = None
    try:
        resolved_path = path.resolve()
        resolved_path.parent.mkdir(parents=True, exist_ok=True)
        temporary = resolved_path.with_name(
            f"{resolved_path.name}.{os.getpid()}.tmp")
        with temporary.open("x", encoding="utf-8", newline="\n") as stream:
            json.dump(payload, stream, ensure_ascii=False, sort_keys=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, resolved_path)
    except OSError as error:
        primary_error = error
    finally:
        if temporary is not None:
            try:
                temporary.unlink(missing_ok=True)
            except OSError as cleanup_error:
                if primary_error is None:
                    primary_error = cleanup_error
    if primary_error is not None:
        raise TraceWriteError(
            f"无法写入 ready 文件 {resolved_path}: {primary_error}"
        ) from primary_error


def _port(value: str) -> int:
    try:
        port = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("端口必须是整数") from error
    if not 0 <= port <= 65535:
        raise argparse.ArgumentTypeError("端口必须是 0..65535 的整数")
    return port


def warn_if_non_loopback(host: str) -> None:
    """Warn for a bind address outside the deliberately local default."""
    try:
        is_loopback = ipaddress.ip_address(host).is_loopback
    except ValueError:
        is_loopback = host.lower() == "localhost"
    if not is_loopback:
        LOGGER.warning("非回环绑定 %s 仅应在隔离测试网络上使用。", host)


def configure_logging(level: str, json_events: bool) -> None:
    handler = logging.StreamHandler()
    handler.setFormatter(JsonEventFormatter() if json_events else logging.Formatter(
        "%(levelname)s %(name)s %(message)s"))
    LOGGER.handlers.clear()
    LOGGER.addHandler(handler)
    LOGGER.setLevel(level)
    LOGGER.propagate = False


def _metrics_payload(core: ServerCore) -> dict[str, Any]:
    metrics = core.metrics
    return {
        "datagrams_received": metrics.datagrams_received,
        "logins_succeeded": metrics.logins_succeeded,
        "logins_rejected": metrics.logins_rejected,
        "datagrams_dropped": metrics.datagrams_dropped,
        "keepalives_accepted": metrics.keepalives_accepted,
        "active_sessions": len(core.sessions),
    }


async def serve(args: argparse.Namespace) -> None:
    """Validate inputs before binding, then run until a termination signal."""
    accounts_path = select_accounts_path(args)
    config = load_application_config(accounts_path)
    require_fictional_credentials(config)
    scenario = (
        load_scenario(args.scenario_file)
        if args.scenario_file else builtin_scenario(args.scenario)
    )
    scenario_name = (
        args.scenario_file.name if args.scenario_file else args.scenario)
    trace_path = select_trace_path(args).resolve()
    recorder = TraceRecorder.open(trace_path)
    try:
        core = ServerCore(config, scenario, trace=recorder)
        recorder.emit(
            "trace_opened",
            details={"path": str(trace_path), "schema_version": 1},
        )
        recorder.emit(
            "configuration_loaded",
            details={
                "accounts_path": str(accounts_path.resolve()),
                "config": config,
            },
        )
        recorder.emit(
            "server_secret_ready",
            details={
                "configured_secret": config.server.server_secret,
                "effective_secret": core.secret,
            },
        )
    except BaseException:
        try:
            recorder.close()
        except TraceWriteError:
            pass
        raise

    fatal_error: TraceWriteError | None = None
    transport: asyncio.DatagramTransport | None = None
    protocol: DrcomDatagramProtocol | None = None
    actual_host = args.listen_host
    actual_port = args.port
    stop_reason = "signal"

    configure_logging(args.log_level, args.json_events)
    LOGGER.warning("完整追踪会记录全部虚构密码和协议秘密；严禁使用真实凭据。")
    for account in config.accounts.values():
        LOGGER.warning(
            "虚构账户 username=%s ipv4=%s mac=%s",
            account.username,
            account.expected_ipv4,
            account.expected_mac.hex(":") if account.expected_mac else None,
        )
    LOGGER.info(
        "准备绑定 host=%s port=%d scenario=%s trace=%s accounts=%s",
        args.listen_host,
        args.port,
        scenario_name,
        trace_path,
        accounts_path.resolve(),
    )
    warn_if_non_loopback(args.listen_host)
    loop = asyncio.get_running_loop()
    stop = asyncio.Event()

    def stop_after_logout() -> None:
        nonlocal stop_reason
        if stop_reason == "trace_write_error":
            return
        stop_reason = "logout_complete"
        loop.call_soon(stop.set)

    def stop_after_fatal(error: BaseException) -> None:
        nonlocal fatal_error, stop_reason
        if isinstance(error, TraceWriteError) and fatal_error is None:
            fatal_error = error
        stop_reason = "trace_write_error"
        stop.set()

    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, stop.set)
        except (NotImplementedError, RuntimeError):
            pass

    try:
        transport, protocol = await loop.create_datagram_endpoint(
            lambda: DrcomDatagramProtocol(
                core,
                on_fatal=stop_after_fatal,
                on_logout_complete=(
                    stop_after_logout if args.exit_after_logout else None),
            ),
            local_addr=(args.listen_host, args.port),
        )
        sockname = transport.get_extra_info("sockname")
        actual_host, actual_port = str(sockname[0]), int(sockname[1])
        ready = build_ready_payload(
            host=actual_host,
            port=actual_port,
            trace_file=trace_path,
            scenario=scenario_name,
            status="ready",
        )
        recorder.emit("server_ready", details=ready)
        if args.ready_file is not None:
            write_ready_file(args.ready_file, ready)
        LOGGER.info(
            "已监听 host=%s port=%d scenario=%s",
            actual_host,
            actual_port,
            scenario_name,
        )
        await stop.wait()
    except TraceWriteError as error:
        if fatal_error is None:
            fatal_error = error
        stop_reason = "trace_write_error"
    finally:
        if transport is not None:
            transport.close()
        await asyncio.sleep(0)
        if protocol is not None and protocol._pending_tasks:
            pending = tuple(protocol._pending_tasks)
            for task in pending:
                task.cancel()
            await asyncio.gather(*pending, return_exceptions=True)

        try:
            metrics = _metrics_payload(core)
            for event in ("server_stopping", "metrics", "server_stopped"):
                if event == "metrics":
                    details = metrics
                elif event == "server_stopped":
                    details = {
                        "stop_reason": stop_reason,
                        "metrics": metrics,
                    }
                else:
                    details = {"stop_reason": stop_reason}
                try:
                    recorder.emit(event, details=details)
                except TraceWriteError as error:
                    if fatal_error is None:
                        fatal_error = error
                        stop_reason = "trace_write_error"

            if args.ready_file is not None and transport is not None:
                try:
                    stopped = build_ready_payload(
                        host=actual_host,
                        port=actual_port,
                        trace_file=trace_path,
                        scenario=scenario_name,
                        status="stopped",
                        stop_reason=stop_reason,
                    )
                    write_ready_file(args.ready_file, stopped)
                except (OSError, TraceWriteError) as error:
                    if fatal_error is None:
                        fatal_error = (
                            error if isinstance(error, TraceWriteError)
                            else TraceWriteError(
                                f"无法写入 stopped ready 文件: {error}"))
                        stop_reason = "trace_write_error"
        finally:
            try:
                recorder.close()
            except TraceWriteError as error:
                if fatal_error is None:
                    fatal_error = error
        LOGGER.info("metrics %s", json.dumps(metrics, sort_keys=True))

    if fatal_error is not None:
        raise fatal_error


def main() -> None:
    parser = build_argument_parser()
    args = parser.parse_args()
    try:
        asyncio.run(serve(args))
    except TraceWriteError as error:
        parser.exit(2, f"drcom520d-mock-server: 致命追踪错误：{error}\n")
    except (ConfigurationError, ScenarioError, OSError) as error:
        parser.exit(2, f"drcom520d-mock-server: 启动失败：{error}\n")
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
