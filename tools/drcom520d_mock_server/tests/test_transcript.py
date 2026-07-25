import json
import unittest
from pathlib import Path

from drcom520d_mock_server.transcript import (
    TranscriptFormatError,
    TranscriptMismatch,
    align_transcripts,
    decision_record,
    extract_transcript_records,
    packet_record,
)


def packet_pair(
    payload: bytes = b"x",
    *,
    operation: str = "challenge",
    client_direction: str = "send",
) -> tuple[dict, dict]:
    server_direction = "receive" if client_direction == "send" else "send"
    client = packet_record(
        side="client", direction=client_direction, operation=operation,
        payload=payload, local_endpoint=("10.0.0.2", 51000),
        remote_endpoint=("127.0.0.1", 61440))
    server = packet_record(
        side="server", direction=server_direction, operation=operation,
        payload=payload, local_endpoint=("127.0.0.1", 61440),
        remote_endpoint=("10.0.0.2", 52000))
    return client, server


def success_decisions(operation: str = "challenge") -> tuple[dict, dict]:
    return (
        decision_record(
            side="client", operation=operation, outcome="success",
            wire_error_code=None, drop_reason=None),
        decision_record(
            operation=operation, outcome="success",
            wire_error_code=None, drop_reason=None),
    )


class TranscriptTests(unittest.TestCase):
    def test_go_client_fixture_aligns_with_server_records(self):
        fixture = Path(__file__).parent / "fixtures" / "go_client_transcript_v1.jsonl"
        client = [
            json.loads(line)
            for line in fixture.read_text(encoding="utf-8").splitlines()
        ]
        challenge = bytes.fromhex("0102" + "00" * 18)
        response = bytes.fromhex("02000000010203047f00000100000000")
        server = [
            packet_record(
                side="server", direction="receive", operation="challenge",
                payload=challenge, local_endpoint=("127.0.0.1", 61440),
                remote_endpoint=("127.0.0.1", 52000)),
            packet_record(
                side="server", direction="send", operation="challenge",
                payload=response, local_endpoint=("127.0.0.1", 61440),
                remote_endpoint=("127.0.0.1", 53000)),
            decision_record(
                operation="challenge", outcome="success",
                wire_error_code=None, drop_reason=None),
        ]
        self.assertEqual(align_transcripts(client, server), [])

    def test_compare_reports_first_payload_mismatch(self):
        client = [packet_record(
            side="client", direction="send", operation="login",
            payload=b"a", local_endpoint=None, remote_endpoint=None)]
        server = [packet_record(
            side="server", direction="receive", operation="login",
            payload=b"b", local_endpoint=None, remote_endpoint=None)]
        mismatches = align_transcripts(client, server)
        self.assertEqual(len(mismatches), 1)
        self.assertIsInstance(mismatches[0], TranscriptMismatch)
        self.assertEqual(mismatches[0].index, 0)
        self.assertEqual(mismatches[0].field_path, "payload_hex")

    def test_extracts_packet_and_decision_records_from_full_server_trace(self):
        record = packet_record(
            side="server", direction="receive", operation="ka1",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        decision = decision_record(
            operation="ka1", outcome="drop", wire_error_code=None,
            drop_reason="ka1_digest_mismatch")
        rows = [{"event": "datagram_received", "details": record},
                {"event": "datagram_dropped", "details": decision},
                {"event": "crypto_check", "details": {"valid": True}}]
        self.assertEqual(extract_transcript_records(rows), [record, decision])

    def test_alignment_rejects_wrong_direction_mapping(self):
        client = [packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)]
        server = [packet_record(
            side="server", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)]
        mismatch = align_transcripts(client, server)[0]
        self.assertIn("direction", mismatch.reason)

    def test_alignment_rejects_invalid_version_as_format_error(self):
        valid_server = packet_record(
            side="server", direction="receive", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        invalid_client = dict(packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None))
        invalid_client["transcript_version"] = 2
        with self.assertRaisesRegex(TranscriptFormatError, "transcript_version"):
            align_transcripts([invalid_client], [valid_server])

    def test_alignment_reports_first_missing_record(self):
        first = packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        second = packet_record(
            side="client", direction="receive", operation="challenge",
            payload=b"y", local_endpoint=None, remote_endpoint=None)
        server = [packet_record(
            side="server", direction="receive", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)]
        mismatch = align_transcripts([first, second], server)[0]
        self.assertEqual(mismatch.index, 1)
        self.assertIs(mismatch.server, None)

    def test_alignment_compares_decision_outcome(self):
        client, server = success_decisions("login")
        server["outcome"] = "failure"
        mismatch = align_transcripts([client], [server])[0]
        self.assertEqual(mismatch.field_path, "outcome")

    def test_packet_and_decision_streams_align_independently_in_type_order(self):
        client_packet_1, server_packet_1 = packet_pair(b"one")
        client_packet_2, server_packet_2 = packet_pair(
            b"two", operation="ka1", client_direction="receive")
        client_decision_1, server_decision_1 = success_decisions("challenge")
        client_decision_2, server_decision_2 = success_decisions("ka1")
        client = [
            client_packet_1, client_decision_1,
            client_packet_2, client_decision_2,
        ]
        server = [
            server_packet_1, server_packet_2,
            server_decision_1, server_decision_2,
        ]
        self.assertEqual(align_transcripts(client, server), [])

        reversed_server = [
            server_packet_1, server_packet_2,
            dict(server_decision_2, outcome="failure"), server_decision_1,
        ]
        mismatch = align_transcripts(client, reversed_server)[0]
        self.assertEqual(
            (mismatch.record_type, mismatch.index, mismatch.field_path),
            ("decision", 0, "outcome"),
        )

    def test_endpoint_objects_and_arrays_normalize_and_ignore_only_client_port(self):
        client, server = packet_pair()
        client["local_endpoint"] = {"host": "10.0.0.2", "port": 51000}
        client["remote_endpoint"] = {"host": "127.0.0.1", "port": 61440}
        server["local_endpoint"] = ["127.0.0.1", 61440]
        server["remote_endpoint"] = ["10.0.0.2", 62000]
        self.assertEqual(align_transcripts([client], [server]), [])

    def test_server_endpoint_host_and_port_are_strict(self):
        client, server = packet_pair()
        changes = (
            (["127.0.0.2", 61440], "endpoint.server.host"),
            (["127.0.0.1", 61441], "endpoint.server.port"),
        )
        for local_endpoint, field_path in changes:
            with self.subTest(field_path=field_path):
                mismatch = align_transcripts(
                    [client], [dict(server, local_endpoint=local_endpoint)])[0]
                self.assertEqual(
                    (mismatch.record_type, mismatch.index, mismatch.field_path),
                    ("packet", 0, field_path),
                )

    def test_client_host_is_strict_but_client_port_is_not(self):
        client, server = packet_pair()
        self.assertEqual(
            align_transcripts(
                [client], [dict(server, remote_endpoint=["10.0.0.2", 65000])]),
            [],
        )
        mismatch = align_transcripts(
            [client], [dict(server, remote_endpoint=["10.0.0.3", 52000])])[0]
        self.assertEqual(mismatch.field_path, "endpoint.client.host")

    def test_payload_hex_hash_and_length_self_integrity_is_mandatory(self):
        client, server = packet_pair(b"payload")
        changes = {
            "payload_hex": "not-hex",
            "payload_sha256": "0" * 64,
            "payload_length": 999,
        }
        for field_path, value in changes.items():
            with self.subTest(field_path=field_path):
                with self.assertRaisesRegex(TranscriptFormatError, field_path):
                    align_transcripts(
                        [dict(client, **{field_path: value})],
                        [dict(server, **{field_path: value})],
                    )

    def test_operation_and_reverse_direction_are_stable(self):
        client, server = packet_pair()
        cases = (
            (dict(server, operation="login"), "operation"),
            (dict(server, direction="send"), "direction"),
        )
        for changed, field_path in cases:
            with self.subTest(field_path=field_path):
                mismatch = align_transcripts([client], [changed])[0]
                self.assertEqual(mismatch.field_path, field_path)

    def test_decision_fields_are_strict_outside_explicit_mapping(self):
        client, server = success_decisions("login")
        changes = {
            "outcome": "failure",
            "wire_error_code": 3,
            "drop_reason": "wrong_password",
        }
        for field_path, value in changes.items():
            with self.subTest(field_path=field_path):
                mismatch = align_transcripts(
                    [client], [dict(server, **{field_path: value})])[0]
                self.assertEqual(
                    (mismatch.record_type, mismatch.index, mismatch.field_path),
                    ("decision", 0, field_path),
                )

    def test_server_drop_maps_to_client_timeout_only_under_exact_conditions(self):
        client = decision_record(
            side="client", operation="login", outcome="timeout",
            wire_error_code=None, drop_reason="timeout")
        server = decision_record(
            operation="login", outcome="drop", wire_error_code=None,
            drop_reason="login_challenge_missing")
        self.assertEqual(align_transcripts([client], [server]), [])

        invalid_servers = (
            dict(server, drop_reason=None),
            dict(server, drop_reason=""),
            dict(server, wire_error_code=3),
        )
        for invalid in invalid_servers:
            with self.subTest(invalid=invalid):
                self.assertTrue(align_transcripts([client], [invalid]))

    def test_first_mismatch_reports_type_stream_index_field_and_full_records(self):
        client_packet, server_packet = packet_pair(b"first")
        _, different_server_packet = packet_pair(b"second")
        client_decision, server_decision = success_decisions()
        mismatch = align_transcripts(
            [client_decision, client_packet],
            [dict(server_decision, outcome="failure"),
             different_server_packet],
        )[0]
        self.assertIsInstance(mismatch, TranscriptMismatch)
        self.assertEqual(
            (mismatch.record_type, mismatch.index, mismatch.field_path),
            ("packet", 0, "payload_hex"),
        )
        self.assertEqual(mismatch.client, client_packet)
        self.assertEqual(mismatch.server, different_server_packet)

    def test_length_mismatch_reports_first_missing_record_in_that_stream(self):
        client_packet_1, server_packet_1 = packet_pair(b"one")
        client_packet_2, _ = packet_pair(b"two")
        mismatch = align_transcripts(
            [client_packet_1, client_packet_2], [server_packet_1])[0]
        self.assertEqual(
            (mismatch.record_type, mismatch.index, mismatch.field_path),
            ("packet", 1, "record"),
        )
        self.assertIsNone(mismatch.server)

    def test_version_and_side_are_schema_errors(self):
        client, server = packet_pair()
        with self.assertRaisesRegex(TranscriptFormatError, "transcript_version"):
            align_transcripts([dict(client, transcript_version=2)], [server])

        client_decision, server_decision = success_decisions()
        with self.assertRaisesRegex(TranscriptFormatError, "side"):
            align_transcripts(
                [client_decision], [dict(server_decision, side="client")])

    def test_decision_record_defaults_to_server_and_allows_client(self):
        server = decision_record(
            operation="login", outcome="success",
            wire_error_code=None, drop_reason=None)
        client = decision_record(
            side="client", operation="login", outcome="success",
            wire_error_code=None, drop_reason=None)
        self.assertEqual(server["side"], "server")
        self.assertEqual(client["side"], "client")

    def test_unknown_record_type_is_never_filtered_even_when_both_sides_match(self):
        for record_type in ("packte", ["packet"]):
            with self.subTest(record_type=record_type):
                malformed = {
                    "transcript_version": 1,
                    "record_type": record_type,
                }
                with self.assertRaisesRegex(TranscriptFormatError, "record_type"):
                    align_transcripts([malformed], [malformed])

    def test_decision_requires_every_schema_field(self):
        client, server = success_decisions("login")
        for field in (
            "transcript_version", "record_type", "side", "operation",
            "outcome", "wire_error_code", "drop_reason",
        ):
            with self.subTest(field=field):
                malformed_client = dict(client)
                malformed_server = dict(server)
                malformed_client.pop(field)
                malformed_server.pop(field)
                with self.assertRaisesRegex(TranscriptFormatError, field):
                    align_transcripts([malformed_client], [malformed_server])

    def test_packet_requires_schema_and_canonical_payload(self):
        client, server = packet_pair(bytes.fromhex("ab"))
        cases = (
            ("direction", None),
            ("payload_hex", "AB"),
            ("payload_hex", "not-hex"),
            ("operation", 3),
        )
        for field, value in cases:
            with self.subTest(field=field, value=value):
                malformed = dict(client)
                if value is None:
                    malformed.pop(field)
                else:
                    malformed[field] = value
                with self.assertRaisesRegex(TranscriptFormatError, field):
                    align_transcripts([malformed], [server])

    def test_endpoint_schema_errors_precede_semantic_alignment(self):
        client, server = packet_pair()
        malformed = dict(
            client,
            local_endpoint={"host": "10.0.0.2", "port": "51000"},
        )
        with self.assertRaisesRegex(
            TranscriptFormatError, "local_endpoint.port"
        ):
            align_transcripts([malformed], [server])

    def test_extract_ignores_ordinary_details_but_rejects_tagged_malformed(self):
        self.assertEqual(
            extract_transcript_records([
                {"event": "crypto_check", "details": {"valid": True}}]),
            [],
        )
        malformed_details = (
            {"transcript_version": 1, "record_type": "packte"},
            {"transcript_version": 2, "record_type": "packet"},
            {"record_type": "decision", "side": "server"},
        )
        for details in malformed_details:
            with self.subTest(details=details):
                with self.assertRaises(TranscriptFormatError):
                    extract_transcript_records([{"details": details}])


if __name__ == "__main__":
    unittest.main()
