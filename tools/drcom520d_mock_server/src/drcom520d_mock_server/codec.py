import hashlib
import hmac
import struct
from dataclasses import dataclass
from ipaddress import IPv4Address


class PacketFormatError(ValueError):
    pass


LOGIN_PACKET_LENGTH = 330
USERNAME = slice(20, 56)
CONTROL_STATUS_OFFSET = 56
ADAPTER_NUM_OFFSET = 57
MAC_XOR = slice(58, 64)
MD5_B = slice(64, 80)
IP_SECTION = slice(80, 97)
MD5_C = slice(97, 105)
IPDOG_OFFSET = 105
DHCP = slice(146, 150)
AUTH_VERSION = slice(310, 312)
AUTH_EXT_MARKER = slice(312, 314)
AUTH_EXT_CRC = slice(314, 318)
AUTH_EXT_RESERVED_BEFORE_MAC = slice(318, 320)
AUTH_EXT_MAC = slice(320, 326)
AUTH_EXT_RESERVED_AFTER_MAC = slice(326, 328)
AUTH_EXT_TAIL = slice(328, 330)


@dataclass(frozen=True)
class LoginRequest:
    md5_a: bytes
    username: str
    control_status: int
    adapter_num: int
    mac_xor: bytes
    md5_b: bytes
    ip_section: bytes
    md5_c: bytes
    ipdog: int
    reported_ipv4: IPv4Address
    dhcp: IPv4Address
    auth_version: bytes
    auth_ext_marker: bytes
    auth_ext_crc: bytes
    auth_ext_reserved_before_mac: bytes
    auth_ext_mac: bytes
    auth_ext_reserved_after_mac: bytes
    auth_ext_tail: bytes


@dataclass(frozen=True)
class Ka1Request:
    md5_a: bytes
    auth_info: bytes
    timestamp: int


@dataclass(frozen=True)
class Ka2Request:
    serial: int
    packet_type: int
    version: bytes
    tail: bytes
    reported_ipv4: IPv4Address | None


@dataclass(frozen=True)
class LogoutRequest:
    md5_a: bytes
    username: str
    control_status: int
    adapter_num: int
    mac_xor: bytes
    auth_info: bytes

    @property
    def recovered_mac(self) -> bytes:
        return bytes(left ^ right for left, right in zip(self.mac_xor, self.md5_a[:6]))


@dataclass(frozen=True, slots=True)
class CryptoComparison:
    input: bytes
    received: bytes
    expected: bytes
    valid: bool


@dataclass(frozen=True, slots=True)
class DigestVerification:
    input: bytes
    received: bytes
    expected: bytes
    valid: bool


@dataclass(frozen=True, slots=True)
class DerivedValue:
    input: bytes
    output: bytes


@dataclass(frozen=True, slots=True)
class LoginCryptoVerification:
    recovered_mac: bytes
    md5_a: CryptoComparison
    md5_b: CryptoComparison
    md5_c: CryptoComparison
    repeated_mac: CryptoComparison
    crc: CryptoComparison

    @property
    def md5_a_valid(self) -> bool:
        return self.md5_a.valid

    @property
    def md5_b_valid(self) -> bool:
        return self.md5_b.valid

    @property
    def md5_c_valid(self) -> bool:
        return self.md5_c.valid

    @property
    def repeated_mac_valid(self) -> bool:
        return self.repeated_mac.valid

    @property
    def crc_valid(self) -> bool:
        return self.crc.valid


def checksum_d_series(data: bytes) -> bytes:
    value = 1234
    for offset in range(0, len(data), 4):
        chunk = data[offset:offset + 4].ljust(4, b"\0")
        value ^= struct.unpack("<I", chunk)[0]
    return struct.pack("<I", (value * 1968) & 0xFFFFFFFF)


def _password_bytes(password: str) -> bytes:
    return password.encode("gbk", "strict")


def _md5_a_input(salt: bytes, password: str) -> bytes:
    return b"\x03\x01" + salt + _password_bytes(password)


def _md5_b_input(salt: bytes, password: str) -> bytes:
    return b"\x01" + _password_bytes(password) + salt + b"\0" * 4


def _md5_c_input(ip_section: bytes) -> bytes:
    if len(ip_section) != 17:
        raise ValueError("IP section must contain 17 bytes")
    return ip_section + b"\x14\x00\x07\x0b"


