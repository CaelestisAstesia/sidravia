import asyncio
import tempfile
import unittest
import json
from argparse import Namespace
from contextlib import redirect_stderr
from datetime import datetime, timezone
from io import StringIO
from pathlib import Path
from unittest.mock import AsyncMock, Mock, patch

from drcom520d_mock_server.__main__ import (
    build_argument_parser,
    build_ready_payload,
    example_accounts_path,
    main,
    select_accounts_path,
    select_trace_path,
    serve,
    warn_if_non_loopback,
)
from drcom520d_mock_server.trace import TraceWriteError


class CliTests(unittest.TestCase):
    def parse_error(self, arguments: list[str]) -> str:
        stderr = StringIO()
        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            build_argument_parser().parse_args(arguments)
        return stderr.getvalue()

    def test_cli_defaults_to_loopback_and_jlu_port(self):
        args = build_argument_parser().parse_args(["--accounts", "accounts.json"])
        self.assertEqual((args.listen_host, args.port), ("127.0.0.1", 61440))

    def test_non_loopback_bind_logs_chinese_warning(self):
        with self.assertLogs("drcom520d_mock_server", level="WARNING") as captured:
            warn_if_non_loopback("0.0.0.0")
        self.assertIn("仅应在隔离测试网络", " ".join(captured.output))

    def test_help_never_accepts_plaintext_password(self):
        help_text = build_argument_parser().format_help()
        self.assertNotIn("--password", help_text)

    def test_example_and_accounts_are_required_and_mutually_exclusive(self):
        parser = build_argument_parser()
        with self.assertRaises(SystemExit):
            parser.parse_args([])
        with self.assertRaises(SystemExit):
            parser.parse_args(["--example", "--accounts", "accounts.json"])
        example = parser.parse_args(["--example"])
        custom = parser.parse_args(["--accounts", "accounts.json"])
        self.assertTrue(example.example)
        self.assertEqual(select_accounts_path(custom), Path("accounts.json"))

    def test_required_and_mutually_exclusive_errors_are_chinese(self):
        required = self.parse_error([])
        self.assertIn("错误：必须指定 --example 或 --accounts 之一", required)
        self.assertNotIn("required", required)
        self.assertNotIn("error:", required)

        exclusive = self.parse_error([
            "--example", "--accounts", "accounts.json",
        ])
        self.assertIn("错误：不能同时使用 --example 和 --accounts", exclusive)
        self.assertNotIn("not allowed", exclusive)
        self.assertNotIn("error:", exclusive)

    def test_invalid_choice_and_non_integer_port_errors_are_chinese(self):
        invalid_choice = self.parse_error([
            "--example", "--scenario", "impossible",
        ])
        self.assertIn("参数 --scenario 的值无效", invalid_choice)
        self.assertIn("可选值", invalid_choice)
        self.assertNotIn("invalid choice", invalid_choice)

        invalid_port = self.parse_error([
            "--example", "--port", "not-a-port",
        ])
        self.assertIn("参数 --port：端口必须是整数", invalid_port)
        self.assertNotIn("port must be", invalid_port)

    def test_missing_value_and_unknown_argument_errors_are_chinese(self):
        missing_value = self.parse_error(["--example", "--port"])
        self.assertIn("参数 --port：需要一个值", missing_value)
        self.assertNotIn("expected one argument", missing_value)

        unknown = self.parse_error(["--example", "--unknown"])
        self.assertIn("无法识别的参数：--unknown", unknown)
        self.assertNotIn("unrecognized arguments", unknown)

    def test_abbreviated_option_is_reported_as_unknown_in_chinese(self):
        abbreviated = self.parse_error(["--example", "--s"])
        self.assertIn("无法识别的参数：--s", abbreviated)
        self.assertNotIn("ambiguous option", abbreviated)

    def test_explicit_value_for_flag_is_rejected_in_chinese(self):
        explicit_value = self.parse_error(["--example=true"])
        self.assertIn("参数 --example：不接受显式值 'true'", explicit_value)
        self.assertNotIn("ignored explicit argument", explicit_value)

    def test_example_resolves_the_single_fictional_account_file(self):
        path = example_accounts_path()
        self.assertEqual(
            path.as_posix().split("/")[-3:],
            ["drcom520d_mock_server", "examples", "accounts.json"],
        )
        self.assertEqual(
            select_accounts_path(
                build_argument_parser().parse_args(["--example"])),
            path,
        )
        payload = json.loads(path.read_text(encoding="utf-8"))
        self.assertTrue(payload["server"]["credentials_are_fictional"])
        self.assertEqual(len(payload["accounts"]), 1)
        account = payload["accounts"][0]
        self.assertEqual(account["username"], "student-test")
        self.assertEqual(account["password"], "local-test-password")
        self.assertEqual(account["expected_ipv4"], "10.0.0.2")
        self.assertEqual(account["expected_mac"], "02:00:00:00:00:01")

    def test_help_is_chinese_and_documents_complete_trace(self):
        help_text = build_argument_parser().format_help()
        self.assertIn("本地 Dr.COM", help_text)
        self.assertIn("用法：", help_text)
        self.assertIn("选项：", help_text)
        self.assertIn("显示帮助并退出", help_text)
        self.assertIn("--example", help_text)
        self.assertIn("--trace-file", help_text)
        self.assertIn("完整追踪", help_text)
        self.assertNotIn("Local-only", help_text)
        self.assertNotIn("show this help message", help_text)

    def test_explicit_and_default_trace_paths(self):
        explicit = build_argument_parser().parse_args(
            ["--example", "--trace-file", "D:/trace/run.jsonl"])
        self.assertEqual(select_trace_path(explicit), Path("D:/trace/run.jsonl"))
        default = build_argument_parser().parse_args(["--example"])
        path = select_trace_path(
            default,
            now=datetime(2026, 7, 22, tzinfo=timezone.utc),
        )
        self.assertEqual(path.name, "2026-07-22T00-00-00.000000+0000.jsonl")

    def test_serve_serializes_trace_path_before_udp_bind(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        trace_path = Path(directory.name) / "trace.jsonl"
        args = build_argument_parser().parse_args([
            "--example",
            "--trace-file", str(trace_path),
        ])

        async def exercise() -> BaseException | None:
            loop = asyncio.get_running_loop()
            with patch.object(
                loop,
                "create_datagram_endpoint",
                new=AsyncMock(side_effect=OSError("stop after trace")),
            ):
                try:
                    await serve(args)
                except BaseException as error:
                    return error
            return None

        error = asyncio.run(exercise())
        self.assertIsInstance(error, OSError)
        self.assertEqual(str(error), "stop after trace")
        rows = [
            json.loads(line)
            for line in trace_path.read_text(encoding="utf-8").splitlines()
        ]
        self.assertEqual(rows[0]["event"], "trace_opened")
        self.assertEqual(rows[0]["details"]["path"], str(trace_path.resolve()))
        self.assertIsInstance(rows[0]["details"]["path"], str)
        configuration = next(
            row for row in rows if row["event"] == "configuration_loaded"
        )
        self.assertEqual(
            configuration["details"]["accounts_path"],
            str(example_accounts_path().resolve()),
        )
        self.assertIsInstance(
            configuration["details"]["accounts_path"], str)

    def test_port_zero_ready_file_and_logout_exit_arguments(self):
        args = build_argument_parser().parse_args([
            "--example",
            "--port", "0",
            "--ready-file", "ready.json",
            "--exit-after-logout",
        ])
        self.assertEqual(args.port, 0)
        self.assertEqual(args.ready_file, Path("ready.json"))
        self.assertTrue(args.exit_after_logout)

    def test_ready_payload_uses_actual_bound_port_and_transcript_version(self):
        payload = build_ready_payload(
            host="127.0.0.1",
            port=49152,
            trace_file=Path("trace.jsonl"),
            scenario="normal",
            status="ready",
            pid=1234,
        )
        self.assertEqual(payload["port"], 49152)
        self.assertEqual(payload["schema_version"], 1)
        self.assertEqual(payload["transcript_version"], 1)
        self.assertEqual(payload["status"], "ready")

    def test_main_reports_trace_failure_in_chinese_and_exits_two(self):
        parser = Mock()
        parser.parse_args.return_value = Namespace()
        parser.exit.side_effect = SystemExit(2)
        with (
            patch(
                "drcom520d_mock_server.__main__.build_argument_parser",
                return_value=parser,
            ),
            patch(
                "drcom520d_mock_server.__main__.serve",
                new=AsyncMock(side_effect=TraceWriteError("test failure")),
            ),
            self.assertRaisesRegex(SystemExit, "2"),
        ):
            main()

        code, message = parser.exit.call_args.args
        self.assertEqual(code, 2)
        self.assertIn("致命追踪错误", message)
