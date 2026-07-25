import unittest
from dataclasses import replace

from drcom520d_mock_server.codec import (
    PacketFormatError, parse_ka1_request, parse_ka2_request,
    parse_login_request, parse_logout_request, verify_ka1, verify_login_crypto,
    verify_ka1_detailed, verify_logout_crypto, verify_logout_crypto_detailed,
)
from tools.drcom520d_mock_server.tests.packet_factory import (
    build_ka1_request, build_ka2_request, build_login_request,
    build_logout_request,
)


class RequestValidationTests(unittest.TestCase):
    def test_parses_and_verifies_complete_login(self):
        packet = build_login_request()
        request = parse_login_request(packet)
        verified = verify_login_crypto(packet, request, "local-test-password",
                                       bytes.fromhex("01020304"))
        self.assertEqual(request.username, "student-test")
        self.assertEqual(str(request.reported_ipv4), "10.0.0.2")
        self.assertEqual(verified.recovered_mac, bytes.fromhex("020000000001"))
        self.assertTrue(verified.md5_a_valid)
        self.assertTrue(verified.md5_b_valid)
        self.assertTrue(verified.md5_c_valid)
        self.assertTrue(verified.repeated_mac_valid)
        self.assertTrue(verified.crc_valid)

    def test_login_crypto_exposes_exact_inputs_expected_and_received_values(self):
        packet = build_login_request()
        request = parse_login_request(packet)
        result = verify_login_crypto(
            packet, request, "local-test-password", bytes.fromhex("01020304"))
        self.assertEqual(
            result.md5_a.input,
            b"\x03\x01" + bytes.fromhex("01020304") + b"local-test-password")
        self.assertEqual(result.md5_a.received, request.md5_a)
        self.assertEqual(result.md5_a.expected, request.md5_a)
        self.assertTrue(result.md5_a.valid)
        self.assertEqual(result.crc.received, request.auth_ext_crc)
        self.assertEqual(result.repeated_mac.expected, result.recovered_mac)
        self.assertTrue(result.md5_a_valid)
        self.assertTrue(result.crc_valid)

    def test_wrong_password_invalidates_both_password_hashes(self):
        packet = build_login_request()
        request = parse_login_request(packet)
        verified = verify_login_crypto(packet, request, "wrong-password",
                                       bytes.fromhex("01020304"))
        self.assertFalse(verified.md5_a_valid)
        self.assertFalse(verified.md5_b_valid)

    def test_crc_corruption_is_distinct_from_business_rejection(self):
        packet = bytearray(build_login_request())
        packet[200] ^= 0x01
        request = parse_login_request(bytes(packet))
        verified = verify_login_crypto(bytes(packet), request,
                                       "local-test-password", bytes.fromhex("01020304"))
        self.assertFalse(verified.crc_valid)

    def test_rejects_truncated_and_non_jlu_login_packets(self):
        with self.assertRaises(PacketFormatError):
            parse_login_request(b"\x03\x01" + b"\0" * 20)
        with self.assertRaises(PacketFormatError):
            parse_login_request(build_login_request() + b"extra")

    def test_rejects_login_auth_extension_reserved_bytes(self):
        for offset in (318, 319, 326, 327):
            with self.subTest(offset=offset):
                packet = bytearray(build_login_request())
                packet[offset] = 1
                with self.assertRaises(PacketFormatError):
                    parse_login_request(bytes(packet))

    def test_allows_variable_login_auth_extension_tail(self):
        packet = bytearray(build_login_request())
        packet[328:330] = b"\xab\xcd"
        self.assertEqual(parse_login_request(bytes(packet)).auth_ext_tail,
                         b"\xab\xcd")

    def test_parses_keepalive_and_logout_requests(self):
        auth = bytes(range(16))
        self.assertEqual(parse_ka1_request(build_ka1_request(
            bytes.fromhex("01020304"), "local-test-password", auth)).auth_info, auth)
        ka2 = parse_ka2_request(build_ka2_request(0, 1, b"\0" * 4,
                                                  "10.0.0.2", bytes.fromhex("0f27")))
        self.assertEqual((ka2.serial, ka2.packet_type), (0, 1))
        logout = parse_logout_request(build_logout_request(
            "student-test", "local-test-password", bytes.fromhex("01020304"),
            bytes.fromhex("020000000001"), auth))
        self.assertEqual(logout.auth_info, auth)

    def test_rejects_unknown_ka2_packet_type(self):
        with self.assertRaises(PacketFormatError):
            parse_ka2_request(build_ka2_request(0, 2, b"\0" * 4,
                                                 "10.0.0.2", bytes.fromhex("0f27")))

    def test_rejects_unknown_ka2_version(self):
        with self.assertRaises(PacketFormatError):
            parse_ka2_request(build_ka2_request(0, 1, b"\0" * 4,
                                                 "10.0.0.2", bytes.fromhex("ffff")))

    def test_verifies_ka1_password_digest(self):
        request = parse_ka1_request(build_ka1_request(
            bytes.fromhex("01020304"), "local-test-password", bytes(range(16))))
        self.assertTrue(verify_ka1(request, "local-test-password",
                                   bytes.fromhex("01020304")))
        self.assertFalse(verify_ka1(request, "wrong-password",
                                    bytes.fromhex("01020304")))

    def test_detailed_ka1_and_logout_digests_expose_material(self):
        salt = bytes.fromhex("01020304")
        ka1 = parse_ka1_request(build_ka1_request(
            salt, "local-test-password", bytes(range(16))))
        ka1_result = verify_ka1_detailed(ka1, "local-test-password", salt)
        self.assertIn(b"local-test-password", ka1_result.input)
        self.assertEqual(ka1_result.received, ka1.md5_a)
        self.assertTrue(ka1_result.valid)

        logout = parse_logout_request(build_logout_request(
            "student-test", "local-test-password", salt,
            bytes.fromhex("020000000001"), bytes(range(16))))
        logout_result = verify_logout_crypto_detailed(
            logout, "local-test-password", salt)
        self.assertEqual(logout_result.received, logout.md5_a)
        self.assertTrue(logout_result.valid)

    def test_crypto_verification_rejects_invalid_salt_lengths(self):
        salt = bytes.fromhex("01020304")
        packet = build_login_request()
        login = parse_login_request(packet)
        ka1 = parse_ka1_request(build_ka1_request(
            salt, "local-test-password", bytes(range(16))))
        logout = parse_logout_request(build_logout_request(
            "student-test", "local-test-password", salt,
            bytes.fromhex("020000000001"), bytes(range(16))))
        for invalid_salt in (b"\0" * 3, b"\0" * 5):
            with self.subTest(salt=invalid_salt):
                with self.assertRaisesRegex(ValueError, "salt must contain four bytes"):
                    verify_login_crypto(packet, login, "local-test-password", invalid_salt)
                with self.assertRaisesRegex(ValueError, "salt must contain four bytes"):
                    verify_ka1(ka1, "local-test-password", invalid_salt)
                with self.assertRaisesRegex(ValueError, "salt must contain four bytes"):
                    verify_logout_crypto(logout, "local-test-password", invalid_salt)

    def test_login_crypto_rejects_nonstandard_ip_section_length(self):
        packet = build_login_request()
        request = replace(parse_login_request(packet), ip_section=b"\0" * 16)
        with self.assertRaisesRegex(ValueError, "IP section must contain 17 bytes"):
            verify_login_crypto(
                packet, request, "local-test-password", bytes.fromhex("01020304"))

    def test_verifies_logout_password_digest_and_recovers_mac(self):
        request = parse_logout_request(build_logout_request(
            "student-test", "local-test-password", bytes.fromhex("01020304"),
            bytes.fromhex("020000000001"), bytes(range(16))))
        self.assertTrue(verify_logout_crypto(request, "local-test-password",
                                              bytes.fromhex("01020304")))
        self.assertFalse(verify_logout_crypto(request, "wrong-password",
                                               bytes.fromhex("01020304")))
        self.assertEqual(request.recovered_mac, bytes.fromhex("020000000001"))


if __name__ == "__main__":
    unittest.main()
