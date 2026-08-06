"""Strict loader and mock-boundary verification for the D520 client vector fixture.

This test does three independent jobs, all without importing anything from
``参考/`` and without starting any UDP listener:

1. Strictly validate the JSON schema of ``d520_client_vectors_v1.json`` --
   every required field must be present and no unknown field is tolerated.
2. Verify that every declared hex value is lowercase canonical hex and that
   every declared byte length is self-consistent with its hex.
3. Feed every client request through the existing mock ``ServerCore`` boundary
   and every crypto intermediate through the mock ``codec`` boundary, proving
   the fixture matches current mock behavior.

The fixture is data, not copied source. It carries only fictional credentials.
"""

import copy
import json
import re
import unittest
from pathlib import Path
from ipaddress import IPv4Address

from drcom520d_mock_server.models import (
    Account,
    ApplicationConfig,
    ServerSettings,
)
from drcom520d_mock_server.scenarios import builtin_scenario
from drcom520d_mock_server.server import ServerCore
from drcom520d_mock_server.codec import (
    checksum_d_series,
    derive_auth_info_detailed,
    derive_ka2_tail_detailed,
    md5_a,
    md5_b,
    md5_c,
)


FIXTURE_PATH = (
    Path(__file__).resolve().parent / "fixtures" / "d520_client_vectors_v1.json"
)
HEX_RE = re.compile(r"^[0-9a-f]*$")
ALLOWED_OPERATIONS = {"challenge", "login", "ka1", "ka2", "logout"}


# Schema description: nested dict = object, list = homogeneous list, leaf = type
# or tuple of types. bool is checked before int because bool is a subtype of int.
SCHEMA = {
    "schema_version": int,
    "fixture_version": str,
    "protocol_id": str,
    "credentials_are_fictional": bool,
    "provenance": {
        "source_projects": [str],
        "source_commits": {str: str},
        "licenses": {str: str},
        "extracted_files": [str],
    },
    "determinism": {
        "server_secret_hex": str,
        "salt_sequence_hex": [str],
        "client_endpoint": {"host": str, "port": int},
        "clock_start": (int, float),
        "scenario": str,
        "server_settings": {
            "challenge_ttl_seconds": (int, float),
            "session_ttl_seconds": (int, float),
            "max_sessions_per_account": int,
            "auth_version_hex": str,
            "keep_alive_version_hex": str,
            "control_check_status_hex": str,
            "ipdog_hex": str,
            "initial_month_traffic_kib": int,
            "initial_balance_cents": int,
        },
    },
    "fictional_inputs": {
        "username": str,
        "password": str,
        "client_ipv4": str,
        "mac_hex": str,
        "primary_dns": str,
        "secondary_dns": str,
        "dhcp_ipv4": str,
        "host_name": str,
        "host_os": str,
        "auth_version_hex": str,
        "keep_alive_version_hex": str,
        "control_check_status_hex": str,
        "ipdog_hex": str,
        "adapter_num_hex": str,
        "os_info_hex": str,
    },
    "fictional_account": {
        "enabled": bool,
        "frozen": bool,
        "require_dhcp": bool,
        "bind_ipv4_and_mac": bool,
        "max_sessions": int,
        "balance_cents": int,
    },
    "crypto_intermediates": {
        "salt_hex": str,
        "md5_a": {"input_hex": str, "output_hex": str},
        "md5_b": {"input_hex": str, "output_hex": str},
        "md5_c": {"input_hex": str, "output_hex": str},
        "mac_xor": {"input_hex": str, "output_hex": str},
        "crc_1968": {"input_hex": str, "output_hex": str},
        "auth_info": {"material_hex": str, "output_hex": str},
        "ka2_tail_type1": {"material_hex": str, "output_hex": str},
        "ka2_tail_type3": {"material_hex": str, "output_hex": str},
    },
    "exchanges": [
        {
            "name": str,
            "operation": str,
            "request_hex": str,
            "request_length": int,
            "declared_response_hex": str,
            "declared_response_length": int,
        }
    ],
    "rejection": {
        "challenge": {
            "request_hex": str,
            "request_length": int,
            "declared_response_hex": str,
            "declared_response_length": int,
        },
        "login": {
            "request_hex": str,
            "request_length": int,
            "declared_response_hex": str,
            "declared_response_length": int,
            "wire_error_code": int,
        },
    },
}


