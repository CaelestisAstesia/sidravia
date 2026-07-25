import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from sidravia_drcom_acceptance.model import Endpoint
from sidravia_drcom_acceptance.pcap_fixture import (
    FixtureDatagram,
    build_ethernet_ipv4_udp_pcap,
)


def find_tshark():
    resolved = shutil.which("tshark.exe")
    if resolved:
        return resolved
    for variable in ("ProgramFiles", "ProgramFiles(x86)"):
        root = os.environ.get(variable)
        if root:
            candidate = Path(root) / "Wireshark" / "tshark.exe"
            if candidate.is_file():
                return str(candidate)
    return None


TSHARK = find_tshark()


class TsharkRuntimeTests(unittest.TestCase):
    @unittest.skipUnless(TSHARK, "tshark.exe is not installed")
    def test_real_tshark_loads_lua_for_valid_unknown_malformed_and_truncated(self):
        client = Endpoint("192.0.2.10", 49152)
        server = Endpoint("192.0.2.20", 61440)
        valid = bytes.fromhex("0102341209") + b"\0" * 15
        unknown = bytes.fromhex("aa55")
        malformed = bytes.fromhex("0102341208") + b"\0" * 15
        truncated = FixtureDatagram(client, server, valid, captured_length=47)
        pcap = build_ethernet_ipv4_udp_pcap((
            FixtureDatagram(client, server, valid),
            FixtureDatagram(client, server, unknown),
            FixtureDatagram(client, server, malformed),
            truncated,
        ))
        repository_root = Path(__file__).resolve().parents[3]
        lua = repository_root / "tools" / "wireshark" / "drcom.lua"

        with tempfile.TemporaryDirectory(prefix="sidravia-tshark-runtime-") as directory:
            capture = Path(directory) / "synthetic.pcap"
            capture.write_bytes(pcap)
            fields = subprocess.run(
                (
                    TSHARK, "-n", "-r", str(capture),
                    "-X", f"lua_script:{lua}", "-T", "fields",
                    "-E", "separator=\t", "-E", "occurrence=f",
                    "-e", "frame.number", "-e", "drcom.packet_kind",
                    "-e", "drcom.direction", "-e", "drcom.valid",
                    "-e", "drcom.truncated", "-e", "drcom.malformed",
                    "-e", "drcom.malformed_reason", "-e", "drcom.unknown",
                ),
                capture_output=True, text=True, encoding="utf-8", errors="replace",
                check=False, timeout=20, shell=False,
            )
            as_json = subprocess.run(
                (
                    TSHARK, "-n", "-r", str(capture),
                    "-X", f"lua_script:{lua}", "-T", "json",
                    "-J", "frame ip udp drcom",
                ),
                capture_output=True, text=True, encoding="utf-8", errors="replace",
                check=False, timeout=20, shell=False,
            )

        self.assertEqual(fields.returncode, 0, fields.stderr)
        rows = [line.split("\t") for line in fields.stdout.splitlines()]
        self.assertEqual(rows, [
            ["1", "challenge_request", "client_to_server", "True", "False",
             "False", "", "False"],
            ["2", "unknown", "client_to_server", "False", "False",
             "False", "unknown_opcode", "True"],
            ["3", "challenge_request", "client_to_server", "False", "False",
             "True", "invalid_fixed_bytes", "False"],
            ["4", "challenge_request", "client_to_server", "False", "True",
             "False", "captured_shorter_than_reported", "False"],
        ])
        self.assertEqual(as_json.returncode, 0, as_json.stderr)
        packets = json.loads(as_json.stdout)
        self.assertEqual(len(packets), 4)
        for packet in packets:
            layers = packet["_source"]["layers"]
            self.assertIn("drcom", layers)
            self.assertIn("drcom.schema_version", layers["drcom"])

    @unittest.skipUnless(TSHARK, "tshark.exe is not installed")
    def test_real_tshark_executes_all_jlu_branches_and_legacy_classifiers(self):
        client = Endpoint("192.0.2.10", 49152)
        server = Endpoint("192.0.2.20", 61440)

        login = bytearray(330)
        login[0:4] = b"\x03\x01\x00\x14"
        login[312:314] = b"\x02\x0c"
        ka2_request = bytearray(40)
        ka2_request[0:10] = b"\x07\x01\x28\x00\x0b\x01\x0f\x27\x2f\x12"
        logout = bytearray(80)
        logout[0:4] = b"\x06\x01\x00\x14"

        client_payloads = (
            bytes(login),
            b"\xff" + b"\0" * 41,
            bytes(ka2_request),
            bytes(logout),
            b"\x08",
            b"\x07\x00\x10\x00",
            b"\x4d\x25",
        )
        server_payloads = (
            b"\x02" + b"\0" * 15,
            b"\x04" + b"\0" * 63,
            b"\x05\0\0\0\x17",
            b"\x07" + b"\0" * 19,
            b"\x07\x01\x28\x00\x0b\x01" + b"\0" * 54,
            b"\x04\0\0\0",
        )
        datagrams = [FixtureDatagram(client, server, payload) for payload in client_payloads]
        datagrams.extend(
            FixtureDatagram(server, client, payload) for payload in server_payloads
        )
        expected = (
            "login_request", "ka1_request", "ka2_request", "logout_request",
            "legacy_unknown_08", "legacy_misc_1000", "legacy_message_25",
            "challenge_response", "login_success", "login_failure",
            "ka1_response", "ka2_response", "logout_ack",
        )
        pcap = build_ethernet_ipv4_udp_pcap(tuple(datagrams))
        repository_root = Path(__file__).resolve().parents[3]
        lua = repository_root / "tools" / "wireshark" / "drcom.lua"

        with tempfile.TemporaryDirectory(prefix="sidravia-tshark-branches-") as directory:
            capture = Path(directory) / "synthetic.pcap"
            capture.write_bytes(pcap)
            result = subprocess.run(
                (
                    TSHARK, "-n", "-r", str(capture),
                    "-X", f"lua_script:{lua}", "-T", "fields",
                    "-E", "separator=\t", "-E", "occurrence=f",
                    "-e", "drcom.packet_kind", "-e", "drcom.valid",
                    "-e", "drcom.malformed_reason",
                ),
                capture_output=True, text=True, encoding="utf-8", errors="replace",
                check=False, timeout=20, shell=False,
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        rows = [line.split("\t") for line in result.stdout.splitlines()]
        self.assertEqual(tuple(row[0] for row in rows), expected)
        self.assertTrue(all(row[1] == "True" for row in rows), rows)
        self.assertTrue(all(len(row) == 2 or row[2] == "" for row in rows), rows)


if __name__ == "__main__":
    unittest.main()
