from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence, TextIO

from .evidence import (
    compare_evidence,
    load_session_snapshot,
    load_transcript,
    load_transport_observation,
    load_tshark_json,
)
from .model import ComparisonResult, EvidenceError
from .preflight import Preflight, render_preflight_zh


@dataclass(slots=True)
class CliDependencies:
    preflight: Any
    stdout: TextIO
    stderr: TextIO


def _repository_root() -> Path:
    return Path(__file__).resolve().parents[4]


def _default_dependencies() -> CliDependencies:
    return CliDependencies(
        preflight=Preflight(repository_root=_repository_root()),
        stdout=sys.stdout,
        stderr=sys.stderr,
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="sidravia-drcom-acceptance",
        description="Sidravia Windows Dr.COM 离线验收与实网研究基础设施",
    )
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument(
        "--preflight", action="store_true",
        help="只读中文预检；不触发 UAC、不收集凭据、不认证",
    )
    mode.add_argument(
        "--validate", action="store_true",
        help="离线比对 transcript、LocalAddr/Session 与 tshark JSON",
    )
    parser.add_argument("--transcript", type=Path, help="application transcript JSONL")
    parser.add_argument("--transport", type=Path, help="transport observation JSON")
    parser.add_argument("--snapshot", type=Path, help="Session Snapshot JSON")
    parser.add_argument("--tshark-json", type=Path, help="tshark -T json 输出")
    parser.add_argument(
        "--require-keepalive", action="store_true",
        help="离线比对时要求 KA1/KA2 生命周期证据",
    )
    return parser


def _render_comparison(result: ComparisonResult, output: TextIO) -> int:
    for check in result.checks:
        label = "通过" if check.passed else "失败"
        print(f"[{label}] {check.code}：{check.summary}", file=output)
    if result.passed:
        print(f"结论：离线三方证据比对通过，共 {result.packet_count} 个目标数据报。", file=output)
        print("说明：这不等于实网认证、Npcap 抓包或校园网验收通过。", file=output)
        return 0
    failure_code = result.failure.code if result.failure is not None else "unknown"
    print(f"结论：离线三方证据比对失败（{failure_code}）。", file=output)
    return 2


def _validate(args: argparse.Namespace, dependencies: CliDependencies) -> int:
    paths = (args.transcript, args.transport, args.snapshot, args.tshark_json)
    if any(path is None for path in paths):
        print(
            "证据无效：--validate 必须同时提供 --transcript、--transport、"
            "--snapshot 和 --tshark-json。",
            file=dependencies.stderr,
        )
        return 2
    try:
        transcript = load_transcript(args.transcript)
        transport = load_transport_observation(args.transport)
        snapshot = load_session_snapshot(args.snapshot)
        tshark_packets = load_tshark_json(args.tshark_json)
        result = compare_evidence(
            transcript,
            transport,
            snapshot,
            tshark_packets,
            require_keepalive=args.require_keepalive,
        )
    except EvidenceError as error:
        print(f"证据无效：{error}", file=dependencies.stderr)
        return 2
    return _render_comparison(result, dependencies.stdout)


def main(
    argv: Sequence[str] | None = None,
    *,
    dependencies: CliDependencies | None = None,
) -> int:
    args = build_parser().parse_args(argv)
    active = dependencies or _default_dependencies()
    try:
        if args.preflight:
            report = active.preflight.run()
            print(render_preflight_zh(report), end="", file=active.stdout)
            return report.exit_code
        if args.validate:
            return _validate(args, active)
        raise AssertionError("parser requires one supported mode")
    except (EvidenceError, ValueError) as error:
        print(f"验收输入错误：{error}", file=active.stderr)
        return 2
    except BaseException:
        print("验收器内部错误：已隐藏原始异常；请运行 --preflight 后检查离线日志。", file=active.stderr)
        return 3


def entrypoint() -> None:
    raise SystemExit(main())


if __name__ == "__main__":
    entrypoint()
