import hashlib
import struct
import unittest

from sidravia_drcom_acceptance.model import (
    ComparisonResult,
    Endpoint,
    EvidenceError,
    PacketRecord,
    SessionSnapshotEvidence,
    TransportObservation,
)
from sidravia_drcom_acceptance.pcap_fixture import (
    FixtureDatagram,
    build_ethernet_ipv4_udp_pcap,
)


def packet_json(payload: bytes) -> dict[str, object]:
    return {
        "transcript_version": 1,
        "record_type": "packet",
        "side": "client",
        "direction": "send",
        "operation": "challenge",
        "payload_hex": payload.hex(),
        "payload_sha256": hashlib.sha256(payload).hexdigest(),
        "payload_length": len(payload),
        "local_endpoint": {"ip": "10.0.0.2", "port": 61440},
        "remote_endpoint": {"ip": "10.100.61.3", "port": 61440},
    }


def transport_json() -> dict[str, object]:
    return {
        "schema_version": 1,
        "run_id": "run-1",
        "authentication_session_id": "acceptance-session-1",
        "socket_local_endpoint": {"ip": "10.0.0.2", "port": 61440},
        "server_endpoint": {"ip": "10.100.61.3", "port": 61440},
    }


class StrictModelTests(unittest.TestCase):
    def test_endpoint_rejects_non_ipv4_and_invalid_ports(self):
        for ip, port in (("::1", 61440), ("not-an-ip", 61440), ("10.0.0.2", 0),
                         ("10.0.0.2", 65536), ("10.0.0.2", True)):
            with self.subTest(ip=ip, port=port):
                with self.assertRaises(EvidenceError):
                    Endpoint(ip, port)

    def test_packet_record_parses_and_validates_payload_identity(self):
        payload = bytes.fromhex("0102000009000000000000000000000000000000")
        record = PacketRecord.from_json(packet_json(payload))
        self.assertEqual(record.payload, payload)
        self.assertEqual(record.operation, "challenge")
        self.assertEqual(record.local_endpoint, Endpoint("10.0.0.2", 61440))

    def test_packet_record_rejects_hash_or_length_mismatch(self):
        payload = bytes.fromhex("0102000009000000000000000000000000000000")
        for field, value, message in (
            ("payload_length", len(payload) + 1, "payload_length"),
            ("payload_sha256", "0" * 64, "payload_sha256"),
            ("payload_hex", "xyz", "payload_hex"),
        ):
            with self.subTest(field=field):
                raw = packet_json(payload)
                raw[field] = value
                with self.assertRaisesRegex(EvidenceError, message):
                    PacketRecord.from_json(raw)

    def test_packet_record_rejects_unknown_enum_and_schema_values(self):
        payload = b"x"
        cases = (
            ("transcript_version", 2),
            ("record_type", "decision"),
            ("side", "proxy"),
            ("direction", "forward"),
            ("operation", "dial"),
        )
        for field, value in cases:
            with self.subTest(field=field):
                raw = packet_json(payload)
                raw[field] = value
                with self.assertRaisesRegex(EvidenceError, field):
                    PacketRecord.from_json(raw)

    def test_transport_observation_requires_version_identity_and_endpoints(self):
        parsed = TransportObservation.from_json(transport_json())
        self.assertEqual(parsed.socket_local_endpoint.port, 61440)
        raw = transport_json()
        raw["schema_version"] = 2
        with self.assertRaisesRegex(EvidenceError, "schema_version"):
            TransportObservation.from_json(raw)

    def test_session_snapshot_accepts_public_camel_case_shape(self):
        snapshot = SessionSnapshotEvidence.from_json({
            "AuthenticationSessionID": "acceptance-session-1",
            "State": "authenticated",
            "SelectedNetworkBinding": {
                "InterfaceID": "{11111111-1111-1111-1111-111111111111}",
                "DisplayName": "以太网",
                "LocalIPv4Address": "10.0.0.2",
            },
        })
        self.assertEqual(snapshot.local_ipv4_address, "10.0.0.2")

    def test_comparison_result_requires_failure_for_failed_result(self):
        with self.assertRaises(ValueError):
            ComparisonResult(passed=False, packet_count=0, checks=(), failure=None)


class ClassicPcapFixtureTests(unittest.TestCase):
    PAYLOAD = bytes.fromhex("0102000009000000000000000000000000000000")

    def datagram(self) -> FixtureDatagram:
        return FixtureDatagram(
            source=Endpoint("10.0.0.2", 61440),
            destination=Endpoint("10.100.61.3", 61440),
            payload=self.PAYLOAD,
        )

    def test_fixture_contains_ethernet_ipv4_udp_payload(self):
        pcap = build_ethernet_ipv4_udp_pcap([self.datagram()])
        self.assertEqual(pcap[:4], bytes.fromhex("d4c3b2a1"))
        self.assertEqual(struct.unpack_from("<I", pcap, 20)[0], 1)
        self.assertIn(self.PAYLOAD, pcap)

    def test_fixture_supports_big_endian_and_nanosecond_headers(self):
        big = build_ethernet_ipv4_udp_pcap([self.datagram()], byte_order="big")
        nano = build_ethernet_ipv4_udp_pcap([self.datagram()], nanosecond=True)
        self.assertEqual(big[:4], bytes.fromhex("a1b2c3d4"))
        self.assertEqual(nano[:4], bytes.fromhex("4d3cb2a1"))

    def test_fixture_marks_capture_truncation_without_changing_original_length(self):
        pcap = build_ethernet_ipv4_udp_pcap([
            FixtureDatagram(
                source=self.datagram().source,
                destination=self.datagram().destination,
                payload=self.PAYLOAD,
                captured_length=50,
            )
        ])
        included, original = struct.unpack_from("<II", pcap, 24 + 8)
        self.assertEqual(included, 50)
        self.assertGreater(original, included)

    def test_fixture_rejects_invalid_byte_order_and_capture_length(self):
        with self.assertRaisesRegex(ValueError, "byte_order"):
            build_ethernet_ipv4_udp_pcap([self.datagram()], byte_order="middle")
        with self.assertRaisesRegex(ValueError, "captured_length"):
            build_ethernet_ipv4_udp_pcap([
                FixtureDatagram(self.datagram().source, self.datagram().destination,
                                self.PAYLOAD, captured_length=9999)
            ])


if __name__ == "__main__":
    unittest.main()
