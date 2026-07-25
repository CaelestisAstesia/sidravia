import tempfile
import unittest
from pathlib import Path

from sidravia_drcom_acceptance.artifacts import (
    ArtifactSecurityError,
    CleanupError,
    SensitiveArtifacts,
)
from sidravia_drcom_acceptance.processes import CommandResult


class FakeRunner:
    def __init__(self, *, acl_status: int = 0):
        self.acl_status = acl_status
        self.calls: list[tuple[str, ...]] = []

    def run(self, argv, *, input_text=None, timeout=10.0):
        call = tuple(str(part) for part in argv)
        self.calls.append(call)
        if Path(call[0]).name.lower() == "whoami.exe":
            return CommandResult(call, 0,
                                 '"TEST\\User","S-1-5-21-111-222-333-1001"\n', "")
        return CommandResult(call, self.acl_status, "", "fixture failure")


class SensitiveArtifactTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.local_app_data = Path(directory.name)

    def test_create_resolves_sid_then_hardens_acl_before_returning_paths(self):
        runner = FakeRunner()
        artifacts = SensitiveArtifacts.create(
            self.local_app_data, runner, run_id="11111111-1111-1111-1111-111111111111"
        )
        self.addCleanup(artifacts.cleanup)

        self.assertEqual(Path(runner.calls[0][0]).name.lower(), "whoami.exe")
        self.assertEqual(Path(runner.calls[1][0]).name.lower(), "icacls.exe")
        acl_call = runner.calls[1]
        self.assertIn("/inheritance:r", acl_call)
        self.assertTrue(any("S-1-5-21-111-222-333-1001" in part for part in acl_call))
        self.assertTrue(any("S-1-5-18" in part for part in acl_call))
        self.assertTrue(artifacts.root.is_dir())
        self.assertTrue(str(artifacts.root).startswith(str(self.local_app_data)))

    def test_acl_failure_removes_new_directory_and_exposes_no_command_output(self):
        runner = FakeRunner(acl_status=5)
        with self.assertRaisesRegex(ArtifactSecurityError, "ACL") as captured:
            SensitiveArtifacts.create(self.local_app_data, runner, run_id="run-acl-failure")
        root = self.local_app_data / "Sidravia" / "acceptance" / "run-acl-failure"
        self.assertFalse(root.exists())
        self.assertNotIn("fixture failure", str(captured.exception))

    def test_invalid_whoami_output_fails_closed(self):
        class InvalidRunner(FakeRunner):
            def run(self, argv, **kwargs):
                call = tuple(str(part) for part in argv)
                self.calls.append(call)
                return CommandResult(call, 0, "not-a-sid", "")

        with self.assertRaisesRegex(ArtifactSecurityError, "SID"):
            SensitiveArtifacts.create(self.local_app_data, InvalidRunner(), run_id="invalid-sid")

    def test_cleanup_attempts_every_sensitive_file_and_root_after_failures(self):
        root = self.local_app_data / "run"
        root.mkdir()
        attempted: list[str] = []

        def unlink(path: Path):
            attempted.append(path.name)
            if path.name == "capture.pcap":
                raise PermissionError("locked")
            path.unlink()

        def rmtree(path: Path):
            attempted.append(path.name)
            raise PermissionError("root locked")

        artifacts = SensitiveArtifacts(root, unlink=unlink, rmtree=rmtree,
                                       sleep=lambda _seconds: None,
                                       retry_delays=(0.0,))
        for path in artifacts.sensitive_paths:
            path.write_text("fixture", encoding="utf-8")

        with self.assertRaises(CleanupError) as captured:
            artifacts.cleanup()
        self.assertTrue(set(artifacts.sensitive_names) <= set(attempted))
        self.assertIn(root.name, attempted)
        self.assertIn("capture.pcap", str(captured.exception))
        self.assertNotIn(str(root.resolve()), str(captured.exception))

    def test_cleanup_is_idempotent_and_removes_unexpected_children(self):
        runner = FakeRunner()
        artifacts = SensitiveArtifacts.create(self.local_app_data, runner, run_id="idempotent")
        artifacts.capture.write_bytes(b"pcap")
        unexpected = artifacts.root / "unexpected-sensitive.tmp"
        unexpected.write_text("secret", encoding="utf-8")
        artifacts.cleanup()
        artifacts.cleanup()
        self.assertFalse(artifacts.root.exists())

    def test_sensitive_path_set_is_complete_and_contains_no_credentials(self):
        artifacts = SensitiveArtifacts(self.local_app_data / "not-created")
        self.assertEqual(set(artifacts.sensitive_names), {
            "capture-manifest.json", "capture-ready.json", "capture-stop.signal",
            "capture.pcap", "application-transcript.jsonl",
            "transport-observation.json", "session-snapshot.json", "tshark.json",
            "internal-report.json",
        })
        self.assertFalse(any("password" in name or "credential" in name
                             for name in artifacts.sensitive_names))


if __name__ == "__main__":
    unittest.main()
