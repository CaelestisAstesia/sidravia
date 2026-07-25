from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum, IntEnum
from ipaddress import IPv4Address


class AuthErrorCode(IntEnum):
    IN_USE_WIRED = 0x01
    SERVER_BUSY = 0x02
    WRONG_PASSWORD = 0x03
    INSUFFICIENT_FUNDS = 0x04
    ACCOUNT_FROZEN = 0x05
    WRONG_IP = 0x07
    WRONG_MAC = 0x0B
    TOO_MANY_IP = 0x14
    WRONG_VERSION = 0x15
    WRONG_IP_MAC_BIND = 0x16
    FORCE_DHCP = 0x17


class Operation(str, Enum):
    CHALLENGE = "challenge"
    LOGIN = "login"
    KA1 = "ka1"
    KA2 = "ka2"
    LOGOUT = "logout"


@dataclass(frozen=True, slots=True)
class ServerSettings:
    challenge_ttl_seconds: float
    session_ttl_seconds: float
    max_sessions_per_account: int
    auth_version: bytes
    keep_alive_version: bytes
    control_check_status: bytes
    ipdog: bytes
    initial_month_traffic_kib: int
    initial_balance_cents: int
    server_secret: bytes | None
    credentials_are_fictional: bool = False


@dataclass(frozen=True, slots=True)
class Account:
    username: str
    password: str
    enabled: bool
    frozen: bool
    require_dhcp: bool
    expected_ipv4: IPv4Address | None
    expected_mac: bytes | None
    bind_ipv4_and_mac: bool
    max_sessions: int | None
    balance_cents: int


@dataclass(frozen=True, slots=True)
class ApplicationConfig:
    server: ServerSettings
    accounts: dict[str, Account]


@dataclass(slots=True)
class PendingChallenge:
    salt: bytes
    issued_at: float


@dataclass(slots=True)
class BillingState:
    month_time_minutes: int
    traffic_kib: int
    balance_ten_thousandths: int
    remaining_minutes: int


@dataclass(slots=True)
class AuthenticatedSession:
    endpoint: tuple[str, int]
    username: str
    login_salt: bytes
    auth_info: bytes
    reported_ipv4: IPv4Address
    mac: bytes
    last_activity: float
    billing: BillingState
    expected_ka2_serial: int = 0
    ka2_tail: bytes = b"\x00" * 4
    first_ka2_seen: bool = False
    second_initial_type1_allowed: bool = True
    last_ka2_type: int | None = None
    last_ka2_request: bytes | None = None
    last_ka2_response: bytes | None = None


@dataclass(slots=True)
class ServerMetrics:
    datagrams_received: int = 0
    logins_succeeded: int = 0
    logins_rejected: int = 0
    datagrams_dropped: int = 0
    keepalives_accepted: int = 0
