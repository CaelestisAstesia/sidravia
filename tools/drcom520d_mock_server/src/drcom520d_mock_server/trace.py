from __future__ import annotations

import json
import sys
import time
from dataclasses import asdict, dataclass, is_dataclass
from datetime import datetime
from enum import Enum
from ipaddress import IPv4Address
from pathlib import Path
from typing import Any, Protocol, TextIO

from .transcript import TRANSCRIPT_VERSION


class TraceWriteError(RuntimeError):
    pass


@dataclass(frozen=True, slots=True)
class TraceEvent:
    schema_version: int
    transcript_version: int
    sequence: int
    timestamp: str
    monotonic_ns: int
    event: str
    operation: str | None
    endpoint: dict[str, Any] | None
    details: dict[str, Any]


class TraceSink(Protocol):
    def emit(
        self,
        event: str,
        *,
        operation: str | None = None,
        endpoint: tuple[str, int] | None = None,
        details: dict[str, Any] | None = None,
    ) -> TraceEvent | None:
        ...


class NullTraceSink:
    def emit(
        self,
        event: str,
        *,
        operation: str | None = None,
        endpoint: tuple[str, int] | None = None,
        details: dict[str, Any] | None = None,
    ) -> None:
        return None


_EVENT_TITLES = {
    "trace_opened": "追踪文件已打开",
    "configuration_loaded": "配置已加载",
    "server_secret_ready": "服务器密钥已就绪",
    "server_started": "服务器已启动",
    "server_stopping": "服务器正在停止",
    "server_stopped": "服务器已停止",
    "datagram_received": "收到 UDP 报文",
    "operation_classified": "报文操作已分类",
    "packet_parsed": "报文解析成功",
    "packet_parse_failed": "报文解析失败",
    "crypto_check": "密码学校验",
    "validation_decision": "认证判断",
    "scenario_action_selected": "故障场景动作",
    "state_changed": "状态已变化",
    "datagram_sent": "发送 UDP 报文",
    "datagram_dropped": "静默丢弃报文",
}


def to_json_value(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, bytes):
        return value.hex()
    if isinstance(value, IPv4Address):
        return str(value)
    if isinstance(value, Enum):
        return to_json_value(value.value)
    if is_dataclass(value) and not isinstance(value, type):
        return to_json_value(asdict(value))
    if isinstance(value, dict):
        return {str(key): to_json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [to_json_value(item) for item in value]
    if isinstance(value, set):
        converted = [to_json_value(item) for item in value]
        return sorted(converted, key=lambda item: json.dumps(
            item, ensure_ascii=False, sort_keys=True))
    raise TypeError(f"unsupported trace value: {type(value).__name__}")


def default_trace_path(now: datetime | None = None) -> Path:
    current = now or datetime.now().astimezone()
    tool_root = Path(__file__).resolve().parents[2]
    name = current.strftime("%Y-%m-%dT%H-%M-%S.%f%z") + ".jsonl"
    return tool_root / "traces" / name


class TraceRecorder:
    def __init__(
        self,
        jsonl: TextIO,
        *,
        terminal: TextIO = sys.stdout,
        wall_clock=lambda: datetime.now().astimezone(),
        monotonic_ns=time.monotonic_ns,
    ):
        self._jsonl = jsonl
        self._terminal = terminal
        self._wall_clock = wall_clock
        self._monotonic_ns = monotonic_ns
        self._sequence = 0

    @classmethod
    def open(cls, path: Path, *, terminal: TextIO = sys.stdout) -> "TraceRecorder":
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            stream = path.open("x", encoding="utf-8", newline="\n")
        except OSError as error:
            raise TraceWriteError(f"无法创建追踪文件 {path}: {error}") from error
        return cls(stream, terminal=terminal)

    def emit(
        self,
        event: str,
        *,
        operation: str | None = None,
        endpoint: tuple[str, int] | None = None,
        details: dict[str, Any] | None = None,
    ) -> TraceEvent:
        self._sequence += 1
        converted_details = to_json_value(details or {})
        trace_event = TraceEvent(
            schema_version=1,
            transcript_version=TRANSCRIPT_VERSION,
            sequence=self._sequence,
            timestamp=self._wall_clock().isoformat(timespec="microseconds"),
            monotonic_ns=self._monotonic_ns(),
            event=event,
            operation=operation,
            endpoint=(
                {"ip": endpoint[0], "port": endpoint[1]}
                if endpoint is not None else None
            ),
            details=converted_details,
        )
        payload = to_json_value(trace_event)
        title = _EVENT_TITLES.get(event, event)
        terminal_text = (
            f"[{trace_event.sequence:06d}] {title}"
            f" operation={operation or '-'} endpoint={endpoint or '-'}\n"
            + json.dumps(converted_details, ensure_ascii=False, indent=2, sort_keys=True)
            + "\n"
        )
        try:
            self._terminal.write(terminal_text)
            self._terminal.flush()
            self._jsonl.write(json.dumps(
                payload, ensure_ascii=False, sort_keys=True) + "\n")
            self._jsonl.flush()
        except (OSError, UnicodeError) as error:
            raise TraceWriteError(str(error)) from error
        return trace_event

    def close(self) -> None:
        try:
            self._jsonl.close()
        except OSError as error:
            raise TraceWriteError(str(error)) from error
