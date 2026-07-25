from __future__ import annotations

import json
import math
from ipaddress import IPv4Address
from pathlib import Path
from typing import Any

from .models import Account, ApplicationConfig, ServerSettings


class ConfigurationError(ValueError):
    """Raised when an account configuration file is not strictly valid."""


_ROOT_FIELDS = frozenset({"server", "accounts"})
_SERVER_REQUIRED_FIELDS = frozenset({
    "challenge_ttl_seconds", "session_ttl_seconds", "max_sessions_per_account",
    "auth_version_hex", "keep_alive_version_hex", "control_check_status_hex",
    "ipdog_hex", "initial_month_traffic_kib", "initial_balance_cents",
})
_SERVER_OPTIONAL_FIELDS = frozenset({
    "server_secret_hex", "credentials_are_fictional",
})
_ACCOUNT_FIELDS = frozenset({
    "username", "password", "enabled", "frozen", "require_dhcp", "expected_ipv4",
    "expected_mac", "bind_ipv4_and_mac", "max_sessions", "balance_cents",
})
_ACCOUNT_OPTIONAL_FIELDS = frozenset({"expected_ipv4", "expected_mac", "max_sessions"})
_ACCOUNT_REQUIRED_FIELDS = _ACCOUNT_FIELDS - _ACCOUNT_OPTIONAL_FIELDS


def load_application_config(path: Path) -> ApplicationConfig:
    """Load one strictly-validated local mock-server account configuration."""
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ConfigurationError(f"cannot read configuration: {error}") from error

    root = _mapping(payload, "root")
    _check_keys(root, _ROOT_FIELDS, "root")
    _check_required(root, _ROOT_FIELDS, "root")
    server = _load_server(_mapping(root["server"], "server"))
    accounts_data = root["accounts"]
    if not isinstance(accounts_data, list):
        raise ConfigurationError("accounts must be an array")
    if not accounts_data:
        raise ConfigurationError("accounts must not be empty")

    accounts: dict[str, Account] = {}
    for index, raw_account in enumerate(accounts_data):
        account = _load_account(_mapping(raw_account, f"accounts[{index}]"))
        if account.username in accounts:
            raise ConfigurationError(f"duplicate account: {account.username}")
        accounts[account.username] = account
    return ApplicationConfig(server=server, accounts=accounts)


def require_fictional_credentials(config: ApplicationConfig) -> None:
    if not config.server.credentials_are_fictional:
        raise ConfigurationError(
            "完整追踪只允许使用虚构凭据；"
            "请在 server 中设置 credentials_are_fictional=true")


def _load_server(raw: dict[str, Any]) -> ServerSettings:
    allowed = _SERVER_REQUIRED_FIELDS | _SERVER_OPTIONAL_FIELDS
    _check_keys(raw, allowed, "server")
    _check_required(raw, _SERVER_REQUIRED_FIELDS, "server")
    return ServerSettings(
        challenge_ttl_seconds=_positive_float(raw["challenge_ttl_seconds"], "server.challenge_ttl_seconds"),
        session_ttl_seconds=_positive_float(raw["session_ttl_seconds"], "server.session_ttl_seconds"),
        max_sessions_per_account=_positive_int(raw["max_sessions_per_account"], "server.max_sessions_per_account", 255),
        auth_version=_hex_bytes(raw["auth_version_hex"], "server.auth_version_hex", 2),
        keep_alive_version=_hex_bytes(raw["keep_alive_version_hex"], "server.keep_alive_version_hex", 2),
        control_check_status=_hex_bytes(raw["control_check_status_hex"], "server.control_check_status_hex", 1),
        ipdog=_hex_bytes(raw["ipdog_hex"], "server.ipdog_hex", 1),
        initial_month_traffic_kib=_nonnegative_int(raw["initial_month_traffic_kib"], "server.initial_month_traffic_kib"),
        initial_balance_cents=_nonnegative_int(raw["initial_balance_cents"], "server.initial_balance_cents"),
        server_secret=(None if "server_secret_hex" not in raw or raw["server_secret_hex"] is None
                       else _hex_bytes(raw["server_secret_hex"], "server.server_secret_hex", 32)),
        credentials_are_fictional=_bool(
            raw.get("credentials_are_fictional", False),
            "server.credentials_are_fictional",
        ),
    )


