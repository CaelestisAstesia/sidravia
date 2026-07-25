-- Sidravia Dr.COM dissector schema v1.
-- Public drcom.* fields are a machine interface for tshark -T json/-T fields.

local drcom = Proto("drcom", "Dr.COM")
local f = {}

f.schema_version = ProtoField.uint8("drcom.schema_version", "Schema version", base.DEC)
f.opcode = ProtoField.uint8("drcom.opcode", "Opcode", base.HEX)
f.packet_kind = ProtoField.string("drcom.packet_kind", "Packet kind")
f.direction = ProtoField.string("drcom.direction", "Direction")
f.profile = ProtoField.string("drcom.profile", "Profile")
f.captured_length = ProtoField.uint32("drcom.captured_length", "Captured payload length", base.DEC)
f.reported_length = ProtoField.uint32("drcom.reported_length", "Reported payload length", base.DEC)
f.valid = ProtoField.bool("drcom.valid", "Structurally valid")
f.truncated = ProtoField.bool("drcom.truncated", "Truncated")
f.malformed = ProtoField.bool("drcom.malformed", "Malformed")
f.malformed_reason = ProtoField.string("drcom.malformed_reason", "Status reason")
f.unknown = ProtoField.bool("drcom.unknown", "Unknown layout")
f.payload = ProtoField.bytes("drcom.payload", "Unparsed payload")

f.challenge_seed = ProtoField.uint16("drcom.challenge.seed", "Challenge seed", base.HEX)
f.challenge_magic = ProtoField.uint8("drcom.challenge.magic", "Challenge magic", base.HEX)
f.challenge_salt = ProtoField.bytes("drcom.challenge.salt", "Challenge salt")
f.challenge_client_ip = ProtoField.ipv4("drcom.challenge.client_ip", "Challenge client IPv4")

f.login_username_length = ProtoField.uint8("drcom.login.username_length", "Username length marker", base.DEC)
f.login_username_bytes = ProtoField.bytes("drcom.login.username_bytes", "Username bytes (GBK)")
f.login_md5_a = ProtoField.bytes("drcom.login.md5_a", "Login MD5-A")
f.login_control = ProtoField.uint8("drcom.login.control_check_status", "ControlCheckStatus", base.HEX)
f.login_adapter_num = ProtoField.uint8("drcom.login.adapter_num", "Adapter number", base.DEC)
f.login_mac_xor = ProtoField.bytes("drcom.login.mac_xor", "MAC XOR")
f.login_md5_b = ProtoField.bytes("drcom.login.md5_b", "Login MD5-B")
f.login_nic_count = ProtoField.uint8("drcom.login.nic_count", "NIC count", base.DEC)
f.login_ipv4_1 = ProtoField.ipv4("drcom.login.ipv4_1", "IPv4 slot 1")
f.login_ipv4_2 = ProtoField.ipv4("drcom.login.ipv4_2", "IPv4 slot 2")
f.login_ipv4_3 = ProtoField.ipv4("drcom.login.ipv4_3", "IPv4 slot 3")
f.login_ipv4_4 = ProtoField.ipv4("drcom.login.ipv4_4", "IPv4 slot 4")
f.login_md5_c = ProtoField.bytes("drcom.login.md5_c", "Login MD5-C")
f.login_ipdog = ProtoField.uint8("drcom.login.ipdog", "IPDog", base.HEX)
f.login_hostname = ProtoField.bytes("drcom.login.hostname_bytes", "Hostname bytes (GBK)")
f.login_primary_dns = ProtoField.ipv4("drcom.login.primary_dns", "Primary DNS")
f.login_dhcp = ProtoField.ipv4("drcom.login.dhcp", "DHCP server")
f.login_secondary_dns = ProtoField.ipv4("drcom.login.secondary_dns", "Secondary DNS")
f.login_os_info = ProtoField.bytes("drcom.login.os_info", "OS information")
f.login_os_name = ProtoField.bytes("drcom.login.os_name_bytes", "OS name bytes (GBK)")
f.login_auth_version = ProtoField.bytes("drcom.login.auth_version", "Auth version")
f.login_auth_ext_marker = ProtoField.bytes("drcom.login.auth_ext_marker", "Auth extension marker")
f.login_auth_ext_crc = ProtoField.bytes("drcom.login.auth_ext_crc", "Auth extension CRC")
f.login_auth_ext_reserved_before_mac = ProtoField.bytes("drcom.login.auth_ext_reserved_before_mac", "Auth extension reserved before MAC")
f.login_auth_ext_mac = ProtoField.bytes("drcom.login.auth_ext_mac", "Auth extension MAC")
f.login_auth_ext_reserved_after_mac = ProtoField.bytes("drcom.login.auth_ext_reserved_after_mac", "Auth extension reserved after MAC")
f.login_auth_ext_tail = ProtoField.bytes("drcom.login.auth_ext_tail", "Auth extension tail")