def _check_type(value, schema, path):
    if schema is bool:
        if not isinstance(value, bool):
            raise AssertionError(f"{path}: expected bool, got {type(value).__name__}")
    elif schema is int:
        if isinstance(value, bool) or not isinstance(value, int):
            raise AssertionError(f"{path}: expected int, got {type(value).__name__}")
    elif schema is str:
        if not isinstance(value, str):
            raise AssertionError(f"{path}: expected str, got {type(value).__name__}")
    elif isinstance(schema, tuple):
        if isinstance(value, bool) or not isinstance(value, schema):
            raise AssertionError(f"{path}: expected {schema}, got {type(value).__name__}")
    else:  # pragma: no cover - defensive
        raise AssertionError(f"{path}: unsupported schema leaf {schema!r}")


def _validate(value, schema, path):
    if isinstance(schema, dict):
        if len(schema) == 1 and not isinstance(next(iter(schema)), str):
            # Typed dict schema like {str: str}: any keys of key_type mapping to
            # values of value_type. Distinguished from an object schema because
            # object schemas use string keys.
            key_type, value_type = next(iter(schema.items()))
            if not isinstance(value, dict):
                raise AssertionError(
                    f"{path}: expected object, got {type(value).__name__}"
                )
            for item_key, item_value in value.items():
                _check_type(item_key, key_type, f"{path}.<key>")
                _check_type(item_value, value_type, f"{path}.{item_key!r}")
            return
        if not isinstance(value, dict):
            raise AssertionError(f"{path}: expected object, got {type(value).__name__}")
        for key in schema:
            if key not in value:
                raise AssertionError(f"{path}.{key}: missing required field")
        for key in value:
            if key not in schema:
                raise AssertionError(f"{path}.{key}: unknown field")
        for key in schema:
            _validate(value[key], schema[key], f"{path}.{key}")
    elif isinstance(schema, list):
        if not isinstance(value, list):
            raise AssertionError(f"{path}: expected list, got {type(value).__name__}")
        if not value:
            raise AssertionError(f"{path}: expected non-empty list")
        for index, item in enumerate(value):
            _validate(item, schema[0], f"{path}[{index}]")
    else:
        _check_type(value, schema, path)


def _collect_hex(value, found, path="root"):
    """Collect (path, hex_string) for every field whose name ends with _hex.

    String values are collected directly; lists of strings (such as the salt
    sequence) are collected element by element so every declared hex fragment
    is canonicalized.
    """
    if isinstance(value, dict):
        for key, child in value.items():
            child_path = f"{path}.{key}"
            if key.endswith("_hex"):
                if isinstance(child, str):
                    found.append((child_path, child))
                elif isinstance(child, list):
                    for index, item in enumerate(child):
                        if isinstance(item, str):
                            found.append((f"{child_path}[{index}]", item))
                        else:
                            _collect_hex(item, found, f"{child_path}[{index}]")
                else:
                    _collect_hex(child, found, child_path)
            else:
                _collect_hex(child, found, child_path)
    elif isinstance(value, list):
        for index, item in enumerate(value):
            _collect_hex(item, found, f"{path}[{index}]")


class _FakeClock:
    def __init__(self, value):
        self.value = value

    def __call__(self):
        return self.value

    def advance(self, seconds):
        self.value += seconds


def _build_core(fixture):
    det = fixture["determinism"]
    settings = det["server_settings"]
    fin = fixture["fictional_inputs"]
    acc = fixture["fictional_account"]
    secret = bytes.fromhex(det["server_secret_hex"])
    endpoint = det["client_endpoint"]

    account = Account(
        username=fin["username"],
        password=fin["password"],
        enabled=acc["enabled"],
        frozen=acc["frozen"],
        require_dhcp=acc["require_dhcp"],
        expected_ipv4=IPv4Address(fin["client_ipv4"]),
        expected_mac=bytes.fromhex(fin["mac_hex"]),
        bind_ipv4_and_mac=acc["bind_ipv4_and_mac"],
        max_sessions=acc["max_sessions"],
        balance_cents=acc["balance_cents"],
    )
    server_settings = ServerSettings(
        challenge_ttl_seconds=settings["challenge_ttl_seconds"],
        session_ttl_seconds=settings["session_ttl_seconds"],
        max_sessions_per_account=settings["max_sessions_per_account"],
        auth_version=bytes.fromhex(settings["auth_version_hex"]),
        keep_alive_version=bytes.fromhex(settings["keep_alive_version_hex"]),
        control_check_status=bytes.fromhex(settings["control_check_status_hex"]),
        ipdog=bytes.fromhex(settings["ipdog_hex"]),
        initial_month_traffic_kib=settings["initial_month_traffic_kib"],
        initial_balance_cents=settings["initial_balance_cents"],
        server_secret=secret,
    )
    config = ApplicationConfig(
        server=server_settings,
        accounts={fin["username"]: account},
    )

    salt_iter = iter(bytes.fromhex(s) for s in det["salt_sequence_hex"])

    def random_bytes(size):
        if size == 4:
            return next(salt_iter)
        return bytes(range(1, size + 1))

    return ServerCore(
        config,
        builtin_scenario(det["scenario"]),
        clock=_FakeClock(det["clock_start"]),
        random_bytes=random_bytes,
    ), (endpoint["host"], endpoint["port"])