def md5_a(salt: bytes, password: str) -> bytes:
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    return hashlib.md5(_md5_a_input(salt, password)).digest()


def md5_b(salt: bytes, password: str) -> bytes:
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    return hashlib.md5(_md5_b_input(salt, password)).digest()


def md5_c(ip_section: bytes) -> bytes:
    return hashlib.md5(_md5_c_input(ip_section)).digest()[:8]


def _decode_username(field: bytes) -> str:
    username, separator, padding = field.partition(b"\0")
    if separator and padding.strip(b"\0"):
        raise PacketFormatError("username field contains non-padding bytes")
    try:
        return username.decode("gbk", "strict")
    except UnicodeDecodeError as error:
        raise PacketFormatError("username is not strict GBK") from error


def _require_packet_length(packet: bytes, length: int, name: str) -> None:
    if len(packet) != length:
        raise PacketFormatError(f"{name} packet must contain {length} bytes")


def parse_login_request(packet: bytes) -> LoginRequest:
    _require_packet_length(packet, LOGIN_PACKET_LENGTH, "Login")
    if packet[:3] != b"\x03\x01\0":
        raise PacketFormatError("Login packet does not use the JLU request header")
    username_field = packet[USERNAME]
    username = _decode_username(username_field)
    username_bytes = username.encode("gbk", "strict")
    if packet[3] != 20 + len(username_bytes):
        raise PacketFormatError("Login packet username length is inconsistent")
    if packet[AUTH_EXT_MARKER] != b"\x02\x0c":
        raise PacketFormatError("Login packet auth extension marker is invalid")
    if (packet[AUTH_EXT_RESERVED_BEFORE_MAC] != b"\0\0" or
            packet[AUTH_EXT_RESERVED_AFTER_MAC] != b"\0\0"):
        raise PacketFormatError("Login packet auth extension reserved bytes are invalid")
    return LoginRequest(
        md5_a=packet[4:20],
        username=username,
        control_status=packet[CONTROL_STATUS_OFFSET],
        adapter_num=packet[ADAPTER_NUM_OFFSET],
        mac_xor=packet[MAC_XOR],
        md5_b=packet[MD5_B],
        ip_section=packet[IP_SECTION],
        md5_c=packet[MD5_C],
        ipdog=packet[IPDOG_OFFSET],
        reported_ipv4=IPv4Address(packet[81:85]),
        dhcp=IPv4Address(packet[DHCP]),
        auth_version=packet[AUTH_VERSION],
        auth_ext_marker=packet[AUTH_EXT_MARKER],
        auth_ext_crc=packet[AUTH_EXT_CRC],
        auth_ext_reserved_before_mac=packet[AUTH_EXT_RESERVED_BEFORE_MAC],
        auth_ext_mac=packet[AUTH_EXT_MAC],
        auth_ext_reserved_after_mac=packet[AUTH_EXT_RESERVED_AFTER_MAC],
        auth_ext_tail=packet[AUTH_EXT_TAIL],
    )


def verify_login_crypto(packet: bytes, request: LoginRequest, password: str,
                        salt: bytes) -> LoginCryptoVerification:
    _require_packet_length(packet, LOGIN_PACKET_LENGTH, "Login")
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    md5_a_input = _md5_a_input(salt, password)
    md5_b_input = _md5_b_input(salt, password)
    md5_c_input = _md5_c_input(request.ip_section)
    expected_md5_a = hashlib.md5(md5_a_input).digest()
    expected_md5_b = hashlib.md5(md5_b_input).digest()
    expected_md5_c = hashlib.md5(md5_c_input).digest()[:8]
    recovered_mac = bytes(left ^ right for left, right in zip(
        request.mac_xor, request.md5_a[:6]))
    repeated_mac_input = request.mac_xor + request.md5_a[:6]
    crc_input = packet[:312] + b"\x01\x26\x07\x11\x00\x00" + request.auth_ext_mac
    expected_crc = checksum_d_series(crc_input)
    return LoginCryptoVerification(
        recovered_mac=recovered_mac,
        md5_a=CryptoComparison(
            input=md5_a_input, received=request.md5_a, expected=expected_md5_a,
            valid=hmac.compare_digest(request.md5_a, expected_md5_a)),
        md5_b=CryptoComparison(
            input=md5_b_input, received=request.md5_b, expected=expected_md5_b,
            valid=hmac.compare_digest(request.md5_b, expected_md5_b)),
        md5_c=CryptoComparison(
            input=md5_c_input, received=request.md5_c, expected=expected_md5_c,
            valid=hmac.compare_digest(request.md5_c, expected_md5_c)),
        repeated_mac=CryptoComparison(
            input=repeated_mac_input, received=request.auth_ext_mac,
            expected=recovered_mac,
            valid=hmac.compare_digest(request.auth_ext_mac, recovered_mac)),
        crc=CryptoComparison(
            input=crc_input, received=request.auth_ext_crc, expected=expected_crc,
            valid=hmac.compare_digest(request.auth_ext_crc, expected_crc)),
    )


