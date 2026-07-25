import io
import json
import struct
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

from drcom520d_mock_server.offline import (
    OfflineFormatError,
    decode_payload,
    main,
    read_pcap,
)
from drcom520d_mock_server.transcript import decision_record, packet_record

from tools.drcom520d_mock_server.tests.packet_factory import (
    build_ka1_request,
    build_login_request,
)


def ethernet_ipv4_udp(
    payload: bytes,
    *,
    source_port: int = 50000,
    destination_port: int = 61440,
    ip_options: bytes = b"",
    fragment_field: int = 0,
    protocol: int = 17,
    udp_length: int | None = None,
) -> bytes:
    if len(ip_options) % 4:
        raise ValueError("IP options must align to four bytes")
    ihl = 5 + len(ip_options) // 4
    declared_udp_length = udp_length if udp_length is not None else 8 + len(payload)
    udp = struct.pack(
        "!HHHH", source_port, destination_port, declared_udp_length, 0) + payload
    ip = struct.pack(
        "!BBHHHBBH4s4s",
        (4 << 4) | ihl, 0, ihl * 4 + len(udp), 1, fragment_field,
        64, protocol, 0,
        bytes((127, 0, 0, 1)), bytes((127, 0, 0, 1)),
    ) + ip_options
    return b"\0" * 12 + b"\x08\x00" + ip + udp


def classic_pcap(
    frames: bytes | list[bytes],
    *,
    endian: str = "<",
    nanoseconds: bool = False,
    link_type: int = 1,
) -> bytes:
    if isinstance(frames, bytes):
        frames = [frames]
    magic = 0xA1B23C4D if nanoseconds else 0xA1B2C3D4
    global_header = struct.pack(
        endian + "IHHIIII", magic, 2, 4, 0, 0, 65535, link_type)
    fraction = 500_000_000 if nanoseconds else 500_000
    records = b"".join(
        struct.pack(endian + "IIII", 1, fraction, len(frame), len(frame)) + frame
        for frame in frames
    )
    return global_header + records


def write_capture(data: bytes, directory: str) -> Path:
    path = Path(directory) / "capture.pcap"
    path.write_bytes(data)
    return path


def write_jsonl(path: Path, rows: list[dict]) -> None:
    path.write_text(
        "".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows),
        encoding="utf-8",
    )


class PayloadDecodeTests(unittest.TestCase):
    def test_decodes_login_payload_with_formal_parser(self):
        payload = build_login_request()
        decoded = decode_payload(payload)
        self.assertEqual(decoded["operation"], "login")
        self.assertEqual(decoded["parsed"]["username"], "student-test")
        self.assertEqual(decoded["parsed"]["reported_ipv4"], "10.0.0.2")
        self.assertEqual(decoded["payload_hex"], payload.hex())

    def test_decodes_ka1_bytes_as_json_hex(self):
        payload = build_ka1_request(
            bytes.fromhex("01020304"), "local-test-password", b"a" * 16)
        decoded = decode_payload(payload)
        self.assertEqual(decoded["operation"], "ka1")
        self.assertEqual(decoded["parsed"]["auth_info"], (b"a" * 16).hex())

    def test_rejects_malformed_formal_packet(self):
        with self.assertRaisesRegex(OfflineFormatError, "Login"):
            decode_payload(b"\x03\x01\x00")

    def test_rejects_unknown_operation(self):
        with self.assertRaisesRegex(OfflineFormatError, "识别"):
            decode_payload(b"\x99\x00")

    def test_validates_challenge_request_length(self):
        decoded = decode_payload(b"\x01\x02" + b"\0" * 18)
        self.assertEqual(decoded["operation"], "challenge")
        self.assertEqual(decoded["parsed"]["length"], 20)
        with self.assertRaisesRegex(OfflineFormatError, "Challenge"):
            decode_payload(b"\x01\x02")


