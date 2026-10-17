from __future__ import annotations

import argparse
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

DIRECTORY = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(DIRECTORY))
import dev


class DeveloperCommands(unittest.TestCase):
    def test_package_rejects_dirty_source_before_running_builder(self):
        args = argparse.Namespace(go="go")
        with mock.patch.object(dev, "metadata", return_value={"dirty": True}):
            with self.assertRaisesRegex(ValueError, "clean"):
                dev.package_commands(args, {})

    def test_package_reuses_existing_producer_and_consumer_with_same_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(go="go", output=str(Path(directory) / "dist"), version="0.1.0", gui_directory=None, iscc=None)
            with mock.patch.object(dev, "metadata", return_value={"dirty": False, "commit": "a" * 40}), mock.patch.object(dev.platform, "system", return_value="Linux"), mock.patch.object(dev.platform, "machine", return_value="x86_64"):
                _, commands = dev.package_commands(args, {})
            self.assertIn("./tools/build", commands[0])
            self.assertIn("tools/developer/verify_distribution.py", commands[1])
            for command in commands:
                self.assertEqual(command[command.index("--build-id") + 1], "a" * 40)
                self.assertEqual(command[command.index("--target") + 1], "linux-amd64")
            self.assertIn("--smoke", commands[1])

    def test_arm_package_uses_selected_target_without_execution(self):
        for target in ("windows-arm64", "linux-arm64"):
            with self.subTest(target=target), tempfile.TemporaryDirectory() as directory:
                args = argparse.Namespace(go="go", output=str(Path(directory) / "dist"), version="0.3.0-alpha.1", target=target)
                with mock.patch.object(dev, "metadata", return_value={"dirty": False, "commit": "a" * 40}), mock.patch.object(dev.platform, "system", return_value="Linux"), mock.patch.object(dev.platform, "machine", return_value="x86_64"):
                    _, commands = dev.package_commands(args, {})
                for command in commands:
                    self.assertEqual(command[command.index("--target") + 1], target)
                    self.assertEqual(command[command.index("--build-id") + 1], "a" * 40)
                self.assertNotIn("--smoke", commands[1])

    def test_parallel_cross_preserves_checks_and_failure(self):
        def run(check, *_):
            return dev.verify.CheckResult(check.argv, int("./internal/cli" in check.argv), "", "")
        with mock.patch.object(dev.verify, "run_and_print", side_effect=run):
            serial = dev.verify.run_cross_checks(Path("/repo"), "go", Path("/build"), {}, 1)
            parallel = dev.verify.run_cross_checks(Path("/repo"), "go", Path("/build"), {}, 2)
        self.assertEqual([(r.argv, r.exit_code) for r in serial], [(r.argv, r.exit_code) for r in parallel])
        self.assertEqual(len(parallel), 9)
        self.assertTrue(any(r.exit_code for r in parallel))

    def test_manifest_hashes_tracked_bytes_and_refuses_untracked(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "source.txt").write_bytes(b"content")
            with mock.patch.object(dev, "ROOT", root), mock.patch.object(dev, "capture", side_effect=["a" * 40, " M source.txt", "", "source.txt\0", "a" * 40, " M source.txt"]):
                manifest = dev.source_manifest({})
            self.assertTrue(manifest["dirty"])
            self.assertEqual(manifest["files"], [{"path": "source.txt", "sha256": dev.hashlib.sha256(b"content").hexdigest()}])
        with mock.patch.object(dev, "capture", side_effect=["a" * 40, "?? new.go", "new.go"]):
            with self.assertRaisesRegex(ValueError, "untracked"):
                dev.source_manifest({})

    def test_manifest_clean_gate(self):
        with mock.patch.object(dev, "capture", side_effect=["a" * 40, " M source.go", ""]):
            with self.assertRaisesRegex(ValueError, "clean"):
                dev.source_manifest({}, require_clean=True)

    def test_generic_package_rejects_windows_before_builder(self):
        with mock.patch.object(dev, "metadata", return_value={"dirty": False}), mock.patch.object(dev.platform, "system", return_value="Windows"):
            with self.assertRaisesRegex(ValueError, "windows --package"):
                dev.package_commands(argparse.Namespace(go="go"), {})

    def test_manifest_rejects_edit_or_head_change_during_hashing(self):
        for after_commit, after_status in (("a" * 40, " M source.txt"), ("b" * 40, "")):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / "source.txt").write_text("content")
                with mock.patch.object(dev, "ROOT", root), mock.patch.object(dev, "capture", side_effect=["a" * 40, "", "", "source.txt\0", after_commit, after_status]):
                    with self.assertRaisesRegex(ValueError, "source changed"):
                        dev.source_manifest({}, require_clean=True)

    def test_fingerprint_detects_different_bytes_with_same_dirty_status(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.txt"
            source.write_text("first")
            with mock.patch.object(dev, "ROOT", root), mock.patch.object(dev.subprocess, "check_output", return_value=b"source.txt\0"):
                before = {"commit": "a" * 40, "status": " M source.txt", "source_fingerprint": dev.source_fingerprint({})}
                source.write_text("second")
                after = dict(before, source_fingerprint=dev.source_fingerprint({}))
            self.assertFalse(dev.same_source(before, after))

    def test_failed_verification_retains_stage_log_and_status(self):
        args = argparse.Namespace(scope="python", go="go", go_checks="full", jobs=1)
        def failing(*_):
            print("useful failure detail")
            return [dev.verify.CheckResult(["test"], 3, "", "", 0.5)]
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(dev.verify, "run_python_checks", side_effect=failing):
            report = dev.verify_once(args, {}, Path(directory), "run-1")
            self.assertFalse(report["passed"])
            self.assertEqual(report["stages"][0]["exit_code"], 3)
            self.assertIn("useful failure detail", Path(report["log"]).read_text())


if __name__ == "__main__":
    unittest.main()
