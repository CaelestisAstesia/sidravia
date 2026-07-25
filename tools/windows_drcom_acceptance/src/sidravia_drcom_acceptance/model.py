from __future__ import annotations

import hashlib
from dataclasses import dataclass
from ipaddress import IPv4Address
from typing import Any, Mapping, Sequence


class EvidenceError(ValueError):
    """Raised when acceptance evidence violates its versioned schema."""


_OPERATIONS = frozenset({"challenge", "login", "ka1", "ka2", "logout"})
_DIRECTIONS = frozenset({"send", "receive"})
_SIDES = frozenset({"client", "server"})


def _mapping(value: Any, name: str) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise EvidenceError(f"{name} must be an object")
    return value


def _required(raw: Mapping[str, Any], field: str) -> Any:
    if field not in raw:
        raise EvidenceError(f"missing {field}")
    return raw[field]


def _text(value: Any, field: str, *, allow_empty: bool = False) -> str:
    if not isinstance(value, str) or (not allow_empty and not value):
        raise EvidenceError(f"{field} must be a non-empty string")
    return value


def _integer(value: Any, field: str, minimum: int = 0) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise EvidenceError(f"{field} must be an integer >= {minimum}")
    return value


@dataclass(frozen=True, slots=True)
class Endpoint:
    ip: str
    port: int

    def __post_init__(self) -> None:
        try:
            address = IPv4Address(self.ip)
        except ValueError as error:
            raise EvidenceError("endpoint ip must be an IPv4 address") from error
        if str(address) != self.ip:
            object.__setattr__(self, "ip", str(address))
        if isinstance(self.port, bool) or not isinstance(self.port, int) or not 1 <= self.port <= 65535:
            raise EvidenceError("endpoint port must be 1..65535")

    @classmethod
    def from_json(cls, value: Any, field: str = "endpoint") -> "Endpoint":
        if isinstance(value, Mapping):
            return cls(
                _text(_required(value, "ip"), f"{field}.ip"),
                _integer(_required(value, "port"), f"{field}.port", 1),
            )
        if isinstance(value, Sequence) and not isinstance(value, (str, bytes)) and len(value) == 2:
            return cls(_text(value[0], f"{field}[0]"), _integer(value[1], f"{field}[1]", 1))
        raise EvidenceError(f"{field} must be an endpoint object or two-item array")


@dataclass(frozen=True, slots=True)
class PacketRecord:
    transcript_version: int
    side: str
    direction: str
    operation: str
    payload: bytes
    payload_sha256: str
    local_endpoint: Endpoint
    remote_endpoint: Endpoint

    @classmethod
    def from_json(cls, value: Any) -> "PacketRecord":
        raw = _mapping(value, "packet record")
        version = _integer(_required(raw, "transcript_version"), "transcript_version", 1)
        if version != 1:
            raise EvidenceError("transcript_version must be 1")
        if _required(raw, "record_type") != "packet":
            raise EvidenceError("record_type must be packet")
        side = _text(_required(raw, "side"), "side")
        if side not in _SIDES:
            raise EvidenceError("side must be client or server")
        direction = _text(_required(raw, "direction"), "direction")
        if direction not in _DIRECTIONS:
            raise EvidenceError("direction must be send or receive")
        operation = _text(_required(raw, "operation"), "operation")
        if operation not in _OPERATIONS:
            raise EvidenceError("operation is unsupported")
        payload_hex = _text(_required(raw, "payload_hex"), "payload_hex", allow_empty=True)
        try:
            payload = bytes.fromhex(payload_hex)
        except ValueError as error:
            raise EvidenceError("payload_hex must be valid hexadecimal") from error
        if payload.hex() != payload_hex.lower():
            raise EvidenceError("payload_hex must be canonical hexadecimal")
        payload_length = _integer(_required(raw, "payload_length"), "payload_length")
        if payload_length != len(payload):
            raise EvidenceError("payload_length does not match payload_hex")
        payload_sha256 = _text(_required(raw, "payload_sha256"), "payload_sha256")
        expected_hash = hashlib.sha256(payload).hexdigest()
        if payload_sha256 != expected_hash:
            raise EvidenceError("payload_sha256 does not match payload_hex")
        return cls(
            transcript_version=version,
            side=side,
            direction=direction,
            operation=operation,
            payload=payload,
            payload_sha256=payload_sha256,
            local_endpoint=Endpoint.from_json(_required(raw, "local_endpoint"), "local_endpoint"),
            remote_endpoint=Endpoint.from_json(_required(raw, "remote_endpoint"), "remote_endpoint"),
        )

    @property
    def payload_length(self) -> int:
        return len(self.payload)


