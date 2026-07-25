from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Sequence, TextIO

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
    confirm: Callable[[str], bool] | None = None
    run_handler: Callable[[argparse.Namespace], Any] | None = None


def _repository_root() -> Path:
    return Path(__file__).resolve().parents[4]


def _default_confirm(prompt: str) -> bool:
    try:
        return input(prompt).strip() == "继续"
    except (EOFError, KeyboardInterrupt):
        return False


def _default_dependencies() -> CliDependencies:
    return CliDependencies(
        preflight=Preflight(repository_root=_repository_root()),
        stdout=sys.stdout,
        stderr=sys.stderr,
        confirm=_default_confirm,
        run_handler=None,
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="sidravia-drcom-acceptance",
        description="Sidravia Windows Dr.COM 离线与实网验收基础设施",
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
    mode.add_argument(
        "--run", action="store_true",
        help="在预检和集成契约通过后编排一次授权的实网验收",
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


def _run(args: argparse.Namespace, dependencies: CliDependencies) -> int:
    report = dependencies.preflight.run()
    print(render_preflight_zh(report), end="", file=dependencies.stdout)
    if report.exit_code != 0:
        return 2

    print(
        "警告：下一步会进行真实校园网认证并弹出一次 UAC；PCAP 与完整证据是敏感临时制品。",
        file=dependencies.stdout,
    )
    confirm = dependencies.confirm or _default_confirm
    if not confirm("确认已获授权并接受网络影响？输入“继续”确认："):
        print("已取消：未触发 UAC、未抓包、未进行认证。", file=dependencies.stdout)
        return 2
    if dependencies.run_handler is None:
        print(
            "实网编排尚未接入 Sidravia 主任务的 acceptance contract；未触发 UAC。",
            file=dependencies.stderr,
        )
        return 3
    dependencies.run_handler(args)
    print("离线与实网证据编排完成；请以清洗后的验收摘要判断结果。", file=dependencies.stdout)
    return 0


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
        return _run(args, active)
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