class FixtureSchemaTests(unittest.TestCase):
    def setUp(self):
        self.fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))

    def test_fixture_schema_is_valid(self):
        _validate(self.fixture, SCHEMA, "fixture")

    def test_schema_rejects_unknown_field(self):
        corrupt = copy.deepcopy(self.fixture)
        corrupt["determinism"]["unexpected_field"] = "x"
        with self.assertRaisesRegex(AssertionError, "unknown field"):
            _validate(corrupt, SCHEMA, "fixture")

    def test_schema_rejects_missing_field(self):
        corrupt = copy.deepcopy(self.fixture)
        del corrupt["crypto_intermediates"]["md5_a"]["output_hex"]
        with self.assertRaisesRegex(AssertionError, "missing required field"):
            _validate(corrupt, SCHEMA, "fixture")

    def test_schema_rejects_wrong_type(self):
        corrupt = copy.deepcopy(self.fixture)
        corrupt["schema_version"] = "1"
        with self.assertRaisesRegex(AssertionError, "expected int"):
            _validate(corrupt, SCHEMA, "fixture")

    def test_credentials_are_fictional(self):
        self.assertTrue(self.fixture["credentials_are_fictional"])

    def test_protocol_id_is_stable(self):
        self.assertEqual(self.fixture["protocol_id"], "drcom-5.2.0-d")

    def test_operations_are_within_allowed_set(self):
        for exchange in self.fixture["exchanges"]:
            self.assertIn(
                exchange["operation"], ALLOWED_OPERATIONS, exchange["name"]
            )


class HexCanonicalTests(unittest.TestCase):
    def setUp(self):
        self.fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))

    def test_all_hex_fields_are_lowercase_canonical(self):
        found = []
        _collect_hex(self.fixture, found)
        self.assertGreater(len(found), 20, "expected many hex fields")
        for path, hex_value in found:
            self.assertGreater(len(hex_value), 0, f"{path}: empty hex")
            self.assertEqual(len(hex_value) % 2, 0, f"{path}: odd hex length")
            self.assertRegex(
                hex_value, HEX_RE, f"{path}: not lowercase canonical hex"
            )

    def test_exchange_lengths_match_hex(self):
        for exchange in self.fixture["exchanges"]:
            self.assertEqual(
                len(bytes.fromhex(exchange["request_hex"])),
                exchange["request_length"],
                exchange["name"] + " request length",
            )
            self.assertEqual(
                len(bytes.fromhex(exchange["declared_response_hex"])),
                exchange["declared_response_length"],
                exchange["name"] + " response length",
            )

    def test_rejection_lengths_match_hex(self):
        rejection = self.fixture["rejection"]
        for stage in ("challenge", "login"):
            block = rejection[stage]
            self.assertEqual(
                len(bytes.fromhex(block["request_hex"])),
                block["request_length"],
                "rejection." + stage + " request length",
            )
            self.assertEqual(
                len(bytes.fromhex(block["declared_response_hex"])),
                block["declared_response_length"],
                "rejection." + stage + " response length",
            )


