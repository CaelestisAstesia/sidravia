import time
import unittest
from dataclasses import replace
from ipaddress import IPv4Address

from drcom520d_mock_server.models import (
    Account,
    ApplicationConfig,
    AuthErrorCode,
    ServerSettings,
)
from drcom520d_mock_server.models import Operation
from drcom520d_mock_server.scenarios import (
    ActionKind,
    ScenarioAction,
    ScenarioProgram,
    builtin_scenario,
)
from drcom520d_mock_server.server import ServerCore
from tools.drcom520d_mock_server.tests.packet_factory import (
    build_ka1_request,
    build_ka2_request,
    build_login_request,
    build_logout_request,
    refresh_login_crc,
)


class FakeClock:
    def __init__(self, value: float = 1000.0):
        self.value = value

    def __call__(self) -> float:
        return self.value

    def advance(self, seconds: float) -> None:
        self.value += seconds


class ServerCoreTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.clock = FakeClock()
        self.endpoint = ("127.0.0.1", 50000)
        self.account = Account(
            username="student-test",
            password="local-test-password",
            enabled=True,
            frozen=False,
            require_dhcp=False,
            expected_ipv4=IPv4Address("10.0.0.2"),
            expected_mac=bytes.fromhex("020000000001"),
            bind_ipv4_and_mac=True,
            max_sessions=1,
            balance_cents=10000,
        )
        self.config = ApplicationConfig(
            server=ServerSettings(
                challenge_ttl_seconds=10,
                session_ttl_seconds=90,
                max_sessions_per_account=1,
                auth_version=bytes.fromhex("2c00"),
                keep_alive_version=bytes.fromhex("dc02"),
                control_check_status=bytes.fromhex("20"),
                ipdog=bytes.fromhex("01"),
                initial_month_traffic_kib=14208,
                initial_balance_cents=10000,
                server_secret=b"s" * 32,
            ),
            accounts={"student-test": self.account},
        )
        self.core = self.make_core()

    def make_core(self, scenario=None, config=None, random_bytes=None):
        return ServerCore(
            config or self.config,
            scenario or builtin_scenario("normal"),
            clock=self.clock,
            random_bytes=random_bytes or (lambda size: bytes(range(1, size + 1))),
        )

    async def issue_challenge(self, core=None, endpoint=None):
        core = core or self.core
        endpoint = endpoint or self.endpoint
        response = await core.handle_datagram(b"\x01\x02" + b"\0" * 18, endpoint)
        self.assertEqual(response[0], 0x02)
        return response[4:8]

    async def login(self, core=None, endpoint=None, **changes):
        core = core or self.core
        endpoint = endpoint or self.endpoint
        salt = await self.issue_challenge(core, endpoint)
        return await core.handle_datagram(
            build_login_request(salt=salt, **changes), endpoint)

    async def authenticated_session(self):
        response = await self.login()
        self.assertEqual(response[0], 0x04)
        return self.core.sessions[self.endpoint]

    async def send_ka2(self, serial, packet_type, tail, version,
                       ipv4="10.0.0.2"):
        return await self.core.handle_datagram(
            build_ka2_request(serial, packet_type, tail, ipv4, version), self.endpoint)

    async def test_challenge_then_valid_login_creates_session_and_metrics(self):
        response = await self.login()

        self.assertEqual(response[0], 0x04)
        self.assertIn(self.endpoint, self.core.sessions)
        self.assertEqual(response[23:39], self.core.sessions[self.endpoint].auth_info)
        self.assertEqual(self.core.account_sessions, {"student-test": {self.endpoint}})
        self.assertEqual(self.core.metrics.logins_succeeded, 1)
        self.assertEqual(self.core.metrics.logins_rejected, 0)

    async def test_successful_login_consumes_its_challenge_and_drops_replay(self):
        salt = await self.issue_challenge()
        packet = build_login_request(salt=salt)
        first_response = await self.core.handle_datagram(packet, self.endpoint)
        first_auth_info = self.core.sessions[self.endpoint].auth_info

        self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))
        self.assertNotIn(self.endpoint, self.core.challenges)
        self.assertEqual(self.core.sessions[self.endpoint].auth_info, first_auth_info)
        self.assertEqual(self.core._session_number, 1)
        self.assertEqual(self.core.metrics.logins_succeeded, 1)
        self.assertEqual(first_response[23:39], first_auth_info)

    async def test_failed_login_keeps_challenge_for_a_corrected_password_retry(self):
        salt = await self.issue_challenge()
        wrong_password = await self.core.handle_datagram(
            build_login_request(salt=salt, password="wrong-password"), self.endpoint)

        self.assertEqual(wrong_password[4], AuthErrorCode.WRONG_PASSWORD)
        self.assertIn(self.endpoint, self.core.challenges)
        corrected = await self.core.handle_datagram(
            build_login_request(salt=salt), self.endpoint)
        self.assertEqual(corrected[0], 0x04)
        self.assertEqual(self.core.metrics.logins_succeeded, 1)

    async def test_login_without_live_challenge_is_silently_dropped(self):
        self.assertIsNone(await self.core.handle_datagram(build_login_request(), self.endpoint))
        salt = await self.issue_challenge()
        self.clock.value += 11
        self.assertIsNone(await self.core.handle_datagram(
            build_login_request(salt=salt), self.endpoint))
        self.assertEqual(self.core.metrics.datagrams_dropped, 2)

    async def test_unknown_disabled_and_wrong_password_are_indistinguishable(self):
        cases = (
            ("unknown-user", "local-test-password", self.config),
            ("student-test", "local-test-password", replace(
                self.config, accounts={"student-test": replace(self.account, enabled=False)})),
            ("student-test", "wrong-password", self.config),
        )
        for username, password, config in cases:
            with self.subTest(username=username, password=password):
                core = self.make_core(config=config)
                response = await self.login(core, username=username, password=password)
                self.assertEqual(response[4], AuthErrorCode.WRONG_PASSWORD)
                self.assertFalse(core.sessions)
                self.assertFalse(core.account_sessions)

    async def test_structural_and_crc_failures_are_silently_dropped_before_business_rules(self):
        salt = await self.issue_challenge()
        malformed = bytearray(build_login_request(salt=salt))
        malformed[312] = 0
        self.assertIsNone(await self.core.handle_datagram(bytes(malformed), self.endpoint))

        salt = await self.issue_challenge()
        corrupt = bytearray(build_login_request(salt=salt))
        corrupt[200] ^= 1
        self.assertIsNone(await self.core.handle_datagram(bytes(corrupt), self.endpoint))
        self.assertFalse(self.core.sessions)

    async def test_login_error_precedence_and_all_business_codes(self):
        cases = (
            ("frozen", {"frozen": True}, {}, AuthErrorCode.ACCOUNT_FROZEN),
            ("funds", {"balance_cents": 0}, {}, AuthErrorCode.INSUFFICIENT_FUNDS),
            ("version", {}, {"auth_version": b"\0\0"}, AuthErrorCode.WRONG_VERSION),
            ("bound_pair", {"expected_ipv4": IPv4Address("10.0.0.9")}, {},
             AuthErrorCode.WRONG_IP_MAC_BIND),
            ("ip", {"bind_ipv4_and_mac": False, "expected_ipv4": IPv4Address("10.0.0.9")}, {},
             AuthErrorCode.WRONG_IP),
            ("mac", {"bind_ipv4_and_mac": False, "expected_mac": b"\x02\0\0\0\0\x09"}, {},
             AuthErrorCode.WRONG_MAC),
            ("dhcp", {"require_dhcp": True}, {"dhcp": "0.0.0.0"}, AuthErrorCode.FORCE_DHCP),
        )
        for name, account_changes, packet_changes, expected in cases:
            with self.subTest(name=name):
                account = replace(self.account, **account_changes)
                config = replace(self.config, accounts={account.username: account})
                core = self.make_core(config=config)
                response = await self.login(core, **packet_changes)
                self.assertEqual(response[4], expected)
                self.assertFalse(core.sessions)
                self.assertEqual(core.metrics.logins_rejected, 1)

    async def test_fixed_jlu_fields_precede_binding_checks(self):
        account = replace(self.account, expected_ipv4=IPv4Address("10.0.0.9"))
        core = self.make_core(config=replace(
            self.config, accounts={account.username: account}))
        salt = await self.issue_challenge(core)
        packet = bytearray(build_login_request(salt=salt))
        packet[56] = 0
        refresh_login_crc(packet)

        response = await core.handle_datagram(bytes(packet), self.endpoint)
        self.assertEqual(response[4], AuthErrorCode.WRONG_VERSION)

    async def test_busy_scenario_precedes_every_authentication_validation(self):
        core = self.make_core(builtin_scenario("busy-then-success"))
        salt = await self.issue_challenge(core)
        invalid = bytearray(build_login_request(salt=salt))
        invalid[312] = 0

        response = await core.handle_datagram(bytes(invalid), self.endpoint)
        self.assertEqual(response[4], AuthErrorCode.SERVER_BUSY)
        self.assertFalse(core.sessions)

    async def test_one_session_account_rejects_second_endpoint(self):
        await self.login()
        second = ("127.0.0.1", 50001)
        response = await self.login(endpoint=second)

        self.assertEqual(response[4], AuthErrorCode.IN_USE_WIRED)
        self.assertEqual(self.core.account_sessions, {"student-test": {self.endpoint}})

    async def test_multi_session_limit_returns_too_many_ip_without_index_mutation(self):
        account = replace(self.account, max_sessions=2)
        core = self.make_core(config=replace(
            self.config, accounts={account.username: account}))
        first = ("127.0.0.1", 50000)
        second = ("127.0.0.1", 50001)
        third = ("127.0.0.1", 50002)
        await self.login(core, first)
        await self.login(core, second)
        response = await self.login(core, third)

        self.assertEqual(response[4], AuthErrorCode.TOO_MANY_IP)
        self.assertEqual(core.account_sessions, {"student-test": {first, second}})
        self.assertNotIn(third, core.sessions)

    async def test_malformed_challenge_and_unknown_operation_are_dropped(self):
        self.assertIsNone(await self.core.handle_datagram(b"\x01\x02", self.endpoint))
        self.assertIsNone(await self.core.handle_datagram(b"\x99", self.endpoint))
        self.assertFalse(self.core.challenges)
        self.assertEqual(self.core.metrics.datagrams_dropped, 2)

    async def test_delay_action_waits_then_processes_the_challenge(self):
        core = self.make_core(ScenarioProgram(actions={
            Operation.CHALLENGE: (ScenarioAction(ActionKind.DELAY, delay_milliseconds=10),),
        }))
        started_at = time.perf_counter()
        response = await core.handle_datagram(b"\x01\x02" + b"\0" * 18, self.endpoint)

        self.assertGreaterEqual(time.perf_counter() - started_at, 0.005)
        self.assertEqual(response[0], 0x02)
        self.assertIn(self.endpoint, core.challenges)
        self.assertEqual(core.metrics.datagrams_received, 1)
        self.assertEqual(core.metrics.datagrams_dropped, 0)

    async def test_truncate_and_wrong_opcode_actions_mutate_challenge_responses(self):
        truncate_core = self.make_core(ScenarioProgram(actions={
            Operation.CHALLENGE: (ScenarioAction(ActionKind.TRUNCATE, truncate_length=5),),
        }))
        truncated = await truncate_core.handle_datagram(
            b"\x01\x02" + b"\0" * 18, self.endpoint)
        self.assertEqual(truncated[0], 0x02)
        self.assertEqual(len(truncated), 5)
        self.assertEqual(truncate_core.metrics.datagrams_received, 1)

        opcode_core = self.make_core(ScenarioProgram(actions={
            Operation.CHALLENGE: (ScenarioAction(ActionKind.WRONG_OPCODE, wrong_opcode=0x99),),
        }))
        wrong_opcode = await opcode_core.handle_datagram(
            b"\x01\x02" + b"\0" * 18, self.endpoint)
        self.assertEqual(wrong_opcode[0], 0x99)
        self.assertEqual(len(wrong_opcode), 16)
        self.assertIn(self.endpoint, opcode_core.challenges)

    async def test_expire_session_action_removes_session_and_account_index(self):
        core = self.make_core(ScenarioProgram(actions={
            Operation.CHALLENGE: (
                ScenarioAction(ActionKind.NORMAL),
                ScenarioAction(ActionKind.EXPIRE_SESSION),
            ),
        }))
        await self.login(core)

        response = await core.handle_datagram(b"\x01\x02" + b"\0" * 18, self.endpoint)
        self.assertEqual(response[0], 0x02)
        self.assertNotIn(self.endpoint, core.sessions)
        self.assertNotIn("student-test", core.account_sessions)
        self.assertEqual(core.metrics.logins_succeeded, 1)

    async def test_ka1_rejects_each_invalid_credential_without_refreshing_activity(self):
        session = await self.authenticated_session()
        initial_activity = session.last_activity
        invalid_packets = (
            build_ka1_request(b"bad!", "local-test-password", session.auth_info),
            build_ka1_request(
                session.login_salt, "wrong-password", session.auth_info),
            build_ka1_request(
                session.login_salt, "local-test-password", b"x" * 16),
        )
        for packet in invalid_packets:
            with self.subTest(packet=packet):
                self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))
                self.assertIs(self.core.sessions[self.endpoint], session)
                self.assertEqual(session.last_activity, initial_activity)

        good = build_ka1_request(
            session.login_salt, "local-test-password", session.auth_info)
        self.assertEqual((await self.core.handle_datagram(good, self.endpoint))[0], 0x07)

    async def test_accepts_drcom_core_initial_1_1_3_sequence(self):
        await self.authenticated_session()
        response0 = await self.send_ka2(0, 1, b"\0" * 4, b"\x0f\x27")
        response1 = await self.send_ka2(1, 1, response0[16:20], b"\xdc\x02")
        response2 = await self.send_ka2(2, 3, response1[16:20], b"\xdc\x02")
        self.assertEqual(self.core.sessions[self.endpoint].expected_ka2_serial, 3)
        self.assertEqual(response2[5], 3)

    async def test_accepts_projectlains_initial_1_3_sequence(self):
        await self.authenticated_session()
        response0 = await self.send_ka2(0, 1, b"\0" * 4, b"\x0f\x27")
        response1 = await self.send_ka2(1, 3, response0[16:20], b"\xdc\x02")
        self.assertEqual(response1[5], 3)

    async def test_ka2_alternates_strictly_after_projectlains_bootstrap(self):
        await self.authenticated_session()
        tail = b"\0" * 4
        for serial, packet_type, version in (
                (0, 1, b"\x0f\x27"),
                (1, 3, b"\xdc\x02"),
                (2, 1, b"\xdc\x02"),
                (3, 3, b"\xdc\x02")):
            response = await self.send_ka2(serial, packet_type, tail, version)
            self.assertIsNotNone(response)
            self.assertEqual(response[5], packet_type)
            tail = response[16:20]
        self.assertEqual(self.core.sessions[self.endpoint].expected_ka2_serial, 4)

    async def test_ka2_serial_wraps_from_255_to_zero(self):
        session = await self.authenticated_session()
        session.expected_ka2_serial = 255
        first = await self.send_ka2(255, 1, b"\0" * 4, b"\x0f\x27")
        self.assertIsNotNone(first)
        self.assertEqual(session.expected_ka2_serial, 0)
        wrapped = await self.send_ka2(0, 3, first[16:20], b"\xdc\x02")
        self.assertIsNotNone(wrapped)
        self.assertEqual(wrapped[5], 3)
        self.assertEqual(session.expected_ka2_serial, 1)

    async def test_duplicate_ka2_retransmission_is_idempotent(self):
        await self.authenticated_session()
        packet = build_ka2_request(0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27")
        first = await self.core.handle_datagram(packet, self.endpoint)
        self.assertEqual(first[0], 0x07)
        billing = self.core.sessions[self.endpoint].billing.traffic_kib
        second = await self.core.handle_datagram(packet, self.endpoint)
        self.assertEqual(second, first)
        self.assertEqual(self.core.sessions[self.endpoint].billing.traffic_kib, billing)

    async def test_wrong_ka2_tail_serial_version_type_and_ip_are_silently_dropped(self):
        await self.authenticated_session()
        wrong_serial = build_ka2_request(1, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27")
        self.assertIsNone(await self.core.handle_datagram(wrong_serial, self.endpoint))
        first = await self.send_ka2(0, 1, b"\0" * 4, b"\x0f\x27")
        tail = first[16:20]
        invalid_packets = (
            build_ka2_request(1, 3, b"bad!", "10.0.0.2", b"\xdc\x02"),
            build_ka2_request(1, 3, tail, "10.0.0.2", b"\x00\x00"),
            build_ka2_request(1, 2, tail, "10.0.0.2", b"\xdc\x02"),
            build_ka2_request(1, 3, tail, "10.0.0.9", b"\xdc\x02"),
        )
        for packet in invalid_packets:
            self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))

    async def test_session_expiry_removes_account_index(self):
        await self.authenticated_session()
        self.clock.advance(91)
        response = await self.core.handle_datagram(
            build_ka1_request(b"salt", "local-test-password", b"x" * 16), self.endpoint)
        self.assertIsNone(response)
        self.assertNotIn(self.endpoint, self.core.sessions)
        self.assertNotIn("student-test", self.core.account_sessions)

    async def test_logout_accepts_fresh_or_login_salt_and_is_idempotent(self):
        salts = iter((b"\x01\x02\x03\x04", b"\x05\x06\x07\x08"))
        core = self.make_core(random_bytes=lambda size: next(salts))
        response = await self.login(core)
        self.assertEqual(response[0], 0x04)
        session = core.sessions[self.endpoint]
        fresh = await self.issue_challenge(core)
        packet = build_logout_request(
            "student-test", "local-test-password", fresh, session.mac, session.auth_info)
        self.assertEqual(await core.handle_datagram(packet, self.endpoint), b"\x04\0\0\0")
        self.assertEqual(await core.handle_datagram(packet, self.endpoint), b"\x04\0\0\0")
        login_salt_packet = build_logout_request(
            "student-test", "local-test-password", session.login_salt,
            session.mac, session.auth_info)
        self.assertNotEqual(login_salt_packet, packet)
        self.assertIsNone(await core.handle_datagram(login_salt_packet, self.endpoint))
        self.clock.advance(2.01)
        self.assertIsNone(await core.handle_datagram(packet, self.endpoint))
        self.assertNotIn(self.endpoint, core.sessions)

    async def test_logout_tombstone_expires_after_two_seconds(self):
        session = await self.authenticated_session()
        packet = build_logout_request(
            "student-test", "local-test-password", session.login_salt,
            session.mac, session.auth_info)
        self.assertEqual(await self.core.handle_datagram(packet, self.endpoint), b"\x04\0\0\0")
        self.clock.advance(2.01)
        self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))

    async def test_wrong_tail_action_mutates_ka2_response_not_session_state(self):
        core = self.make_core(ScenarioProgram(actions={
            Operation.KA2: (ScenarioAction(ActionKind.WRONG_TAIL),),
        }))
        await self.login(core)
        response = await core.handle_datagram(
            build_ka2_request(0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27"),
            self.endpoint)
        session = core.sessions[self.endpoint]
        self.assertNotEqual(response[16:20], session.ka2_tail)
        self.assertNotEqual(response, session.last_ka2_response)
        canonical = session.last_ka2_response
        self.assertEqual(await core.handle_datagram(
            build_ka2_request(0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27"),
            self.endpoint), canonical)

    async def test_forged_logout_does_not_remove_session(self):
        session = await self.authenticated_session()
        forged = build_logout_request(
            "student-test", "wrong-password", session.login_salt,
            session.mac, session.auth_info)
        self.assertIsNone(await self.core.handle_datagram(forged, self.endpoint))
        self.assertIn(self.endpoint, self.core.sessions)


if __name__ == "__main__":
    unittest.main()