f.response_month_traffic = ProtoField.uint32("drcom.response.month_traffic_kib", "Month traffic (KiB)", base.DEC)
f.response_balance_cents = ProtoField.uint32("drcom.response.balance_cents", "Balance (cents)", base.DEC)
f.response_auth_info = ProtoField.bytes("drcom.response.auth_info", "Auth Info")
f.response_error_code = ProtoField.uint8("drcom.response.error_code", "Login error code", base.HEX)

f.ka1_md5_a = ProtoField.bytes("drcom.ka1.md5_a", "KA1 MD5-A")
f.ka1_auth_info = ProtoField.bytes("drcom.ka1.auth_info", "KA1 Auth Info")
f.ka1_timestamp = ProtoField.uint16("drcom.ka1.timestamp", "KA1 timestamp", base.DEC)
f.ka1_padding = ProtoField.bytes("drcom.ka1.padding", "KA1 padding")

f.ka2_serial = ProtoField.uint8("drcom.ka2.serial", "KA2 serial", base.DEC)
f.ka2_type = ProtoField.uint8("drcom.ka2.type", "KA2 type", base.DEC)
f.ka2_version = ProtoField.bytes("drcom.ka2.version", "KA2 version")
f.ka2_tail = ProtoField.bytes("drcom.ka2.tail", "KA2 tail")
f.ka2_reported_ipv4 = ProtoField.ipv4("drcom.ka2.reported_ipv4", "KA2 reported IPv4")
f.ka2_month_time = ProtoField.uint32("drcom.ka2.month_time", "KA2 month time", base.DEC)
f.ka2_traffic = ProtoField.uint32("drcom.ka2.traffic_kib", "KA2 traffic (KiB)", base.DEC)
f.ka2_balance = ProtoField.uint32("drcom.ka2.balance_ten_thousandths", "KA2 balance (ten-thousandths)", base.DEC)
f.ka2_remaining = ProtoField.uint32("drcom.ka2.remaining_seconds", "KA2 remaining seconds", base.DEC)

f.legacy_subtype = ProtoField.uint16("drcom.legacy.subtype", "Legacy subtype", base.HEX)
f.legacy_step = ProtoField.uint8("drcom.legacy.step", "Legacy step", base.HEX)

local registered_fields = {}
for _, field in pairs(f) do
    table.insert(registered_fields, field)
end
drcom.fields = registered_fields

local malformed_expert = ProtoExpert.new(
    "drcom.expert.malformed", "Malformed or truncated Dr.COM payload",
    expert.group.MALFORMED, expert.severity.ERROR
)
local unknown_expert = ProtoExpert.new(
    "drcom.expert.unknown", "Unknown Dr.COM layout",
    expert.group.UNDECODED, expert.severity.NOTE
)
drcom.experts = { malformed_expert, unknown_expert }

local function safe_range(state, offset, length)
    if offset < 0 or length < 0 or offset + length > state.captured_length then
        return nil
    end
    local tvb = state.tvb
    return tvb(offset, length)
end

local function add_range(state, field, offset, length, little_endian)
    local range = safe_range(state, offset, length)
    if range == nil then
        return nil
    end
    if little_endian then
        state.root:add_le(field, range)
    else
        state.root:add(field, range)
    end
    return range
end

local function range_hex(state, offset, length)
    local range = safe_range(state, offset, length)
    if range == nil then
        return nil
    end
    return range:bytes():tohex(true, "")
end

local function is_zero(state, offset, length)
    local value = range_hex(state, offset, length)
    return value ~= nil and value == string.rep("00", length)
end

local function set_reason(state, reason)
    if state.reason == nil then
        state.reason = reason
    end
end

local function mark_truncated(state, reason, text)
    state.truncated = true
    set_reason(state, reason)
    state.root:add_proto_expert_info(malformed_expert, text or reason)
