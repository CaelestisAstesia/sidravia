import io
import json
import tempfile
import unittest
from dataclasses import dataclass
from datetime import datetime, timezone
from enum import Enum
from ipaddress import IPv4Address
from pathlib import Path

from drcom520d_mock_server.trace import (
    TraceRecorder,
    TraceWriteError,
    default_trace_path,
    to_json_value,
)
from drcom520d_mock_server.transcript import (
    TRANSCRIPT_VERSION,
    decision_record,
    packet_record,
)


class SampleKind(str, Enum):
    LOGIN = "login"


@dataclass(frozen=True)
class SampleState:
    secret: bytes
    address: IPv4Address


class FailingWriter(io.StringIO):
    def write(self, value: str) -> int:
        raise OSError("disk full")


class RecordingWriter(io.StringIO):
    def __init__(self):
        super().__init__()
        self.calls: list[str] = []

    def write(self, value: str) -> int:
        self.calls.append("write")
        return super().write(value)

    def flush(self) -> None:
        self.calls.append("flush")
        super().flush()


class TraceTests(unittest.TestCase):
    def test_serializes_protocol_values_without_repr_fallback(self):
        value = {
            "bytes": bytes.fromhex("00a1ff"),
            "kind": SampleKind.LOGIN,
            "state": SampleState(b"secret", IPv4Address("10.0.0.2")),
            "set": {"b", "a"},
        }
        self.assertEqual(to_json_value(value), {
            "bytes": "00a1ff",
            "kind": "login",
            "state": {"secret": "736563726574", "address": "10.0.0.2"},
            "set": ["a", "b"],
        })
        with self.assertRaises(TypeError):
            to_json_value(object())

    def test_terminal_and_jsonl_share_sequence_and_complete_details(self):
        terminal = io.StringIO()
        jsonl = io.StringIO()
        recorder = TraceRecorder(
            jsonl, terminal=terminal,
            wall_clock=lambda: datetime(2026, 7, 22, tzinfo=timezone.utc),
            monotonic_ns=lambda: 123,
        )
        first = recorder.emit(
            "configuration_loaded",
            details={"password": "local-test-password", "secret": b"s" * 32},
        )
        second = recorder.emit(
            "datagram_received",
            operation="login",
            endpoint=("127.0.0.1", 50000),
            details={"packet": bytes.fromhex("0301")},
        )
        rows = [json.loads(line) for line in jsonl.getvalue().splitlines()]
        self.assertEqual([first.sequence, second.sequence], [1, 2])
        self.assertEqual([row["sequence"] for row in rows], [1, 2])
        self.assertEqual(rows[0]["details"]["password"], "local-test-password")
        self.assertEqual(rows[0]["details"]["secret"], "73" * 32)
        self.assertEqual(rows[1]["details"]["packet"], "0301")
        self.assertIn("[000001]", terminal.getvalue())
        self.assertIn("local-test-password", terminal.getvalue())
        self.assertIn("0301", terminal.getvalue())

    def test_default_path_is_timestamped_under_tool_traces(self):
        path = default_trace_path(datetime(
            2026, 7, 22, 12, 34, 56, 123456, tzinfo=timezone.utc))
        self.assertEqual(path.parent.name, "traces")
        self.assertEqual(path.name, "2026-07-22T12-34-56.123456+0000.jsonl")

    def test_write_failure_is_a_fatal_trace_error(self):
        recorder = TraceRecorder(FailingWriter(), terminal=io.StringIO())
        with self.assertRaisesRegex(TraceWriteError, "disk full"):
            recorder.emit("datagram_received", details={"packet": b"x"})

    def test_each_emit_writes_then_flushes_terminal_and_jsonl(self):
        terminal = RecordingWriter()
        jsonl = RecordingWriter()
        recorder = TraceRecorder(jsonl, terminal=terminal)

        recorder.emit("trace_opened")
        self.assertEqual(terminal.calls, ["write", "flush"])
        self.assertEqual(jsonl.calls, ["write", "flush"])

        recorder.emit("server_started")
        self.assertEqual(terminal.calls, ["write", "flush", "write", "flush"])
        self.assertEqual(jsonl.calls, ["write", "flush", "write", "flush"])

    def test_packet_record_has_version_and_complete_wire_identity(self):
        record = packet_record(
            side="server",
            direction="send",
            operation="login",
            payload=bytes.fromhex("0301"),
            local_endpoint=("127.0.0.1", 61440),
            remote_endpoint=("127.0.0.1", 50000),
        )
        self.assertEqual(record["transcript_version"], TRANSCRIPT_VERSION)
        self.assertEqual(record["payload_hex"], "0301")
        self.assertEqual(
            record["payload_sha256"],
            "67294d0eff78c6dbf4ae91576d495f81ca8f9967119cff318132f79d41285dfc",
        )
        self.assertEqual(record["payload_length"], 2)

    def test_packet_record_rejects_invalid_side_and_direction(self):
        args = {
            "operation": "login",
            "payload": b"x",
            "local_endpoint": None,
            "remote_endpoint": None,
        }
        with self.assertRaisesRegex(ValueError, "side"):
            packet_record(side="proxy", direction="send", **args)
        with self.assertRaisesRegex(ValueError, "direction"):
            packet_record(side="server", direction="forward", **args)

    def test_decision_record_uses_the_shared_transcript_version(self):
        record = decision_record(
            operation="login",
            outcome="accepted",
            wire_error_code=None,
            drop_reason=None,
        )
        self.assertEqual(record["transcript_version"], TRANSCRIPT_VERSION)


if __name__ == "__main__":
    unittest.main()
