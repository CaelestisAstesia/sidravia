from __future__ import annotations

import contextlib
import importlib.util
import io
import os
import shlex
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[3]
VERIFIER_PATH = ROOT / "tools" / "developer" / "verify_repository.py"

# Load the verifier module once for all tests
spec = importlib.util.spec_from_file_location("verify_repository", VERIFIER_PATH)
assert spec is not None and spec.loader is not None
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class TestGetGofmtPath(unittest.TestCase):
    def test_default_go(self) -> None:
        self.assertEqual(verifier.get_gofmt_path("go"), "gofmt")

    def test_custom_go(self) -> None:
        custom_go = "/path/to/go"
        self.assertEqual(verifier.get_gofmt_path(custom_go), str(Path(custom_go).parent / "gofmt"))


class TestSafeArgv(unittest.TestCase):
    def test_no_spaces(self) -> None:
        argv = ["go", "test", "./..."]
        self.assertEqual(verifier.safe_argv(argv), shlex.join(argv))

    def test_with_spaces(self) -> None:
        argv = ["go", "build", "-o", "/path/with spaces/file.exe"]
        self.assertEqual(verifier.safe_argv(argv), shlex.join(argv))


class TestRunCheck(unittest.TestCase):
    def test_utf8_diagnostics_preserve_failure(self) -> None:
        script = "import os; os.write(1, '检查失败'.encode('utf-8')); os.write(2, '错误原因'.encode('utf-8')); raise SystemExit(7)"
        result = verifier.run_check(
            verifier.Check([sys.executable, "-c", script]), ROOT, dict(os.environ)
        )
        self.assertEqual(result.exit_code, 7)
        self.assertEqual(result.stdout, "检查失败")
        self.assertEqual(result.stderr, "错误原因")

    def test_ci_utf8_capture_and_console_preserve_failure(self) -> None:
        child = "import os; os.write(1, '检查失败'.encode('utf-8')); os.write(2, '错误原因'.encode('utf-8')); raise SystemExit(7)"
        script = (
            f"import sys, os; from pathlib import Path; sys.path.insert(0, {str(VERIFIER_PATH.parent)!r}); "
            "import verify_repository as v; "
            f"r = v.run_and_print(v.Check([sys.executable, '-c', {child!r}]), Path.cwd(), dict(os.environ)); "
            "raise SystemExit(r.exit_code)"
        )
        env = dict(os.environ, PYTHONUTF8="1")
        env.pop("PYTHONIOENCODING", None)
        result = verifier.subprocess.run(
            [sys.executable, "-c", script], cwd=ROOT, env=env, capture_output=True
        )
        self.assertEqual(result.returncode, 7)
        self.assertIn("检查失败", result.stdout.decode("utf-8"))
        self.assertIn("错误原因", result.stderr.decode("utf-8"))

    def setUp(self) -> None:
        self.original_env = dict(os.environ)

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self.original_env)

    @mock.patch("subprocess.run")
    def test_env_merge(self, mock_run: mock.Mock) -> None:
        mock_run.return_value = mock.Mock(
            returncode=0, stdout="", stderr=""
        )
        base_env = {"A": "1", "B": "2"}
        check_env = {"B": "3", "C": "4"}
        check = verifier.Check(argv=["echo"], env=check_env)

        verifier.run_check(check, Path("/repo"), base_env)

        # Verify env was merged correctly
        call_env = mock_run.call_args[1]["env"]
        self.assertEqual(call_env["A"], "1")
        self.assertEqual(call_env["B"], "3")  # Check env overrides base
        self.assertEqual(call_env["C"], "4")

    @mock.patch("subprocess.run")
    def test_exception_converted_to_failure(self, mock_run: mock.Mock) -> None:
        mock_run.side_effect = Exception("Test exception")
        check = verifier.Check(argv=["echo"])

        result = verifier.run_check(check, Path("/repo"), {})

        self.assertEqual(result.exit_code, 1)
        self.assertIn("Exception: Test exception", result.stderr)

    def test_duration_recorded_for_success_and_failure(self) -> None:
        for outcome, code in ((mock.Mock(returncode=0, stdout="ok", stderr=""), 0),
                              (mock.Mock(returncode=7, stdout="", stderr="failed"), 7),
                              (OSError("cannot start"), 1)):
            with self.subTest(code=code):
                with mock.patch.object(verifier.time, "monotonic", side_effect=[10.0, 10.25]):
                    with mock.patch.object(verifier.subprocess, "run") as run:
                        if isinstance(outcome, Exception):
                            run.side_effect = outcome
                        else:
                            run.return_value = outcome
                        result = verifier.run_check(verifier.Check(["fictional"]), Path("/repo"), {})
                self.assertEqual(result.exit_code, code)
                self.assertEqual(result.duration_seconds, 0.25)


