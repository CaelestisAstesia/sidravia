import hashlib
import json
import tempfile
import unittest
from dataclasses import asdict, replace
from pathlib import Path

from sidravia_drcom_acceptance.evidence import (
    compare_evidence,
    load_session_snapshot,
    load_transcript,
    load_transport_observation,
    load_tshark_json,
)
from sidravia_drcom_acceptance.model import (
    Endpoint,
    EvidenceError,
    PacketRecord,
    SessionSnapshotEvidence,
    TransportObservation,
    TsharkPacket,
)


LOCAL = Endpoint("10.0.0.2", 61440)
REMOTE = Endpoint("10.100.61.3", 61440)


def payload(index: int) -> bytes:
    return bytes((index, index ^ 0xA5, 0x5A))


def complete_transcript() -> tuple[PacketRecord, ...]:
    records = []
    index = 1
    for operation in ("challenge", "login", "ka1", "ka2", "logout"):
        for direction in ("send", "receive"):
            body = payload(index)
            records.append(PacketRecord(
                transcript_version=1,
                side="client",
                direction=direction,
                operation=operation,
                payload=body,
                payload_sha256=hashlib.sha256(body).hexdigest(),
                local_endpoint=LOCAL,
                remote_endpoint=REMOTE,
            ))
            index += 1
    return tuple(records)


def matching_tshark_packets() -> tuple[TsharkPacket, ...]:
    packets = []
    for index, record in enumerate(complete_transcript(), 1):
        sending = record.direction == "send"
        packets.append(TsharkPacket(
            frame_number=index,
            operation=record.operation,
            direction="client_to_server" if sending else "server_to_client",
            payload_sha256=record.payload_sha256,
            payload_length=record.payload_length,
            source=LOCAL if sending else REMOTE,
            destination=REMOTE if sending else LOCAL,
            valid=True,
            truncated=False,
            malformed=False,
            unknown=False,
        ))
    return tuple(packets)


def transport(local_port: int = 61440) -> TransportObservation:
    return TransportObservation(
        schema_version=1,
        run_id="run-1",
        authentication_session_id="acceptance-session-1",
        socket_local_endpoint=Endpoint("10.0.0.2", local_port),
        server_endpoint=REMOTE,
    )


def snapshot(local_ip: str = "10.0.0.2") -> SessionSnapshotEvidence:
    return SessionSnapshotEvidence(
        authentication_session_id="acceptance-session-1",
        state="authenticated",
        interface_id="{11111111-1111-1111-1111-111111111111}",
        display_name="以太网",
        local_ipv4_address=local_ip,
    )


class EvidenceComparisonTests(unittest.TestCase):
    def test_compare_proves_order_direction_endpoints_and_one_source_port(self):
        result = compare_evidence(
            complete_transcript(), transport(), snapshot(), matching_tshark_packets(),
            require_keepalive=True,
        )
        self.assertTrue(result.passed)
        self.assertEqual(result.packet_count, 10)
        self.assertTrue(all(check.passed for check in result.checks))

    def test_compare_reports_first_hash_difference_without_payload(self):
        packets = list(matching_tshark_packets())
        packets[2] = replace(packets[2], payload_sha256="0" * 64)
        result = compare_evidence(
            complete_transcript(), transport(), snapshot(), tuple(packets),
            require_keepalive=True,
        )
        self.assertFalse(result.passed)
        self.assertEqual(result.failure.code, "packet_hash_mismatch")
        self.assertEqual(result.failure.evidence_index, 2)
        rendered = json.dumps(asdict(result.failure), ensure_ascii=False)
        self.assertNotIn("payload_hex", rendered)
        self.assertNotIn(complete_transcript()[2].payload.hex(), rendered)

    def test_compare_rejects_source_port_change(self):
        packets = list(matching_tshark_packets())
        packets[6] = replace(packets[6], source=Endpoint("10.0.0.2", 50001))
        result = compare_evidence(
            complete_transcript(), transport(), snapshot(), tuple(packets),
            require_keepalive=True,
        )
        self.assertEqual(result.failure.code, "pcap_local_endpoint_mismatch")

    def test_compare_rejects_snapshot_ip_or_session_identity_mismatch(self):
        result = compare_evidence(
            complete_transcript(), transport(), snapshot("10.0.0.9"),
            matching_tshark_packets(), require_keepalive=True,
        )
        self.assertEqual(result.failure.code, "snapshot_local_ip_mismatch")

        other_session = replace(snapshot(), authentication_session_id="other")
        result = compare_evidence(
            complete_transcript(), transport(), other_session,
            matching_tshark_packets(), require_keepalive=True,
        )
        self.assertEqual(result.failure.code, "session_identity_mismatch")

    def test_compare_rejects_invalid_lua_status(self):
        for field in ("truncated", "malformed", "unknown", "expert_error"):
            with self.subTest(field=field):
                packets = list(matching_tshark_packets())
                packets[1] = replace(packets[1], **{field: True})
                result = compare_evidence(
                    complete_transcript(), transport(), snapshot(), tuple(packets),
                    require_keepalive=True,
                )
                self.assertEqual(result.failure.code, "pcap_dissector_status_invalid")

    def test_compare_requires_keepalive_only_when_requested(self):
        transcript = tuple(record for record in complete_transcript()
                           if record.operation not in {"ka1", "ka2"})
        packets = tuple(packet for packet in matching_tshark_packets()
                        if packet.operation not in {"ka1", "ka2"})
        self.assertTrue(compare_evidence(
            transcript, transport(), snapshot(), packets, require_keepalive=False).passed)
        result = compare_evidence(
            transcript, transport(), snapshot(), packets, require_keepalive=True)
        self.assertEqual(result.failure.code, "operation_coverage_missing")

    def test_compare_ignores_unrelated_pcap_packets(self):
        unrelated = replace(
            matching_tshark_packets()[0],
            frame_number=99,
            source=Endpoint("192.0.2.1", 50000),
            destination=Endpoint("192.0.2.2", 61440),
        )
        packets = (unrelated,) + matching_tshark_packets()
        result = compare_evidence(
            complete_transcript(), transport(), snapshot(), packets,
            require_keepalive=True,
        )
        self.assertTrue(result.passed)


class EvidenceLoaderTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)

    def write_json(self, name: str, value) -> Path:
        path = self.root / name
        path.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")
        return path

    def test_loads_jsonl_transcript_and_json_evidence(self):
        transcript_path = self.root / "transcript.jsonl"
        rows = []
        for record in complete_transcript():
            rows.append(json.dumps({
                "transcript_version": 1,
                "record_type": "packet",
                "side": record.side,
                "direction": record.direction,
                "operation": record.operation,
                "payload_hex": record.payload.hex(),
                "payload_sha256": record.payload_sha256,
                "payload_length": record.payload_length,
                "local_endpoint": asdict(record.local_endpoint),
                "remote_endpoint": asdict(record.remote_endpoint),
            }))
        transcript_path.write_text("\n".join(rows) + "\n", encoding="utf-8")
        self.assertEqual(load_transcript(transcript_path), complete_transcript())

        transport_path = self.write_json("transport.json", {
            "schema_version": 1, "run_id": "run-1",
            "authentication_session_id": "acceptance-session-1",
            "socket_local_endpoint": asdict(LOCAL),
            "server_endpoint": asdict(REMOTE),
        })
        self.assertEqual(load_transport_observation(transport_path), transport())

        snapshot_path = self.write_json("snapshot.json", {
            "AuthenticationSessionID": "acceptance-session-1",
            "State": "authenticated",
            "SelectedNetworkBinding": {
                "InterfaceID": snapshot().interface_id,
                "DisplayName": "以太网",
                "LocalIPv4Address": "10.0.0.2",
            },
        })
        self.assertEqual(load_session_snapshot(snapshot_path), snapshot())

    def test_loads_tshark_json_shape_and_computes_udp_payload_hash(self):
        body = bytes.fromhex("010200")
        tshark = [{"_source": {"layers": {
            "frame": {"frame.number": "7"},
            "ip": {"ip.src": "10.0.0.2", "ip.dst": "10.100.61.3"},
            "udp": {
                "udp.srcport": "61440", "udp.dstport": "61440",
                "udp.payload": "01:02:00",
            },
            "drcom": {
                "drcom.packet_kind": "challenge_request",
                "drcom.direction": "client_to_server",
                "drcom.valid": "1", "drcom.truncated": "0",
                "drcom.malformed": "0", "drcom.unknown": "0",
            },
        }}}]
        packets = load_tshark_json(self.write_json("tshark.json", tshark))
        self.assertEqual(len(packets), 1)
        self.assertEqual(packets[0].frame_number, 7)
        self.assertEqual(packets[0].operation, "challenge")
        self.assertEqual(packets[0].payload_sha256, hashlib.sha256(body).hexdigest())

    def test_loaders_reject_invalid_json_or_empty_evidence(self):
        invalid = self.root / "invalid.jsonl"
        invalid.write_text("{\n", encoding="utf-8")
        with self.assertRaises(EvidenceError):
            load_transcript(invalid)
        with self.assertRaises(EvidenceError):
            load_tshark_json(self.write_json("empty.json", []))


if __name__ == "__main__":
    unittest.main()
