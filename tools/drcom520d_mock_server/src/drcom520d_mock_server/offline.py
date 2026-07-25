from __future__ import annotations

import argparse
import json
import struct
import sys
from ipaddress import IPv4Address
from pathlib import Path
from typing import Any

from .codec import (
    PacketFormatError,
    parse_ka1_request,
    parse_ka2_request,
    parse_login_request,
    parse_logout_request,
)
from .models import Operation
from .server import classify_operation
from .trace import to_json_value
from .transcript import (
    TranscriptFormatError,
    align_transcripts,
    extract_transcript_records,
    packet_record,
    validate_transcript_records,
)


class OfflineFormatError(ValueError):
    pass


_REQUEST_PARSERS = {
    Operation.LOGIN: parse_login_request,
    Operation.KA1: parse_ka1_request,
    Operation.KA2: parse_ka2_request,
    Operation.LOGOUT: parse_logout_request,
}


def decode_payload(payload: bytes) -> dict[str, Any]:
    """Decode a request payload without opening a socket or capture device."""
    operation = classify_operation(payload)
    if operation is None:
        raise OfflineFormatError("无法识别 payload 对应的 Dr.COM 操作")

    if operation is Operation.CHALLENGE:
        if len(payload) < 20:
            raise OfflineFormatError("Challenge packet must contain at least 20 bytes")
        parsed: Any = {"length": len(payload), "request": payload}
    else:
        parser = _REQUEST_PARSERS[operation]
        try:
            parsed = parser(payload)
        except PacketFormatError as error:
            raise OfflineFormatError(str(error)) from error

    return {
        "operation": operation.value,
        "payload_hex": payload.hex(),
        "payload_length": len(payload),
        "parsed": to_json_value(parsed),
    }


_PCAP_MAGICS = {
    b"\xd4\xc3\xb2\xa1": ("<", 1_000_000, 1_000),
    b"\xa1\xb2\xc3\xd4": (">", 1_000_000, 1_000),
    b"\x4d\x3c\xb2\xa1": ("<", 1_000_000_000, 1),
    b"\xa1\xb2\x3c\x4d": (">", 1_000_000_000, 1),
}


def _parse_ethernet_ipv4_udp(
    frame: bytes,
    *,
    record_index: int,
    server_port: int,
) -> dict[str, Any] | None:
    location = f"PCAP record {record_index}"
    if len(frame) < 14:
        raise OfflineFormatError(f"{location} Ethernet 帧截断")
    if struct.unpack("!H", frame[12:14])[0] != 0x0800:
        return None

    ip = frame[14:]
    if len(ip) < 20:
        raise OfflineFormatError(f"{location} IPv4 头截断")
    version = ip[0] >> 4
    ihl = (ip[0] & 0x0F) * 4
    if version != 4:
        raise OfflineFormatError(f"{location} Ethernet IPv4 类型包含非 IPv4 版本")
    if ihl < 20:
        raise OfflineFormatError(f"{location} IPv4 IHL 长度无效")
    if len(ip) < ihl:
        raise OfflineFormatError(f"{location} IPv4 options 截断")

    total_length = struct.unpack("!H", ip[2:4])[0]
    if total_length < ihl:
        raise OfflineFormatError(f"{location} IPv4 total length 小于 IHL 长度")
    if total_length > len(ip):
        raise OfflineFormatError(f"{location} IPv4 数据截断")

    fragment_field = struct.unpack("!H", ip[6:8])[0]
    if fragment_field & 0x3FFF:
        raise OfflineFormatError(f"{location} 不支持 IPv4 分片")
    if ip[9] != 17:
        return None

    udp = ip[ihl:total_length]
    if len(udp) < 8:
        raise OfflineFormatError(f"{location} UDP 头截断")
    source_port, destination_port, udp_length, _ = struct.unpack("!HHHH", udp[:8])
    if udp_length < 8:
        raise OfflineFormatError(f"{location} UDP 长度小于头部长度")
    if udp_length != len(udp):
        raise OfflineFormatError(
            f"{location} UDP 长度必须精确等于 IPv4 payload 长度")
    if source_port == server_port and destination_port == server_port:
        raise OfflineFormatError(f"{location} UDP 两端口均为 server_port，方向有歧义")
    if source_port != server_port and destination_port != server_port:
        return None

    source_ip = str(IPv4Address(ip[12:16]))
    destination_ip = str(IPv4Address(ip[16:20]))
    if destination_port == server_port:
        direction = "receive"
        local_endpoint = (destination_ip, destination_port)
        remote_endpoint = (source_ip, source_port)
    else:
        direction = "send"
        local_endpoint = (source_ip, source_port)
        remote_endpoint = (destination_ip, destination_port)

    payload = udp[8:udp_length]
    operation = classify_operation(payload)
    return packet_record(
        side="server",
        direction=direction,
        operation=operation.value if operation is not None else None,
        payload=payload,
        local_endpoint=local_endpoint,
        remote_endpoint=remote_endpoint,
    )


