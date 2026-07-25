import hashlib
import unittest
from dataclasses import replace
from ipaddress import IPv4Address

from drcom520d_mock_server.models import (
    Account,
    ApplicationConfig,
    Operation,
    PendingChallenge,
    ServerSettings,
)
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


class SequenceClock:
    def __init__(self, *values: float):
        self.values = values
        self.calls = 0

    def __call__(self) -> float:
        value = self.values[min(self.calls, len(self.values) - 1)]
        self.calls += 1
        return value


class MemoryTrace:
    def __init__(self):
        self.events: list[dict[str, object]] = []

    def emit(self, event, *, operation=None, endpoint=None, details=None):
        self.events.append({
            "event": event,
            "operation": operation,
            "endpoint": endpoint,
            "details": details or {},
        })
        return None

    def named(self, name: str):
        return [item for item in self.events if item["event"] == name]


class ServerTraceTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.clock = FakeClock()
        self.trace = MemoryTrace()
        self.endpoint = ("127.0.0.1", 50000)
        self.local_endpoint = ("127.0.0.1", 61440)
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
            accounts={self.account.username: self.account},
        )
        self.core = self.make_core()

    def make_core(self, *, config=None, scenario=None, random_bytes=None):
        return ServerCore(
            config or self.config,
            scenario or builtin_scenario("normal"),
            clock=self.clock,
            trace=self.trace,
            random_bytes=random_bytes or (lambda length: (
                b"\x01\x02\x03\x04" if length == 4 else b"s" * length)),
        )

    async def issue_challenge(self, core=None):
        core = core or self.core
        response = await core.handle_datagram(
            b"\x01\x02" + b"\0" * 18,
            self.endpoint,
            local_endpoint=self.local_endpoint,
        )
        self.assertEqual(response[0], 0x02)
        return response[4:8]

    async def login(self, core=None):
        core = core or self.core
        salt = await self.issue_challenge(core)
        response = await core.handle_datagram(
            build_login_request(salt=salt), self.endpoint)
        self.assertEqual(response[0], 0x04)
        return core.sessions[self.endpoint]

    async def test_ka2_trace_contains_derivation_billing_and_retransmission(self):
        session = await self.login()
        packet = build_ka2_request(
            0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27")
        self.clock.advance(1)

        first = await self.core.handle_datagram(packet, self.endpoint)
        state_after_first = self.core.state_snapshot()
        second = await self.core.handle_datagram(packet, self.endpoint)

        self.assertEqual(first, second)
        state_after_second = self.core.state_snapshot()
        self.assertEqual(
            state_after_second["sessions"], state_after_first["sessions"])
        self.assertEqual(
            state_after_second["account_sessions"],
            state_after_first["account_sessions"],
        )
        self.assertEqual(
            state_after_second["metrics"]["keepalives_accepted"],
            state_after_first["metrics"]["keepalives_accepted"],
        )
        derived_events = self.trace.named("ka2_tail_derived")
        self.assertTrue(derived_events, "missing ka2_tail_derived event")
        derived = derived_events[-1]["details"]
        self.assertEqual(derived["secret"], b"s" * 32)
        self.assertEqual(derived["input"], (
            b"ka2-tail\0" + session.auth_info + b"\0" * 4 + b"\0\x01"))
        self.assertEqual(derived["old_tail"], b"\0" * 4)
        self.assertEqual(derived["new_tail"], first[16:20])
        retransmit_events = self.trace.named("ka2_retransmission")
        self.assertTrue(retransmit_events, "missing ka2_retransmission event")
        retransmit = retransmit_events[-1]["details"]
        self.assertEqual(retransmit["cached_request"], packet)
        self.assertEqual(retransmit["cached_response"], first)
        self.assertFalse(retransmit["state_advanced"])

        changed = next(
            item["details"] for item in self.trace.named("state_changed")
            if item["details"].get("reason") == "ka2_accepted")
        before_session = changed["before"]["sessions"]["127.0.0.1:50000"]
        after_session = changed["after"]["sessions"]["127.0.0.1:50000"]
        self.assertEqual(before_session["billing"]["traffic_kib"], 14208)
        self.assertEqual(after_session["billing"]["traffic_kib"], 14209)
        self.assertEqual(before_session["expected_ka2_serial"], 0)
        self.assertEqual(after_session["expected_ka2_serial"], 1)
        self.assertEqual(before_session["last_ka2_type"], None)
        self.assertEqual(after_session["last_ka2_type"], 1)
        self.assertEqual(before_session["ka2_tail"], b"\0" * 4)
        self.assertEqual(after_session["ka2_tail"], first[16:20])
        self.assertEqual(before_session["last_activity"], 1000.0)
        self.assertEqual(after_session["last_activity"], 1001.0)
        self.assertEqual(after_session["last_ka2_response"], first)

    async def test_invalid_ka1_trace_contains_password_salt_and_auth_info(self):
        session = await self.login()
        initial_activity = session.last_activity
        bad = build_ka1_request(
            session.login_salt, "wrong-password", session.auth_info)

        self.assertIsNone(await self.core.handle_datagram(bad, self.endpoint))
        self.assertEqual(session.last_activity, initial_activity)
        parsed_events = [
            item for item in self.trace.named("packet_parsed")
            if item["operation"] == "ka1"
        ]
        self.assertTrue(parsed_events, "missing KA1 packet_parsed event")
        parsed = parsed_events[-1]["details"]
        self.assertEqual(parsed["packet"], bad)
        self.assertEqual(parsed["request"].auth_info, session.auth_info)
        check = self.trace.named("crypto_check")[-1]["details"]
        self.assertEqual(check["username"], "student-test")
        self.assertEqual(check["password"], "local-test-password")
        self.assertEqual(check["salt"], session.login_salt)
        self.assertEqual(check["received_auth_info"], session.auth_info)
        self.assertEqual(check["expected_auth_info"], session.auth_info)
        self.assertTrue(check["auth_info_valid"])
        self.assertFalse(check["digest"].valid)
        self.assertEqual(check["digest"].received, bad[1:17])
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "ka1_authentication_failed",
        )

        self.clock.advance(1)
        good = build_ka1_request(
            session.login_salt, "local-test-password", session.auth_info)
        response = await self.core.handle_datagram(good, self.endpoint)
        self.assertEqual(response[0], 0x07)
        changed = next(
            item["details"] for item in self.trace.named("state_changed")
            if item["details"].get("reason") == "ka1_accepted")
        self.assertEqual(changed["last_activity_before"], initial_activity)
        self.assertEqual(changed["last_activity_after"], self.clock.value)
        self.assertEqual(
            changed["after"]["sessions"]["127.0.0.1:50000"]["last_activity"],
            self.clock.value,
        )
        created = self.trace.named("response_created")[-1]["details"]
        self.assertEqual(created["request"], good)
        self.assertEqual(created["response"], response)

    async def test_logout_tombstone_and_expiry_are_traced(self):
        salts = iter((b"\x01\x02\x03\x04", b"\x05\x06\x07\x08"))
        core = self.make_core(random_bytes=lambda length: next(salts))
        session = await self.login(core)
        fresh_salt = await self.issue_challenge(core)
        packet = build_logout_request(
            "student-test", "local-test-password", fresh_salt,
            session.mac, session.auth_info)

        ack = await core.handle_datagram(packet, self.endpoint)

        self.assertEqual(ack, b"\x04\0\0\0")
        created_events = self.trace.named("logout_tombstone_created")
        self.assertTrue(created_events, "missing logout_tombstone_created event")
        created = created_events[-1]["details"]
        self.assertEqual(created["request"], packet)
        self.assertEqual(created["request_digest"], hashlib.sha256(packet).digest())
        self.assertEqual(created["ack"], ack)
        self.assertEqual(created["expires_at"], self.clock.value + 2)
        self.assertEqual(created["expires_in_seconds"], 2)
        self.assertNotIn(self.endpoint, core.sessions)
        checks = [
            item["details"] for item in self.trace.named("crypto_check")
            if item["operation"] == "logout"
        ]
        self.assertEqual([check["salt"] for check in checks], [
            session.login_salt, fresh_salt])
        self.assertEqual([check["digest"].valid for check in checks], [False, True])
        self.assertTrue(all(check["mac_valid"] for check in checks))
        self.assertTrue(all(check["auth_info_valid"] for check in checks))

        repeated = await core.handle_datagram(packet, self.endpoint)
        self.assertEqual(repeated, ack)
        tombstone_hit = self.trace.named("logout_retransmission")[-1]["details"]
        self.assertEqual(tombstone_hit["request_digest"], created["request_digest"])
        self.assertEqual(tombstone_hit["cached_ack"], ack)
        self.assertFalse(tombstone_hit["state_advanced"])

        self.clock.advance(2.01)
        self.assertIsNone(await core.handle_datagram(packet, self.endpoint))
        expired = [
            item["details"] for item in self.trace.named("state_expired")
            if item["details"]["state_type"] == "logout_tombstone"
        ][-1]
        self.assertEqual(expired["endpoint"], self.endpoint)
        self.assertEqual(expired["expired_at"], created["expires_at"])
        self.assertEqual(expired["removed_object"][1], created["request_digest"])

    async def test_challenge_and_session_expiry_trace_removed_objects(self):
        await self.issue_challenge()
        challenge = self.core.challenges[self.endpoint]
        self.clock.advance(10.01)
        self.assertIsNone(await self.core.handle_datagram(b"\xff", self.endpoint))
        challenge_expired_events = [
            item["details"] for item in self.trace.named("state_expired")
            if item["details"]["state_type"] == "challenge"
        ]
        self.assertTrue(challenge_expired_events, "missing challenge state_expired event")
        challenge_expired = challenge_expired_events[-1]
        self.assertEqual(challenge_expired["removed_object"], challenge)
        self.assertEqual(challenge_expired["timestamp"], challenge.issued_at)
        self.assertEqual(challenge_expired["ttl_seconds"], 10)

        self.clock.value = 1000.0
        session = await self.login()
        self.clock.advance(90.01)
        packet = build_ka1_request(
            session.login_salt, "local-test-password", session.auth_info)
        self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))
        session_expired = [
            item["details"] for item in self.trace.named("state_expired")
            if item["details"]["state_type"] == "session"
        ][-1]
        self.assertEqual(session_expired["removed_object"], session)
        self.assertEqual(session_expired["timestamp"], session.last_activity)
        self.assertEqual(session_expired["ttl_seconds"], 90)
        removed = self.trace.named("session_removed")[-1]["details"]
        self.assertIn(
            "127.0.0.1:50000",
            removed["account_index_before"]["student-test"],
        )
        self.assertNotIn("student-test", removed["account_index_after"])

    async def test_wrong_tail_scenario_traces_canonical_and_wire_responses(self):
        core = self.make_core(scenario=ScenarioProgram(actions={
            Operation.KA2: (ScenarioAction(ActionKind.WRONG_TAIL),),
        }))
        await self.login(core)
        packet = build_ka2_request(
            0, 1, b"\0" * 4, "10.0.0.2", b"\x0f\x27")

        wire_response = await core.handle_datagram(packet, self.endpoint)

        canonical = core.sessions[self.endpoint].last_ka2_response
        self.assertNotEqual(wire_response, canonical)
        scenario = self.trace.named("scenario_action_selected")[-1]["details"]
        self.assertIs(scenario["action"].kind, ActionKind.WRONG_TAIL)
        mutation = self.trace.named("response_mutated")[-1]["details"]
        self.assertEqual(mutation["before"], canonical)
        self.assertEqual(mutation["after"], wire_response)
        built_events = self.trace.named("response_created")
        self.assertTrue(built_events, "missing response_created event")
        built = built_events[-1]["details"]
        self.assertEqual(built["response"], canonical)
        self.assertEqual(await core.handle_datagram(packet, self.endpoint), canonical)

    async def test_challenge_and_login_emit_raw_crypto_decision_and_state(self):
        challenge = await self.core.handle_datagram(
            b"\x01\x02" + b"\0" * 18, self.endpoint)
        login_packet = build_login_request(salt=challenge[4:8])
        response = await self.core.handle_datagram(login_packet, self.endpoint)

        self.assertEqual(response[0], 0x04)
        self.assertEqual(
            self.trace.named("datagram_received")[-1]["details"]["packet"],
            login_packet,
        )
        crypto = self.trace.named("crypto_check")[-1]["details"]
        self.assertEqual(crypto["password"], "local-test-password")
        self.assertEqual(crypto["salt"], bytes.fromhex("01020304"))
        self.assertTrue(crypto["verification"].md5_a_valid)
        decisions = self.trace.named("validation_decision")
        self.assertTrue(any(
            item["details"].get("decision") == "login_success"
            for item in decisions))
        state = self.trace.named("state_changed")[-1]["details"]
        self.assertEqual(
            state["after"]["sessions"]["127.0.0.1:50000"]["username"],
            "student-test",
        )

        derived = self.trace.named("auth_info_derived")[-1]["details"]
        self.assertEqual(derived["secret"], b"s" * 32)
        self.assertEqual(derived["output"], response[23:39])
        self.assertEqual(
            derived["input"],
            b"auth-info\0" + b"127.0.0.1\0\xc3Pstudent-test\0"
            + bytes.fromhex("01020304") + b"\0" * 7 + b"\x01",
        )

    async def test_packet_envelope_records_operation_scenario_and_wire_identity(self):
        packet = b"\x01\x02" + b"\0" * 18
        response = await self.core.handle_datagram(
            packet, self.endpoint, local_endpoint=self.local_endpoint)

        names = [item["event"] for item in self.trace.events]
        self.assertLess(names.index("datagram_received"), names.index("operation_classified"))
        self.assertLess(
            names.index("operation_classified"), names.index("scenario_action_selected"))
        self.assertLess(names.index("scenario_action_selected"), names.index("datagram_sent"))

        received = self.trace.named("datagram_received")[-1]
        self.assertEqual(received["operation"], "challenge")
        self.assertEqual(received["details"]["packet"], packet)
        self.assertEqual(received["details"]["length"], len(packet))
        self.assertEqual(received["details"]["first_byte"], 0x01)
        self.assertEqual(received["details"]["payload_hex"], packet.hex())
        self.assertEqual(received["details"]["payload_length"], len(packet))
        self.assertEqual(received["details"]["direction"], "receive")
        self.assertEqual(received["details"]["local_endpoint"], self.local_endpoint)
        self.assertEqual(received["details"]["remote_endpoint"], self.endpoint)

        scenario = self.trace.named("scenario_action_selected")[-1]["details"]
        self.assertEqual(scenario["action"].kind.value, "normal")
        self.assertEqual(scenario["operation_count"], 1)

        sent = self.trace.named("datagram_sent")[-1]["details"]
        self.assertEqual(sent["packet"], response)
        self.assertEqual(sent["payload_hex"], response.hex())
        self.assertEqual(sent["direction"], "send")
        self.assertEqual(sent["local_endpoint"], self.local_endpoint)
        self.assertEqual(sent["remote_endpoint"], self.endpoint)

    async def test_state_snapshot_uses_stable_endpoint_keys(self):
        salt = await self.issue_challenge()
        challenge_snapshot = self.core.state_snapshot()
        self.assertEqual(
            challenge_snapshot["challenges"]["127.0.0.1:50000"]["salt"], salt)
        self.assertNotIn(self.endpoint, challenge_snapshot["challenges"])

        await self.core.handle_datagram(
            build_login_request(salt=salt), self.endpoint)
        session_snapshot = self.core.state_snapshot()
        self.assertEqual(
            session_snapshot["sessions"]["127.0.0.1:50000"]["username"],
            "student-test",
        )
        self.assertEqual(
            session_snapshot["account_sessions"]["student-test"],
            {"127.0.0.1:50000"},
        )

    async def test_unknown_disabled_and_wrong_password_have_distinct_trace_reasons(self):
        cases = (
            ("unknown-user", "local-test-password", self.config, "account_not_found"),
            (
                "student-test",
                "local-test-password",
                replace(
                    self.config,
                    accounts={
                        self.account.username: replace(self.account, enabled=False),
                    },
                ),
                "account_disabled",
            ),
            ("student-test", "wrong-password", self.config,
             "password_digest_mismatch"),
        )
        for username, password, config, expected_reason in cases:
            with self.subTest(expected_reason=expected_reason):
                self.trace.events.clear()
                core = self.make_core(config=config)
                salt = await self.issue_challenge(core)
                response = await core.handle_datagram(
                    build_login_request(
                        username=username, password=password, salt=salt),
                    self.endpoint,
                )
                self.assertEqual(response[4], 0x03)
                decision = self.trace.named("validation_decision")[-1]["details"]
                self.assertEqual(decision["decision"], "login_failure")
                self.assertEqual(decision["reason"], expected_reason)
                self.assertEqual(decision["wire_error_code"], 0x03)
                self.assertEqual(decision["outcome"], "failure")
                self.assertIsNone(decision["drop_reason"])

    async def test_wrong_password_trace_reveals_exact_reason_but_wire_stays_03(self):
        challenge = await self.core.handle_datagram(
            b"\x01\x02" + b"\0" * 18, self.endpoint)
        response = await self.core.handle_datagram(
            build_login_request(salt=challenge[4:8], password="wrong-password"),
            self.endpoint,
        )
        self.assertEqual(response[4], 0x03)
        decision = self.trace.named("validation_decision")[-1]["details"]
        self.assertEqual(decision["reason"], "password_digest_mismatch")
        self.assertEqual(decision["wire_error_code"], 0x03)

    async def test_login_success_records_every_validation_stage_in_wire_order(self):
        salt = await self.issue_challenge()
        response = await self.core.handle_datagram(
            build_login_request(salt=salt), self.endpoint)
        self.assertEqual(response[0], 0x04)

        decisions = [
            item["details"]["decision"]
            for item in self.trace.named("validation_decision")
            if item["operation"] == "login"
        ]
        expected = [
            "account_lookup",
            "account_enabled",
            "password_digests",
            "crc",
            "account_frozen",
            "account_balance",
            "jlu_fields",
            "ip_mac_binding",
            "ip_address",
            "mac_address",
            "dhcp",
            "session_limit",
            "login_success",
        ]
        positions = [decisions.index(name) for name in expected]
        self.assertEqual(positions, sorted(positions))
        by_name = {
            item["details"]["decision"]: item["details"]
            for item in self.trace.named("validation_decision")
            if item["operation"] == "login"
        }
        self.assertEqual(
            [by_name[name]["priority"] for name in expected],
            list(range(1, len(expected) + 1)),
        )

    async def test_malformed_login_trace_names_silent_drop_reason(self):
        self.assertIsNone(await self.core.handle_datagram(
            b"\x03\x01bad", self.endpoint))
        parse_failure = self.trace.named("packet_parse_failed")[-1]["details"]
        self.assertEqual(parse_failure["reason"], "login_parse_failed")
        self.assertIn("packet must contain 330 bytes", parse_failure["parse_error"])
        dropped = self.trace.named("datagram_dropped")[-1]["details"]
        self.assertEqual(dropped["reason"], "login_parse_failed")
        self.assertIn("packet must contain 330 bytes", dropped["parse_error"])
        decision = self.trace.named("validation_decision")[-1]["details"]
        self.assertEqual(decision["outcome"], "drop")
        self.assertEqual(decision["drop_reason"], "login_parse_failed")

    async def test_missing_expired_and_bad_crc_login_drops_have_exact_precedence(self):
        packet = build_login_request()
        self.assertIsNone(await self.core.handle_datagram(packet, self.endpoint))
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "login_challenge_missing",
        )

        self.trace.events.clear()
        salt = await self.issue_challenge()
        self.clock.value += 11
        self.assertIsNone(await self.core.handle_datagram(
            build_login_request(salt=salt), self.endpoint))
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "login_challenge_expired",
        )
        expired_state = next(
            item["details"]
            for item in self.trace.named("state_changed")
            if item["operation"] == "login"
            and item["details"].get("reason") == "challenge_expired"
        )
        self.assertIn("127.0.0.1:50000", expired_state["before"]["challenges"])
        self.assertNotIn("127.0.0.1:50000", expired_state["after"]["challenges"])

        self.trace.events.clear()
        self.clock.value = 1000.0
        salt = await self.issue_challenge()
        corrupt = bytearray(build_login_request(salt=salt))
        corrupt[200] ^= 1
        self.assertIsNone(await self.core.handle_datagram(bytes(corrupt), self.endpoint))
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "login_crc_mismatch",
        )

    async def test_login_expiry_uses_one_now_for_prediction_cleanup_and_trace(self):
        clock = SequenceClock(1010.0, 1010.001)
        core = ServerCore(
            self.config,
            builtin_scenario("normal"),
            clock=clock,
            trace=self.trace,
            random_bytes=lambda length: b"s" * length,
        )
        core.challenges[self.endpoint] = PendingChallenge(
            salt=bytes.fromhex("01020304"), issued_at=1000.0)

        response = await core.handle_datagram(
            build_login_request(salt=bytes.fromhex("01020304")),
            self.endpoint,
        )

        self.assertIsNotNone(response)
        self.assertEqual(response[0], 0x04)
        self.assertEqual(clock.calls, 1)
        self.assertFalse(any(
            item["details"].get("reason") == "challenge_expired"
            for item in self.trace.named("state_changed")
            if item["operation"] == "login"
        ))
        self.assertFalse(any(
            item["details"].get("reason") in {
                "login_challenge_missing", "login_challenge_expired"}
            for item in self.trace.named("datagram_dropped")
        ))

    async def test_challenge_and_unknown_operation_drops_name_exact_reasons(self):
        self.assertIsNone(await self.core.handle_datagram(b"\x01\x02", self.endpoint))
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "challenge_parse_failed",
        )
        self.assertIsNone(await self.core.handle_datagram(b"\x99", self.endpoint))
        self.assertEqual(
            self.trace.named("datagram_dropped")[-1]["details"]["reason"],
            "unknown_operation",
        )

    async def test_login_crc_decision_exposes_official_detailed_comparison(self):
        salt = await self.issue_challenge()
        packet = bytearray(build_login_request(salt=salt))
        packet[81:85] = IPv4Address("10.0.0.9").packed
        refresh_login_crc(packet)
        response = await self.core.handle_datagram(bytes(packet), self.endpoint)

        self.assertEqual(response[4], 0x16)
        crypto = self.trace.named("crypto_check")[-1]["details"]["verification"]
        crc_decision = next(
            item["details"] for item in self.trace.named("validation_decision")
            if item["details"].get("decision") == "crc")
        self.assertIs(crc_decision["comparison"], crypto.crc)
        self.assertTrue(crc_decision["comparison"].valid)


if __name__ == "__main__":
    unittest.main()
