from __future__ import annotations

import subprocess
from dataclasses import dataclass
from typing import Mapping, Sequence


@dataclass(frozen=True, slots=True)
class CommandResult:
    argv: tuple[str, ...]
    returncode: int
    stdout: str
    stderr: str


class CommandRunner:
    """Run fixed argv vectors without a command shell."""

    def __init__(self, *, environment: Mapping[str, str] | None = None):
        self._environment = dict(environment) if environment is not None else None

    def run(
        self,
        argv: Sequence[str],
        *,
        input_text: str | None = None,
        timeout: float = 10.0,
    ) -> CommandResult:
        normalized = tuple(str(part) for part in argv)
        try:
            completed = subprocess.run(
                normalized,
                input=input_text,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=timeout,
                check=False,
                shell=False,
                env=self._environment,
            )
            return CommandResult(
                argv=normalized,
                returncode=completed.returncode,
                stdout=completed.stdout,
                stderr=completed.stderr,
            )
        except FileNotFoundError:
            return CommandResult(normalized, 127, "", "executable not found")
        except subprocess.TimeoutExpired:
            return CommandResult(normalized, 124, "", "command timed out")
        except OSError as error:
            return CommandResult(normalized, 126, "", type(error).__name__)
