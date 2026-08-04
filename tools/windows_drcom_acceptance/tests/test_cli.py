import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from sidravia_drcom_acceptance.__main__ import (
    CliDependencies,
    build_parser,
    main,
)
from sidravia_drcom_acceptance.model import (
    ComparisonCheck,
    ComparisonFailure,
    ComparisonResult,
    EvidenceError,
)
from sidravia_drcom_acceptance.preflight import (
    CheckStatus,
    PreflightCheck,
    PreflightReport,
)


class FakePreflight:
    def __init__(self, report):
        self.report = report
        self.calls = 0

    def run(self):
        self.calls += 1
        return self.report


def report(status=CheckStatus.PASS):
    return PreflightReport((PreflightCheck(
        "fixture", "测试前置", status,
        "前置满足。" if status is CheckStatus.PASS else "缺少工具。",
        None if status is CheckStatus.PASS else "安装 Wireshark/Npcap。",
    ),))


class CliTests(unittest.TestCase):
    def setUp(self):
        self.stdout = io.StringIO()
        self.stderr = io.StringIO()

    def dependencies(self, preflight=None, **kwargs):
        return CliDependencies(
            preflight=preflight or FakePreflight(report()),
            stdout=self.stdout,
            stderr=self.stderr,
            **kwargs,
        )

    def test_help_is_chinese_and_has_no_credential_arguments(self):
        help_text = build_parser().format_help()
        self.assertIn("离线验收与实网研究基础设施", help_text)
        self.assertIn("--preflight", help_text)
        self.assertIn("--validate", help_text)
        self.assertNotIn("--run", help_text)
        self.assertNotIn("--password", help_text)
        self.assertNotIn("--username", help_text)

    def test_preflight_returns_two_for_missing_tools_and_prints_chinese(self):
        preflight = FakePreflight(report(CheckStatus.FAIL))
        status = main(["--preflight"], dependencies=self.dependencies(preflight))
        self.assertEqual(status, 2)
        self.assertIn("预检未通过", self.stdout.getvalue())
        self.assertIn("未触发 UAC", self.stdout.getvalue())
        self.assertEqual(self.stderr.getvalue(), "")

    @patch("sidravia_drcom_acceptance.__main__.compare_evidence")
    @patch("sidravia_drcom_acceptance.__main__.load_tshark_json")
    @patch("sidravia_drcom_acceptance.__main__.load_session_snapshot")
    @patch("sidravia_drcom_acceptance.__main__.load_transport_observation")
    @patch("sidravia_drcom_acceptance.__main__.load_transcript")
    def test_validate_loads_all_evidence_and_returns_comparison_status(
        self, load_transcript, load_transport, load_snapshot, load_tshark, compare
    ):
        load_transcript.return_value = ("transcript",)
        load_transport.return_value = "transport"
        load_snapshot.return_value = "snapshot"
        load_tshark.return_value = ("pcap",)
        compare.return_value = ComparisonResult(
            True, 7, (ComparisonCheck("identity", True, "证据一致。"),), None
        )
        paths = [Path("transcript.jsonl"), Path("transport.json"),
                 Path("snapshot.json"), Path("tshark.json")]
        status = main([
            "--validate", "--transcript", str(paths[0]),
            "--transport", str(paths[1]), "--snapshot", str(paths[2]),
            "--tshark-json", str(paths[3]), "--require-keepalive",
        ], dependencies=self.dependencies())
        self.assertEqual(status, 0)
        compare.assert_called_once_with(
            ("transcript",), "transport", "snapshot", ("pcap",),
            require_keepalive=True,
        )
        self.assertIn("离线三方证据比对通过", self.stdout.getvalue())

    @patch("sidravia_drcom_acceptance.__main__.load_transcript")
    def test_validate_evidence_error_is_safe_and_returns_two(self, load_transcript):
        load_transcript.side_effect = EvidenceError("bad transcript fixture")
        status = main([
            "--validate", "--transcript", "a", "--transport", "b",
            "--snapshot", "c", "--tshark-json", "d",
        ], dependencies=self.dependencies())
        self.assertEqual(status, 2)
        self.assertIn("证据无效", self.stderr.getvalue())
        self.assertNotIn("Traceback", self.stderr.getvalue())

    def test_run_is_rejected_by_parser_before_preflight(self):
        preflight = FakePreflight(report())
        with self.assertRaises(SystemExit) as raised:
            main(["--run"], dependencies=self.dependencies(preflight))
        self.assertEqual(raised.exception.code, 2)
        self.assertEqual(preflight.calls, 0)

    def test_internal_error_returns_three_without_raw_exception_or_traceback(self):
        secret = "SENTINEL-SECRET"
        preflight = FakePreflight(report())
        preflight.run = lambda: (_ for _ in ()).throw(RuntimeError(secret))
        status = main(["--preflight"], dependencies=self.dependencies(preflight))
        self.assertEqual(status, 3)
        self.assertNotIn(secret, self.stderr.getvalue())
        self.assertNotIn("Traceback", self.stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
