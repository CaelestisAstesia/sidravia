from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any, Iterable, Mapping, Sequence

from .model import (
    ComparisonCheck,
    ComparisonFailure,
    ComparisonResult,
    Endpoint,
    EvidenceError,
    PacketRecord,
    SessionSnapshotEvidence,
    TransportObservation,
    TsharkPacket,
)


_MAX_EVIDENCE_BYTES = 64 * 1024 * 1024
_PACKET_KIND_OPERATIONS = {
    "challenge_request": "challenge",
    "challenge_response": "challenge",
    "login_request": "login",
    "login_success": "login",
    "login_failure": "login",
    "ka1_request": "ka1",
    "ka1_response": "ka1",
    "ka2_request": "ka2",
    "ka2_response": "ka2",
    "logout_request": "logout",
    "logout_ack": "logout",
}


def _read_text(path: Path) -> str:
    try:
        size = path.stat().st_size
        if size > _MAX_EVIDENCE_BYTES:
            raise EvidenceError(f"evidence file exceeds {_MAX_EVIDENCE_BYTES} bytes")
        return path.read_text(encoding="utf-8")
    except EvidenceError:
        raise
    except (OSError, UnicodeError) as error:
        raise EvidenceError(f"cannot read evidence file {path.name}: {type(error).__name__}") from error


def _read_json(path: Path) -> Any:
    try:
        return json.loads(_read_text(path))
    except json.JSONDecodeError as error:
        raise EvidenceError(f"invalid JSON in {path.name} at line {error.lineno}") from error


def load_transcript(path: Path) -> tuple[PacketRecord, ...]:
    records: list[PacketRecord] = []
    for line_number, line in enumerate(_read_text(path).splitlines(), 1):
        if not line.strip():
            continue
        try:
            payload = json.loads(line)
        except json.JSONDecodeError as error:
            raise EvidenceError(f"invalid transcript JSON at line {line_number}") from error
        try:
            records.append(PacketRecord.from_json(payload))
        except EvidenceError as error:
            raise EvidenceError(f"invalid transcript line {line_number}: {error}") from error
    if not records:
        raise EvidenceError("application transcript contains no packet records")
    return tuple(records)


def load_transport_observation(path: Path) -> TransportObservation:
    return TransportObservation.from_json(_read_json(path))


def load_session_snapshot(path: Path) -> SessionSnapshotEvidence:
    return SessionSnapshotEvidence.from_json(_read_json(path))


def _mapping(value: Any, name: str) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise EvidenceError(f"{name} must be an object")
    return value


def _scalar(value: Any, field: str) -> Any:
    if isinstance(value, list):
        if len(value) != 1:
            raise EvidenceError(f"{field} must contain exactly one value")
        return value[0]
    return value


def _field(layers: Mapping[str, Any], layer_name: str, field_name: str) -> Any:
    layer = _mapping(layers.get(layer_name), layer_name)
    if field_name not in layer:
        raise EvidenceError(f"missing tshark field {field_name}")
    return _scalar(layer[field_name], field_name)


def _bool_field(layers: Mapping[str, Any], field_name: str) -> bool:
    value = _field(layers, "drcom", field_name)
    if isinstance(value, bool):
        return value
    if isinstance(value, int) and not isinstance(value, bool) and value in (0, 1):
        return bool(value)
    if isinstance(value, str) and value.lower() in {"0", "1", "false", "true"}:
        return value.lower() in {"1", "true"}
    raise EvidenceError(f"{field_name} must be boolean")


def _int_field(layers: Mapping[str, Any], layer: str, field: str) -> int:
    value = _field(layers, layer, field)
    try:
        parsed = int(value)
    except (TypeError, ValueError) as error:
        raise EvidenceError(f"{field} must be an integer") from error
    if parsed < 0:
        raise EvidenceError(f"{field} must be non-negative")
    return parsed


def _has_expert_error(value: Any, key: str = "") -> bool:
    if isinstance(value, Mapping):
        return any(_has_expert_error(item, str(name)) for name, item in value.items())
    if isinstance(value, list):
        return any(_has_expert_error(item, key) for item in value)
    if "expert" in key.lower() and isinstance(value, str):
        lowered = value.lower()
        return "error" in lowered or "malformed" in lowered
    return False


