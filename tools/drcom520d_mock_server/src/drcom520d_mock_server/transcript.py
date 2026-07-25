from __future__ import annotations

import hashlib
from dataclasses import dataclass
from typing import Any


TRANSCRIPT_VERSION = 1


class TranscriptFormatError(ValueError):
    pass


@dataclass(frozen=True, slots=True)
class TranscriptMismatch:
    record_type: str
    index: int
    field_path: str
    reason: str
    client: dict[str, Any] | None
    server: dict[str, Any] | None


def packet_record(
    *,
    side: str,
    direction: str,
    operation: str | None,
    payload: bytes,
    local_endpoint: tuple[str, int] | None,
    remote_endpoint: tuple[str, int] | None,
) -> dict[str, Any]:
    if side not in {"client", "server"}:
        raise ValueError("side must be client or server")
    if direction not in {"send", "receive"}:
        raise ValueError("direction must be send or receive")
    return {
        "transcript_version": TRANSCRIPT_VERSION,
        "record_type": "packet",
        "side": side,
        "direction": direction,
        "operation": operation,
        "payload_hex": payload.hex(),
        "payload_sha256": hashlib.sha256(payload).hexdigest(),
        "payload_length": len(payload),
        "local_endpoint": local_endpoint,
        "remote_endpoint": remote_endpoint,
    }


def decision_record(
    *,
    operation: str,
    outcome: str,
    wire_error_code: int | None,
    drop_reason: str | None,
    side: str = "server",
) -> dict[str, Any]:
    if side not in {"client", "server"}:
        raise ValueError("side must be client or server")
    return {
        "transcript_version": TRANSCRIPT_VERSION,
        "record_type": "decision",
        "side": side,
        "operation": operation,
        "outcome": outcome,
        "wire_error_code": wire_error_code,
        "drop_reason": drop_reason,
    }