class PcapTests(unittest.TestCase):
    def test_reads_all_classic_magics_with_ipv4_options(self):
        payload = b"\x01\x02" + b"\0" * 18
        frame = ethernet_ipv4_udp(payload, ip_options=b"\x01\x01\x00\x00")
        for endian in ("<", ">"):
            for nanoseconds in (False, True):
                with self.subTest(endian=endian, nanoseconds=nanoseconds):
                    with tempfile.TemporaryDirectory() as directory:
                        path = write_capture(classic_pcap(
                            frame, endian=endian, nanoseconds=nanoseconds), directory)
                        rows = read_pcap(path, server_port=61440)
                    self.assertEqual(len(rows), 1)
                    self.assertEqual(rows[0]["payload_hex"], payload.hex())
                    self.assertEqual(rows[0]["capture_timestamp_ns"], 1_500_000_000)

    def test_infers_server_direction_and_endpoints(self):
        request = ethernet_ipv4_udp(b"request")
        response = ethernet_ipv4_udp(
            b"response", source_port=61440, destination_port=50000)
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(classic_pcap([request, response]), directory)
            rows = read_pcap(path, server_port=61440)
        self.assertEqual(
            [(row["side"], row["direction"]) for row in rows],
            [("server", "receive"), ("server", "send")],
        )
        self.assertEqual(rows[0]["local_endpoint"], ("127.0.0.1", 61440))
        self.assertEqual(rows[0]["remote_endpoint"], ("127.0.0.1", 50000))
        self.assertEqual(rows[1]["local_endpoint"], ("127.0.0.1", 61440))
        self.assertEqual(rows[1]["remote_endpoint"], ("127.0.0.1", 50000))

    def test_filters_non_server_ports_and_non_udp(self):
        ignored_port = ethernet_ipv4_udp(
            b"ignored", source_port=40000, destination_port=40001)
        ignored_tcp = ethernet_ipv4_udp(b"ignored", protocol=6)
        accepted = ethernet_ipv4_udp(b"accepted")
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(
                classic_pcap([ignored_port, ignored_tcp, accepted]), directory)
            rows = read_pcap(path, server_port=61440)
        self.assertEqual([row["payload_hex"] for row in rows], [b"accepted".hex()])

    def test_rejects_pcapng_and_unsupported_link_type(self):
        with tempfile.TemporaryDirectory() as directory:
            pcapng = write_capture(bytes.fromhex("0a0d0d0a") + b"\0" * 24, directory)
            with self.assertRaisesRegex(OfflineFormatError, "PCAPNG"):
                read_pcap(pcapng, server_port=61440)

            unsupported = write_capture(
                classic_pcap([], link_type=101), directory)
            with self.assertRaisesRegex(OfflineFormatError, "Ethernet"):
                read_pcap(unsupported, server_port=61440)

    def test_rejects_truncated_global_record_and_frame(self):
        frame = ethernet_ipv4_udp(b"x")
        valid_header = classic_pcap([])
        cases = {
            "global": valid_header[:10],
            "record": valid_header + b"\0" * 8,
            "frame": valid_header + struct.pack("<IIII", 1, 0, len(frame), len(frame)) + frame[:-1],
            "ethernet": classic_pcap(b"short"),
        }
        for name, capture in cases.items():
            with self.subTest(name=name):
                with tempfile.TemporaryDirectory() as directory:
                    path = write_capture(capture, directory)
                    with self.assertRaisesRegex(OfflineFormatError, "截断"):
                        read_pcap(path, server_port=61440)

    def test_rejects_ipv4_fragments(self):
        for fragment_field in (0x2000, 0x0001):
            with self.subTest(fragment_field=fragment_field):
                frame = ethernet_ipv4_udp(b"fragment", fragment_field=fragment_field)
                with tempfile.TemporaryDirectory() as directory:
                    path = write_capture(classic_pcap(frame), directory)
                    with self.assertRaisesRegex(OfflineFormatError, "分片"):
                        read_pcap(path, server_port=61440)

    def test_rejects_invalid_ipv4_and_udp_lengths(self):
        truncated_ip = ethernet_ipv4_udp(b"x")[:-1]
        short_udp = ethernet_ipv4_udp(b"x", udp_length=7)
        long_udp = ethernet_ipv4_udp(b"x", udp_length=100)
        for name, frame in (
            ("ip", truncated_ip),
            ("udp-short", short_udp),
            ("udp-long", long_udp),
        ):
            with self.subTest(name=name):
                with tempfile.TemporaryDirectory() as directory:
                    path = write_capture(classic_pcap(frame), directory)
                    with self.assertRaisesRegex(OfflineFormatError, "长度|截断"):
                        read_pcap(path, server_port=61440)

    def test_rejects_udp_length_smaller_than_ipv4_payload(self):
        frame = ethernet_ipv4_udp(b"trailing", udp_length=8)
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(classic_pcap(frame), directory)
            with self.assertRaisesRegex(OfflineFormatError, "UDP 长度"):
                read_pcap(path, server_port=61440)

    def test_rejects_direction_when_both_ports_equal_server_port(self):
        frame = ethernet_ipv4_udp(
            b"ambiguous", source_port=61440, destination_port=61440)
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(classic_pcap(frame), directory)
            with self.assertRaisesRegex(OfflineFormatError, "方向|歧义"):
                read_pcap(path, server_port=61440)

    def test_rejects_captured_length_greater_than_original_length(self):
        frame = ethernet_ipv4_udp(b"x")
        capture = (
            classic_pcap([])
            + struct.pack("<IIII", 1, 0, len(frame), len(frame) - 1)
            + frame
        )
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(capture, directory)
            with self.assertRaisesRegex(OfflineFormatError, "captured 长度"):
                read_pcap(path, server_port=61440)

    def test_rejects_invalid_ihl_total_length_and_truncated_options(self):
        invalid_ihl = bytearray(ethernet_ipv4_udp(b"x"))
        invalid_ihl[14] = 0x44
        short_total = bytearray(ethernet_ipv4_udp(b"x"))
        short_total[16:18] = struct.pack("!H", 19)
        truncated_options = b"\0" * 12 + b"\x08\x00" + b"\x46" + b"\0" * 19
        cases = (
            ("ihl", bytes(invalid_ihl), "IHL"),
            ("total", bytes(short_total), "total length"),
            ("options", truncated_options, "options"),
        )
        for name, frame, message in cases:
            with self.subTest(name=name):
                with tempfile.TemporaryDirectory() as directory:
                    path = write_capture(classic_pcap(frame), directory)
                    with self.assertRaisesRegex(OfflineFormatError, message):
                        read_pcap(path, server_port=61440)


