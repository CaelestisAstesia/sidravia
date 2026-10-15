#!/usr/bin/env python3
"""Verify repository health with Go and Python checks."""

from __future__ import annotations

import argparse
import contextlib
from concurrent.futures import ThreadPoolExecutor
import os
import shlex
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Iterator, Sequence


def main() -> int:
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[2]
    base_env = dict(os.environ)
    base_env["PYTHONDONTWRITEBYTECODE"] = "1"

    results: list[CheckResult] = []

    # Leave Go's standard or caller-configured cache intact by default.
    with contextlib.ExitStack() as gocache_stack:
        if args.cold_cache and args.scope in ("go", "all"):
            temp_gocache = tempfile.TemporaryDirectory(prefix="sidravia-gocache-")
            gocache_stack.enter_context(temp_gocache)
            base_env["GOCACHE"] = temp_gocache.name
        if args.scope in ("go", "all"):
            cache_mode = "cold" if args.cold_cache else ("configured" if "GOCACHE" in base_env else "standard")
            print(f"Go compile cache: {cache_mode}")

        if args.scope in ("go", "all"):
            go_executable = args.go or "go"
            gofmt_path = get_gofmt_path(go_executable)

            # Run go checks with shared build dir that cleans up after all builds
            with contextlib.ExitStack() as build_stack:
                build_dir_ctx = tempfile.TemporaryDirectory(prefix="sidravia-build-")
                build_dir = Path(build_stack.enter_context(build_dir_ctx))

                go_results = run_go_checks(
                    repo_root=repo_root,
                    go_executable=go_executable,
                    gofmt_path=gofmt_path,
                    build_dir=build_dir,
                    base_env=base_env,
                    mode=args.go_checks,
                    jobs=args.jobs,
                )
                results.extend(go_results)

        if args.scope in ("python", "all"):
            python_results = run_python_checks(
                repo_root=repo_root,
                base_env=base_env,
            )
            results.extend(python_results)

    print_summary(results)
    return 0 if all(r.exit_code == 0 for r in results) else 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Verify repository health")
    parser.add_argument(
        "--scope",
        choices=["go", "python", "all"],
        default="all",
        help="Scope of checks to run (default: all)",
    )
    parser.add_argument(
        "--go",
        help="Path to Go executable (default: go from PATH)",
    )
    parser.add_argument(
        "--cold-cache",
        action="store_true",
        help="Use and clean a temporary Go compile cache for this run (Go scopes only)",
    )
    parser.add_argument(
        "--go-checks", choices=["full", "native", "cross"], default="full",
        help="Go stages to run (default: full; cross checks compile but do not execute)",
    )
    parser.add_argument("--jobs", type=int, choices=range(1, 5), default=1,
                        help="Concurrent cross builds (1-4; default: 1)")
    return parser.parse_args()


def get_gofmt_path(go_executable: str) -> str:
    """Get path to gofmt, either from same directory as go or just 'gofmt'."""
    if go_executable == "go":
        return "gofmt"
    go_path = Path(go_executable)
    return str(go_path.parent / "gofmt")


def safe_argv(argv: Sequence[str]) -> str:
    """Return a safely quoted representation of the argument list using shlex.join."""
    return shlex.join(argv)


class Check:
    def __init__(
        self,
        argv: Sequence[str],
        env: dict[str, str] | None = None,
        cwd: Path | None = None,
    ):
        self.argv = list(argv)
        self.env = env
        self.cwd = cwd


class CheckResult:
    def __init__(
        self,
        argv: Sequence[str],
        exit_code: int,
        stdout: str,
        stderr: str,
        duration_seconds: float = 0.0,
    ):
        self.argv = list(argv)
        self.exit_code = exit_code
        self.stdout = stdout
        self.stderr = stderr
        self.duration_seconds = duration_seconds


def create_fake_check_result(argv: Sequence[str], stderr: str) -> CheckResult:
    """Create a failed check result without running a process."""
    return CheckResult(
        argv=argv,
        exit_code=1,
        stdout="",
        stderr=stderr,
    )


