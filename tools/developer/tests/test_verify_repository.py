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


class TestParseArgs(unittest.TestCase):
    def setUp(self) -> None:
        self.original_argv = list(sys.argv)

    def tearDown(self) -> None:
        sys.argv = self.original_argv

    def test_defaults(self) -> None:
        sys.argv = ["verify_repository.py"]
        args = verifier.parse_args()
        self.assertEqual(args.scope, "all")
        self.assertIsNone(args.go)

    def test_scope_go(self) -> None:
        sys.argv = ["verify_repository.py", "--scope", "go"]
        args = verifier.parse_args()
        self.assertEqual(args.scope, "go")

    def test_scope_python(self) -> None:
        sys.argv = ["verify_repository.py", "--scope", "python"]
        args = verifier.parse_args()
        self.assertEqual(args.scope, "python")

    def test_custom_go(self) -> None:
        custom_go = "/path/to/custom/go"
        sys.argv = ["verify_repository.py", "--go", custom_go]
        args = verifier.parse_args()
        self.assertEqual(args.go, custom_go)


class TestGetGofmtPath(unittest.TestCase):
    def test_default_go(self) -> None:
        self.assertEqual(verifier.get_gofmt_path("go"), "gofmt")

    def test_custom_go(self) -> None:
        custom_go = "/path/to/go"
        self.assertEqual(verifier.get_gofmt_path(custom_go), "/path/to/gofmt")


class TestSafeArgv(unittest.TestCase):
    def test_no_spaces(self) -> None:
        argv = ["go", "test", "./..."]
        self.assertEqual(verifier.safe_argv(argv), shlex.join(argv))

    def test_with_spaces(self) -> None:
        argv = ["go", "build", "-o", "/path/with spaces/file.exe"]
        self.assertEqual(verifier.safe_argv(argv), shlex.join(argv))


class TestRunCheck(unittest.TestCase):
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


class TestBuildPythonChecks(unittest.TestCase):
    def test_two_public_checks(self) -> None:
        repo_root = Path("/fake/repo")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        checks = verifier.build_python_checks(repo_root, base_env)

        self.assertEqual(len(checks), 2)

        # Check 1: Dr.COM mock with PYTHONPATH
        self.assertIn("drcom520d_mock_server/tests", checks[0].argv[5])
        self.assertIsNotNone(checks[0].env)
        self.assertIn("drcom520d_mock_server/src", checks[0].env["PYTHONPATH"])

        # Check 2: Windows Dr.COM acceptance with PYTHONPATH
        self.assertIn("windows_drcom_acceptance/tests", checks[1].argv[5])
        self.assertIsNotNone(checks[1].env)
        self.assertIn("windows_drcom_acceptance/src", checks[1].env["PYTHONPATH"])


class TestRunGoChecks(unittest.TestCase):
    def setUp(self) -> None:
        self.original_env = dict(os.environ)

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self.original_env)

    @mock.patch.object(verifier, "run_and_print")
    def test_seven_results(self, mock_run_and_print: mock.Mock) -> None:
        repo_root = Path("/repo")
        go_exec = "go"
        gofmt = "gofmt"
        build_dir = Path("/build")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        # Set up mock results
        mock_results = []
        for i in range(7):
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

        self.assertEqual(len(results), 7)
        # First check is git ls-files
        self.assertEqual(mock_run_and_print.call_args_list[0][0][0].argv, ["git", "ls-files", "*.go"])

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
            for i in range(6):
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
                argv=["gofmt", "-l", "file1.go"], exit_code=0, stdout="file1.go", stderr=""
            )
            # Rest results: pass
            mock_results = [git_ls_result, gofmt_result]
            for i in range(5):
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
            for i in range(6):
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
        for i in range(5):
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

        # Check that gofmt was not run (only git ls + 5 others = 6)
        self.assertEqual(mock_run_and_print.call_count, 6)
        # But still have 7 results total
        self.assertEqual(len(results), 7)
        # Check that gofmt result is fake
        self.assertIn("Skipped: git ls-files failed", results[1].stderr)
        # Check that test/vet/diff/builds were still run
        self.assertEqual(mock_run_and_print.call_args_list[1][0][0].argv[0], go_exec)
        self.assertEqual(mock_run_and_print.call_args_list[1][0][0].argv[1], "test")
        self.assertEqual(mock_run_and_print.call_args_list[2][0][0].argv[1], "vet")
        self.assertEqual(mock_run_and_print.call_args_list[3][0][0].argv[0], "git")
        self.assertEqual(mock_run_and_print.call_args_list[3][0][0].argv[1], "diff")