end

local function mark_malformed(state, reason, text)
    state.malformed = true
    set_reason(state, reason)
    state.root:add_proto_expert_info(malformed_expert, text or reason)
end

local function mark_unknown(state, reason, text)
    state.unknown = true
    set_reason(state, reason)
    state.root:add_proto_expert_info(unknown_expert, text or reason)
end

local function classify(state, profile, packet_kind)
    state.known = true
    state.profile = profile
    state.packet_kind = packet_kind
end

local function require_min(state, minimum)
    if state.reported_length < minimum or state.captured_length < minimum then
        mark_truncated(state, "required_bytes_missing", "Known packet is shorter than its required layout")
        return false
    end
    return true
end

local function require_exact(state, expected)
    local complete = true
    if state.reported_length ~= expected then
        mark_malformed(state, "invalid_exact_length", "Known packet has an invalid exact length")
        complete = false
    end
    if state.captured_length < expected then
        mark_truncated(state, "required_bytes_missing", "Known packet is missing captured bytes")
        complete = false
    end
    return complete
end

local function add_remaining_payload(state, offset)
    if offset < state.captured_length then
        add_range(state, f.payload, offset, state.captured_length - offset, false)
    end
end

local function validate_username(state)
    local marker_range = add_range(state, f.login_username_length, 3, 1, false)
    local username_range = add_range(state, f.login_username_bytes, 20, 36, false)
    if marker_range == nil or username_range == nil then
        mark_truncated(state, "required_bytes_missing", "Username fields are truncated")
        return
    end
    local marker = marker_range:uint()
    local used = 36
    local padding_started = false
    local padding_valid = true
    for index = 0, 35 do
        local byte_range = safe_range(state, 20 + index, 1)
        local value = byte_range:uint()
        if value == 0 and not padding_started then
            used = index
            padding_started = true
        elseif value ~= 0 and padding_started then
            padding_valid = false
        end
    end
    if marker < 20 or marker > 56 or marker ~= 20 + used then
        mark_malformed(state, "invalid_username_length", "Username length marker is inconsistent")
    elseif not padding_valid then
        mark_malformed(state, "invalid_fixed_bytes", "Username padding contains non-zero bytes")
    end
end

local function dissect_challenge_request(state)
    classify(state, "jlu_520d", "challenge_request")
    if not require_min(state, 20) then return end
    if range_hex(state, 0, 2) ~= "0102" then
        mark_malformed(state, "invalid_fixed_bytes", "Challenge request header must be 01 02")
    end
    add_range(state, f.challenge_seed, 2, 2, true)
    add_range(state, f.challenge_magic, 4, 1, false)
    if range_hex(state, 4, 1) ~= "09" then
        mark_malformed(state, "invalid_fixed_bytes", "Challenge magic must be 09")
    end
    add_remaining_payload(state, 5)
end

local function dissect_challenge_response(state)
    classify(state, "jlu_520d", "challenge_response")
    if not require_min(state, 16) then return end
    add_range(state, f.challenge_salt, 4, 4, false)
    add_range(state, f.challenge_client_ip, 8, 4, false)
    add_remaining_payload(state, 12)
end

local function dissect_login_request(state)
    classify(state, "jlu_520d", "login_request")
    if not require_exact(state, 330) then return end
    if range_hex(state, 0, 3) ~= "030100" then
        mark_malformed(state, "invalid_fixed_bytes", "Login request header must be 03 01 00")
    end
    validate_username(state)
    add_range(state, f.login_md5_a, 4, 16, false)
    add_range(state, f.login_control, 56, 1, false)
    add_range(state, f.login_adapter_num, 57, 1, false)
    add_range(state, f.login_mac_xor, 58, 6, false)
    add_range(state, f.login_md5_b, 64, 16, false)
    add_range(state, f.login_nic_count, 80, 1, false)
    add_range(state, f.login_ipv4_1, 81, 4, false)
    add_range(state, f.login_ipv4_2, 85, 4, false)
    add_range(state, f.login_ipv4_3, 89, 4, false)
    add_range(state, f.login_ipv4_4, 93, 4, false)
    add_range(state, f.login_md5_c, 97, 8, false)
    add_range(state, f.login_ipdog, 105, 1, false)
    add_range(state, f.login_hostname, 110, 32, false)
    add_range(state, f.login_primary_dns, 142, 4, false)
    add_range(state, f.login_dhcp, 146, 4, false)
    add_range(state, f.login_secondary_dns, 150, 4, false)
    add_range(state, f.login_os_info, 162, 20, false)
    add_range(state, f.login_os_name, 182, 32, false)
    add_range(state, f.login_auth_version, 310, 2, false)
    add_range(state, f.login_auth_ext_marker, 312, 2, false)
    add_range(state, f.login_auth_ext_crc, 314, 4, false)
    add_range(state, f.login_auth_ext_reserved_before_mac, 318, 2, false)
    add_range(state, f.login_auth_ext_mac, 320, 6, false)
    add_range(state, f.login_auth_ext_reserved_after_mac, 326, 2, false)
    add_range(state, f.login_auth_ext_tail, 328, 2, false)
    if range_hex(state, 312, 2) ~= "020c" or
       not is_zero(state, 318, 2) or not is_zero(state, 326, 2) then
        mark_malformed(state, "invalid_fixed_bytes", "Login auth extension fixed bytes are invalid")
    end