class CryptoIntermediateTests(unittest.TestCase):
    def setUp(self):
        self.fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))
        self.crypto = self.fixture["crypto_intermediates"]
        self.fin = self.fixture["fictional_inputs"]
        self.det = self.fixture["determinism"]
        self.salt = bytes.fromhex(self.crypto["salt_hex"])
        self.password = self.fin["password"]
        self.secret = bytes.fromhex(self.det["server_secret_hex"])
        self.endpoint = (
            self.det["client_endpoint"]["host"],
            self.det["client_endpoint"]["port"],
        )
        self.username = self.fin["username"]

    def test_md5_a_matches_mock_codec(self):
        block = self.crypto["md5_a"]
        self.assertEqual(md5_a(self.salt, self.password).hex(), block["output_hex"])
        expected_input = b"\x03\x01" + self.salt + self.password.encode("gbk")
        self.assertEqual(expected_input.hex(), block["input_hex"])

    def test_md5_b_matches_mock_codec(self):
        block = self.crypto["md5_b"]
        self.assertEqual(md5_b(self.salt, self.password).hex(), block["output_hex"])
        expected_input = (
            b"\x01" + self.password.encode("gbk") + self.salt + b"\x00" * 4
        )
        self.assertEqual(expected_input.hex(), block["input_hex"])

    def test_md5_c_matches_mock_codec(self):
        block = self.crypto["md5_c"]
        ip_section = b"\x01" + IPv4Address(self.fin["client_ipv4"]).packed + b"\x00" * 12
        self.assertEqual(md5_c(ip_section).hex(), block["output_hex"])
        self.assertEqual(
            (ip_section + b"\x14\x00\x07\x0b").hex(), block["input_hex"]
        )

    def test_mac_xor_matches_mock_codec(self):
        block = self.crypto["mac_xor"]
        mac = bytes.fromhex(self.fin["mac_hex"])
        md5a = md5_a(self.salt, self.password)
        expected = bytes(a ^ b for a, b in zip(mac, md5a[:6]))
        self.assertEqual(expected.hex(), block["output_hex"])
        self.assertEqual((mac + md5a[:6]).hex(), block["input_hex"])

    def test_crc_1968_matches_mock_codec(self):
        block = self.crypto["crc_1968"]
        self.assertEqual(
            checksum_d_series(bytes.fromhex(block["input_hex"])).hex(),
            block["output_hex"],
        )

    def test_auth_info_matches_mock_codec(self):
        block = self.crypto["auth_info"]
        derived = derive_auth_info_detailed(
            self.secret, self.endpoint, self.username, self.salt, 1
        )
        self.assertEqual(derived.input.hex(), block["material_hex"])
        self.assertEqual(derived.output.hex(), block["output_hex"])

    def test_ka2_tail_type1_matches_mock_codec(self):
        block = self.crypto["ka2_tail_type1"]
        auth_info = bytes.fromhex(self.crypto["auth_info"]["output_hex"])
        derived = derive_ka2_tail_detailed(
            self.secret, auth_info, b"\x00" * 4, 0, 1
        )
        self.assertEqual(derived.input.hex(), block["material_hex"])
        self.assertEqual(derived.output.hex(), block["output_hex"])

    def test_ka2_tail_type3_matches_mock_codec(self):
        block = self.crypto["ka2_tail_type3"]
        auth_info = bytes.fromhex(self.crypto["auth_info"]["output_hex"])
        old_tail = bytes.fromhex(self.crypto["ka2_tail_type1"]["output_hex"])
        derived = derive_ka2_tail_detailed(
            self.secret, auth_info, old_tail, 1, 3
        )
        self.assertEqual(derived.input.hex(), block["material_hex"])
        self.assertEqual(derived.output.hex(), block["output_hex"])

    def test_auth_info_appears_in_login_success_response(self):
        login_response = bytes.fromhex(
            self.fixture["exchanges"][1]["declared_response_hex"]
        )
        self.assertEqual(
            login_response[23:39],
            bytes.fromhex(self.crypto["auth_info"]["output_hex"]),
        )

    def test_ka2_tails_appear_in_ka2_responses(self):
        ka2_t1 = bytes.fromhex(self.fixture["exchanges"][3]["declared_response_hex"])
        ka2_t3 = bytes.fromhex(self.fixture["exchanges"][4]["declared_response_hex"])
        self.assertEqual(
            ka2_t1[16:20],
            bytes.fromhex(self.crypto["ka2_tail_type1"]["output_hex"]),
        )
        self.assertEqual(
            ka2_t3[16:20],
            bytes.fromhex(self.crypto["ka2_tail_type3"]["output_hex"]),
        )


class ServerCoreBoundaryTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))

    async def test_success_exchanges_match_server_core(self):
        core, endpoint = _build_core(self.fixture)
        for exchange in self.fixture["exchanges"]:
            request = bytes.fromhex(exchange["request_hex"])
            expected = bytes.fromhex(exchange["declared_response_hex"])
            actual = await core.handle_datagram(request, endpoint)
            self.assertEqual(
                actual,
                expected,
                f"{exchange['name']}: response mismatch",
            )

    async def test_rejection_exchange_matches_server_core(self):
        core, endpoint = _build_core(self.fixture)
        rejection = self.fixture["rejection"]
        challenge = rejection["challenge"]
        actual_chal = await core.handle_datagram(
            bytes.fromhex(challenge["request_hex"]), endpoint
        )
        self.assertEqual(
            actual_chal,
            bytes.fromhex(challenge["declared_response_hex"]),
            "rejection.challenge mismatch",
        )
        login = rejection["login"]
        actual_login = await core.handle_datagram(
            bytes.fromhex(login["request_hex"]), endpoint
        )
        self.assertEqual(
            actual_login,
            bytes.fromhex(login["declared_response_hex"]),
            "rejection.login mismatch",
        )
        self.assertEqual(actual_login[4], login["wire_error_code"])


if __name__ == "__main__":
    unittest.main()