def _parse_tshark_packet(value: Any) -> TsharkPacket:
    root = _mapping(value, "tshark packet")
    source = _mapping(root.get("_source"), "_source")
    layers = _mapping(source.get("layers"), "layers")
    payload_text = str(_field(layers, "udp", "udp.payload")).replace(":", "")
    try:
        payload = bytes.fromhex(payload_text)
    except ValueError as error:
        raise EvidenceError("udp.payload must be hexadecimal") from error
    packet_kind = str(_field(layers, "drcom", "drcom.packet_kind"))
    operation = _PACKET_KIND_OPERATIONS.get(packet_kind, "unknown")
    return TsharkPacket(
        frame_number=_int_field(layers, "frame", "frame.number"),
        operation=operation,
        direction=str(_field(layers, "drcom", "drcom.direction")),
        payload_sha256=hashlib.sha256(payload).hexdigest(),
        payload_length=len(payload),
        source=Endpoint(
            str(_field(layers, "ip", "ip.src")),
            _int_field(layers, "udp", "udp.srcport"),
        ),
        destination=Endpoint(
            str(_field(layers, "ip", "ip.dst")),
            _int_field(layers, "udp", "udp.dstport"),
        ),
        valid=_bool_field(layers, "drcom.valid"),
        truncated=_bool_field(layers, "drcom.truncated"),
        malformed=_bool_field(layers, "drcom.malformed"),
        unknown=_bool_field(layers, "drcom.unknown"),
        expert_error=_has_expert_error(layers),
    )


def load_tshark_json(path: Path) -> tuple[TsharkPacket, ...]:
    payload = _read_json(path)
    if not isinstance(payload, list):
        raise EvidenceError("tshark JSON must be an array")
    packets = tuple(_parse_tshark_packet(item) for item in payload)
    if not packets:
        raise EvidenceError("tshark JSON contains no packets")
    return packets


def _failure(
    checks: list[ComparisonCheck],
    code: str,
    summary: str,
    *,
    packet_count: int,
    index: int | None = None,
    details: Iterable[tuple[str, str]] = (),
) -> ComparisonResult:
    checks.append(ComparisonCheck(code=code, passed=False, summary=summary))
    return ComparisonResult(
        passed=False,
        packet_count=packet_count,
        checks=tuple(checks),
        failure=ComparisonFailure(
            code=code,
            summary=summary,
            evidence_index=index,
            details=tuple(details),
        ),
    )


def _passed(checks: list[ComparisonCheck], code: str, summary: str) -> None:
    checks.append(ComparisonCheck(code=code, passed=True, summary=summary))


def _belongs_to_run(packet: TsharkPacket, transport: TransportObservation) -> bool:
    local_ip = transport.socket_local_endpoint.ip
    server = transport.server_endpoint
    return (
        packet.source.ip == local_ip
        and packet.destination == server
    ) or (
        packet.destination.ip == local_ip
        and packet.source == server
    )