end

local function dissect_login_success(state)
    classify(state, "jlu_520d", "login_success")
    if not require_min(state, 39) then return end
    add_range(state, f.response_month_traffic, 9, 4, true)
    add_range(state, f.response_balance_cents, 13, 4, true)
    add_range(state, f.response_auth_info, 23, 16, false)
    add_remaining_payload(state, 39)
end

local function dissect_logout_ack(state)
    classify(state, "jlu_520d", "logout_ack")
    if not require_exact(state, 4) then return end
    if range_hex(state, 0, 4) ~= "04000000" then
        mark_malformed(state, "invalid_fixed_bytes", "Logout ACK fixed bytes are invalid")
    end
end

local function dissect_login_failure(state)
    classify(state, "jlu_520d", "login_failure")
    if not require_min(state, 5) then return end
    add_range(state, f.response_error_code, 4, 1, false)
    add_remaining_payload(state, 5)
end

local function dissect_ka1_request(state)
    classify(state, "jlu_520d", "ka1_request")
    if state.reported_length ~= 38 and state.reported_length ~= 42 then
        mark_malformed(state, "invalid_exact_length", "KA1 request must contain 38 or 42 bytes")
        if state.captured_length < 38 then
            mark_truncated(state, "required_bytes_missing", "KA1 request is truncated")
        end
        return
    end
    if state.captured_length < state.reported_length then
        mark_truncated(state, "required_bytes_missing", "KA1 request is truncated")
        return
    end
    add_range(state, f.ka1_md5_a, 1, 16, false)
    add_range(state, f.ka1_padding, 17, 3, false)
    add_range(state, f.ka1_auth_info, 20, 16, false)
    add_range(state, f.ka1_timestamp, 36, 2, false)
    if not is_zero(state, 17, 3) then
        mark_malformed(state, "invalid_fixed_bytes", "KA1 fixed padding is not zero")
    end
    if state.reported_length == 42 then
        add_range(state, f.ka1_padding, 38, 4, false)
        if not is_zero(state, 38, 4) then
            mark_malformed(state, "invalid_fixed_bytes", "KA1 trailing padding is not zero")
        end
    end
end

local function dissect_ka1_response(state)
    classify(state, "jlu_520d", "ka1_response")
    require_exact(state, 20)
    add_remaining_payload(state, 1)
end

local function dissect_ka2_request(state)
    classify(state, "jlu_520d", "ka2_request")
    if not require_exact(state, 40) then return end
    add_range(state, f.ka2_serial, 1, 1, false)
    add_range(state, f.ka2_type, 5, 1, false)
    add_range(state, f.ka2_version, 6, 2, false)
    add_range(state, f.ka2_tail, 16, 4, false)
    local packet_type = safe_range(state, 5, 1):uint()
    local version = range_hex(state, 6, 2)
    if range_hex(state, 2, 3) ~= "28000b" or
       range_hex(state, 8, 2) ~= "2f12" or
       not is_zero(state, 10, 6) or not is_zero(state, 20, 4) or
       (version ~= "0f27" and version ~= "dc02") then
        mark_malformed(state, "invalid_fixed_bytes", "KA2 fixed bytes or version are invalid")
    end
    if packet_type == 1 then
        if not is_zero(state, 24, 16) then
            mark_malformed(state, "invalid_fixed_bytes", "KA2 type 1 tail area is not zero")
        end
    elseif packet_type == 3 then
        add_range(state, f.ka2_reported_ipv4, 28, 4, false)
        if not is_zero(state, 24, 4) or not is_zero(state, 32, 8) then
            mark_malformed(state, "invalid_fixed_bytes", "KA2 type 3 reserved bytes are not zero")
        end
    else
        mark_malformed(state, "invalid_fixed_bytes", "KA2 type must be 1 or 3")
    end
