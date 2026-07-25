import re
import unittest
from pathlib import Path


LUA_PATH = Path(__file__).parents[2] / "wireshark" / "drcom.lua"

REQUIRED_FIELDS = {
    "drcom.schema_version", "drcom.opcode", "drcom.packet_kind",
    "drcom.direction", "drcom.profile", "drcom.captured_length",
    "drcom.reported_length", "drcom.valid", "drcom.truncated",
    "drcom.malformed", "drcom.malformed_reason", "drcom.unknown",
    "drcom.payload", "drcom.challenge.seed", "drcom.challenge.magic",
    "drcom.challenge.salt", "drcom.challenge.client_ip",
    "drcom.login.username_length", "drcom.login.username_bytes",
    "drcom.login.md5_a", "drcom.login.control_check_status",
    "drcom.login.adapter_num", "drcom.login.mac_xor", "drcom.login.md5_b",
    "drcom.login.nic_count", "drcom.login.ipv4_1", "drcom.login.ipv4_2",
    "drcom.login.ipv4_3", "drcom.login.ipv4_4", "drcom.login.md5_c",
    "drcom.login.ipdog", "drcom.login.hostname_bytes",
    "drcom.login.primary_dns", "drcom.login.dhcp",
    "drcom.login.secondary_dns", "drcom.login.os_info",
    "drcom.login.os_name_bytes", "drcom.login.auth_version",
    "drcom.login.auth_ext_marker", "drcom.login.auth_ext_crc",
    "drcom.login.auth_ext_mac", "drcom.login.auth_ext_tail",
    "drcom.response.month_traffic_kib", "drcom.response.balance_cents",
    "drcom.response.auth_info", "drcom.response.error_code",
    "drcom.ka1.md5_a", "drcom.ka1.auth_info", "drcom.ka1.timestamp",
    "drcom.ka1.padding", "drcom.ka2.serial", "drcom.ka2.type",
    "drcom.ka2.version", "drcom.ka2.tail", "drcom.ka2.reported_ipv4",
    "drcom.ka2.month_time", "drcom.ka2.traffic_kib",
    "drcom.ka2.balance_ten_thousandths", "drcom.ka2.remaining_seconds",
    "drcom.legacy.subtype", "drcom.legacy.step",
}

REASONS = {
    "missing_opcode", "captured_shorter_than_reported", "required_bytes_missing",
    "unexpected_direction", "invalid_exact_length", "invalid_fixed_bytes",
    "invalid_username_length", "unknown_opcode", "unknown_subtype",
    "internal_dissector_error",
}

LEGACY_KINDS = {
    "legacy_start_request", "legacy_start_response", "legacy_login",
    "legacy_success", "legacy_failure", "legacy_logout", "legacy_misc",
    "legacy_misc_0800", "legacy_misc_1000", "legacy_misc_1001",
    "legacy_misc_2800", "legacy_misc_3000", "legacy_misc_f400",
    "legacy_unknown_08", "legacy_new_password_09", "legacy_message",
    "legacy_message_25", "legacy_message_26", "legacy_message_38",
    "legacy_message_3a", "legacy_alive_fe", "legacy_alive_ff",
}


class LuaContractTests(unittest.TestCase):
    def source(self):
        return LUA_PATH.read_text(encoding="utf-8")

    def test_lua_registers_all_machine_fields_once(self):
        source = self.source()
        fields = re.findall(r'ProtoField\.[^(]+\("([^"]+)"', source)
        self.assertEqual(len(fields), len(set(fields)))
        self.assertTrue(REQUIRED_FIELDS <= set(fields))

    def test_has_explicit_range_guards_status_finalizer_and_pcall_boundary(self):
        source = self.source()
        self.assertIn("local function safe_range", source)
        self.assertIn("offset + length > state.captured_length", source)
        self.assertIn("tvb:reported_len()", source)
        self.assertIn("local function finalize_status", source)
        self.assertIn("pcall(dissect_payload", source)
        self.assertIn('mark_malformed(state, "internal_dissector_error"', source)

    def test_registers_port_and_decode_as_and_has_stable_reason_codes(self):
        source = self.source()
        self.assertIn('DissectorTable.get("udp.port")', source)
        self.assertIn("udp_table:add(61440, drcom)", source)
        self.assertIn("udp_table:add_for_decode_as(drcom)", source)
        for reason in REASONS:
            self.assertIn(f'"{reason}"', source)

    def test_preserves_documented_legacy_kinds_and_subtypes(self):
        source = self.source()
        for kind in LEGACY_KINDS:
            self.assertIn(f'"{kind}"', source)
        for subtype in ("0x0800", "0x1000", "0x1001", "0x2800", "0x3000", "0xf400"):
            self.assertIn(f"[{subtype}]", source)
        for subtype in ("0x25", "0x26", "0x38", "0x3a"):
            self.assertIn(f"[{subtype}]", source)

    def test_has_no_historical_typos_or_unchecked_direct_tvb_ranges(self):
        source = self.source()
        self.assertNotIn("unkonwn", source)
        self.assertNotIn("brcomcast", source)
        direct_ranges = re.findall(r"\btvb\s*\([^\n]+", source)
        self.assertEqual(direct_ranges, ["tvb(offset, length)"])


if __name__ == "__main__":
    unittest.main()