class TestBuildPythonChecks(unittest.TestCase):
    def test_three_repository_checks(self) -> None:
        repo_root = Path("/fake/repo")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        checks = verifier.build_python_checks(repo_root, base_env)

        self.assertEqual(len(checks), 3)

        # Check 1: Dr.COM mock with PYTHONPATH
        self.assertEqual(Path(checks[0].argv[5]), repo_root / "tools/drcom520d_mock_server/tests")
        self.assertIsNotNone(checks[0].env)
        self.assertEqual(Path(checks[0].env["PYTHONPATH"]), repo_root / "tools/drcom520d_mock_server/src")

        # Check 2: Windows Dr.COM acceptance with PYTHONPATH
        self.assertEqual(Path(checks[1].argv[5]), repo_root / "tools/windows_drcom_acceptance/tests")
        self.assertIsNotNone(checks[1].env)
        self.assertEqual(Path(checks[1].env["PYTHONPATH"]), repo_root / "tools/windows_drcom_acceptance/src")

        # Check 3: the verifier's own unit suite runs without recursively
        # invoking the verifier executable.
        self.assertEqual(Path(checks[2].argv[5]), repo_root / "tools/developer/tests")
        self.assertIsNotNone(checks[2].env)
        self.assertEqual(checks[2].env["PYTHONDONTWRITEBYTECODE"], "1")


