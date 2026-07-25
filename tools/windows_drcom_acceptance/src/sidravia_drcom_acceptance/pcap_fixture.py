from __future__ import annotations

import struct
from dataclasses import dataclass
from ipaddress import IPv4Address
from typing import Literal, Sequence

from .model import Endpoint


@dataclass(frozen=True, slots=True)
class FixtureDatagram:
    source: Endpoint
    destination: Endpoint
    payload: bytes
    captured_length: int | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.payload, bytes):
            raise TypeError("payload must be bytes")


def _ipv4_checksum(header: bytes) -> int:
    if len(header) % 2:
        header += b"\0"
    total = sum(struct.unpack(f"!{len(header) // 2}H", header))
    while total >> 16:
        total = (total & 0xFFFF) + (total >> 16)
    return (~total) & 0xFFFF


def _ethernet_ipv4_udp(datagram: FixtureDatagram, identification: int) -> bytes:
    udp_length = 8 + len(datagram.payload)
    udp = struct.pack("!HHHH", datagram.source.port, datagram.destination.port,
                      udp_length, 0) + datagram.payload
    source_ip = IPv4Address(datagram.source.ip).packed
    destination_ip = IPv4Address(datagram.destination.ip).packed
    ip_header = struct.pack(
        "!BBHHHBBH4s4s",
        0x45,
        0,
        20 + udp_length,
        identification & 0xFFFF,
        0x4000,
        64,
        17,
        0,
        source_ip,
        destination_ip,
    )
    checksum = _ipv4_checksum(ip_header)
    ip_header = ip_header[:10] + struct.pack("!H", checksum) + ip_header[12:]
    ethernet = bytes.fromhex("0200000000020200000000010800")
    return ethernet + ip_header + udp


def build_ethernet_ipv4_udp_pcap(
    datagrams: Sequence[FixtureDatagram],
    *,
    byte_order: Literal["little", "big"] = "little",
    nanosecond: bool = False,
) -> bytes:
    """Build a deterministic classic-PCAP Ethernet/IPv4/UDP fixture."""
    if byte_order not in {"little", "big"}:
        raise ValueError("byte_order must be little or big")
    endian = "<" if byte_order == "little" else ">"
    if byte_order == "little":
        magic = bytes.fromhex("4d3cb2a1" if nanosecond else "d4c3b2a1")
    else:
        magic = bytes.fromhex("a1b23c4d" if nanosecond else "a1b2c3d4")
    output = bytearray(magic)
    output.extend(struct.pack(f"{endian}HHIIII", 2, 4, 0, 0, 65535, 1))
    for index, datagram in enumerate(datagrams):
        frame = _ethernet_ipv4_udp(datagram, identification=index + 1)
        captured = len(frame) if datagram.captured_length is None else datagram.captured_length
        if isinstance(captured, bool) or not isinstance(captured, int) or not 0 <= captured <= len(frame):
            raise ValueError("captured_length must be between zero and frame length")
        fraction = (index + 1) * (1_000 if nanosecond else 1)
        output.extend(struct.pack(f"{endian}IIII", 1_700_000_000 + index,
                                  fraction, captured, len(frame)))
        output.extend(frame[:captured])
    return bytes(output)
