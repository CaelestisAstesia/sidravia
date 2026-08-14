from __future__ import annotations

import json
import os
import re
import shutil
import sys
import tempfile
from dataclasses import dataclass
from enum import Enum
from pathlib import Path
from typing import Callable, Mapping, Protocol, Sequence

from .model import Endpoint
from .pcap_fixture import FixtureDatagram, build_ethernet_ipv4_udp_pcap
from .processes import CommandResult, CommandRunner


class CheckStatus(str, Enum):
    PASS = "pass"
    WARN = "warn"
    FAIL = "fail"
    SKIP = "skip"


@dataclass(frozen=True, slots=True)
class PreflightCheck:
    code: str
    title: str
    status: CheckStatus
    summary: str
    action: str | None = None
    details: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class PreflightReport:
    checks: tuple[PreflightCheck, ...]

    @property
    def exit_code(self) -> int:
        return 2 if any(check.status is CheckStatus.FAIL for check in self.checks) else 0


class Runner(Protocol):
    def run(
        self,
        argv: Sequence[str],
        *,
        input_text: str | None = None,
        timeout: float = 10.0,
    ) -> CommandResult:
        ...


_CAPABILITIES = frozenset({
    "stdin_credentials",
    "application_transcript_v1",
    "transport_observation_v1",
    "session_snapshot_v1",
    "idempotent_cleanup",
})

_SKIP_TITLES = (
    ("python", "Python 版本"),
    ("wireshark", "Wireshark"),
    ("tshark", "tshark"),
    ("dumpcap", "dumpcap"),
    ("npcap", "Npcap"),
    ("lua_load", "Lua 加载"),
    ("capture_devices", "抓包设备枚举"),
    ("adapter_mapping", "网卡映射"),
    ("capture_permission", "抓包权限"),
    ("sidravia", "sidravia"),
    ("sidraviad", "sidraviad"),
    ("sidravia_contract", "Sidravia 验收契约"),
)


