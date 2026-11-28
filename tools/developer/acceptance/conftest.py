"""Select the actual component profiles supplied by the package producer."""

from dataclasses import dataclass
from pathlib import Path

import pytest


@dataclass(frozen=True)
class Distribution:
    profile: str
    directory: Path
    gui: Path | None = None


def pytest_addoption(parser):
    group = parser.getgroup("distribution")
    group.addoption("--core-directory", type=Path, help="Built Core distribution")
    group.addoption("--gui-directory", type=Path, help="Built GUI distribution")
    group.addoption("--gui-input", type=Path, help="GUI runtime used by the producer")
    group.addoption("--target", help="Producer target, e.g. linux-amd64")
    group.addoption("--product-version", help="Product version passed to the producer")
    group.addoption("--build-id", help="Source commit passed to the producer")
    group.addoption("--source-repo", type=Path, default=Path(__file__).resolve().parents[3],
                    help="Source root used to build the candidate")
    group.addoption("--go", default="go", help="Go executable for build metadata inspection")
    group.addoption("--native-help", action="store_true", help="Run isolated native help smoke")
    group.addoption("--receipt-dir", type=Path, help="Directory for successful product receipts")


def pytest_generate_tests(metafunc):
    if "distribution" not in metafunc.fixturenames:
        return
    get = metafunc.config.getoption
    for name in ("--target", "--product-version", "--build-id", "--receipt-dir"):
        if get(name) is None:
            raise pytest.UsageError(f"distribution acceptance requires {name}")
    core = get("--core-directory")
    gui = get("--gui-directory")
    gui_input = get("--gui-input")
    if (gui is None) != (gui_input is None):
        raise pytest.UsageError("--gui-directory and --gui-input must be supplied together")
    cases = []
    if core is not None:
        cases.append(Distribution("core", core))
    if gui is not None:
        cases.append(Distribution("gui", gui, gui_input))
    if not cases:
        raise pytest.UsageError("provide at least one of --core-directory or --gui-directory")
    metafunc.parametrize("distribution", cases, ids=lambda case: case.profile)