end

local function dissect_ka2_response(state)
    classify(state, "jlu_520d", "ka2_response")
    if not require_min(state, 20) then return end
    add_range(state, f.ka2_serial, 1, 1, false)
    add_range(state, f.ka2_type, 5, 1, false)
    add_range(state, f.ka2_tail, 16, 4, false)
    if state.reported_length == 60 and state.captured_length >= 60 then
        add_range(state, f.ka2_month_time, 44, 4, true)
        add_range(state, f.ka2_traffic, 48, 4, true)
        add_range(state, f.ka2_balance, 52, 4, true)
        add_range(state, f.ka2_remaining, 56, 4, true)
    end
    add_remaining_payload(state, 20)
end

local function dissect_logout_request(state)
    classify(state, "jlu_520d", "logout_request")
    if not require_exact(state, 80) then return end
    if range_hex(state, 0, 3) ~= "060100" then
        mark_malformed(state, "invalid_fixed_bytes", "Logout request header must be 06 01 00")
    end
    validate_username(state)
    add_range(state, f.login_md5_a, 4, 16, false)
    add_range(state, f.login_control, 56, 1, false)
    add_range(state, f.login_adapter_num, 57, 1, false)
    add_range(state, f.login_mac_xor, 58, 6, false)
    add_range(state, f.response_auth_info, 64, 16, false)
end

local legacy_misc_subtypes = {
    [0x0800] = "legacy_misc_0800",
    [0x1000] = "legacy_misc_1000",
    [0x1001] = "legacy_misc_1001",
    [0x2800] = "legacy_misc_2800",
    [0x3000] = "legacy_misc_3000",
    [0xf400] = "legacy_misc_f400",
}

local legacy_message_subtypes = {
    [0x25] = "legacy_message_25",
    [0x26] = "legacy_message_26",
    [0x38] = "legacy_message_38",
    [0x3a] = "legacy_message_3a",
}

local legacy_opcode_kinds = {
    [0x01] = "legacy_start_request",
    [0x02] = "legacy_start_response",
    [0x03] = "legacy_login",
    [0x04] = "legacy_success",
    [0x05] = "legacy_failure",
    [0x06] = "legacy_logout",
    [0x08] = "legacy_unknown_08",
    [0x09] = "legacy_new_password_09",
    [0x4d] = "legacy_message",
    [0xfe] = "legacy_alive_fe",
    [0xff] = "legacy_alive_ff",
}

local function dissect_legacy(state, opcode)
    local kind = legacy_opcode_kinds[opcode]
    if opcode == 0x07 then
        kind = "legacy_misc"
        if state.captured_length >= 4 then
            local subtype_range = add_range(state, f.legacy_subtype, 2, 2, false)
            local subtype = subtype_range:uint()
            if legacy_misc_subtypes[subtype] ~= nil then
                kind = legacy_misc_subtypes[subtype]
            else
                mark_unknown(state, "unknown_subtype", "Unknown legacy 0x07 subtype")
            end
        else
            mark_truncated(state, "required_bytes_missing", "Legacy 0x07 subtype is truncated")
        end
    elseif opcode == 0x4d then
        if state.captured_length >= 2 then
            local step_range = add_range(state, f.legacy_step, 1, 1, false)
            local subtype = step_range:uint()
            if legacy_message_subtypes[subtype] ~= nil then
                kind = legacy_message_subtypes[subtype]
            else
                mark_unknown(state, "unknown_subtype", "Unknown legacy 0x4d subtype")
            end
        else
            mark_truncated(state, "required_bytes_missing", "Legacy message subtype is truncated")
        end
    end
    if kind == nil then
        classify(state, "unknown", "unknown")
        mark_unknown(state, "unknown_opcode", "Unknown Dr.COM opcode")
    else
        classify(state, "legacy_2011", kind)
    end
    add_remaining_payload(state, 1)