class Preflight:
    def __init__(
        self,
        *,
        repository_root: Path,
        runner: Runner | None = None,
        resolver: Callable[[str], str | None] = shutil.which,
        environ: Mapping[str, str] = os.environ,
        platform_name: str = sys.platform,
        python_version: tuple[int, int, int] = sys.version_info[:3],
    ):
        self.repository_root = Path(repository_root).resolve()
        self.runner = runner or CommandRunner()
        self.resolver = resolver
        self.environ = environ
        self.platform_name = platform_name
        self.python_version = python_version

    def run(self) -> PreflightReport:
        if self.platform_name != "win32":
            checks = [PreflightCheck(
                "platform", "Windows 平台", CheckStatus.FAIL,
                "该验收器只支持 Windows。", "请在 Windows 主机上运行。",
            )]
            checks.extend(
                PreflightCheck(code, title, CheckStatus.SKIP, "跳过：平台不受支持。")
                for code, title in _SKIP_TITLES
            )
            return PreflightReport(tuple(checks))

        checks: list[PreflightCheck] = [PreflightCheck(
            "platform", "Windows 平台", CheckStatus.PASS, "当前平台为 Windows。"
        )]
        checks.append(self._python_check())

        wireshark = self._find_executable("Wireshark.exe")
        tshark = self._find_executable("tshark.exe")
        dumpcap = self._find_executable("dumpcap.exe")
        sidravia = self._find_executable("sidraviactl.exe")
        sidraviad = self._find_executable("sidraviad.exe")

        wireshark_check = self._version_check(
            "wireshark", "Wireshark", wireshark,
            "安装 Wireshark 并选择 Npcap；本工具不会自动安装。",
        )
        tshark_check = self._version_check(
            "tshark", "tshark", tshark,
            "确认 Wireshark 安装包含 tshark.exe，并重新打开终端。",
        )
        dumpcap_check = self._version_check(
            "dumpcap", "dumpcap", dumpcap,
            "确认 Wireshark 安装包含 dumpcap.exe。",
        )
        checks.extend((wireshark_check, tshark_check, dumpcap_check))
        npcap_check = self._npcap_check()
        checks.append(npcap_check)

        if tshark_check.status is CheckStatus.PASS:
            checks.append(self._lua_check(tshark))
        else:
            checks.append(PreflightCheck(
                "lua_load", "Lua 加载", CheckStatus.SKIP,
                "跳过：前置工具 tshark 缺失或不可用。",
            ))

        devices: tuple[tuple[str, str], ...] = ()
        if dumpcap_check.status is CheckStatus.PASS and npcap_check.status is CheckStatus.PASS:
            device_check, devices = self._device_check(dumpcap)
            checks.append(device_check)
        else:
            checks.append(PreflightCheck(
                "capture_devices", "抓包设备枚举", CheckStatus.SKIP,
                "跳过：前置工具 dumpcap 或 Npcap 缺失。",
            ))

        if devices:
            checks.append(self._adapter_mapping_check(devices))
            checks.append(PreflightCheck(
                "capture_permission", "抓包权限", CheckStatus.PASS,
                "普通权限可以枚举 Npcap 设备；真实抓包阶段只提升 dumpcap 代理。",
            ))
        else:
            checks.append(PreflightCheck(
                "adapter_mapping", "网卡映射", CheckStatus.SKIP,
                "跳过：没有可映射的 dumpcap 设备。",
            ))
            checks.append(PreflightCheck(
                "capture_permission", "抓包权限", CheckStatus.SKIP,
                "跳过：尚不能在普通权限下枚举抓包设备。",
            ))

        sidravia_check = self._version_check(
            "sidravia", "sidravia", sidravia,
            "先构建或安装当前分支的 sidraviactl.exe。",
        )
        sidraviad_check = self._version_check(
            "sidraviad", "sidraviad", sidraviad,
            "先构建或安装当前分支的 sidraviad.exe。",
        )
        checks.extend((sidravia_check, sidraviad_check))
        if sidravia_check.status is CheckStatus.PASS:
            checks.append(self._contract_check(sidravia))
        else:
            checks.append(PreflightCheck(
                "sidravia_contract", "Sidravia 验收契约", CheckStatus.SKIP,
                "跳过：sidraviactl.exe 尚不可用。",
            ))
        return PreflightReport(tuple(checks))

    def _python_check(self) -> PreflightCheck:
        version = ".".join(str(part) for part in self.python_version)
        if self.python_version < (3, 11, 0):
            return PreflightCheck(
                "python", "Python 版本", CheckStatus.FAIL,
                f"当前 Python {version} 低于 3.11。", "安装 Python 3.11 或更高版本。",
            )
        return PreflightCheck("python", "Python 版本", CheckStatus.PASS, f"Python {version} 可用。")

    def _find_executable(self, name: str) -> str | None:
        resolved = self.resolver(name)
        if resolved:
            return str(resolved)
        for variable in ("ProgramFiles", "ProgramFiles(x86)"):
            root = self.environ.get(variable)
            if root:
                candidate = Path(root) / "Wireshark" / name
                if candidate.is_file():
                    return str(candidate)
        return None

    def _version_check(
        self, code: str, title: str, executable: str | None, action: str
    ) -> PreflightCheck:
        if executable is None:
            return PreflightCheck(code, title, CheckStatus.FAIL, f"未找到 {title}。", action)
        response = self.runner.run((executable, "--version"), timeout=10)
        if response.returncode != 0:
            return PreflightCheck(code, title, CheckStatus.FAIL,
                                  f"{title} 存在但版本查询失败。", action)
        first_line = next((line.strip() for line in response.stdout.splitlines() if line.strip()), "版本可用")
        return PreflightCheck(code, title, CheckStatus.PASS, first_line, details=(executable,))

    def _npcap_check(self) -> PreflightCheck:
        sc = self.resolver("sc.exe") or "sc.exe"
        response = self.runner.run((sc, "query", "npcap"), timeout=10)
        if response.returncode != 0:
            return PreflightCheck(
                "npcap", "Npcap", CheckStatus.FAIL, "未检测到 Npcap service/driver。",
                "安装 Wireshark 时选择 Npcap；本工具不会自动安装。",
            )
        if "RUNNING" not in response.stdout.upper():
            return PreflightCheck(
                "npcap", "Npcap", CheckStatus.FAIL, "Npcap service 存在但未运行。",
                "检查 Npcap 服务状态或重新安装 Npcap。",
            )
        return PreflightCheck("npcap", "Npcap", CheckStatus.PASS, "Npcap service 正在运行。")

    def _lua_check(self, tshark: str | None) -> PreflightCheck:
        assert tshark is not None
        lua = self.repository_root / "tools" / "wireshark" / "drcom.lua"
        if not lua.is_file():
            return PreflightCheck(
                "lua_load", "Lua 加载", CheckStatus.FAIL, "仓库中的 drcom.lua 不存在。",
                "确认在包含 tools/wireshark/drcom.lua 的提交上运行。",
            )
        fixture = build_ethernet_ipv4_udp_pcap((FixtureDatagram(
            source=Endpoint("10.0.0.2", 49152),
            destination=Endpoint("10.100.61.3", 61440),
            payload=bytes.fromhex("0102000009000000000000000000000000000000"),
        ),))
        with tempfile.TemporaryDirectory(prefix="sidravia-preflight-") as directory:
            capture = Path(directory) / "challenge.pcap"
            capture.write_bytes(fixture)
            response = self.runner.run((
                tshark, "-n", "-r", str(capture), "-X", f"lua_script:{lua}",
                "-T", "fields", "-E", "separator=\t",
                "-e", "drcom.packet_kind", "-e", "drcom.direction", "-e", "drcom.valid",
            ), timeout=20)
        columns = tuple(
            line.split("\t") for line in response.stdout.splitlines() if line.strip()
        )
        valid_row = any(
            len(row) == 3
            and row[0] == "challenge_request"
            and row[1] == "client_to_server"
            and row[2].lower() in {"1", "true"}
            for row in columns
        )
        if response.returncode != 0 or not valid_row:
            return PreflightCheck(
                "lua_load", "Lua 加载", CheckStatus.FAIL,
                "tshark 未能加载 Lua 或字段结果不符合 schema v1。",
                "使用 README 中的 tshark 命令检查 Lua 语法和字段注册。",
            )
        return PreflightCheck("lua_load", "Lua 加载", CheckStatus.PASS,
                              "Lua schema v1 已由 tshark fixture 验证。")

    def _device_check(self, dumpcap: str | None) -> tuple[PreflightCheck, tuple[tuple[str, str], ...]]:
        assert dumpcap is not None
        response = self.runner.run((dumpcap, "-D"), timeout=15)
        if response.returncode != 0:
            return PreflightCheck(
                "capture_devices", "抓包设备枚举", CheckStatus.FAIL,
                "普通权限运行 dumpcap -D 失败。",
                "检查 Npcap 安装与设备访问权限；preflight 不会触发 UAC。",
            ), ()
        devices: list[tuple[str, str]] = []
        for line in response.stdout.splitlines():
            match = re.match(r"^\s*\d+\.\s+(\\Device\\NPF_\{?([^}()]+)\}?)(?:\s+\((.*)\))?", line)
            if match:
                devices.append((match.group(2).strip("{}").lower(), (match.group(3) or "").strip()))
        if not devices:
            return PreflightCheck(
                "capture_devices", "抓包设备枚举", CheckStatus.FAIL,
                "dumpcap -D 未返回可识别的 NPF 网卡。",
                "确认 Npcap 已启用并检查 dumpcap -D 的原始输出。",
            ), ()
        return PreflightCheck(
            "capture_devices", "抓包设备枚举", CheckStatus.PASS,
            f"普通权限枚举到 {len(devices)} 个 NPF 设备。",
        ), tuple(devices)

    def _adapter_mapping_check(self, devices: tuple[tuple[str, str], ...]) -> PreflightCheck:
        powershell = self.resolver("powershell.exe") or "powershell.exe"
        script = (
            "$ErrorActionPreference='Stop'; "
            "$adapters = Get-NetAdapter | ForEach-Object { "
            "$a=$_; $ips=@(Get-NetIPAddress -InterfaceIndex $a.ifIndex -AddressFamily IPv4 "
            "-ErrorAction SilentlyContinue | ForEach-Object {$_.IPAddress}); "
            "[pscustomobject]@{Name=$a.Name;InterfaceDescription=$a.InterfaceDescription;"
            "InterfaceGuid=$a.InterfaceGuid.Guid;ifIndex=$a.ifIndex;MacAddress=$a.MacAddress;"
            "Status=$a.Status;IPv4=$ips}}; @($adapters) | ConvertTo-Json -Depth 4 -Compress"
        )
        response = self.runner.run((powershell, "-NoProfile", "-NonInteractive", "-Command", script),
                                   timeout=20)
        if response.returncode != 0:
            return PreflightCheck(
                "adapter_mapping", "网卡映射", CheckStatus.FAIL,
                "普通权限读取 Windows 网卡事实失败。",
                "检查当前账户对 StandardCimv2 的只读权限后重试；不要提升整个验收器。",
            )
        try:
            payload = json.loads(response.stdout)
        except json.JSONDecodeError:
            return PreflightCheck(
                "adapter_mapping", "网卡映射", CheckStatus.FAIL,
                "Windows 网卡事实不是有效 JSON。", "检查 PowerShell Get-NetAdapter 输出。",
            )
        adapters = payload if isinstance(payload, list) else [payload]
        guid_counts: dict[str, int] = {}
        for adapter in adapters:
            if isinstance(adapter, dict) and isinstance(adapter.get("InterfaceGuid"), str):
                guid = adapter["InterfaceGuid"].strip("{}").lower()
                guid_counts[guid] = guid_counts.get(guid, 0) + 1
        unmatched = [guid for guid, _description in devices if guid_counts.get(guid) != 1]
        if unmatched:
            return PreflightCheck(
                "adapter_mapping", "网卡映射", CheckStatus.FAIL,
                "NPF 设备无法与 Windows 网卡建立唯一 GUID 映射。",
                "比较 dumpcap -D 与 Get-NetAdapter 的 InterfaceGuid。",
                details=tuple(unmatched),
            )
        return PreflightCheck(
            "adapter_mapping", "网卡映射", CheckStatus.PASS,
            f"{len(devices)} 个 NPF 设备均已唯一映射到 Windows 网卡。",
        )

    def _contract_check(self, sidravia: str | None) -> PreflightCheck:
        assert sidravia is not None
        response = self.runner.run((
            sidravia, "acceptance", "drcom", "contract", "--output", "json"
        ), timeout=10)
        if response.returncode != 0:
            return PreflightCheck(
                "sidravia_contract", "Sidravia 验收契约", CheckStatus.FAIL,
                "sidraviactl.exe 尚未实现 acceptance contract probe。",
                "由主任务实现设计规格第 13 节集成契约。",
            )
        try:
            payload = json.loads(response.stdout)
            version = payload["schema_version"]
            capabilities = frozenset(payload["capabilities"])
        except (json.JSONDecodeError, KeyError, TypeError):
            return PreflightCheck(
                "sidravia_contract", "Sidravia 验收契约", CheckStatus.FAIL,
                "acceptance contract 输出不是有效 schema。",
                "检查 schema_version 和 capabilities。",
            )
        missing = sorted(_CAPABILITIES - capabilities)
        if version != 1 or missing:
            detail = "、".join(missing) if missing else f"schema_version={version}"
            return PreflightCheck(
                "sidravia_contract", "Sidravia 验收契约", CheckStatus.FAIL,
                f"acceptance contract 不完整：{detail}。",
                "由主任务补齐 schema v1 所需能力。",
            )
        return PreflightCheck(
            "sidravia_contract", "Sidravia 验收契约", CheckStatus.PASS,
            "Sidravia acceptance contract schema v1 完整。",
        )


def render_preflight_zh(report: PreflightReport) -> str:
    labels = {
        CheckStatus.PASS: "通过",
        CheckStatus.WARN: "警告",
        CheckStatus.FAIL: "失败",
        CheckStatus.SKIP: "跳过",
    }
    lines = ["Sidravia Windows Dr.COM 验收预检"]
    for check in report.checks:
        lines.append(f"[{labels[check.status]}] {check.title}：{check.summary}")
        if check.action:
            lines.append(f"  操作：{check.action}")
        for detail in check.details:
            lines.append(f"  详情：{detail}")
    if report.exit_code == 0:
        lines.append("结论：预检通过，可以在获得实网授权后开始验收。")
    else:
        lines.append("结论：预检未通过；未启动抓包、未触发 UAC、未进行认证。")
    return "\n".join(lines) + "\n"