def compare_evidence(
    transcript: Sequence[PacketRecord],
    transport: TransportObservation,
    snapshot: SessionSnapshotEvidence,
    tshark_packets: Sequence[TsharkPacket],
    *,
    require_keepalive: bool,
) -> ComparisonResult:
    checks: list[ComparisonCheck] = []
    if snapshot.authentication_session_id != transport.authentication_session_id:
        return _failure(
            checks, "session_identity_mismatch",
            "Session Snapshot 与 transport observation 的 Session ID 不一致。",
            packet_count=0,
        )
    _passed(checks, "session_identity", "Session ID 一致。")

    if snapshot.local_ipv4_address != transport.socket_local_endpoint.ip:
        return _failure(
            checks, "snapshot_local_ip_mismatch",
            "Session 所选 IPv4 与 socket LocalAddr IPv4 不一致。",
            packet_count=0,
            details=(("snapshot_ip", snapshot.local_ipv4_address),
                     ("socket_ip", transport.socket_local_endpoint.ip)),
        )
    _passed(checks, "snapshot_local_ip", "Session 所选 IPv4 与 socket LocalAddr 一致。")

    required = {"challenge", "login", "logout"}
    if require_keepalive:
        required.update({"ka1", "ka2"})
    observed_operations = {record.operation for record in transcript}
    missing = sorted(required - observed_operations)
    if missing:
        return _failure(
            checks, "operation_coverage_missing",
            "Application transcript 缺少必需协议阶段。",
            packet_count=0,
            details=(("missing", ",".join(missing)),),
        )
    if any(record.side != "client" for record in transcript):
        return _failure(
            checks, "transcript_side_invalid",
            "Application transcript 必须只包含 client 侧 packet records。",
            packet_count=0,
        )
    _passed(checks, "operation_coverage", "Application transcript 生命周期覆盖完整。")

    selected = tuple(packet for packet in tshark_packets if _belongs_to_run(packet, transport))
    if len(selected) != len(transcript):
        return _failure(
            checks, "packet_count_mismatch",
            "Application transcript 与目标 PCAP 流的数据报数量不一致。",
            packet_count=len(selected),
            details=(("transcript_count", str(len(transcript))),
                     ("pcap_count", str(len(selected)))),
        )
    _passed(checks, "packet_count", f"双方均包含 {len(selected)} 个目标数据报。")

    for index, (record, packet) in enumerate(zip(transcript, selected)):
        if (not packet.valid or packet.truncated or packet.malformed or
                packet.unknown or packet.expert_error):
            return _failure(
                checks, "pcap_dissector_status_invalid",
                "PCAP 中存在无效、截断、畸形、未知或 expert error 报文。",
                packet_count=len(selected), index=index,
                details=(("frame_number", str(packet.frame_number)),),
            )
        if packet.operation != record.operation:
            return _failure(
                checks, "packet_operation_mismatch", "协议阶段顺序不一致。",
                packet_count=len(selected), index=index,
                details=(("transcript_operation", record.operation),
                         ("pcap_operation", packet.operation)),
            )
        expected_direction = "client_to_server" if record.direction == "send" else "server_to_client"
        if packet.direction != expected_direction:
            return _failure(
                checks, "packet_direction_mismatch", "数据报方向不一致。",
                packet_count=len(selected), index=index,
                details=(("transcript_direction", record.direction),
                         ("pcap_direction", packet.direction)),
            )
        if packet.payload_sha256 != record.payload_sha256:
            return _failure(
                checks, "packet_hash_mismatch", "payload SHA-256 不一致。",
                packet_count=len(selected), index=index,
                details=(("frame_number", str(packet.frame_number)),),
            )
        if packet.payload_length != record.payload_length:
            return _failure(
                checks, "packet_length_mismatch", "payload 长度不一致。",
                packet_count=len(selected), index=index,
                details=(("transcript_length", str(record.payload_length)),
                         ("pcap_length", str(packet.payload_length))),
            )
        if (record.local_endpoint != transport.socket_local_endpoint or
                record.remote_endpoint != transport.server_endpoint):
            return _failure(
                checks, "transcript_endpoint_mismatch",
                "Application transcript endpoint 与 transport observation 不一致。",
                packet_count=len(selected), index=index,
            )
        pcap_local = packet.source if record.direction == "send" else packet.destination
        pcap_remote = packet.destination if record.direction == "send" else packet.source
        if pcap_local != transport.socket_local_endpoint:
            return _failure(
                checks, "pcap_local_endpoint_mismatch",
                "PCAP 中的 client endpoint 与 socket LocalAddr 不一致。",
                packet_count=len(selected), index=index,
                details=(("frame_number", str(packet.frame_number)),
                         ("pcap_local", f"{pcap_local.ip}:{pcap_local.port}"),
                         ("socket_local", f"{transport.socket_local_endpoint.ip}:{transport.socket_local_endpoint.port}")),
            )
        if pcap_remote != transport.server_endpoint:
            return _failure(
                checks, "pcap_remote_endpoint_mismatch",
                "PCAP 中的 server endpoint 与配置不一致。",
                packet_count=len(selected), index=index,
                details=(("frame_number", str(packet.frame_number)),),
            )

    _passed(checks, "packet_identity", "所有 packet 的阶段、方向、SHA-256 和长度一致。")
    _passed(checks, "endpoint_identity", "所有 packet 均使用同一 LocalAddr 和 ServerEndpoint。")
    return ComparisonResult(
        passed=True,
        packet_count=len(selected),
        checks=tuple(checks),
        failure=None,
    )