def run_check(check: Check, repo_root: Path, base_env: dict[str, str]) -> CheckResult:
    """Run a single check and return its result."""
    env = dict(base_env)
    if check.env:
        env.update(check.env)

    cwd = check.cwd if check.cwd is not None else repo_root

    started = time.monotonic()
    try:
        proc = subprocess.run(
            check.argv,
            cwd=cwd,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        return CheckResult(
            argv=check.argv,
            exit_code=proc.returncode,
            stdout=proc.stdout,
            stderr=proc.stderr,
            duration_seconds=time.monotonic() - started,
        )
    except Exception as e:
        return CheckResult(
            argv=check.argv,
            exit_code=1,
            stdout="",
            stderr=f"Exception: {e}",
            duration_seconds=time.monotonic() - started,
        )


def run_and_print(check: Check, repo_root: Path, base_env: dict[str, str]) -> CheckResult:
    """Run a check, print its output, and return the result."""
    print(f"Running: {safe_argv(check.argv)}")
    result = run_check(check, repo_root, base_env)
    print(f"Exit code: {result.exit_code}")
    print(f"Duration: {result.duration_seconds:.3f}s")
    if result.stdout:
        print(result.stdout.rstrip())
    if result.stderr:
        print(result.stderr.rstrip(), file=sys.stderr)
    print()
    return result


def run_go_checks(
    repo_root: Path,
    go_executable: str,
    gofmt_path: str,
    build_dir: Path,
    base_env: dict[str, str],
    mode: str = "full",
    jobs: int = 1,
) -> list[CheckResult]:
    """Run all Go checks and return their results."""
    if mode == "cross":
        return run_cross_checks(repo_root, go_executable, build_dir, base_env, jobs)
    results: list[CheckResult] = []

    # 1. git ls-files *.go
    git_ls_check = Check(argv=["git", "ls-files", "*.go"])
    git_ls_result = run_and_print(git_ls_check, repo_root, base_env)
    results.append(git_ls_result)

    # 2. gofmt check
    gofmt_argv = [gofmt_path, "-l"]
    if git_ls_result.exit_code != 0:
        # git ls-files failed, create explicit failed gofmt check without running
        gofmt_result = create_fake_check_result(
            argv=gofmt_argv,
            stderr="Skipped: git ls-files failed",
        )
        print(f"Running: {safe_argv(gofmt_result.argv)}")
        print(f"Exit code: {gofmt_result.exit_code}")
        if gofmt_result.stderr:
            print(gofmt_result.stderr.rstrip(), file=sys.stderr)
        print()
    else:
        tracked_go_files = git_ls_result.stdout.strip().split("\n")
        tracked_go_files = [f for f in tracked_go_files if f]
        # Retain only tracked Go paths that currently exist as regular files
        # below repo_root. Tracked-but-deleted files (unstaged deletions) are
        # omitted so gofmt never lstat's a path absent from the working tree.
        tracked_go_files = [
            f for f in tracked_go_files if (repo_root / f).is_file()
        ]
        if tracked_go_files:
            gofmt_argv.extend(tracked_go_files)
        gofmt_check = Check(argv=gofmt_argv)
        gofmt_result = run_and_print(gofmt_check, repo_root, base_env)

        # If gofmt exited 0 but has stdout, treat as failure
        if gofmt_result.exit_code == 0 and gofmt_result.stdout.strip() != "":
            gofmt_result = CheckResult(
                argv=gofmt_result.argv,
                exit_code=1,
                stdout=gofmt_result.stdout,
                stderr=gofmt_result.stderr + "\nFormatting issues found",
                duration_seconds=gofmt_result.duration_seconds,
            )
            # Reprint with corrected status
            print(f"Exit code: {gofmt_result.exit_code} (corrected)")
            if gofmt_result.stderr:
                print(gofmt_result.stderr.rstrip(), file=sys.stderr)
            print()

    results.append(gofmt_result)

    # 3. go test -count=1 ./...
    test_check = Check(argv=[go_executable, "test", "-count=1", "./..."])
    results.append(run_and_print(test_check, repo_root, base_env))

    # 4. go vet ./...
    vet_check = Check(argv=[go_executable, "vet", "./..."])
    results.append(run_and_print(vet_check, repo_root, base_env))

    # 5. git diff --check
    diff_check = Check(argv=["git", "diff", "--check"])
    results.append(run_and_print(diff_check, repo_root, base_env))

    if mode == "full":
        results.extend(run_cross_checks(repo_root, go_executable, build_dir, base_env, jobs))
    return results


def run_cross_checks(
    repo_root: Path, go_executable: str, build_dir: Path, base_env: dict[str, str],
    jobs: int = 1,
) -> list[CheckResult]:
    """Compile target-specific code once; these binaries are not executed."""
    checks: list[Check] = []
    # Three Windows amd64 production cross-builds
    for binary_name, cmd_path in [
        ("sidraviactl.exe", "./cmd/sidravia"),
        ("sidraviad.exe", "./cmd/sidraviad"),
        ("sidraviamgr.exe", "./cmd/sidraviamgr"),
    ]:
        build_check = Check(
            argv=[go_executable, "build", "-o", str(build_dir / binary_name), cmd_path],
            env={
                "GOOS": "windows",
                "GOARCH": "amd64",
                "CGO_ENABLED": "0",
            },
        )
        checks.append(build_check)

    # Cross-compile platform-specific test packages. The resulting
    # binaries are only compiler evidence; native execution stays in hosted CI.
    platform_test_packages = [
        ("windows", "windows-amd64-internal-cli.test.exe", "./internal/cli"),
        (
            "windows",
            "windows-amd64-internal-daemon-environment.test.exe",
            "./internal/daemon/environment",
        ),
        (
            "windows",
            "windows-amd64-internal-daemon-persistence-jsonfile.test.exe",
            "./internal/daemon/persistence/jsonfile",
        ),
        ("darwin", "darwin-amd64-internal-cli.test", "./internal/cli"),
        (
            "darwin",
            "darwin-amd64-internal-daemon-environment.test",
            "./internal/daemon/environment",
        ),
        (
            "darwin",
            "darwin-amd64-internal-daemon-host.test",
            "./internal/daemon/host",
        ),
    ]
    for goos, binary_name, package_path in platform_test_packages:
        test_compile_check = Check(
            argv=[
                go_executable,
                "test",
                "-c",
                "-o",
                str(build_dir / binary_name),
                package_path,
            ],
            env={
                "GOOS": goos,
                "GOARCH": "amd64",
                "CGO_ENABLED": "0",
            },
        )
        checks.append(test_compile_check)

    if jobs == 1:
        return [run_and_print(check, repo_root, base_env) for check in checks]
    with ThreadPoolExecutor(max_workers=jobs) as pool:
        return list(pool.map(lambda check: run_and_print(check, repo_root, base_env), checks))


def build_python_checks(
    repo_root: Path,
    base_env: dict[str, str],
) -> list[Check]:
    """Build the list of Python checks without running them."""
    checks: list[Check] = []

    # tools/drcom520d_mock_server/tests with src on PYTHONPATH
    drcom_mock_tests = repo_root / "tools" / "drcom520d_mock_server" / "tests"
    drcom_mock_src = repo_root / "tools" / "drcom520d_mock_server" / "src"
    env = dict(base_env)
    pythonpath = str(drcom_mock_src)
    if "PYTHONPATH" in env:
        pythonpath = os.pathsep.join([pythonpath, env["PYTHONPATH"]])
    env["PYTHONPATH"] = pythonpath
    checks.append(
        Check(
            argv=[sys.executable, "-m", "unittest", "discover", "-s", str(drcom_mock_tests), "-v"],
            env=env,
        )
    )

    # tools/windows_drcom_acceptance/tests with src on PYTHONPATH
    wda_tests = repo_root / "tools" / "windows_drcom_acceptance" / "tests"
    wda_src = repo_root / "tools" / "windows_drcom_acceptance" / "src"
    env = dict(base_env)
    pythonpath = str(wda_src)
    if "PYTHONPATH" in env:
        pythonpath = os.pathsep.join([pythonpath, env["PYTHONPATH"]])
    env["PYTHONPATH"] = pythonpath
    checks.append(
        Check(
            argv=[sys.executable, "-m", "unittest", "discover", "-s", str(wda_tests), "-v"],
            env=env,
        )
    )

    # Verifier unit tests run as a public repository check. They import the
    # module directly, so this does not recursively invoke the verifier.
    verifier_tests = repo_root / "tools" / "developer" / "tests"
    checks.append(
        Check(
            argv=[
                sys.executable,
                "-m",
                "unittest",
                "discover",
                "-s",
                str(verifier_tests),
                "-v",
            ],
            env=dict(base_env),
        )
    )

    return checks


def run_python_checks(
    repo_root: Path,
    base_env: dict[str, str],
) -> list[CheckResult]:
    """Run all Python checks and return their results."""
    checks = build_python_checks(repo_root, base_env)
    results: list[CheckResult] = []
    for check in checks:
        results.append(run_and_print(check, repo_root, base_env))
    return results


def print_summary(results: list[CheckResult]) -> None:
    """Print the final summary of results."""
    passed = sum(1 for r in results if r.exit_code == 0)
    failed = sum(1 for r in results if r.exit_code != 0)

    print("=" * 60)
    print(f"Summary: {passed} passed, {failed} failed")
    for result in results:
        status = "PASS" if result.exit_code == 0 else "FAIL"
        print(f"  {status}: {safe_argv(result.argv)} ({result.duration_seconds:.3f}s)")
    print("=" * 60)


if __name__ == "__main__":
    sys.exit(main())
