from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import verify_repository as verifier


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
        self.assertFalse(args.cold_cache)
        self.assertEqual(args.go_checks, "full")

    def test_explicit_cold_cache(self) -> None:
        sys.argv = ["verify_repository.py", "--scope", "go", "--cold-cache"]
        self.assertTrue(verifier.parse_args().cold_cache)

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


class TestMain(unittest.TestCase):
    def setUp(self) -> None:
        environment = mock.patch.dict(os.environ)
        environment.start()
        self.addCleanup(environment.stop)
        self.go_checks = mock.Mock(return_value=[])
        self.python_checks = mock.Mock(return_value=[])
        checks = mock.patch.multiple(
            verifier,
            run_go_checks=self.go_checks,
            run_python_checks=self.python_checks,
            print_summary=mock.Mock(),
        )
        checks.start()
        self.addCleanup(checks.stop)

    def run_main(self, *args: str) -> int:
        with (
            mock.patch.object(sys, "argv", ["verify_repository.py", *args]),
            mock.patch.object(sys, "dont_write_bytecode", True),
            contextlib.redirect_stdout(io.StringIO()),
            contextlib.redirect_stderr(io.StringIO()),
        ):
            return verifier.main()

    def test_scope_go_only(self) -> None:
        self.assertEqual(self.run_main("--scope", "go"), 0)
        self.go_checks.assert_called_once()
        self.python_checks.assert_not_called()

    def test_scope_python_only(self) -> None:
        self.assertEqual(self.run_main("--scope", "python"), 0)
        self.go_checks.assert_not_called()
        self.python_checks.assert_called_once()

    def test_custom_go_executable(self) -> None:
        custom_go = "/custom/go/path"
        self.assertEqual(self.run_main("--scope", "go", "--go", custom_go), 0)
        self.assertEqual(self.go_checks.call_args.kwargs["go_executable"], custom_go)

    def test_gocache_preserved_when_provided(self) -> None:
        os.environ["GOCACHE"] = "/existing/gocache"
        self.assertEqual(self.run_main("--scope", "go"), 0)
        self.assertEqual(self.go_checks.call_args.kwargs["base_env"]["GOCACHE"], "/existing/gocache")

    def test_standard_gocache_used_when_not_provided(self) -> None:
        os.environ.pop("GOCACHE", None)
        self.assertEqual(self.run_main("--scope", "go"), 0)
        self.assertNotIn("GOCACHE", self.go_checks.call_args.kwargs["base_env"])

    def test_python_scope_no_temp_gocache(self) -> None:
        os.environ.pop("GOCACHE", None)
        with mock.patch.object(tempfile, "TemporaryDirectory", wraps=tempfile.TemporaryDirectory) as temp:
            self.assertEqual(self.run_main("--scope", "python"), 0)
        temp.assert_not_called()

    def test_cold_cache_overrides_configured_cache_and_cleans_up(self) -> None:
        os.environ["GOCACHE"] = "/existing/gocache"
        observed = []

        def inspect_cache(**kwargs):
            cache = Path(kwargs["base_env"]["GOCACHE"])
            self.assertTrue(cache.is_dir())
            self.assertNotEqual(str(cache), "/existing/gocache")
            observed.append(cache)
            return []

        self.go_checks.side_effect = inspect_cache
        self.assertEqual(self.run_main("--scope", "go", "--cold-cache"), 0)
        cache, = observed
        self.assertFalse(cache.exists())
        self.assertEqual(os.environ["GOCACHE"], "/existing/gocache")

    def test_default_cleans_build_output_only(self) -> None:
        os.environ.pop("GOCACHE", None)
        observed = []

        def inspect_build(**kwargs):
            self.assertNotIn("GOCACHE", kwargs["base_env"])
            build_dir = kwargs["build_dir"]
            self.assertTrue(build_dir.is_dir())
            observed.append(build_dir)
            return []

        self.go_checks.side_effect = inspect_build
        self.assertEqual(self.run_main("--scope", "go"), 0)
        build_dir, = observed
        self.assertFalse(build_dir.exists())

    def test_failure_aggregation_exit_code(self) -> None:
        self.go_checks.return_value = [
            verifier.CheckResult(argv=["check1"], exit_code=0, stdout="", stderr=""),
            verifier.CheckResult(argv=["check2"], exit_code=1, stdout="", stderr=""),
        ]
        self.python_checks.return_value = [
            verifier.CheckResult(argv=["check3"], exit_code=0, stdout="", stderr=""),
        ]
        self.assertEqual(self.run_main("--scope", "all"), 1)

    def test_temp_gocache_cleanup(self) -> None:
        os.environ.pop("GOCACHE", None)
        observed = []

        def inspect_directories(**kwargs):
            cache = Path(kwargs["base_env"]["GOCACHE"])
            build = kwargs["build_dir"]
            self.assertTrue(cache.is_dir())
            self.assertTrue(build.is_dir())
            self.assertNotEqual(cache, build)
            observed.append((cache, build))
            return [verifier.CheckResult(argv=["check"], exit_code=1, stdout="", stderr="")]

        self.go_checks.side_effect = inspect_directories
        self.assertEqual(self.run_main("--scope", "go", "--cold-cache"), 1)
        (cache, build), = observed
        self.assertFalse(cache.exists())
        self.assertFalse(build.exists())


if __name__ == "__main__":
    unittest.main()