class CliTests(unittest.TestCase):
    def run_cli(self, arguments: list[str]) -> tuple[int, str, str]:
        stdout = io.StringIO()
        stderr = io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            result = main(arguments)
        return result, stdout.getvalue(), stderr.getvalue()

    def run_compare_rows(
        self,
        client_rows: list[dict],
        server_details: list[dict],
    ) -> tuple[int, str, str]:
        with tempfile.TemporaryDirectory() as directory:
            client_path = Path(directory) / "client.jsonl"
            server_path = Path(directory) / "server.jsonl"
            write_jsonl(client_path, client_rows)
            write_jsonl(server_path, [
                {"event": "trace", "details": details}
                for details in server_details
            ])
            return self.run_cli([
                "compare", str(client_path), str(server_path)])

    def test_payload_emits_utf8_jsonl_and_invalid_hex_returns_two(self):
        payload = build_login_request()
        result, stdout, stderr = self.run_cli(["payload", payload.hex()])
        self.assertEqual((result, stderr), (0, ""))
        self.assertEqual(json.loads(stdout)["parsed"]["username"], "student-test")

        result, stdout, stderr = self.run_cli(["payload", "not-hex"])
        self.assertEqual((result, stdout), (2, ""))
        self.assertIn("格式错误", stderr)

    def test_pcap_emits_one_json_line_per_filtered_packet(self):
        frame = ethernet_ipv4_udp(b"payload")
        with tempfile.TemporaryDirectory() as directory:
            path = write_capture(classic_pcap(frame), directory)
            result, stdout, stderr = self.run_cli([
                "pcap", str(path), "--server-port", "61440"])
        self.assertEqual((result, stderr), (0, ""))
        self.assertEqual(json.loads(stdout)["payload_hex"], b"payload".hex())

    def test_compare_extracts_full_server_trace_and_reports_success(self):
        client_record = packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=("127.0.0.1", 50000),
            remote_endpoint=("127.0.0.1", 61440))
        client_record.update({"timestamp": "ignored", "pid": 12, "sequence": 99})
        server_record = packet_record(
            side="server", direction="receive", operation="challenge",
            payload=b"x", local_endpoint=("127.0.0.1", 61440),
            remote_endpoint=("127.0.0.1", 60000))
        client_decision = decision_record(
            side="client", operation="challenge", outcome="timeout",
            wire_error_code=None, drop_reason="timeout")
        server_decision = decision_record(
            operation="challenge", outcome="drop", wire_error_code=None,
            drop_reason="challenge_parse_failed")
        with tempfile.TemporaryDirectory() as directory:
            client_path = Path(directory) / "client.jsonl"
            server_path = Path(directory) / "server.jsonl"
            write_jsonl(client_path, [client_record, client_decision])
            write_jsonl(server_path, [
                {"event": "datagram_received", "details": server_record},
                {"event": "datagram_dropped", "details": server_decision},
                {"event": "crypto_check", "details": {"valid": True}},
            ])
            result, stdout, stderr = self.run_cli([
                "compare", str(client_path), str(server_path)])
        self.assertEqual((result, stderr), (0, ""))
        self.assertIn("对齐成功", stdout)
        self.assertIn("2", stdout)

    def test_compare_mismatch_returns_one_with_readable_location(self):
        client_record = packet_record(
            side="client", direction="send", operation="login",
            payload=b"a", local_endpoint=None, remote_endpoint=None)
        server_record = packet_record(
            side="server", direction="receive", operation="login",
            payload=b"b", local_endpoint=None, remote_endpoint=None)
        with tempfile.TemporaryDirectory() as directory:
            client_path = Path(directory) / "client.jsonl"
            server_path = Path(directory) / "server.jsonl"
            write_jsonl(client_path, [client_record])
            write_jsonl(server_path, [
                {"event": "datagram_received", "details": server_record}])
            result, stdout, stderr = self.run_cli([
                "compare", str(client_path), str(server_path)])
        self.assertEqual((result, stderr), (1, ""))
        summary = json.loads(stdout)
        self.assertEqual(summary["summary"], "对齐失败")
        self.assertEqual(summary["record_type"], "packet")
        self.assertEqual(summary["index"], 0)
        self.assertEqual(summary["field_path"], "payload_hex")
        self.assertEqual(
            summary["client"], json.loads(json.dumps(client_record)))
        self.assertEqual(
            summary["server"], json.loads(json.dumps(server_record)))
        self.assertIn("payload_hex", summary["reason"])

    def test_malformed_jsonl_returns_two(self):
        with tempfile.TemporaryDirectory() as directory:
            client_path = Path(directory) / "client.jsonl"
            server_path = Path(directory) / "server.jsonl"
            client_path.write_text("{broken\n", encoding="utf-8")
            server_path.write_text("", encoding="utf-8")
            result, stdout, stderr = self.run_cli([
                "compare", str(client_path), str(server_path)])
        self.assertEqual((result, stdout), (2, ""))
        self.assertIn("格式错误", stderr)

    def test_malformed_transcript_version_returns_two(self):
        client_record = packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        client_record["transcript_version"] = 2
        server_record = packet_record(
            side="server", direction="receive", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        with tempfile.TemporaryDirectory() as directory:
            client_path = Path(directory) / "client.jsonl"
            server_path = Path(directory) / "server.jsonl"
            write_jsonl(client_path, [client_record])
            write_jsonl(server_path, [
                {"event": "datagram_received", "details": server_record}])
            result, stdout, stderr = self.run_cli([
                "compare", str(client_path), str(server_path)])
        self.assertEqual((result, stdout), (2, ""))
        self.assertIn("transcript_version", stderr)

    def test_unknown_record_type_on_both_sides_returns_two(self):
        malformed = {"transcript_version": 1, "record_type": "packte"}
        result, stdout, stderr = self.run_compare_rows(
            [malformed], [malformed])
        self.assertEqual((result, stdout), (2, ""))
        self.assertIn("record_type", stderr)

    def test_decision_missing_required_fields_returns_two(self):
        client = decision_record(
            side="client", operation="login", outcome="success",
            wire_error_code=None, drop_reason=None)
        server = decision_record(
            operation="login", outcome="success",
            wire_error_code=None, drop_reason=None)
        for field in ("outcome", "wire_error_code", "drop_reason"):
            with self.subTest(field=field):
                malformed_client = dict(client)
                malformed_server = dict(server)
                malformed_client.pop(field)
                malformed_server.pop(field)
                result, stdout, stderr = self.run_compare_rows(
                    [malformed_client], [malformed_server])
                self.assertEqual((result, stdout), (2, ""))
                self.assertIn(field, stderr)

    def test_endpoint_and_payload_self_integrity_errors_return_two(self):
        client, server = (
            packet_record(
                side="client", direction="send", operation="challenge",
                payload=b"x", local_endpoint=("10.0.0.2", 51000),
                remote_endpoint=("127.0.0.1", 61440)),
            packet_record(
                side="server", direction="receive", operation="challenge",
                payload=b"x", local_endpoint=("127.0.0.1", 61440),
                remote_endpoint=("10.0.0.2", 52000)),
        )
        cases = (
            ("local_endpoint.port", {
                "local_endpoint": {"host": "10.0.0.2", "port": "51000"}}),
            ("payload_hex", {"payload_hex": "not-hex"}),
            ("payload_sha256", {"payload_sha256": "0" * 64}),
            ("payload_length", {"payload_length": 999}),
        )
        for field, changes in cases:
            with self.subTest(field=field):
                result, stdout, stderr = self.run_compare_rows(
                    [dict(client, **changes)], [server])
                self.assertEqual((result, stdout), (2, ""))
                self.assertIn(field, stderr)

    def test_server_full_trace_transcript_typo_returns_two(self):
        client = packet_record(
            side="client", direction="send", operation="challenge",
            payload=b"x", local_endpoint=None, remote_endpoint=None)
        malformed_server = {
            "transcript_version": 1,
            "record_type": "packte",
            "side": "server",
        }
        result, stdout, stderr = self.run_compare_rows(
            [client], [malformed_server])
        self.assertEqual((result, stdout), (2, ""))
        self.assertIn("record_type", stderr)


if __name__ == "__main__":
    unittest.main()