class TestRunPythonChecks(unittest.TestCase):
    @mock.patch.object(verifier, "run_and_print")
    def test_all_checks_attempted(self, mock_run_and_print: mock.Mock) -> None:
        repo_root = Path("/repo")
        base_env = {"PYTHONDONTWRITEBYTECODE": "1"}

        # Make some checks fail
        mock_results = [
            verifier.CheckResult(argv=["check1"], exit_code=1, stdout="", stderr="error"),
            verifier.CheckResult(argv=["check2"], exit_code=0, stdout="", stderr=""),
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
        self.assertEqual(mock_run_and_print.call_count, 2)
        self.assertEqual(len(results), 2)


class TestPrintSummary(unittest.TestCase):
    def test_summary(self) -> None:
        results = [
            verifier.CheckResult(argv=["check1"], exit_code=0, stdout="", stderr=""),
            verifier.CheckResult(argv=["check2"], exit_code=1, stdout="", stderr=""),
            verifier.CheckResult(argv=["check3"], exit_code=0, stdout="", stderr=""),
        ]

        stdout_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            verifier.print_summary(results)

        stdout = stdout_buf.getvalue()
        self.assertIn("Summary: 2 passed, 1 failed", stdout)
        self.assertIn("PASS: check1", stdout)
        self.assertIn("FAIL: check2", stdout)
        self.assertIn("PASS: check3", stdout)


class TestMain(unittest.TestCase):
    def setUp(self) -> None:
        self.original_env = dict(os.environ)
        self.original_argv = list(sys.argv)
        self.original_dont_write_bytecode = sys.dont_write_bytecode
        sys.dont_write_bytecode = True

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self.original_env)
        sys.argv = self.original_argv
        sys.dont_write_bytecode = self.original_dont_write_bytecode

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_scope_go_only(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        sys.argv = ["verify_repository.py", "--scope", "go"]
        mock_run_go.return_value = []
        mock_print_summary.return_value = None

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                verifier.main()

        mock_run_go.assert_called_once()
        mock_run_python.assert_not_called()

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_scope_python_only(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        sys.argv = ["verify_repository.py", "--scope", "python"]
        mock_run_python.return_value = []
        mock_print_summary.return_value = None

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                verifier.main()

        mock_run_go.assert_not_called()
        mock_run_python.assert_called_once()

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_custom_go_executable(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        custom_go = "/custom/go/path"
        sys.argv = ["verify_repository.py", "--scope", "go", "--go", custom_go]
        mock_run_go.return_value = []
        mock_print_summary.return_value = None

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                verifier.main()

        self.assertEqual(mock_run_go.call_args[1]["go_executable"], custom_go)

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_gocache_preserved_when_provided(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        os.environ["GOCACHE"] = "/existing/gocache"
        sys.argv = ["verify_repository.py", "--scope", "go"]
        mock_run_go.return_value = []
        mock_print_summary.return_value = None

        captured_base_env = None
        def capture_base_env(*args, **kwargs):
            nonlocal captured_base_env
            captured_base_env = kwargs["base_env"]
            return []
        mock_run_go.side_effect = capture_base_env

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                verifier.main()

        # GOCACHE should be preserved
        self.assertIsNotNone(captured_base_env)
        self.assertEqual(captured_base_env["GOCACHE"], "/existing/gocache")

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_gocache_created_when_not_provided(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        os.environ.pop("GOCACHE", None)
        sys.argv = ["verify_repository.py", "--scope", "go"]
        mock_run_go.return_value = []
        mock_print_summary.return_value = None

        captured_base_env = None
        def capture_base_env(*args, **kwargs):
            nonlocal captured_base_env
            captured_base_env = kwargs["base_env"]
            return []
        mock_run_go.side_effect = capture_base_env

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                verifier.main()

        # GOCACHE should be set
        self.assertIsNotNone(captured_base_env)
        self.assertIn("sidravia-gocache-", captured_base_env["GOCACHE"])

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_python_scope_no_temp_gocache(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        os.environ.pop("GOCACHE", None)
        sys.argv = ["verify_repository.py", "--scope", "python"]
        mock_run_python.return_value = []
        mock_print_summary.return_value = None

        temp_dirs_created = []
        original_tempdir = tempfile.TemporaryDirectory
        def track_temp_dir(*args, **kwargs):
            temp_dir = original_tempdir(*args, **kwargs)
            temp_dirs_created.append(temp_dir)
            return temp_dir

        with mock.patch("tempfile.TemporaryDirectory", side_effect=track_temp_dir):
            stdout_buf = io.StringIO()
            stderr_buf = io.StringIO()
            with mock.patch("sys.stdout", new=stdout_buf):
                with mock.patch("sys.stderr", new=stderr_buf):
                    verifier.main()

        # No temp GOCACHE should be created for Python scope
        self.assertEqual(len(temp_dirs_created), 0)

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_failure_aggregation_exit_code(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        sys.argv = ["verify_repository.py", "--scope", "all"]
        # Some failures
        mock_run_go.return_value = [
            verifier.CheckResult(argv=["check1"], exit_code=0, stdout="", stderr=""),
            verifier.CheckResult(argv=["check2"], exit_code=1, stdout="", stderr=""),
        ]
        mock_run_python.return_value = [
            verifier.CheckResult(argv=["check3"], exit_code=0, stdout="", stderr=""),
        ]
        mock_print_summary.return_value = None

        stdout_buf = io.StringIO()
        stderr_buf = io.StringIO()
        with mock.patch("sys.stdout", new=stdout_buf):
            with mock.patch("sys.stderr", new=stderr_buf):
                exit_code = verifier.main()

        # Should exit with non-zero when any check fails
        self.assertEqual(exit_code, 1)


class TestTempCleanup(unittest.TestCase):
    def setUp(self) -> None:
        self.original_env = dict(os.environ)
        self.original_argv = list(sys.argv)

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self.original_env)
        sys.argv = self.original_argv

    @mock.patch.object(verifier, "run_go_checks")
    @mock.patch.object(verifier, "run_python_checks")
    @mock.patch.object(verifier, "print_summary")
    def test_temp_gocache_cleanup(
        self,
        mock_print_summary: mock.Mock,
        mock_run_python: mock.Mock,
        mock_run_go: mock.Mock,
    ) -> None:
        os.environ.pop("GOCACHE", None)
        sys.argv = ["verify_repository.py", "--scope", "go"]
        mock_run_go.return_value = []
        mock_print_summary.return_value = None

        def make_temp_dir(name: str) -> mock.Mock:
            temp_dir_mock = mock.Mock()
            temp_dir_mock.name = name
            temp_dir_mock.__enter__ = mock.Mock(return_value=temp_dir_mock)
            temp_dir_mock.__exit__ = mock.Mock(return_value=None)
            temp_dir_mock.__fspath__ = mock.Mock(return_value=name)
            return temp_dir_mock

        gocache_mock = make_temp_dir("/temp/gocache")
        build_mock = make_temp_dir("/temp/build")

        temp_dirs = [gocache_mock, build_mock]
        with mock.patch("tempfile.TemporaryDirectory", side_effect=temp_dirs):
            stdout_buf = io.StringIO()
            stderr_buf = io.StringIO()
            with mock.patch("sys.stdout", new=stdout_buf):
                with mock.patch("sys.stderr", new=stderr_buf):
                    verifier.main()

        # Verify cleanup was called via ExitStack for both temp dirs
        gocache_mock.__exit__.assert_called_once()
        build_mock.__exit__.assert_called_once()


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
            "os",
            "shlex",
            "subprocess",
            "sys",
            "tempfile",
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
