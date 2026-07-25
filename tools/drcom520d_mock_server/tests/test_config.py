import json
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path

from drcom520d_mock_server.config import (
    ConfigurationError,
    load_application_config,
    require_fictional_credentials,
)


class ConfigurationTests(unittest.TestCase):
    def write_config(self, payload: dict) -> Path:
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = Path(directory.name) / "accounts.json"
        path.write_text(json.dumps(payload), encoding="utf-8")
        return path

    def valid_payload(self) -> dict:
        return {
            "server": {
                "challenge_ttl_seconds": 10,
                "session_ttl_seconds": 90,
                "max_sessions_per_account": 1,
                "auth_version_hex": "2c00",
                "keep_alive_version_hex": "dc02",
                "control_check_status_hex": "20",
                "ipdog_hex": "01",
                "initial_month_traffic_kib": 14208,
                "initial_balance_cents": 10000,
                "server_secret_hex": "11" * 32,
            },
            "accounts": [{
                "username": "student-test",
                "password": "local-test-password",
                "enabled": True,
                "frozen": False,
                "require_dhcp": False,
                "expected_ipv4": "10.0.0.2",
                "expected_mac": "02:00:00:00:00:01",
                "bind_ipv4_and_mac": True,
                "max_sessions": 1,
                "balance_cents": 10000,
            }],
        }

    def test_loads_typed_jlu_configuration(self):
        config = load_application_config(self.write_config(self.valid_payload()))
        self.assertEqual(config.server.auth_version, bytes.fromhex("2c00"))
        self.assertEqual(config.server.keep_alive_version, bytes.fromhex("dc02"))
        self.assertEqual(config.accounts["student-test"].expected_mac,
                         bytes.fromhex("020000000001"))
        self.assertEqual(str(config.accounts["student-test"].expected_ipv4), "10.0.0.2")

    def test_loads_omitted_optional_account_fields_as_none(self):
        optional_fields = ("expected_ipv4", "expected_mac", "max_sessions")
        for omitted_fields in *((field,) for field in optional_fields), optional_fields:
            with self.subTest(omitted_fields=omitted_fields):
                payload = self.valid_payload()
                account = payload["accounts"][0]
                for field in omitted_fields:
                    account.pop(field)

                loaded = load_application_config(self.write_config(payload)).accounts["student-test"]

                for field in omitted_fields:
                    self.assertIsNone(getattr(loaded, field))

    def test_loads_null_optional_account_fields_as_none(self):
        payload = self.valid_payload()
        account = payload["accounts"][0]
        for field in ("expected_ipv4", "expected_mac", "max_sessions"):
            account[field] = None

        loaded = load_application_config(self.write_config(payload)).accounts["student-test"]

        self.assertIsNone(loaded.expected_ipv4)
        self.assertIsNone(loaded.expected_mac)
        self.assertIsNone(loaded.max_sessions)

    def test_rejects_unknown_fields(self):
        payload = self.valid_payload()
        payload["server"]["surprise"] = True
        with self.assertRaisesRegex(ConfigurationError, "unknown server fields: surprise"):
            load_application_config(self.write_config(payload))

    def test_rejects_unknown_account_fields(self):
        payload = self.valid_payload()
        payload["accounts"][0]["surprise"] = True
        with self.assertRaisesRegex(ConfigurationError, "unknown account fields: surprise"):
            load_application_config(self.write_config(payload))

    def test_rejects_duplicate_usernames(self):
        payload = self.valid_payload()
        payload["accounts"].append(dict(payload["accounts"][0]))
        with self.assertRaisesRegex(ConfigurationError, "duplicate account"):
            load_application_config(self.write_config(payload))

    def test_rejects_empty_accounts(self):
        payload = self.valid_payload()
        payload["accounts"] = []
        with self.assertRaisesRegex(ConfigurationError, "accounts must not be empty"):
            load_application_config(self.write_config(payload))

    def test_rejects_real_jlu_server_address_field(self):
        payload = self.valid_payload()
        payload["server"]["server_ip"] = "10.100.61.3"
        with self.assertRaisesRegex(ConfigurationError, "unknown server fields: server_ip"):
            load_application_config(self.write_config(payload))

    def test_rejects_invalid_strict_configuration_values(self):
        cases = (
            ("missing root field", lambda p: p.pop("accounts"),
             "missing root fields: accounts"),
            ("wrong numeric type", lambda p: p["server"].__setitem__("challenge_ttl_seconds", True),
             "server.challenge_ttl_seconds must be a positive number"),
            ("out of range numeric value", lambda p: p["server"].__setitem__("max_sessions_per_account", 0),
             "server.max_sessions_per_account must be an integer from 1 to 255"),
            ("invalid fixed width hex", lambda p: p["server"].__setitem__("auth_version_hex", "2c"),
             "server.auth_version_hex must contain exactly 2 bytes"),
            ("invalid IPv4", lambda p: p["accounts"][0].__setitem__("expected_ipv4", "not-an-ip"),
             "account.expected_ipv4 must be an IPv4 address or null"),
            ("invalid MAC", lambda p: p["accounts"][0].__setitem__("expected_mac", "02:00:00"),
             "account.expected_mac must be a six-byte MAC address or null"),
            ("empty username", lambda p: p["accounts"][0].__setitem__("username", ""),
             "account.username must be a non-empty string"),
            ("empty password", lambda p: p["accounts"][0].__setitem__("password", ""),
             "account.password must be a non-empty string"),
            ("short server secret", lambda p: p["server"].__setitem__("server_secret_hex", "11" * 31),
             "server.server_secret_hex must contain exactly 32 bytes"),
            ("invalid fictional declaration", lambda p: p["server"].__setitem__("credentials_are_fictional", "yes"),
             "server.credentials_are_fictional must be a boolean"),
            ("empty accounts", lambda p: p.__setitem__("accounts", []),
             "accounts must not be empty"),
        )
        for name, mutate, message in cases:
            with self.subTest(name=name):
                payload = deepcopy(self.valid_payload())
                mutate(payload)
                with self.assertRaisesRegex(ConfigurationError, message):
                    load_application_config(self.write_config(payload))

    def test_full_secret_trace_requires_explicit_fictional_credentials(self):
        raw = self.valid_payload()
        raw["server"].pop("credentials_are_fictional", None)
        config = load_application_config(self.write_config(raw))
        self.assertFalse(config.server.credentials_are_fictional)
        with self.assertRaisesRegex(
                ConfigurationError, "完整追踪只允许使用虚构凭据"):
            require_fictional_credentials(config)

        raw["server"]["credentials_are_fictional"] = False
        with self.assertRaisesRegex(
                ConfigurationError, "完整追踪只允许使用虚构凭据"):
            require_fictional_credentials(
                load_application_config(self.write_config(raw)))

        raw["server"]["credentials_are_fictional"] = True
        fictional = load_application_config(self.write_config(raw))
        self.assertTrue(fictional.server.credentials_are_fictional)
        require_fictional_credentials(fictional)


if __name__ == "__main__":
    unittest.main()