end

local function dissect_payload(state, opcode)
    local direction = state.direction
    if opcode == 0x01 and direction == "client_to_server" then
        dissect_challenge_request(state)
    elseif opcode == 0x02 and direction == "server_to_client" then
        dissect_challenge_response(state)
    elseif opcode == 0x03 and direction == "client_to_server" then
        dissect_login_request(state)
    elseif opcode == 0x04 and direction == "server_to_client" then
        if state.reported_length == 4 then
            dissect_logout_ack(state)
        else
            dissect_login_success(state)
        end
    elseif opcode == 0x05 and direction == "server_to_client" then
        dissect_login_failure(state)
    elseif opcode == 0x06 and direction == "client_to_server" then
        dissect_logout_request(state)
    elseif opcode == 0xff and direction == "client_to_server" then
        dissect_ka1_request(state)
    elseif opcode == 0x07 and direction == "server_to_client" and
           state.reported_length == 20 then
        dissect_ka1_response(state)
    elseif opcode == 0x07 and direction == "server_to_client" then
        dissect_ka2_response(state)
    elseif opcode == 0x07 and direction == "client_to_server" and
           (state.reported_length == 40 or range_hex(state, 2, 3) == "28000b") then
        dissect_ka2_request(state)
    elseif direction == "unknown" and
           (opcode == 0x01 or opcode == 0x02 or opcode == 0x03 or
            opcode == 0x04 or opcode == 0x05 or opcode == 0x06 or
            opcode == 0x07 or opcode == 0xff) then
        classify(state, "unknown", "unknown")
        mark_unknown(state, "unexpected_direction", "Packet direction is ambiguous")
        add_remaining_payload(state, 1)
    else
        dissect_legacy(state, opcode)
    end
end

local function finalize_status(state)
    local valid = state.known and not state.truncated and
        not state.malformed and not state.unknown
    state.root:add(f.schema_version, 1)
    state.root:add(f.packet_kind, state.packet_kind)
    state.root:add(f.direction, state.direction)
    state.root:add(f.profile, state.profile)
    state.root:add(f.captured_length, state.captured_length)
    state.root:add(f.reported_length, state.reported_length)
    state.root:add(f.valid, valid)
    state.root:add(f.truncated, state.truncated)
    state.root:add(f.malformed, state.malformed)
    if state.reason ~= nil then
        state.root:add(f.malformed_reason, state.reason)
    end
    state.root:add(f.unknown, state.unknown)
end

function drcom.dissector(tvb, pinfo, tree)
    pinfo.cols.protocol = "DRCOM"
    local captured_length = tvb:len()
    local state = {
        tvb = tvb,
        captured_length = captured_length,
        reported_length = tvb:reported_len(),
        root = nil,
        direction = "unknown",
        profile = "unknown",
        packet_kind = "unknown",
        known = false,
        truncated = false,
        malformed = false,
        unknown = false,
        reason = nil,
    }
    local packet_range = safe_range(state, 0, captured_length)
    if packet_range ~= nil then
        state.root = tree:add(drcom, packet_range, "Dr.COM")
    else
        state.root = tree:add(drcom, "Dr.COM")
    end

    local source_port = tonumber(pinfo.src_port)
    local destination_port = tonumber(pinfo.dst_port)
    if destination_port == 61440 and source_port ~= 61440 then
        state.direction = "client_to_server"
    elseif source_port == 61440 and destination_port ~= 61440 then
        state.direction = "server_to_client"
    end

    if state.captured_length < state.reported_length then
        mark_truncated(state, "captured_shorter_than_reported", "Capture cut the UDP payload short")
    end

    local opcode_range = safe_range(state, 0, 1)
    if opcode_range == nil then
        mark_truncated(state, "missing_opcode", "Dr.COM payload has no captured opcode")
        finalize_status(state)
        return
    end
    state.root:add(f.opcode, opcode_range)
    local opcode = opcode_range:uint()
    local ok = pcall(dissect_payload, state, opcode)
    if not ok then
        mark_malformed(state, "internal_dissector_error", "Internal dissector error")
        add_remaining_payload(state, 1)
    end
    finalize_status(state)
end

local udp_table = DissectorTable.get("udp.port")
udp_table:add(61440, drcom)
udp_table:add_for_decode_as(drcom)