@dataclass(frozen=True, slots=True)
class TransportObservation:
    schema_version: int
    run_id: str
    authentication_session_id: str
    socket_local_endpoint: Endpoint
    server_endpoint: Endpoint

    @classmethod
    def from_json(cls, value: Any) -> "TransportObservation":
        raw = _mapping(value, "transport observation")
        version = _integer(_required(raw, "schema_version"), "schema_version", 1)
        if version != 1:
            raise EvidenceError("schema_version must be 1")
        return cls(
            schema_version=version,
            run_id=_text(_required(raw, "run_id"), "run_id"),
            authentication_session_id=_text(
                _required(raw, "authentication_session_id"),
                "authentication_session_id",
            ),
            socket_local_endpoint=Endpoint.from_json(
                _required(raw, "socket_local_endpoint"), "socket_local_endpoint"
            ),
            server_endpoint=Endpoint.from_json(_required(raw, "server_endpoint"), "server_endpoint"),
        )


@dataclass(frozen=True, slots=True)
class SessionSnapshotEvidence:
    authentication_session_id: str
    state: str
    interface_id: str
    display_name: str
    local_ipv4_address: str

    @classmethod
    def from_json(cls, value: Any) -> "SessionSnapshotEvidence":
        raw = _mapping(value, "session snapshot")
        binding = _mapping(_required(raw, "SelectedNetworkBinding"), "SelectedNetworkBinding")
        address_text = _text(
            _required(binding, "LocalIPv4Address"),
            "SelectedNetworkBinding.LocalIPv4Address",
        )
        try:
            address = IPv4Address(address_text)
        except ValueError as error:
            raise EvidenceError("SelectedNetworkBinding.LocalIPv4Address must be IPv4") from error
        return cls(
            authentication_session_id=_text(
                _required(raw, "AuthenticationSessionID"), "AuthenticationSessionID"
            ),
            state=_text(_required(raw, "State"), "State"),
            interface_id=_text(_required(binding, "InterfaceID"), "SelectedNetworkBinding.InterfaceID"),
            display_name=_text(
                _required(binding, "DisplayName"),
                "SelectedNetworkBinding.DisplayName",
                allow_empty=True,
            ),
            local_ipv4_address=str(address),
        )


@dataclass(frozen=True, slots=True)
class TsharkPacket:
    frame_number: int
    operation: str
    direction: str
    payload_sha256: str
    payload_length: int
    source: Endpoint
    destination: Endpoint
    valid: bool
    truncated: bool
    malformed: bool
    unknown: bool
    expert_error: bool = False


@dataclass(frozen=True, slots=True)
class ComparisonCheck:
    code: str
    passed: bool
    summary: str


@dataclass(frozen=True, slots=True)
class ComparisonFailure:
    code: str
    summary: str
    evidence_index: int | None = None
    details: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True, slots=True)
class ComparisonResult:
    passed: bool
    packet_count: int
    checks: tuple[ComparisonCheck, ...]
    failure: ComparisonFailure | None

    def __post_init__(self) -> None:
        if self.passed and self.failure is not None:
            raise ValueError("passed comparison cannot contain failure")
        if not self.passed and self.failure is None:
            raise ValueError("failed comparison requires failure")