def parse_ka1_request(packet: bytes) -> Ka1Request:
    if len(packet) not in (38, 42):
        raise PacketFormatError("KA1 packet must contain 38 or 42 bytes")
    if packet[0] != 0xff or packet[17:20] != b"\0" * 3:
        raise PacketFormatError("KA1 packet fixed fields are invalid")
    if len(packet) == 42 and packet[38:42] != b"\0" * 4:
        raise PacketFormatError("KA1 packet trailing padding is invalid")
    return Ka1Request(
        md5_a=packet[1:17],
        auth_info=packet[20:36],
        timestamp=struct.unpack("!H", packet[36:38])[0],
    )


def verify_ka1_detailed(request: Ka1Request, password: str,
                        salt: bytes) -> DigestVerification:
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    material = _md5_a_input(salt, password)
    expected = hashlib.md5(material).digest()
    return DigestVerification(
        input=material,
        received=request.md5_a,
        expected=expected,
        valid=hmac.compare_digest(request.md5_a, expected),
    )


def verify_ka1(request: Ka1Request, password: str, salt: bytes) -> bool:
    return verify_ka1_detailed(request, password, salt).valid


def parse_ka2_request(packet: bytes) -> Ka2Request:
    _require_packet_length(packet, 40, "KA2")
    if packet[0] != 0x07 or packet[2:5] != b"\x28\0\x0b":
        raise PacketFormatError("KA2 packet header is invalid")
    if packet[8:10] != b"\x2f\x12" or packet[10:16] != b"\0" * 6:
        raise PacketFormatError("KA2 packet fixed fields are invalid")
    if packet[20:24] != b"\0" * 4:
        raise PacketFormatError("KA2 packet middle padding is invalid")
    packet_type = packet[5]
    if packet_type not in (1, 3):
        raise PacketFormatError("KA2 packet type is unsupported")
    if packet[6:8] not in (b"\x0f\x27", b"\xdc\x02"):
        raise PacketFormatError("KA2 packet version is unsupported")
    if packet_type == 3:
        if packet[24:28] != b"\0" * 4 or packet[32:40] != b"\0" * 8:
            raise PacketFormatError("KA2 IP-report fields are invalid")
        reported_ipv4 = IPv4Address(packet[28:32])
    else:
        if packet[24:40] != b"\0" * 16:
            raise PacketFormatError("KA2 trailing padding is invalid")
        reported_ipv4 = None
    return Ka2Request(
        serial=packet[1],
        packet_type=packet_type,
        version=packet[6:8],
        tail=packet[16:20],
        reported_ipv4=reported_ipv4,
    )


def parse_logout_request(packet: bytes) -> LogoutRequest:
    _require_packet_length(packet, 80, "Logout")
    if packet[:3] != b"\x06\x01\0":
        raise PacketFormatError("Logout packet header is invalid")
    username_field = packet[20:56]
    username = _decode_username(username_field)
    username_bytes = username.encode("gbk", "strict")
    if packet[3] != 20 + len(username_bytes):
        raise PacketFormatError("Logout packet username length is inconsistent")
    return LogoutRequest(
        md5_a=packet[4:20],
        username=username,
        control_status=packet[56],
        adapter_num=packet[57],
        mac_xor=packet[58:64],
        auth_info=packet[64:80],
    )


