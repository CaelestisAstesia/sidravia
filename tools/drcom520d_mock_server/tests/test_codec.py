import hashlib
import struct
import unittest
from ipaddress import IPv4Address

from drcom520d_mock_server.codec import (
    build_challenge_response, build_ka1_response, build_ka2_response,
    build_login_failure_response, build_login_success_response,
    build_logout_response, checksum_d_series, derive_auth_info,
    derive_auth_info_detailed, derive_ka2_tail, derive_ka2_tail_detailed,
    md5_a, md5_b, md5_c,
)


class CodecAlgorithmTests(unittest.TestCase):
    def test_md5_algorithms_match_reference_formulae(self):
        salt = bytes.fromhex("01020304")
        password = "local-test-password"
        password_bytes = password.encode("gbk")
        self.assertEqual(md5_a(salt, password),
                         hashlib.md5(b"\x03\x01" + salt + password_bytes).digest())
        self.assertEqual(md5_b(salt, password),
                         hashlib.md5(b"\x01" + password_bytes + salt + b"\0" * 4).digest())
        ip_section = b"\x01" + IPv4Address("10.0.0.2").packed + b"\0" * 12
        self.assertEqual(md5_c(ip_section),
                         hashlib.md5(ip_section + b"\x14\x00\x07\x0b").digest()[:8])

    def test_checksum_uses_little_endian_xor_and_1968_multiplier(self):
        self.assertEqual(checksum_d_series(bytes.fromhex("010203040506")),
                         bytes.fromhex("206d16d7"))

    def test_challenge_response_places_salt_and_source_ip(self):
        response = build_challenge_response(bytes.fromhex("01020304"), IPv4Address("127.0.0.1"))
        self.assertEqual(len(response), 16)
        self.assertEqual(response[0], 0x02)
        self.assertEqual(response[4:8], bytes.fromhex("01020304"))
        self.assertEqual(response[8:12], IPv4Address("127.0.0.1").packed)

    def test_challenge_response_rejects_invalid_salt_lengths(self):
        for salt in (b"\0" * 3, b"\0" * 5):
            with self.subTest(salt=salt):
                with self.assertRaisesRegex(ValueError, "salt must contain four bytes"):
                    build_challenge_response(salt, IPv4Address("127.0.0.1"))

    def test_challenge_response_rejects_non_ipv4_address(self):
        with self.assertRaisesRegex(TypeError, "source_ip must be an IPv4Address"):
            build_challenge_response(b"\0" * 4, "127.0.0.1")

    def test_login_responses_use_reference_offsets(self):
        auth_info = bytes(range(16))
        response = build_login_success_response(auth_info, 14208, 10000)
        self.assertEqual(len(response), 64)
        self.assertEqual(response[0], 0x04)
        self.assertEqual(struct.unpack_from("<I", response, 9)[0], 14208)
        self.assertEqual(struct.unpack_from("<I", response, 13)[0], 10000)
        self.assertEqual(response[23:39], auth_info)
        self.assertEqual(build_login_failure_response(0x17)[0:5], b"\x05\0\0\0\x17")

    def test_login_success_response_rejects_invalid_auth_info_lengths(self):
        for auth_info in (b"\0" * 15, b"\0" * 17):
            with self.subTest(auth_info=auth_info):
                with self.assertRaisesRegex(ValueError, "auth_info must contain 16 bytes"):
                    build_login_success_response(auth_info, 14208, 10000)

    def test_keepalive_and_logout_response_layouts(self):
        self.assertEqual(len(build_ka1_response()), 20)
        self.assertEqual(build_ka1_response()[0], 0x07)
        ka2 = build_ka2_response(7, 3, b"tail", 5, 6, 70000, 8)
        self.assertEqual(len(ka2), 60)
        self.assertEqual(ka2[0:2], b"\x07\x07")
        self.assertEqual(ka2[5], 3)
        self.assertEqual(ka2[16:20], b"tail")
        self.assertEqual(struct.unpack_from("<IIII", ka2, 44), (5, 6, 70000, 8))
        self.assertEqual(build_logout_response(), b"\x04\0\0\0")

    def test_ka2_response_rejects_invalid_tail_lengths(self):
        for tail in (b"\0" * 3, b"\0" * 5):
            with self.subTest(tail=tail):
                with self.assertRaisesRegex(ValueError, "tail must contain four bytes"):
                    build_ka2_response(7, 3, tail, 5, 6, 70000, 8)

    def test_hmac_tokens_are_stable_and_domain_separated(self):
        secret = b"s" * 32
        auth = derive_auth_info(secret, ("127.0.0.1", 50000), "student-test",
                                b"salt", 1)
        tail = derive_ka2_tail(secret, auth, b"\0" * 4, 0, 1)
        self.assertEqual(len(auth), 16)
        self.assertEqual(len(tail), 4)
        self.assertNotEqual(auth[:4], tail)
        self.assertEqual(auth, derive_auth_info(secret, ("127.0.0.1", 50000),
                                                "student-test", b"salt", 1))

    def test_detailed_hmac_derivations_expose_material_and_legacy_outputs(self):
        secret = b"s" * 32
        salt = bytes.fromhex("01020304")
        derived = derive_auth_info_detailed(
            secret, ("127.0.0.1", 50000), "student-test", salt, 1)
        self.assertIn(b"student-test", derived.input)
        self.assertEqual(len(derived.output), 16)
        self.assertEqual(
            derive_auth_info(secret, ("127.0.0.1", 50000), "student-test", salt, 1),
            derived.output,
        )

        tail = derive_ka2_tail_detailed(secret, derived.output, b"\0" * 4, 2, 3)
        self.assertEqual(tail.input[:9], b"ka2-tail\0")
        self.assertEqual(len(tail.output), 4)
        self.assertEqual(
            derive_ka2_tail(secret, derived.output, b"\0" * 4, 2, 3), tail.output)


if __name__ == "__main__":
    unittest.main()