class TestRunGoChecks(unittest.TestCase):
    def setUp(self) -> None:
        self.original_env = dict(os.environ)

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self.original_env)

    @mock.patch.object(verifier, "run_and_print")
    def test_complete_target_coverage(self, mock_run_and_print: mock.Mock) -> None:
        repo_root = Path("/repo")
        go_exec = "go"
        gofmt = "gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        # Set up mock results
        mock_results = []
        for i in range(15):
            mock_results.append(verifier.CheckResult(
                argv=[f"check{i}"], exit_code=0, stdout="", stderr=""
            ))
        mock_run_and_print.side_effect = mock_results

        # Run go checks
        results = verifier.run_go_checks(
            repo_root=repo_root,
            go_executable=go_exec,
            gofmt_path=gofmt,
            build_dir=build_dir,
            base_env=base_env
        )

        self.assertEqual(len(results), 15)
        expected_existing_checks = [
            (["git", "ls-files", "*.go"], None),
            ([gofmt, "-l"], None),
            ([go_exec, "test", "-count=1", "./..."], None),
            ([go_exec, "vet", "./..."], None),
            (["git", "diff", "--check"], None),
            (
                [go_exec, "build", "-o", str(build_dir / "sidraviactl.exe"), "./cmd/sidraviactl"],
                {"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"},
            ),
            (
                [go_exec, "build", "-o", str(build_dir / "sidraviad.exe"), "./cmd/sidraviad"],
                {"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"},
            ),
            (
                [go_exec, "build", "-o", str(build_dir / "sidraviamgr.exe"), "./cmd/sidraviamgr"],
                {"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"},
            ),
            (
                [go_exec, "build", "-o", str(build_dir / "sidraviaupdater.exe"), "./cmd/sidraviaupdater"],
                {"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"},
            ),
        ]
        for index, (argv, env) in enumerate(expected_existing_checks):
            check = mock_run_and_print.call_args_list[index][0][0]
            self.assertEqual(check.argv, argv)
            self.assertEqual(check.env, env)

        expected_platform_checks = [
            ("windows", "windows-amd64-internal-cli.test.exe", "./internal/cli"),
            ("windows", "windows-amd64-internal-connection-environment.test.exe", "./internal/connection/environment"),
            ("windows", "windows-amd64-internal-document-securefile.test.exe", "./internal/document/securefile"),
            ("darwin", "darwin-amd64-internal-cli.test", "./internal/cli"),
            ("darwin", "darwin-amd64-internal-connection-environment.test", "./internal/connection/environment"),
            ("darwin", "darwin-amd64-internal-daemon-host.test", "./internal/daemon/host"),
        ]
        for index, (goos, binary_name, package) in enumerate(expected_platform_checks, start=9):
            check = mock_run_and_print.call_args_list[index][0][0]
            self.assertEqual(
                check.argv,
                [go_exec, "test", "-c", "-o", str(build_dir / binary_name), package],
            )
            self.assertEqual(
                check.env,
                {"GOOS": goos, "GOARCH": "amd64", "CGO_ENABLED": "0"},
            )

    @mock.patch.object(verifier, "run_and_print")
    def test_gofmt_with_files(self, mock_run_and_print: mock.Mock) -> None:
        go_exec = "go"
        gofmt = "/custom/gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        with tempfile.TemporaryDirectory() as repo_dir:
            repo_root = Path(repo_dir)
            # Real tracked Go files so the existing-file filter retains them
            (repo_root / "file1.go").write_text("package file1\n", encoding="utf-8")
            (repo_root / "file2.go").write_text("package file2\n", encoding="utf-8")

            # First result: git ls-files succeeds with files
            git_ls_result = verifier.CheckResult(
                argv=["git", "ls-files", "*.go"], exit_code=0, stdout="file1.go\nfile2.go", stderr=""
            )
            # Rest results: pass
            mock_results = [git_ls_result]
            for i in range(14):
                mock_results.append(verifier.CheckResult(
                    argv=[f"check{i}"], exit_code=0, stdout="", stderr=""
                ))
            mock_run_and_print.side_effect = mock_results

            results = verifier.run_go_checks(
                repo_root=repo_root,
                go_executable=go_exec,
                gofmt_path=gofmt,
                build_dir=build_dir,
                base_env=base_env
            )

            # Check gofmt args
            gofmt_call = mock_run_and_print.call_args_list[1]
            self.assertEqual(gofmt_call[0][0].argv[0], "/custom/gofmt")
            self.assertEqual(gofmt_call[0][0].argv[1], "-l")
            self.assertIn("file1.go", gofmt_call[0][0].argv)
            self.assertIn("file2.go", gofmt_call[0][0].argv)

    @mock.patch.object(verifier, "run_and_print")
    def test_gofmt_nonempty_stdout_becomes_failure(self, mock_run_and_print: mock.Mock) -> None:
        go_exec = "go"
        gofmt = "gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        with tempfile.TemporaryDirectory() as repo_dir:
            repo_root = Path(repo_dir)
            # Real tracked Go file so the existing-file filter retains it
            (repo_root / "file1.go").write_text("package file1\n", encoding="utf-8")

            # First result: git ls-files succeeds with files
            git_ls_result = verifier.CheckResult(
                argv=["git", "ls-files", "*.go"], exit_code=0, stdout="file1.go", stderr=""
            )
            # Second result: gofmt exits 0 but has stdout
            gofmt_result = verifier.CheckResult(
                argv=["gofmt", "-l", "file1.go"], exit_code=0, stdout="file1.go", stderr="",
                duration_seconds=0.25,
            )
            # Rest results: pass
            mock_results = [git_ls_result, gofmt_result]
            for i in range(13):
                mock_results.append(verifier.CheckResult(
                    argv=[f"check{i}"], exit_code=0, stdout="", stderr=""
                ))
            mock_run_and_print.side_effect = mock_results

            stdout_buf = io.StringIO()
            stderr_buf = io.StringIO()
            with mock.patch("sys.stdout", new=stdout_buf):
                with mock.patch("sys.stderr", new=stderr_buf):
                    results = verifier.run_go_checks(
                        repo_root=repo_root,
                        go_executable=go_exec,
                        gofmt_path=gofmt,
                        build_dir=build_dir,
                        base_env=base_env
                    )

            # Check that gofmt result was corrected
            self.assertEqual(results[1].exit_code, 1)
            self.assertIn("Formatting issues found", results[1].stderr)
            self.assertEqual(results[1].duration_seconds, 0.25)

    @mock.patch.object(verifier, "run_and_print")
    def test_gofmt_omits_absent_tracked_files(self, mock_run_and_print: mock.Mock) -> None:
        # A present tracked Go file reaches gofmt; a tracked-but-deleted file
        # (absent from the working tree) is omitted so gofmt never lstat's it.
        go_exec = "go"
        gofmt = "gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        with tempfile.TemporaryDirectory() as repo_dir:
            repo_root = Path(repo_dir)
            present_rel = "present.go"
            absent_rel = "absent.go"
            (repo_root / present_rel).write_text("package present\n", encoding="utf-8")
            # absent_rel is intentionally not created on disk

            git_ls_result = verifier.CheckResult(
                argv=["git", "ls-files", "*.go"],
                exit_code=0,
                stdout=f"{present_rel}\n{absent_rel}",
                stderr="",
            )
            mock_results = [git_ls_result]
            for i in range(14):
                mock_results.append(verifier.CheckResult(
                    argv=[f"check{i}"], exit_code=0, stdout="", stderr=""
                ))
            mock_run_and_print.side_effect = mock_results

            verifier.run_go_checks(
                repo_root=repo_root,
                go_executable=go_exec,
                gofmt_path=gofmt,
                build_dir=build_dir,
                base_env=base_env,
            )

            # gofmt is the second run_and_print call
            gofmt_argv = mock_run_and_print.call_args_list[1][0][0].argv
            self.assertEqual(gofmt_argv[0], "gofmt")
            self.assertEqual(gofmt_argv[1], "-l")
            self.assertIn(present_rel, gofmt_argv)
            self.assertNotIn(absent_rel, gofmt_argv)

    @mock.patch.object(verifier, "run_and_print")
    def test_git_ls_failed_skips_real_gofmt(self, mock_run_and_print: mock.Mock) -> None:
        repo_root = Path("/repo")
        go_exec = "go"
        gofmt = "gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        # First result: git ls-files fails
        git_ls_result = verifier.CheckResult(
            argv=["git", "ls-files", "*.go"], exit_code=1, stdout="", stderr="git error"
        )
        # Rest results: pass
        mock_results = [git_ls_result]
        for i in range(13):
            mock_results.append(verifier.CheckResult(
                argv=[f"check{i}"], exit_code=0, stdout="", stderr=""
            ))
        mock_run_and_print.side_effect = mock_results

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                results = verifier.run_go_checks(
                    repo_root=repo_root,
                    go_executable=go_exec,
                    gofmt_path=gofmt,
                    build_dir=build_dir,
                    base_env=base_env
                )

        # gofmt is represented by a failed result but was not invoked.
        self.assertEqual(mock_run_and_print.call_count, 14)
        # Retain a result for every required check.
        self.assertEqual(len(results), 15)
        # Check that gofmt result is fake
        self.assertIn("Skipped: git ls-files failed", results[1].stderr)
        # Check that test/vet/diff/builds were still run.
        self.assertEqual(mock_run_and_print.call_args_list[1][0][0].argv[0], go_exec)
        self.assertEqual(mock_run_and_print.call_args_list[1][0][0].argv[1], "test")
        self.assertEqual(mock_run_and_print.call_args_list[2][0][0].argv[1], "vet")
        self.assertEqual(mock_run_and_print.call_args_list[3][0][0].argv[0], "git")
        self.assertEqual(mock_run_and_print.call_args_list[3][0][0].argv[1], "diff")


    def test_native_and_cross_partition_full_checks(self) -> None:
        commands = {}
        for mode in ("full", "native", "cross"):
            calls = []
            def capture(check, *_args):
                calls.append((check.argv, check.env))
                return verifier.CheckResult(check.argv, 0, "", "")
            with mock.patch.object(verifier, "run_and_print", side_effect=capture):
                with contextlib.redirect_stdout(io.StringIO()):
                    verifier.run_go_checks(Path("/repo"), "go", "gofmt", Path("/build"), {}, mode=mode)
            commands[mode] = calls
        self.assertEqual(commands["full"], commands["native"] + commands["cross"])
        self.assertEqual(len(commands["native"]), 5)
        self.assertEqual(len(commands["cross"]), 10)
        self.assertIn((["go", "test", "-count=1", "./..."], None), commands["native"])
        self.assertTrue(all(env and env["CGO_ENABLED"] == "0" for _, env in commands["cross"]))


