from __future__ import annotations

import re
import shutil
import time
import uuid
from pathlib import Path
from typing import Callable, Sequence

from .processes import CommandRunner


class ArtifactSecurityError(RuntimeError):
    """The sensitive artifact directory could not be created safely."""


class CleanupError(RuntimeError):
    """One or more sensitive artifacts could not be removed."""


_RUN_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z")
_SID = re.compile(r"S-\d+(?:-\d+)+")
_SYSTEM_SID = "S-1-5-18"


class SensitiveArtifacts:
    """Own one acceptance run's sensitive, short-lived files.

    Instances returned by :meth:`create` are not exposed until Windows ACL
    inheritance has been removed and access has been granted only to the
    current user and Local System.  Tests may construct an instance directly
    to exercise cleanup without changing a host ACL.
    """

    _NAMES = (
        "capture-manifest.json",
        "capture-ready.json",
        "capture-stop.signal",
        "capture.pcap",
        "application-transcript.jsonl",
        "transport-observation.json",
        "session-snapshot.json",
        "tshark.json",
        "internal-report.json",
    )

    def __init__(
        self,
        root: Path,
        *,
        unlink: Callable[[Path], None] | None = None,
        rmtree: Callable[[Path], None] | None = None,
        sleep: Callable[[float], None] = time.sleep,
        retry_delays: Sequence[float] = (0.0, 0.05, 0.2),
    ):
        self.root = Path(root)
        self.manifest = self.root / self._NAMES[0]
        self.ready = self.root / self._NAMES[1]
        self.stop_signal = self.root / self._NAMES[2]
        self.capture = self.root / self._NAMES[3]
        self.application_transcript = self.root / self._NAMES[4]
        self.transport_observation = self.root / self._NAMES[5]
        self.session_snapshot = self.root / self._NAMES[6]
        self.tshark_json = self.root / self._NAMES[7]
        self.internal_report = self.root / self._NAMES[8]
        self._unlink = unlink or (lambda path: path.unlink())
        self._rmtree = rmtree or shutil.rmtree
        self._sleep = sleep
        self._retry_delays = tuple(retry_delays) or (0.0,)

    @property
    def sensitive_paths(self) -> tuple[Path, ...]:
        return tuple(self.root / name for name in self._NAMES)

    @property
    def sensitive_names(self) -> tuple[str, ...]:
        return self._NAMES

    @classmethod
    def create(
        cls,
        local_app_data: Path,
        runner: CommandRunner,
        *,
        run_id: str | None = None,
    ) -> "SensitiveArtifacts":
        identifier = run_id or str(uuid.uuid4())
        if not _RUN_ID.fullmatch(identifier):
            raise ArtifactSecurityError("运行标识不安全，未创建敏感制品目录。")

        base = Path(local_app_data).resolve()
        root = base / "Sidravia" / "acceptance" / identifier
        if root.exists() or root.is_symlink():
            raise ArtifactSecurityError("敏感制品目录已存在，拒绝复用。")

        try:
            root.mkdir(parents=True, exist_ok=False)
            sid_result = runner.run(("whoami.exe", "/user", "/fo", "csv", "/nh"))
            sid_match = _SID.search(sid_result.stdout) if sid_result.returncode == 0 else None
            if sid_match is None:
                raise ArtifactSecurityError("无法确认当前用户 SID，未开放敏感制品目录。")

            sid = sid_match.group(0)
            acl_result = runner.run(
                (
                    "icacls.exe",
                    str(root),
                    "/inheritance:r",
                    "/grant:r",
                    f"*{sid}:(OI)(CI)F",
                    "/grant:r",
                    f"*{_SYSTEM_SID}:(OI)(CI)F",
                )
            )
            if acl_result.returncode != 0:
                raise ArtifactSecurityError("敏感制品目录 ACL 加固失败，目录已回收。")
        except ArtifactSecurityError:
            shutil.rmtree(root, ignore_errors=True)
            raise
        except OSError as error:
            shutil.rmtree(root, ignore_errors=True)
            raise ArtifactSecurityError(
                f"敏感制品目录创建失败（{type(error).__name__}），未开始验收。"
            ) from None

        return cls(root)

    def cleanup(self) -> None:
        """Best-effort all deletions, then fail if the controlled root remains."""

        if not self.root.exists() and not self.root.is_symlink():
            return

        failed_names: list[str] = []
        for path in self.sensitive_paths:
            if not path.exists() and not path.is_symlink():
                continue
            if not self._retry(lambda path=path: self._unlink(path)):
                failed_names.append(path.name)

        root_removed = self._retry(lambda: self._rmtree(self.root))
        if root_removed or (not self.root.exists() and not self.root.is_symlink()):
            return

        names = sorted(set(failed_names))
        names.append(self.root.name)
        raise CleanupError("敏感临时制品清理失败：" + "、".join(names))

    def _retry(self, operation: Callable[[], None]) -> bool:
        for delay in self._retry_delays:
            if delay:
                self._sleep(delay)
            try:
                operation()
                return True
            except FileNotFoundError:
                return True
            except OSError:
                continue
        return False
