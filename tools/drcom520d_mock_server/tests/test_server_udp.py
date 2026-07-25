import asyncio
import json
import tempfile
import unittest
from contextlib import redirect_stderr
from io import StringIO
from ipaddress import IPv4Address
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock, patch

from drcom520d_mock_server.config import ConfigurationError
from drcom520d_mock_server.models import Account, ApplicationConfig, ServerSettings
from drcom520d_mock_server.scenarios import builtin_scenario
from drcom520d_mock_server.server import ServerCore
from drcom520d_mock_server.__main__ import (
    DrcomDatagramProtocol,
    build_argument_parser,
    build_ready_payload,
    example_accounts_path,
    serve,
    write_ready_file,
)
from drcom520d_mock_server.trace import TraceWriteError
from tools.drcom520d_mock_server.tests.packet_factory import (
    build_ka1_request,
    build_ka2_request,
    build_login_request,
    build_logout_request,
)


class ClientProtocol(asyncio.DatagramProtocol):
    def __init__(self):
        self.responses: asyncio.Queue[bytes] = asyncio.Queue()

    def datagram_received(self, data: bytes, addr: tuple[str, int]) -> None:
        self.responses.put_nowait(data)


class ExplodingCore:
    def __init__(self, error: BaseException | None = None):
        self.started = asyncio.Event()
        self.error = error or RuntimeError("test-only core failure")

    async def handle_datagram(
        self,
        data: bytes,
        endpoint: tuple[str, int],
        *,
        local_endpoint: tuple[str, int] | None = None,
    ) -> None:
        self.started.set()
        raise self.error


class RecordingCore:
    def __init__(self, response: bytes | None):
        self.response = response
        self.local_endpoints: list[tuple[str, int] | None] = []

    async def handle_datagram(
        self,
        data: bytes,
        endpoint: tuple[str, int],
        *,
        local_endpoint: tuple[str, int] | None = None,
    ) -> bytes | None:
        self.local_endpoints.append(local_endpoint)
        return self.response


class RecordingTransport:
    def __init__(self, order: list[str] | None = None):
        self.order = order if order is not None else []
        self.sent: list[tuple[bytes, tuple[str, int]]] = []

    def get_extra_info(self, name: str):
        return ("127.0.0.1", 49152) if name == "sockname" else None

    def sendto(self, data: bytes, addr: tuple[str, int]) -> None:
        self.order.append("sendto")
        self.sent.append((data, addr))

    def close(self) -> None:
        self.order.append("close")


class FailingSendTransport(RecordingTransport):
    def sendto(self, data: bytes, addr: tuple[str, int]) -> None:
        self.order.append("sendto")
        raise OSError("test send failure")


class FakeRecorder:
    def __init__(self):
        self.events: list[dict[str, object]] = []
        self.closed = False

    def emit(self, event: str, **fields: object) -> None:
        self.events.append({"event": event, **fields})

    def close(self) -> None:
        self.closed = True


class MemoryTrace:
    def __init__(self):
        self.events: list[dict[str, object]] = []

    def emit(self, event, *, operation=None, endpoint=None, details=None):
        self.events.append({
            "event": event,
            "operation": operation,
            "endpoint": endpoint,
            "details": details or {},
        })
        return None


class TransientStoppingRecorder(FakeRecorder):
    def __init__(self):
        super().__init__()
        self.error = TraceWriteError("transient server_stopping failure")
        self.failed = False

    def emit(self, event: str, **fields: object) -> None:
        if event == "server_stopping" and not self.failed:
            self.failed = True
            raise self.error
        super().emit(event, **fields)


class ServeCore:
    def __init__(self, *, error: TraceWriteError | None = None):
        self.error = error
        self.started = asyncio.Event()
        self.secret = b"e" * 32
        self.sessions: dict[tuple[str, int], object] = {}
        self.metrics = SimpleNamespace(
            datagrams_received=0,
            logins_succeeded=0,
            logins_rejected=0,
            datagrams_dropped=0,
            keepalives_accepted=0,
        )

    async def handle_datagram(
        self,
        data: bytes,
        endpoint: tuple[str, int],
        *,
        local_endpoint: tuple[str, int] | None = None,
    ) -> bytes | None:
        self.started.set()
        self.metrics.datagrams_received += 1
        if self.error is not None:
            raise self.error
        if data.startswith(b"\x06\x01"):
            return b"\x04\0\0\0"
        return None


class UdpAdapterTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        account = Account(
            username="student-test", password="local-test-password", enabled=True,
            frozen=False, require_dhcp=False, expected_ipv4=IPv4Address("10.0.0.2"),
            expected_mac=bytes.fromhex("020000000001"), bind_ipv4_and_mac=True,
            max_sessions=1, balance_cents=10000,
        )
        config = ApplicationConfig(
            server=ServerSettings(
                challenge_ttl_seconds=10, session_ttl_seconds=90,
                max_sessions_per_account=1, auth_version=bytes.fromhex("2c00"),
                keep_alive_version=bytes.fromhex("dc02"),
                control_check_status=bytes.fromhex("20"), ipdog=bytes.fromhex("01"),
                initial_month_traffic_kib=14208, initial_balance_cents=10000,
                server_secret=b"s" * 32,
            ),
            accounts={account.username: account},
        )
        self.trace = MemoryTrace()
        loop = asyncio.get_running_loop()
        self.server_transport, _ = await loop.create_datagram_endpoint(
            lambda: DrcomDatagramProtocol(ServerCore(
                config, builtin_scenario("normal"), trace=self.trace)),
            local_addr=("127.0.0.1", 0),
        )
        server_port = self.server_transport.get_extra_info("sockname")[1]
        self.client = ClientProtocol()
        self.client_transport, _ = await loop.create_datagram_endpoint(
            lambda: self.client, remote_addr=("127.0.0.1", server_port),
        )

    async def asyncTearDown(self):
        self.client_transport.close()
        self.server_transport.close()

    async def _exchange(self, packet: bytes) -> bytes:
        self.client_transport.sendto(packet)
        return await asyncio.wait_for(self.client.responses.get(), timeout=1)

    async def test_real_loopback_exchanges_challenge_login_keepalives_and_logout(self):
        challenge = await self._exchange(b"\x01\x02" + b"\0" * 20)
        self.assertEqual((challenge[0], len(challenge)), (0x02, 16))
        salt = challenge[4:8]

        login = await self._exchange(build_login_request(salt=salt))
        self.assertEqual((login[0], len(login)), (0x04, 64))
        auth_info = login[23:39]

        ka1 = await self._exchange(build_ka1_request(salt, "local-test-password", auth_info))
        self.assertEqual((ka1[0], len(ka1)), (0x07, 20))

        ka2_type1 = await self._exchange(
            build_ka2_request(0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27"))
        self.assertEqual((ka2_type1[0], ka2_type1[5], len(ka2_type1)), (0x07, 1, 60))
        ka2_type3 = await self._exchange(
            build_ka2_request(1, 3, ka2_type1[16:20], "10.0.0.2", b"\xdc\x02"))
        self.assertEqual((ka2_type3[0], ka2_type3[5], len(ka2_type3)), (0x07, 3, 60))

        logout = await self._exchange(build_logout_request(
            "student-test", "local-test-password", salt,
            bytes.fromhex("020000000001"), auth_info))
        self.assertEqual(logout, b"\x04\0\0\0")

        trace_rows = self.trace.events
        event_names = [row["event"] for row in trace_rows]
        for required in (
            "datagram_received",
            "operation_classified",
            "packet_parsed",
            "crypto_check",
            "validation_decision",
            "state_changed",
            "datagram_sent",
        ):
            with self.subTest(event=required):
                self.assertIn(required, event_names)
        self.assertEqual(
            [
                row["operation"]
                for row in trace_rows
                if row["event"] == "datagram_received"
            ],
            ["challenge", "login", "ka1", "ka2", "ka2", "logout"],
        )
        self.assertTrue(any(
            row["details"].get("password") == "local-test-password"
            for row in trace_rows
        ))

    async def test_adapter_observes_core_task_failures_without_loop_exceptions(self):
        loop = asyncio.get_running_loop()
        previous_handler = loop.get_exception_handler()
        loop_exceptions: list[dict[str, object]] = []
        loop.set_exception_handler(
            lambda _loop, context: loop_exceptions.append(context))
        failing_core = ExplodingCore()
        failing_protocol = DrcomDatagramProtocol(failing_core)
        failing_transport, _ = await loop.create_datagram_endpoint(
            lambda: failing_protocol,
            local_addr=("127.0.0.1", 0),
        )
        failing_port = failing_transport.get_extra_info("sockname")[1]
        client_transport, _ = await loop.create_datagram_endpoint(
            asyncio.DatagramProtocol, remote_addr=("127.0.0.1", failing_port),
        )
        try:
            with self.assertLogs("drcom520d_mock_server", level="ERROR") as captured:
                client_transport.sendto(b"\x99")
                await asyncio.wait_for(failing_core.started.wait(), timeout=1)
                await asyncio.sleep(0)
            self.assertIn("udp_handler_failed", " ".join(captured.output))
            self.assertEqual(failing_protocol._pending_tasks, set())
            self.assertEqual(loop_exceptions, [])
        finally:
            client_transport.close()
            failing_transport.close()
            loop.set_exception_handler(previous_handler)

    async def test_adapter_passes_actual_transport_sockname_to_core(self):
        core = RecordingCore(None)
        protocol = DrcomDatagramProtocol(core)
        protocol.connection_made(RecordingTransport())

        await protocol._respond(b"\x99", ("127.0.0.1", 60000))

        self.assertEqual(core.local_endpoints, [("127.0.0.1", 49152)])

    async def test_trace_write_failure_calls_fatal_and_reclaims_pending_task(self):
        loop = asyncio.get_running_loop()
        previous_handler = loop.get_exception_handler()
        loop_exceptions: list[dict[str, object]] = []
        loop.set_exception_handler(
            lambda _loop, context: loop_exceptions.append(context))
        error = TraceWriteError("runtime trace failure")
        core = ExplodingCore(error)
        fatal_errors: list[BaseException] = []
        protocol = DrcomDatagramProtocol(core, on_fatal=fatal_errors.append)
        protocol.connection_made(RecordingTransport())
        try:
            protocol.datagram_received(b"\x99", ("127.0.0.1", 60000))
            await asyncio.wait_for(core.started.wait(), timeout=1)
            await asyncio.sleep(0)

            self.assertEqual(fatal_errors, [error])
            self.assertEqual(protocol._pending_tasks, set())
            self.assertEqual(loop_exceptions, [])
        finally:
            protocol.connection_lost(None)
            loop.set_exception_handler(previous_handler)

    async def test_non_trace_failure_logs_only_safe_event(self):
        core = ExplodingCore(RuntimeError("do-not-log-this-secret"))
        fatal_errors: list[BaseException] = []
        protocol = DrcomDatagramProtocol(core, on_fatal=fatal_errors.append)
        protocol.connection_made(RecordingTransport())

        with self.assertLogs("drcom520d_mock_server", level="ERROR") as captured:
            protocol.datagram_received(b"\x99", ("127.0.0.1", 60000))
            await asyncio.wait_for(core.started.wait(), timeout=1)
            await asyncio.sleep(0)

        output = " ".join(captured.output)
        self.assertIn("udp_handler_failed", output)
        self.assertNotIn("do-not-log-this-secret", output)
        self.assertEqual(fatal_errors, [])
        self.assertEqual(protocol._pending_tasks, set())

    async def test_successful_logout_ack_is_sent_before_completion_callback(self):
        order: list[str] = []
        core = RecordingCore(b"\x04\0\0\0")
        protocol = DrcomDatagramProtocol(
            core, on_logout_complete=lambda: order.append("complete"))
        transport = RecordingTransport(order)
        protocol.connection_made(transport)

        await protocol._respond(b"\x06\x01request", ("127.0.0.1", 60000))

        self.assertEqual(order, ["sendto", "complete"])

    async def test_failed_or_silent_logout_does_not_complete(self):
        for response in (b"\x05\0\0\0", None):
            with self.subTest(response=response):
                completions: list[str] = []
                protocol = DrcomDatagramProtocol(
                    RecordingCore(response),
                    on_logout_complete=lambda: completions.append("complete"),
                )
                protocol.connection_made(RecordingTransport())

                await protocol._respond(
                    b"\x06\x01request", ("127.0.0.1", 60000))

                self.assertEqual(completions, [])

    async def test_send_failure_does_not_complete_logout(self):
        completions: list[str] = []
        protocol = DrcomDatagramProtocol(
            RecordingCore(b"\x04\0\0\0"),
            on_logout_complete=lambda: completions.append("complete"),
        )
        protocol.connection_made(FailingSendTransport())

        with self.assertRaisesRegex(OSError, "test send failure"):
            await protocol._respond(
                b"\x06\x01request", ("127.0.0.1", 60000))

        self.assertEqual(completions, [])

    async def test_invalid_and_forged_logout_do_not_complete(self):
        cases = (
            (b"\x06\x02invalid", b"\x04\0\0\0"),
            (b"\x06\x01forged", None),
        )
        for request, response in cases:
            with self.subTest(request=request, response=response):
                completions: list[str] = []
                protocol = DrcomDatagramProtocol(
                    RecordingCore(response),
                    on_logout_complete=lambda: completions.append("complete"),
                )
                protocol.connection_made(RecordingTransport())

                await protocol._respond(request, ("127.0.0.1", 60000))

                self.assertEqual(completions, [])


class ServeLifecycleTests(unittest.IsolatedAsyncioTestCase):
    async def _wait_for_ready(self, path: Path) -> dict[str, object]:
        async def load_when_ready() -> dict[str, object]:
            while True:
                if path.exists():
                    payload = json.loads(path.read_text(encoding="utf-8"))
                    if payload["status"] == "ready":
                        return payload
                await asyncio.sleep(0.01)

        return await asyncio.wait_for(load_when_ready(), timeout=1)

    async def test_fictional_gate_rejects_before_trace_or_bind(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        raw = json.loads(example_accounts_path().read_text(encoding="utf-8"))
        raw["server"]["credentials_are_fictional"] = False
        accounts = Path(directory.name) / "accounts.json"
        accounts.write_text(json.dumps(raw), encoding="utf-8")
        args = build_argument_parser().parse_args([
            "--accounts", str(accounts),
            "--trace-file", str(Path(directory.name) / "trace.jsonl"),
        ])
        recorder_open = Mock()
        create_endpoint = AsyncMock(
            side_effect=AssertionError("must not bind"))
        loop = asyncio.get_running_loop()

        with (
            patch("drcom520d_mock_server.__main__.TraceRecorder.open", recorder_open),
            patch.object(loop, "create_datagram_endpoint", create_endpoint),
            self.assertRaisesRegex(
                ConfigurationError, "完整追踪只允许使用虚构凭据"),
        ):
            await serve(args)

        recorder_open.assert_not_called()
        create_endpoint.assert_not_awaited()

    async def test_trace_open_failure_happens_before_udp_bind(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        args = build_argument_parser().parse_args([
            "--example",
            "--trace-file", str(Path(directory.name) / "trace.jsonl"),
        ])
        error = TraceWriteError("cannot create trace")
        create_endpoint = AsyncMock(
            side_effect=AssertionError("must not bind"))
        loop = asyncio.get_running_loop()

        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                side_effect=error,
            ),
            patch.object(loop, "create_datagram_endpoint", create_endpoint),
            self.assertRaisesRegex(TraceWriteError, "cannot create trace"),
        ):
            await serve(args)

        create_endpoint.assert_not_awaited()

    async def test_core_initialization_failure_closes_trace_before_bind(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        args = build_argument_parser().parse_args([
            "--example",
            "--trace-file", str(Path(directory.name) / "trace.jsonl"),
        ])
        recorder = FakeRecorder()
        create_endpoint = AsyncMock(
            side_effect=AssertionError("must not bind"))
        loop = asyncio.get_running_loop()

        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                side_effect=RuntimeError("core initialization failed"),
            ),
            patch.object(loop, "create_datagram_endpoint", create_endpoint),
            self.assertRaisesRegex(RuntimeError, "core initialization failed"),
        ):
            await serve(args)

        self.assertTrue(recorder.closed)
        create_endpoint.assert_not_awaited()

    async def test_port_zero_ready_and_stopped_files_use_actual_endpoint(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        trace_path = root / "trace.jsonl"
        ready_path = root / "ready.json"
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--trace-file", str(trace_path),
            "--ready-file", str(ready_path),
            "--exit-after-logout",
        ])
        recorder = FakeRecorder()
        core = ServeCore()

        def make_core(config, scenario, *, trace):
            self.assertIs(trace, recorder)
            return core

        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                side_effect=make_core,
            ),
        ):
            task = asyncio.create_task(serve(args))
            client_transport = None
            try:
                ready = await self._wait_for_ready(ready_path)
                self.assertNotEqual(ready["port"], 0)
                ready_event = next(
                    event for event in recorder.events
                    if event["event"] == "server_ready")
                self.assertEqual(
                    ready_event["details"]["port"], ready["port"])

                client = ClientProtocol()
                client_transport, _ = await asyncio.get_running_loop().create_datagram_endpoint(
                    lambda: client,
                    remote_addr=(str(ready["host"]), int(ready["port"])),
                )
                client_transport.sendto(b"\x06\x01request")
                self.assertEqual(
                    await asyncio.wait_for(client.responses.get(), timeout=1),
                    b"\x04\0\0\0",
                )
                await asyncio.wait_for(task, timeout=1)
            finally:
                if client_transport is not None:
                    client_transport.close()
                if not task.done():
                    task.cancel()
                    await asyncio.gather(task, return_exceptions=True)

        stopped = json.loads(ready_path.read_text(encoding="utf-8"))
        self.assertEqual(stopped["status"], "stopped")
        self.assertEqual(stopped["stop_reason"], "logout_complete")
        self.assertEqual(stopped["transcript_version"], 1)
        self.assertEqual(stopped["port"], ready["port"])
        self.assertFalse(list(root.glob("ready.json.*.tmp")))
        self.assertTrue(recorder.closed)
        self.assertEqual(
            [event["event"] for event in recorder.events],
            [
                "trace_opened",
                "configuration_loaded",
                "server_secret_ready",
                "server_ready",
                "server_stopping",
                "metrics",
                "server_stopped",
            ],
        )

    async def test_startup_safe_logs_never_include_trace_secrets(self):
        for json_events in (False, True):
            with self.subTest(json_events=json_events):
                directory = tempfile.TemporaryDirectory()
                self.addCleanup(directory.cleanup)
                root = Path(directory.name)
                ready_path = root / "ready.json"
                argv = [
                    "--example",
                    "--port", "0",
                    "--trace-file", str(root / "trace.jsonl"),
                    "--ready-file", str(ready_path),
                    "--exit-after-logout",
                ]
                if json_events:
                    argv.append("--json-events")
                args = build_argument_parser().parse_args(argv)
                recorder = FakeRecorder()
                core = ServeCore()
                stderr = StringIO()

                with (
                    redirect_stderr(stderr),
                    patch(
                        "drcom520d_mock_server.__main__.TraceRecorder.open",
                        return_value=recorder,
                    ),
                    patch(
                        "drcom520d_mock_server.__main__.ServerCore",
                        return_value=core,
                    ),
                ):
                    task = asyncio.create_task(serve(args))
                    client_transport = None
                    try:
                        ready = await self._wait_for_ready(ready_path)
                        client = ClientProtocol()
                        client_transport, _ = await asyncio.get_running_loop().create_datagram_endpoint(
                            lambda: client,
                            remote_addr=(
                                str(ready["host"]), int(ready["port"])),
                        )
                        client_transport.sendto(b"\x06\x01request")
                        await asyncio.wait_for(client.responses.get(), timeout=1)
                        await asyncio.wait_for(task, timeout=1)
                    finally:
                        if client_transport is not None:
                            client_transport.close()
                        if not task.done():
                            task.cancel()
                            await asyncio.gather(task, return_exceptions=True)

                output = stderr.getvalue()
                self.assertIn("student-test", output)
                for forbidden in (
                    "password",
                    "local-test-password",
                    "server_secret",
                    "11" * 32,
                    "65" * 32,
                    "digest",
                    "raw",
                    "AuthInfo",
                    "Tail",
                ):
                    with self.subTest(
                            json_events=json_events, forbidden=forbidden):
                        self.assertNotIn(forbidden, output)

    async def test_runtime_trace_failure_stops_and_is_rethrown(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        ready_path = root / "ready.json"
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--trace-file", str(root / "trace.jsonl"),
            "--ready-file", str(ready_path),
        ])
        recorder = FakeRecorder()
        core = ServeCore(error=TraceWriteError("runtime trace failure"))

        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                return_value=core,
            ),
        ):
            task = asyncio.create_task(serve(args))
            client_transport = None
            try:
                ready = await self._wait_for_ready(ready_path)
                client_transport, _ = await asyncio.get_running_loop().create_datagram_endpoint(
                    asyncio.DatagramProtocol,
                    remote_addr=(str(ready["host"]), int(ready["port"])),
                )
                client_transport.sendto(b"\x99")
                with self.assertRaisesRegex(
                        TraceWriteError, "runtime trace failure"):
                    await asyncio.wait_for(task, timeout=1)
            finally:
                if client_transport is not None:
                    client_transport.close()
                if not task.done():
                    task.cancel()
                    await asyncio.gather(task, return_exceptions=True)

        stopped = json.loads(ready_path.read_text(encoding="utf-8"))
        self.assertEqual(stopped["status"], "stopped")
        self.assertEqual(stopped["stop_reason"], "trace_write_error")
        self.assertEqual(stopped["transcript_version"], 1)
        self.assertTrue(recorder.closed)

    async def test_fatal_stop_reason_wins_over_later_logout_completion(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        ready_path = root / "ready.json"
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--trace-file", str(root / "trace.jsonl"),
            "--ready-file", str(ready_path),
            "--exit-after-logout",
        ])
        recorder = FakeRecorder()
        core = ServeCore()
        primary = TraceWriteError("primary runtime trace failure")
        transport = RecordingTransport()

        async def create_endpoint(factory, *, local_addr):
            protocol = factory()
            protocol.connection_made(transport)
            protocol.on_fatal(primary)
            protocol.on_logout_complete()
            return transport, protocol

        loop = asyncio.get_running_loop()
        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                return_value=core,
            ),
            patch.object(
                loop,
                "create_datagram_endpoint",
                side_effect=create_endpoint,
            ),
        ):
            with self.assertRaises(TraceWriteError) as captured:
                await serve(args)

        self.assertIs(captured.exception, primary)
        stopped = json.loads(ready_path.read_text(encoding="utf-8"))
        self.assertEqual(stopped["status"], "stopped")
        self.assertEqual(stopped["stop_reason"], "trace_write_error")
        self.assertTrue(recorder.closed)

    def test_ready_file_writer_atomically_replaces_complete_payload(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        ready_path = Path(directory.name) / "nested" / "ready.json"
        ready = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=Path(directory.name) / "trace.jsonl",
            scenario="normal",
            status="ready",
            pid=1234,
        )
        write_ready_file(ready_path, ready)
        stopped = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=Path(directory.name) / "trace.jsonl",
            scenario="normal",
            status="stopped",
            stop_reason="logout_complete",
            pid=1234,
        )
        write_ready_file(ready_path, stopped)

        self.assertEqual(
            json.loads(ready_path.read_text(encoding="utf-8")), stopped)
        self.assertFalse(list(ready_path.parent.glob("ready.json.*.tmp")))

    def test_ready_file_mkdir_failure_is_a_trace_error(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        ready_path = Path(directory.name) / "nested" / "ready.json"
        payload = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=Path(directory.name) / "trace.jsonl",
            scenario="normal",
            status="ready",
            pid=1234,
        )

        with (
            patch.object(
                Path, "mkdir", side_effect=OSError("mkdir failed")),
            self.assertRaisesRegex(
                TraceWriteError, "无法写入 ready 文件.*mkdir failed"),
        ):
            write_ready_file(ready_path, payload)

    def test_ready_file_replace_failure_cleans_temporary_file(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        ready_path = root / "ready.json"
        payload = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=root / "trace.jsonl",
            scenario="normal",
            status="ready",
            pid=1234,
        )

        with (
            patch(
                "drcom520d_mock_server.__main__.os.replace",
                side_effect=OSError("replace failed"),
            ),
            self.assertRaisesRegex(
                TraceWriteError, "无法写入 ready 文件.*replace failed"),
        ):
            write_ready_file(ready_path, payload)

        self.assertFalse(list(root.glob("ready.json.*.tmp")))

    def test_ready_file_cleanup_failure_does_not_mask_replace_error(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        ready_path = root / "ready.json"
        payload = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=root / "trace.jsonl",
            scenario="normal",
            status="ready",
            pid=1234,
        )

        with (
            patch(
                "drcom520d_mock_server.__main__.os.replace",
                side_effect=OSError("replace failed"),
            ),
            patch.object(
                Path, "unlink", side_effect=OSError("cleanup failed")),
            self.assertRaises(TraceWriteError) as captured,
        ):
            write_ready_file(ready_path, payload)

        self.assertIn("replace failed", str(captured.exception))
        self.assertNotIn("cleanup failed", str(captured.exception))
        self.assertIsInstance(captured.exception.__cause__, OSError)
        self.assertEqual(str(captured.exception.__cause__), "replace failed")

    async def test_stopped_ready_failure_closes_recorder_and_preserves_primary(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--trace-file", str(root / "trace.jsonl"),
            "--ready-file", str(root / "ready.json"),
        ])
        recorder = FakeRecorder()
        core = ServeCore()
        primary = TraceWriteError("primary trace failure")
        transport = RecordingTransport()

        async def create_endpoint(factory, *, local_addr):
            protocol = factory()
            protocol.connection_made(transport)
            protocol.on_fatal(primary)
            return transport, protocol

        ready_writer = Mock(side_effect=[
            None,
            OSError("stopped ready write failed"),
        ])
        loop = asyncio.get_running_loop()
        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                return_value=core,
            ),
            patch(
                "drcom520d_mock_server.__main__.write_ready_file",
                ready_writer,
            ),
            patch.object(
                loop,
                "create_datagram_endpoint",
                side_effect=create_endpoint,
            ),
        ):
            with self.assertRaises(TraceWriteError) as captured:
                await serve(args)

        self.assertIs(captured.exception, primary)
        self.assertTrue(recorder.closed)
        self.assertEqual(ready_writer.call_count, 2)

    async def test_recovered_stopped_event_uses_trace_failure_reason(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        ready_path = root / "ready.json"
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--trace-file", str(root / "trace.jsonl"),
            "--ready-file", str(ready_path),
            "--exit-after-logout",
        ])
        recorder = TransientStoppingRecorder()
        core = ServeCore()
        transport = RecordingTransport()

        async def create_endpoint(factory, *, local_addr):
            protocol = factory()
            protocol.connection_made(transport)
            protocol.on_logout_complete()
            return transport, protocol

        loop = asyncio.get_running_loop()
        with (
            patch(
                "drcom520d_mock_server.__main__.TraceRecorder.open",
                return_value=recorder,
            ),
            patch(
                "drcom520d_mock_server.__main__.ServerCore",
                return_value=core,
            ),
            patch.object(
                loop,
                "create_datagram_endpoint",
                side_effect=create_endpoint,
            ),
        ):
            with self.assertRaises(TraceWriteError) as captured:
                await serve(args)

        self.assertIs(captured.exception, recorder.error)
        server_stopped = next(
            event for event in recorder.events
            if event["event"] == "server_stopped")
        self.assertEqual(
            server_stopped["details"]["stop_reason"],
            "trace_write_error",
        )
        stopped = json.loads(ready_path.read_text(encoding="utf-8"))
        self.assertEqual(stopped["stop_reason"], "trace_write_error")
        self.assertTrue(recorder.closed)