def _load_account(raw: dict[str, Any]) -> Account:
    _check_keys(raw, _ACCOUNT_FIELDS, "account")
    _check_required(raw, _ACCOUNT_REQUIRED_FIELDS, "account")
    username = _nonempty_string(raw["username"], "account.username")
    return Account(
        username=username,
        password=_nonempty_string(raw["password"], "account.password"),
        enabled=_bool(raw["enabled"], "account.enabled"),
        frozen=_bool(raw["frozen"], "account.frozen"),
        require_dhcp=_bool(raw["require_dhcp"], "account.require_dhcp"),
        expected_ipv4=_ipv4(raw.get("expected_ipv4"), "account.expected_ipv4"),
        expected_mac=_mac(raw.get("expected_mac"), "account.expected_mac"),
        bind_ipv4_and_mac=_bool(raw["bind_ipv4_and_mac"], "account.bind_ipv4_and_mac"),
        max_sessions=(None if raw.get("max_sessions") is None else _positive_int(raw["max_sessions"], "account.max_sessions", 255)),
        balance_cents=_nonnegative_int(raw["balance_cents"], "account.balance_cents"),
    )


def _mapping(value: Any, name: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ConfigurationError(f"{name} must be an object")
    return value


def _check_keys(raw: dict[str, Any], allowed: frozenset[str], name: str) -> None:
    unknown = sorted(set(raw) - allowed)
    if unknown:
        raise ConfigurationError(f"unknown {name} fields: {', '.join(unknown)}")


def _check_required(raw: dict[str, Any], required: frozenset[str], name: str) -> None:
    missing = sorted(required - set(raw))
    if missing:
        raise ConfigurationError(f"missing {name} fields: {', '.join(missing)}")


def _positive_float(value: Any, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ConfigurationError(f"{name} must be a positive number")
    return float(value)


def _positive_int(value: Any, name: str, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= maximum:
        raise ConfigurationError(f"{name} must be an integer from 1 to {maximum}")
    return value


def _nonnegative_int(value: Any, name: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ConfigurationError(f"{name} must be a non-negative integer")
    return value


def _bool(value: Any, name: str) -> bool:
    if not isinstance(value, bool):
        raise ConfigurationError(f"{name} must be a boolean")
    return value


def _nonempty_string(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value:
        raise ConfigurationError(f"{name} must be a non-empty string")
    return value


def _hex_bytes(value: Any, name: str, length: int) -> bytes:
    if not isinstance(value, str):
        raise ConfigurationError(f"{name} must be a hexadecimal string")
    try:
        result = bytes.fromhex(value)
    except ValueError as error:
        raise ConfigurationError(f"{name} must be a hexadecimal string") from error
    if len(result) != length:
        raise ConfigurationError(f"{name} must contain exactly {length} bytes")
    return result


def _ipv4(value: Any, name: str) -> IPv4Address | None:
    if value is None:
        return None
    if not isinstance(value, str):
        raise ConfigurationError(f"{name} must be an IPv4 address or null")
    try:
        return IPv4Address(value)
    except ValueError as error:
        raise ConfigurationError(f"{name} must be an IPv4 address or null") from error


def _mac(value: Any, name: str) -> bytes | None:
    if value is None:
        return None
    if not isinstance(value, str):
        raise ConfigurationError(f"{name} must be a six-byte MAC address or null")
    hex_value = value.replace(":", "").replace("-", "")
    try:
        result = bytes.fromhex(hex_value)
    except ValueError as error:
        raise ConfigurationError(f"{name} must be a six-byte MAC address or null") from error
    if len(result) != 6:
        raise ConfigurationError(f"{name} must be a six-byte MAC address or null")
    return result
