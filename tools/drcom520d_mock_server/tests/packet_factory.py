import hashlib
import struct
from ipaddress import IPv4Address


def _checksum(data: bytes) -> bytes:
    value = 1234
    for offset in range(0, len(data), 4):
        value ^= struct.unpack("<I", data[offset:offset + 4].ljust(4, b"\0"))[0]
    return struct.pack("<I", (value * 1968) & 0xFFFFFFFF)


def refresh_login_crc(packet: bytearray) -> None:
    if len(packet) != 330:
        raise ValueError("Login packet must contain 330 bytes")
    packet[314:318] = _checksum(
        bytes(packet[:312]) + b"\x01\x26\x07\x11\x00\x00" + bytes(packet[320:326]))


def build_login_request(*, username: str = "student-test",
                        password: str = "local-test-password",
                        salt: bytes = bytes.fromhex("01020304"),
                        ipv4: str = "10.0.0.2",
                        dhcp: str = "10.0.0.1",
                        mac: bytes = bytes.fromhex("020000000001"),
                        auth_version: bytes = bytes.fromhex("2c00")) -> bytes:
    username_bytes = username.encode("gbk", "strict")
    password_bytes = password.encode("gbk", "strict")
    md5a = hashlib.md5(b"\x03\x01" + salt + password_bytes).digest()
    packet = bytearray(b"\x03\x01\x00" + bytes((20 + len(username_bytes),)))
    packet.extend(md5a)
    packet.extend(username_bytes.ljust(36, b"\0"))
    packet.extend(b"\x20\x01")
    packet.extend(bytes(left ^ right for left, right in zip(mac, md5a[:6])))
    packet.extend(hashlib.md5(b"\x01" + password_bytes + salt + b"\0" * 4).digest())
    ip_section = b"\x01" + IPv4Address(ipv4).packed + b"\0" * 12
    packet.extend(ip_section)
    packet.extend(hashlib.md5(ip_section + b"\x14\x00\x07\x0b").digest()[:8])
    packet.extend(b"\x01" + b"\0" * 4)
    packet.extend(b"local-test-host".ljust(32, b"\0"))
    packet.extend(IPv4Address("10.10.10.10").packed)
    packet.extend(IPv4Address(dhcp).packed)
    packet.extend(IPv4Address("202.98.18.3").packed)
    packet.extend(b"\0" * 8)
    packet.extend(bytes.fromhex("940000000600000000000000280a000002000000"))
    packet.extend(b"Windows 10".ljust(32, b"\0"))
    packet.extend(b"\0" * 96)
    packet.extend(auth_version)
    crc = _checksum(bytes(packet) + b"\x01\x26\x07\x11\x00\x00" + mac)
    packet.extend(b"\x02\x0c" + crc + b"\0\0" + mac + b"\0\0\x12\x34")
    assert len(packet) == 330
    return bytes(packet)


def build_ka1_request(salt: bytes, password: str, auth_info: bytes,
                      timestamp: int = 1234) -> bytes:
    digest = hashlib.md5(b"\x03\x01" + salt + password.encode("gbk")).digest()
    return b"\xff" + digest + b"\0" * 3 + auth_info + struct.pack("!H", timestamp) + b"\0" * 4


def build_ka2_request(serial: int, packet_type: int, tail: bytes, ipv4: str,
                      version: bytes) -> bytes:
    packet = bytearray(b"\x07" + bytes((serial,)) + b"\x28\x00\x0b" + bytes((packet_type,)))
    packet.extend(version + b"\x2f\x12" + b"\0" * 6 + tail + b"\0" * 4)
    if packet_type == 3:
        packet.extend(b"\0" * 4 + IPv4Address(ipv4).packed + b"\0" * 8)
    else:
        packet.extend(b"\0" * 16)
    assert len(packet) == 40
    return bytes(packet)


def build_logout_request(username: str, password: str, salt: bytes,
                         mac: bytes, auth_info: bytes) -> bytes:
    username_bytes = username.encode("gbk", "strict")
    digest = hashlib.md5(b"\x03\x01" + salt + password.encode("gbk")).digest()
    packet = bytearray(b"\x06\x01\x00" + bytes((20 + len(username_bytes),)))
    packet.extend(digest)
    packet.extend(username_bytes.ljust(36, b"\0"))
    packet.extend(b"\x20\x01")
    packet.extend(bytes(left ^ right for left, right in zip(mac, digest[:6])))
    packet.extend(auth_info)
    assert len(packet) == 80
    return bytes(packet)