class TestRunPythonChecks(unittest.TestCase):
    @mock.patch.object(verifier, "run_and_print")
    def test_all_checks_attempted(self, mock_run_and_print: mock.Mock) -> None:
        repo_root = Path("/repo")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        # Make some checks fail
        mock_results = [
            verifier.CheckResult(argv=["check1"], exit_code=1, stdout="", stderr="error"),
            verifier.CheckResult(argv=["check2"], exit_code=0, stdout="", stderr=""),
            verifier.CheckResult(argv=["check3"], exit_code=0, stdout="", stderr=""),
        ]
        mock_run_and_print.side_effect = mock_results

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                results = verifier.run_python_checks(
                    repo_root=repo_root,
                    base_env=base_env
                )

        # All checks should have been run
        self.assertEqual(mock_run_and_print.call_count, 3)
        self.assertEqual(len(results), 3)


class TestVerifyWorkflowTopology(unittest.TestCase):
    def test_three_native_runner_matrix_uses_public_verifier(self) -> None:
        workflow = (ROOT / ".github" / "workflows" / "verify.yml").read_text(
            encoding="utf-8"
        )

        self.assertRegex(
            workflow,
            r"(?ms)^\s*include:\s*\n\s*- os: ubuntu-24.04\s*\n\s*scope: all\s*\n\s*- os: windows-latest\s*\n\s*scope: go\s*\n\s*- os: macos-latest\s*\n\s*scope: go\s*\n",
        )
        self.assertIn("fail-fast: false", workflow)
        self.assertRegex(workflow, r"python-version:\s*['\"]?3\.14\.4")
        self.assertRegex(
            workflow,
            r"python tools/developer/verify_repository\.py --scope \$\{\{ matrix\.scope \}\} --go go --go-checks native",
        )
        self.assertNotIn("continue-on-error", workflow)
        self.assertEqual(workflow.count("--scope go --go-checks cross --go go"), 1)
        self.assertIn("branches: [main]", workflow)
        self.assertIn("workflow_dispatch:", workflow)