def read_pcap(path: Path, server_port: int) -> list[dict[str, Any]]:
    """Read Ethernet/IPv4/UDP packets from a classic PCAP file."""
    if not 1 <= server_port <= 65535:
        raise OfflineFormatError("server_port 必须在 1 到 65535 之间")
    try:
        data = path.read_bytes()
    except OSError as error:
        raise OfflineFormatError(f"无法读取 PCAP 文件 {path}: {error}") from error

    if len(data) >= 4 and data[:4] == b"\x0a\x0d\x0d\x0a":
        raise OfflineFormatError("不支持 PCAPNG；请提供 classic PCAP 文件")
    if len(data) < 4:
        raise OfflineFormatError("classic PCAP 全局头截断")
    magic = _PCAP_MAGICS.get(data[:4])
    if magic is None:
        raise OfflineFormatError("无法识别 classic PCAP magic")
    if len(data) < 24:
        raise OfflineFormatError("classic PCAP 全局头截断")

    endian, fraction_limit, fraction_to_ns = magic
    _, major, minor, _, _, _, link_type = struct.unpack(
        endian + "IHHIIII", data[:24])
    if (major, minor) != (2, 4):
        raise OfflineFormatError(
            f"不支持 classic PCAP 版本 {major}.{minor}，需要 2.4")
    if link_type != 1:
        raise OfflineFormatError(
            f"PCAP linktype {link_type} 不是受支持的 Ethernet linktype 1")

    rows: list[dict[str, Any]] = []
    offset = 24
    record_index = 0
    while offset < len(data):
        record_index += 1
        if len(data) - offset < 16:
            raise OfflineFormatError(f"PCAP record {record_index} 头截断")
        ts_seconds, ts_fraction, captured_length, original_length = struct.unpack(
            endian + "IIII", data[offset:offset + 16])
        offset += 16
        if ts_fraction >= fraction_limit:
            raise OfflineFormatError(f"PCAP record {record_index} 时间戳分量无效")
        if captured_length > original_length:
            raise OfflineFormatError(f"PCAP record {record_index} captured 长度无效")
        if captured_length > len(data) - offset:
            raise OfflineFormatError(f"PCAP record {record_index} 帧截断")

        frame = data[offset:offset + captured_length]
        offset += captured_length
        row = _parse_ethernet_ipv4_udp(
            frame, record_index=record_index, server_port=server_port)
        if row is None:
            continue
        row["capture_record"] = record_index
        row["capture_timestamp_ns"] = (
            ts_seconds * 1_000_000_000 + ts_fraction * fraction_to_ns)
        rows.append(row)

    return rows


def _read_jsonl(path: Path) -> list[dict[str, Any]]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        raise OfflineFormatError(f"无法读取 UTF-8 JSONL 文件 {path}: {error}") from error

    rows = []
    for line_number, line in enumerate(lines, start=1):
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError as error:
            raise OfflineFormatError(
                f"{path} 第 {line_number} 行不是有效 JSON: {error.msg}") from error
        if not isinstance(row, dict):
            raise OfflineFormatError(f"{path} 第 {line_number} 行必须是 JSON object")
        rows.append(row)
    return rows


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Dr.COM transcript、payload 与 classic PCAP 离线诊断工具")
    subparsers = parser.add_subparsers(dest="command", required=True)

    payload_parser = subparsers.add_parser(
        "payload", help="解析十六进制 Dr.COM 请求 payload")
    payload_parser.add_argument("payload_hex", help="十六进制 payload")

    pcap_parser = subparsers.add_parser(
        "pcap", help="解析 classic PCAP 中的 Ethernet/IPv4/UDP payload")
    pcap_parser.add_argument("path", type=Path, help="classic PCAP 文件")
    pcap_parser.add_argument(
        "--server-port", type=int, default=61440, help="Dr.COM UDP 服务端口")

    compare_parser = subparsers.add_parser(
        "compare", help="对齐 Go client transcript 与完整 server trace")
    compare_parser.add_argument("client", type=Path, help="client transcript JSONL")
    compare_parser.add_argument("server", type=Path, help="server trace JSONL")
    return parser


def _emit_jsonl(row: dict[str, Any]) -> None:
    print(json.dumps(row, ensure_ascii=False, separators=(",", ":")))


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        for stream in (sys.stdout, sys.stderr):
            reconfigure = getattr(stream, "reconfigure", None)
            if reconfigure is not None:
                reconfigure(encoding="utf-8")

    arguments = _build_parser().parse_args(argv)
    try:
        if arguments.command == "payload":
            try:
                payload = bytes.fromhex(arguments.payload_hex)
            except ValueError as error:
                raise OfflineFormatError(f"payload 不是有效十六进制：{error}") from error
            _emit_jsonl(decode_payload(payload))
            return 0

        if arguments.command == "pcap":
            for row in read_pcap(arguments.path, arguments.server_port):
                _emit_jsonl(row)
            return 0

        client_rows = _read_jsonl(arguments.client)
        server_rows = _read_jsonl(arguments.server)
        client_records = client_rows
        validate_transcript_records(
            client_records, expected_side="client", context="client")
        server_records = extract_transcript_records(server_rows)
        mismatches = align_transcripts(client_records, server_records)
        if mismatches:
            mismatch = mismatches[0]
            _emit_jsonl({
                "summary": "对齐失败",
                "record_type": mismatch.record_type,
                "index": mismatch.index,
                "field_path": mismatch.field_path,
                "reason": mismatch.reason,
                "client": mismatch.client,
                "server": mismatch.server,
            })
            return 1
        print(f"对齐成功：{len(client_records)} 条 transcript record 完全一致")
        return 0
    except (OfflineFormatError, TranscriptFormatError) as error:
        print(f"格式错误：{error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