def extract_transcript_records(
    trace_rows: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    records = []
    for index, row in enumerate(trace_rows):
        details = row.get("details")
        if not isinstance(details, dict):
            continue
        if "transcript_version" not in details and "record_type" not in details:
            continue
        _validate_transcript_record(
            details,
            expected_side="server",
            label=f"server_trace[{index}].details",
        )
        records.append(details)
    return records


def _format_error(label: str, field_path: str, message: str) -> None:
    raise TranscriptFormatError(f"{label}.{field_path}: {message}")


def _require_fields(
    row: dict[str, Any],
    fields: tuple[str, ...],
    *,
    label: str,
) -> None:
    for field in fields:
        if field not in row:
            _format_error(label, field, "缺少必填字段")


def _validate_endpoint_schema(value: Any, *, label: str) -> None:
    if value is None:
        return
    if isinstance(value, dict):
        if set(value) != {"host", "port"}:
            _format_error(label, "endpoint", "object 必须且只能包含 host 和 port")
        host, port = value["host"], value["port"]
    elif isinstance(value, (list, tuple)):
        if len(value) != 2:
            _format_error(label, "endpoint", "array 必须包含 host 和 port 两项")
        host, port = value
    else:
        _format_error(label, "endpoint", "必须为 null、{host, port} 或 [host, port]")
    if not isinstance(host, str) or not host:
        _format_error(label, "host", "必须为非空字符串")
    if isinstance(port, bool) or not isinstance(port, int) or not 0 <= port <= 65535:
        _format_error(label, "port", "必须为 0 到 65535 的整数")


def _validate_common_record(
    row: dict[str, Any],
    *,
    expected_side: str | None,
    label: str,
) -> str:
    _require_fields(row, ("transcript_version", "record_type"), label=label)
    version = row["transcript_version"]
    if isinstance(version, bool) or not isinstance(version, int):
        _format_error(label, "transcript_version", "必须为整数 1")
    if version != TRANSCRIPT_VERSION:
        _format_error(
            label, "transcript_version", f"必须为 {TRANSCRIPT_VERSION}")
    record_type = row["record_type"]
    if record_type not in ("packet", "decision"):
        _format_error(label, "record_type", "必须为 packet 或 decision")
    _require_fields(row, ("side",), label=label)
    side = row["side"]
    if side not in ("client", "server"):
        _format_error(label, "side", "必须为 client 或 server")
    if expected_side is not None and side != expected_side:
        _format_error(label, "side", f"必须为 {expected_side}")
    return record_type


def _validate_packet_schema(row: dict[str, Any], *, label: str) -> None:
    _require_fields(
        row,
        (
            "direction", "operation", "payload_hex", "payload_sha256",
            "payload_length", "local_endpoint", "remote_endpoint",
        ),
        label=label,
    )
    if row["direction"] not in ("send", "receive"):
        _format_error(label, "direction", "必须为 send 或 receive")
    operation = row["operation"]
    if operation is not None and not isinstance(operation, str):
        _format_error(label, "operation", "必须为字符串或 null")

    payload_hex = row["payload_hex"]
    if not isinstance(payload_hex, str):
        _format_error(label, "payload_hex", "必须为无空格的小写十六进制字符串")
    try:
        payload = bytes.fromhex(payload_hex)
    except ValueError as error:
        raise TranscriptFormatError(
            f"{label}.payload_hex: 不是有效十六进制") from error
    if payload.hex() != payload_hex:
        _format_error(label, "payload_hex", "必须为无空格的小写十六进制")

    payload_length = row["payload_length"]
    if (isinstance(payload_length, bool)
            or not isinstance(payload_length, int)
            or payload_length < 0):
        _format_error(label, "payload_length", "必须为非负整数")
    if payload_length != len(payload):
        _format_error(label, "payload_length", "与 payload_hex 字节数不一致")

    payload_sha256 = row["payload_sha256"]
    if not isinstance(payload_sha256, str):
        _format_error(label, "payload_sha256", "必须为 SHA-256 小写十六进制")
    expected_hash = hashlib.sha256(payload).hexdigest()
    if payload_sha256 != expected_hash:
        _format_error(label, "payload_sha256", "与 payload_hex 内容不一致")

    _validate_endpoint_schema(
        row["local_endpoint"], label=f"{label}.local_endpoint")
    _validate_endpoint_schema(
        row["remote_endpoint"], label=f"{label}.remote_endpoint")


def _validate_decision_schema(row: dict[str, Any], *, label: str) -> None:
    _require_fields(
        row,
        ("operation", "outcome", "wire_error_code", "drop_reason"),
        label=label,
    )
    if not isinstance(row["operation"], str) or not row["operation"]:
        _format_error(label, "operation", "必须为非空字符串")
    if not isinstance(row["outcome"], str) or not row["outcome"]:
        _format_error(label, "outcome", "必须为非空字符串")
    wire_error_code = row["wire_error_code"]
    if (wire_error_code is not None
            and (isinstance(wire_error_code, bool)
                 or not isinstance(wire_error_code, int))):
        _format_error(label, "wire_error_code", "必须为整数或 null")
    drop_reason = row["drop_reason"]
    if drop_reason is not None and not isinstance(drop_reason, str):
        _format_error(label, "drop_reason", "必须为字符串或 null")


def _validate_transcript_record(
    row: Any,
    *,
    expected_side: str | None,
    label: str,
) -> None:
    if not isinstance(row, dict):
        raise TranscriptFormatError(f"{label}: 必须为 JSON object")
    record_type = _validate_common_record(
        row, expected_side=expected_side, label=label)
    if record_type == "packet":
        _validate_packet_schema(row, label=label)
    else:
        _validate_decision_schema(row, label=label)


def validate_transcript_records(
    rows: list[dict[str, Any]],
    *,
    expected_side: str | None = None,
    context: str = "transcript",
) -> None:
    """Validate transcript schema and packet self-integrity before alignment."""
    if not isinstance(rows, list):
        raise TranscriptFormatError(f"{context}: 必须为 record list")
    for index, row in enumerate(rows):
        _validate_transcript_record(
            row, expected_side=expected_side, label=f"{context}[{index}]")


def _mismatch(
    record_type: str,
    index: int,
    field_path: str,
    reason: str,
    client: dict[str, Any] | None,
    server: dict[str, Any] | None,
) -> list[TranscriptMismatch]:
    return [TranscriptMismatch(
        record_type, index, field_path, reason, client, server)]


def _field_mismatch(
    record_type: str,
    index: int,
    field_path: str,
    client: dict[str, Any],
    server: dict[str, Any],
    client_value: Any,
    server_value: Any,
) -> list[TranscriptMismatch]:
    return _mismatch(
        record_type,
        index,
        field_path,
        f"{record_type}[{index}].{field_path} 不匹配："
        f"client={client_value!r}, server={server_value!r}",
        client,
        server,
    )


def _normalize_endpoint(value: Any) -> tuple[str, int] | None:
    if value is None:
        return None
    if isinstance(value, dict):
        if "host" not in value or "port" not in value:
            raise ValueError("endpoint object 必须包含 host 和 port")
        host, port = value["host"], value["port"]
    elif isinstance(value, (list, tuple)) and len(value) == 2:
        host, port = value
    else:
        raise ValueError("endpoint 必须为 {host, port} object 或 [host, port] array")
    if not isinstance(host, str) or not host:
        raise ValueError("endpoint host 必须为非空字符串")
    if isinstance(port, bool) or not isinstance(port, int) or not 0 <= port <= 65535:
        raise ValueError("endpoint port 必须为 0 到 65535 的整数")
    return host, port


def _validate_record(
    record_type: str,
    index: int,
    label: str,
    row: dict[str, Any],
    expected_side: str,
    other: dict[str, Any],
) -> list[TranscriptMismatch] | None:
    for field_path, expected in (
        ("transcript_version", TRANSCRIPT_VERSION),
        ("record_type", record_type),
        ("side", expected_side),
    ):
        if row.get(field_path) != expected:
            client = row if label == "client" else other
            server = other if label == "client" else row
            return _field_mismatch(
                record_type, index, field_path, client, server,
                client.get(field_path), server.get(field_path))
    return None


def _compare_packet(
    index: int,
    client: dict[str, Any],
    server: dict[str, Any],
) -> list[TranscriptMismatch]:
    for label, row, side, other in (
        ("client", client, "client", server),
        ("server", server, "server", client),
    ):
        invalid = _validate_record("packet", index, label, row, side, other)
        if invalid is not None:
            return invalid

    expected_server_direction = {"send": "receive", "receive": "send"}
    client_direction = client.get("direction")
    required_direction = expected_server_direction.get(client_direction)
    if required_direction is None:
        return _field_mismatch(
            "packet", index, "direction", client, server,
            client_direction, server.get("direction"))
    if server.get("direction") != required_direction:
        return _field_mismatch(
            "packet", index, "direction", client, server,
            client_direction, server.get("direction"))

    for field_path in (
        "operation", "payload_hex", "payload_sha256", "payload_length",
    ):
        if client.get(field_path) != server.get(field_path):
            return _field_mismatch(
                "packet", index, field_path, client, server,
                client.get(field_path), server.get(field_path))

    endpoint_inputs = (
        ("endpoint.server", client.get("remote_endpoint"),
         server.get("local_endpoint")),
        ("endpoint.client", client.get("local_endpoint"),
         server.get("remote_endpoint")),
    )
    normalized: dict[str, tuple[tuple[str, int] | None,
                                tuple[str, int] | None]] = {}
    for field_path, client_value, server_value in endpoint_inputs:
        try:
            normalized[field_path] = (
                _normalize_endpoint(client_value),
                _normalize_endpoint(server_value),
            )
        except ValueError as error:
            return _mismatch(
                "packet", index, field_path, str(error), client, server)

    client_server_endpoint, server_server_endpoint = normalized["endpoint.server"]
    if client_server_endpoint is None or server_server_endpoint is None:
        if client_server_endpoint != server_server_endpoint:
            return _field_mismatch(
                "packet", index, "endpoint.server", client, server,
                client_server_endpoint, server_server_endpoint)
    else:
        if client_server_endpoint[0] != server_server_endpoint[0]:
            return _field_mismatch(
                "packet", index, "endpoint.server.host", client, server,
                client_server_endpoint[0], server_server_endpoint[0])
        if client_server_endpoint[1] != server_server_endpoint[1]:
            return _field_mismatch(
                "packet", index, "endpoint.server.port", client, server,
                client_server_endpoint[1], server_server_endpoint[1])

    client_client_endpoint, server_client_endpoint = normalized["endpoint.client"]
    if client_client_endpoint is None or server_client_endpoint is None:
        if client_client_endpoint != server_client_endpoint:
            return _field_mismatch(
                "packet", index, "endpoint.client", client, server,
                client_client_endpoint, server_client_endpoint)
    elif client_client_endpoint[0] != server_client_endpoint[0]:
        return _field_mismatch(
            "packet", index, "endpoint.client.host", client, server,
            client_client_endpoint[0], server_client_endpoint[0])
    return []


def _is_drop_timeout_mapping(
    client: dict[str, Any],
    server: dict[str, Any],
) -> bool:
    server_reason = server.get("drop_reason")
    return (
        client.get("outcome") == "timeout"
        and client.get("wire_error_code") is None
        and client.get("drop_reason") == "timeout"
        and server.get("outcome") == "drop"
        and server.get("wire_error_code") is None
        and isinstance(server_reason, str)
        and bool(server_reason)
    )


def _compare_decision(
    index: int,
    client: dict[str, Any],
    server: dict[str, Any],
) -> list[TranscriptMismatch]:
    for label, row, side, other in (
        ("client", client, "client", server),
        ("server", server, "server", client),
    ):
        invalid = _validate_record("decision", index, label, row, side, other)
        if invalid is not None:
            return invalid
    if _is_drop_timeout_mapping(client, server):
        return []
    for field_path in ("outcome", "wire_error_code", "drop_reason"):
        if client.get(field_path) != server.get(field_path):
            return _field_mismatch(
                "decision", index, field_path, client, server,
                client.get(field_path), server.get(field_path))
    return []


def _align_stream(
    record_type: str,
    client_rows: list[dict[str, Any]],
    server_rows: list[dict[str, Any]],
) -> list[TranscriptMismatch]:
    compare = _compare_packet if record_type == "packet" else _compare_decision
    shared_length = min(len(client_rows), len(server_rows))
    for index in range(shared_length):
        mismatch = compare(index, client_rows[index], server_rows[index])
        if mismatch:
            return mismatch
    if len(client_rows) != len(server_rows):
        index = shared_length
        client = client_rows[index] if index < len(client_rows) else None
        server = server_rows[index] if index < len(server_rows) else None
        missing = "server" if server is None else "client"
        return _mismatch(
            record_type, index, "record",
            f"{record_type}[{index}] 缺少 {missing} record",
            client, server)
    return []


def align_transcripts(
    client_rows: list[dict[str, Any]],
    server_rows: list[dict[str, Any]],
) -> list[TranscriptMismatch]:
    """Return the first stable, wire-relevant difference between two transcripts."""
    validate_transcript_records(
        client_rows, expected_side="client", context="client")
    validate_transcript_records(
        server_rows, expected_side="server", context="server")
    for record_type in ("packet", "decision"):
        client_stream = [
            row for row in client_rows if row.get("record_type") == record_type]
        server_stream = [
            row for row in server_rows if row.get("record_type") == record_type]
        mismatch = _align_stream(record_type, client_stream, server_stream)
        if mismatch:
            return mismatch
    return []