class TestPrintSummary(unittest.TestCase):
    def test_summary(self) -> None:
        results = [
            verifier.CheckResult(argv=["check1"], exit_code=0, stdout="", stderr="", duration_seconds=0.25),
            verifier.CheckResult(argv=["check2"], exit_code=1, stdout="", stderr=""),
            verifier.CheckResult(argv=["check3"], exit_code=0, stdout="", stderr=""),
        ]

        stdout_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            verifier.print_summary(results)

        stdout = stdout_buf.getvalue()
        self.assertIn("Summary: 2 passed, 1 failed", stdout)
        self.assertIn("PASS: check1", stdout)
        self.assertIn("PASS: check1 (0.250s)", stdout)
        self.assertIn("FAIL: check2", stdout)
        self.assertIn("PASS: check3", stdout)


class TestCodeQuality(unittest.TestCase):
    def test_no_shell_true_used(self) -> None:
        content = VERIFIER_PATH.read_text(encoding="utf-8")
        self.assertNotIn("shell=True", content)

    def test_uses_standard_library_only(self) -> None:
        content = VERIFIER_PATH.read_text(encoding="utf-8")
        lines = content.splitlines()

        allowed_imports = {
            "argparse",
            "contextlib",
            "concurrent",
            "os",
            "shlex",
            "subprocess",
            "sys",
            "tempfile",
            "time",
            "pathlib",
            "__future__",
            "typing",
        }

        for line in lines:
            stripped = line.strip()
            if stripped.startswith("import "):
                module = stripped[7:].split()[0].split(".")[0]
                self.assertIn(module, allowed_imports, f"Unexpected import: {module}")
            elif stripped.startswith("from "):
                module = stripped[5:].split()[0].split(".")[0]
                self.assertIn(module, allowed_imports, f"Unexpected import: {module}")


if __name__ == "__main__":
    unittest.main()