def verify_logout_crypto_detailed(request: LogoutRequest, password: str,
                                  salt: bytes) -> DigestVerification:
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    material = _md5_a_input(salt, password)
    expected = hashlib.md5(material).digest()
    return DigestVerification(
        input=material,
        received=request.md5_a,
        expected=expected,
        valid=hmac.compare_digest(request.md5_a, expected),
    )


def verify_logout_crypto(request: LogoutRequest, password: str,
                         salt: bytes) -> bool:
    return verify_logout_crypto_detailed(request, password, salt).valid


def _auth_info_material(endpoint: tuple[str, int], username: str, salt: bytes,
                        session_number: int) -> bytes:
    return (b"auth-info\0" + endpoint[0].encode("ascii") + b"\0" +
            struct.pack("!H", endpoint[1]) + username.encode("gbk") + b"\0" +
            salt + struct.pack("!Q", session_number))


def derive_auth_info_detailed(secret: bytes, endpoint: tuple[str, int],
                              username: str, salt: bytes,
                              session_number: int) -> DerivedValue:
    material = _auth_info_material(endpoint, username, salt, session_number)
    return DerivedValue(
        input=material,
        output=hmac.new(secret, material, hashlib.sha256).digest()[:16],
    )


def derive_auth_info(secret: bytes, endpoint: tuple[str, int], username: str,
                     salt: bytes, session_number: int) -> bytes:
    return derive_auth_info_detailed(
        secret, endpoint, username, salt, session_number).output


def _ka2_tail_material(auth_info: bytes, old_tail: bytes, serial: int,
                       packet_type: int) -> bytes:
    return (b"ka2-tail\0" + auth_info + old_tail +
            bytes((serial & 0xFF, packet_type & 0xFF)))


def derive_ka2_tail_detailed(secret: bytes, auth_info: bytes, old_tail: bytes,
                             serial: int, packet_type: int) -> DerivedValue:
    material = _ka2_tail_material(auth_info, old_tail, serial, packet_type)
    return DerivedValue(
        input=material,
        output=hmac.new(secret, material, hashlib.sha256).digest()[:4],
    )


def derive_ka2_tail(secret: bytes, auth_info: bytes, old_tail: bytes,
                    serial: int, packet_type: int) -> bytes:
    return derive_ka2_tail_detailed(
        secret, auth_info, old_tail, serial, packet_type).output


def build_challenge_response(salt: bytes, source_ip: IPv4Address) -> bytes:
    if len(salt) != 4:
        raise ValueError("salt must contain four bytes")
    if not isinstance(source_ip, IPv4Address):
        raise TypeError("source_ip must be an IPv4Address")
    response = bytearray(16)
    response[0] = 0x02
    response[4:8] = salt
    response[8:12] = source_ip.packed
    return bytes(response)


def build_login_success_response(auth_info: bytes, traffic_kib: int,
                                 balance_cents: int) -> bytes:
    if len(auth_info) != 16:
        raise ValueError("auth_info must contain 16 bytes")
    response = bytearray(64)
    response[0] = 0x04
    struct.pack_into("<I", response, 9, traffic_kib)
    struct.pack_into("<I", response, 13, balance_cents)
    response[23:39] = auth_info
    return bytes(response)


def build_login_failure_response(code: int) -> bytes:
    if not 0 <= code <= 0xFF:
        raise ValueError("login failure code must fit one byte")
    response = bytearray(32)
    response[0] = 0x05
    response[4] = code
    return bytes(response)


def build_ka1_response() -> bytes:
    response = bytearray(20)
    response[0] = 0x07
    return bytes(response)


def build_ka2_response(serial: int, packet_type: int, tail: bytes,
                       month_minutes: int, traffic_kib: int,
                       balance_ten_thousandths: int,
                       remaining_minutes: int) -> bytes:
    if len(tail) != 4:
        raise ValueError("tail must contain four bytes")
    response = bytearray(60)
    response[0:6] = bytes((0x07, serial & 0xFF, 0x28, 0x00, 0x0B,
                           packet_type & 0xFF))
    response[16:20] = tail
    struct.pack_into("<IIII", response, 44, month_minutes, traffic_kib,
                     balance_ten_thousandths, remaining_minutes)
    return bytes(response)


def build_logout_response() -> bytes:
    return b"\x04\0\0\0"
