from __future__ import annotations

import asyncio
import hashlib
import hmac
import secrets
import time
from collections.abc import Callable
from dataclasses import asdict
from ipaddress import IPv4Address
from typing import Any

from .codec import (
    PacketFormatError,
    build_challenge_response,
    build_ka1_response,
    build_ka2_response,
    build_login_failure_response,
    build_login_success_response,
    build_logout_response,
    derive_auth_info_detailed,
    derive_ka2_tail_detailed,
    parse_ka1_request,
    parse_ka2_request,
    parse_login_request,
    parse_logout_request,
    verify_ka1_detailed,
    verify_login_crypto,
    verify_logout_crypto_detailed,
)
from .models import (
    Account,
    ApplicationConfig,
    AuthErrorCode,
    AuthenticatedSession,
    BillingState,
    Operation,
    PendingChallenge,
    ServerMetrics,
)
from .scenarios import ActionKind, ScenarioAction, ScenarioProgram
from .trace import NullTraceSink, TraceSink
from .transcript import decision_record, packet_record


def classify_operation(data: bytes) -> Operation | None:
    """Classify a datagram without accepting its operation-specific structure."""
    if data.startswith(b"\x01\x02"):
        return Operation.CHALLENGE
    if data.startswith(b"\x03\x01"):
        return Operation.LOGIN
    if data.startswith(b"\x06\x01"):
        return Operation.LOGOUT
    if data.startswith(b"\xff"):
        return Operation.KA1
    if data.startswith(b"\x07"):
        return Operation.KA2
    return None


class ServerCore:
    """Stateful, in-process core for the local Dr.COM mock UDP server."""

    def __init__(self, config: ApplicationConfig, scenario: ScenarioProgram,
                 *, clock: Callable[[], float] = time.monotonic,
                 random_bytes: Callable[[int], bytes] = secrets.token_bytes,
                 trace: TraceSink | None = None):
        self.config = config
        self.scenario = scenario
        self.clock = clock
        self.random_bytes = random_bytes
        self.trace = trace if trace is not None else NullTraceSink()
        self.secret = config.server.server_secret or random_bytes(32)
        self.challenges: dict[tuple[str, int], PendingChallenge] = {}
        self.sessions: dict[tuple[str, int], AuthenticatedSession] = {}
        self.account_sessions: dict[str, set[tuple[str, int]]] = {}
        self.logout_tombstones: dict[tuple[str, int], tuple[float, bytes, bytes]] = {}
        self.metrics = ServerMetrics()
        self._session_number = 0
        self._state_lock = asyncio.Lock()

    async def handle_datagram(
        self,
        data: bytes,
        endpoint: tuple[str, int],
        *,
        local_endpoint: tuple[str, int] | None = None,
    ) -> bytes | None:
        operation = classify_operation(data)
        async with self._state_lock:
            self.metrics.datagrams_received += 1
            received = packet_record(
                side="server",
                direction="receive",
                operation=operation.value if operation is not None else None,
                payload=data,
                local_endpoint=local_endpoint,
                remote_endpoint=endpoint,
            )
            self._trace(
                "datagram_received",
                operation=operation,
                endpoint=endpoint,
                detail_fields=received,
                packet=data,
                length=len(data),
                first_byte=data[0] if data else None,
            )
            self._trace(
                "operation_classified",
                operation=operation,
                endpoint=endpoint,
                classification=(operation.value if operation is not None else "unknown"),
                first_byte=data[0] if data else None,
            )
            if operation is None:
                return self._drop(
                    "unknown_operation", operation=None, endpoint=endpoint,
                    packet=data)
            action = self.scenario.next_action(operation)
            self._trace(
                "scenario_action_selected",
                operation=operation,
                endpoint=endpoint,
                action=action,
                operation_count=self.scenario.counters[operation],
            )
            if action.kind is ActionKind.DROP:
                return self._drop(
                    "scenario_drop", operation=operation, endpoint=endpoint,
                    action=action)

        if action.kind is ActionKind.DELAY:
            await asyncio.sleep((action.delay_milliseconds or 0) / 1000)

        async with self._state_lock:
            now = self.clock()
            before_expiry = self.state_snapshot()
            login_challenge = (
                self.challenges.get(endpoint)
                if operation is Operation.LOGIN else None)
            login_challenge_expired = (
                login_challenge is not None and self._is_expired(
                    login_challenge.issued_at,
                    self.config.server.challenge_ttl_seconds,
                    now,
                )
            )
            self._remove_expired_state(now, operation=operation)
            if login_challenge_expired:
                self._trace(
                    "state_changed",
                    operation=operation,
                    endpoint=endpoint,
                    reason="challenge_expired",
                    before=before_expiry,
                    after=self.state_snapshot(),
                )
            if action.kind is ActionKind.EXPIRE_SESSION:
                self._remove_session(
                    endpoint,
                    operation=operation,
                    reason="scenario_expire_session",
                )

            if operation is Operation.LOGIN and action.kind is ActionKind.REJECT:
                response = self._login_failure(
                    action.reject_code or 0,
                    endpoint=endpoint,
                    reason="scenario_reject",
                    action=action,
                )
            elif operation is Operation.CHALLENGE:
                response = self._handle_challenge(data, endpoint, now=now)
            elif operation is Operation.LOGIN:
                response = self._handle_login(
                    data, endpoint,
                    now=now,
                    challenge_was_expired=login_challenge_expired)
            elif operation is Operation.KA1:
                response = self._handle_ka1(data, endpoint, now=now)
            elif operation is Operation.KA2:
                response = self._handle_ka2(data, endpoint, now=now)
            elif operation is Operation.LOGOUT:
                response = self._handle_logout(data, endpoint, now=now)
            else:
                response = self._drop(
                    "operation_not_handled", operation=operation,
                    endpoint=endpoint)

            wire_response = self._mutate_response(action, response)
            if response is not None and wire_response != response:
                self._trace(
                    "response_mutated",
                    operation=operation,
                    endpoint=endpoint,
                    action=action,
                    before=response,
                    after=wire_response,
                )
            if wire_response is not None:
                sent = packet_record(
                    side="server",
                    direction="send",
                    operation=operation.value,
                    payload=wire_response,
                    local_endpoint=local_endpoint,
                    remote_endpoint=endpoint,
                )
                self._trace(
                    "datagram_sent",
                    operation=operation,
                    endpoint=endpoint,
                    detail_fields=sent,
                    packet=wire_response,
                    length=len(wire_response),
                    first_byte=wire_response[0] if wire_response else None,
                )
            return wire_response

    def _handle_challenge(self, data: bytes, endpoint: tuple[str, int], *,
                          now: float) -> bytes | None:
        if len(data) < 20 or data[:2] != b"\x01\x02":
            error = "Challenge packet must contain at least 20 bytes"
            self._trace(
                "packet_parse_failed",
                operation=Operation.CHALLENGE,
                endpoint=endpoint,
                reason="challenge_parse_failed",
                parse_error=error,
                packet=data,
            )
            return self._drop(
                "challenge_parse_failed", operation=Operation.CHALLENGE,
                endpoint=endpoint, parse_error=error)
        try:
            source_ip = IPv4Address(endpoint[0])
        except ValueError as error:
            self._trace(
                "packet_parse_failed",
                operation=Operation.CHALLENGE,
                endpoint=endpoint,
                reason="challenge_source_ip_invalid",
                parse_error=str(error),
                packet=data,
            )
            return self._drop(
                "challenge_source_ip_invalid", operation=Operation.CHALLENGE,
                endpoint=endpoint, parse_error=str(error))
        self._trace(
            "packet_parsed",
            operation=Operation.CHALLENGE,
            endpoint=endpoint,
            request=data,
            source_ip=source_ip,
        )
        before = self.state_snapshot()
        salt = self.random_bytes(4)
        if len(salt) != 4:
            raise ValueError("random_bytes must return the requested number of bytes")
        self.challenges[endpoint] = PendingChallenge(salt=salt, issued_at=now)
        self._decision(
            Operation.CHALLENGE,
            endpoint,
            decision="challenge_accepted",
            outcome="success",
            salt=salt,
            source_ip=source_ip,
        )
        self._trace(
            "state_changed",
            operation=Operation.CHALLENGE,
            endpoint=endpoint,
            reason="challenge_created",
            before=before,
            after=self.state_snapshot(),
        )
        return build_challenge_response(salt, source_ip)

    def _handle_login(self, data: bytes,
                      endpoint: tuple[str, int], *,
                      now: float,
                      challenge_was_expired: bool = False) -> bytes | None:
        try:
            request = parse_login_request(data)
        except PacketFormatError as error:
            self._trace(
                "packet_parse_failed",
                operation=Operation.LOGIN,
                endpoint=endpoint,
                reason="login_parse_failed",
                parse_error=str(error),
                packet=data,
            )
            return self._drop(
                "login_parse_failed", operation=Operation.LOGIN,
                endpoint=endpoint, parse_error=str(error))

        self._trace(
            "packet_parsed",
            operation=Operation.LOGIN,
            endpoint=endpoint,
            request=request,
            packet=data,
        )

        challenge = self.challenges.get(endpoint)
        if challenge is None:
            self.challenges.pop(endpoint, None)
            reason = (
                "login_challenge_expired"
                if challenge_was_expired else "login_challenge_missing")
            return self._drop(
                reason, operation=Operation.LOGIN, endpoint=endpoint,
                username=request.username)
        if self._is_expired(
                challenge.issued_at,
                self.config.server.challenge_ttl_seconds,
                now):
            before = self.state_snapshot()
            self.challenges.pop(endpoint, None)
            self._trace(
                "state_changed",
                operation=Operation.LOGIN,
                endpoint=endpoint,
                reason="challenge_expired",
                before=before,
                after=self.state_snapshot(),
            )
            return self._drop(
                "login_challenge_expired", operation=Operation.LOGIN,
                endpoint=endpoint, username=request.username)

        account = self.config.accounts.get(request.username)
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="account_lookup",
            outcome="pass" if account is not None else "failure",
            priority=1,
            username=request.username,
            account=account,
        )
        if account is None:
            return self._login_failure(
                AuthErrorCode.WRONG_PASSWORD,
                endpoint=endpoint,
                reason="account_not_found",
                priority=1,
                username=request.username,
                account=None,
            )
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="account_enabled",
            outcome="pass" if account.enabled else "failure",
            priority=2,
            enabled=account.enabled,
            account=account,
        )
        if not account.enabled:
            return self._login_failure(
                AuthErrorCode.WRONG_PASSWORD,
                endpoint=endpoint,
                reason="account_disabled",
                priority=2,
                username=request.username,
                account=account,
            )

        crypto = verify_login_crypto(data, request, account.password, challenge.salt)
        self._trace(
            "crypto_check",
            operation=Operation.LOGIN,
            endpoint=endpoint,
            username=account.username,
            password=account.password,
            salt=challenge.salt,
            verification=crypto,
        )
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="password_digests",
            outcome=("pass" if crypto.md5_a_valid and crypto.md5_b_valid
                     else "failure"),
            priority=3,
            md5_a=crypto.md5_a,
            md5_b=crypto.md5_b,
        )
        if not crypto.md5_a_valid or not crypto.md5_b_valid:
            return self._login_failure(
                AuthErrorCode.WRONG_PASSWORD,
                endpoint=endpoint,
                reason="password_digest_mismatch",
                priority=3,
                md5_a=crypto.md5_a,
                md5_b=crypto.md5_b,
            )
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="crc",
            outcome="pass" if crypto.crc_valid else "drop",
            priority=4,
            comparison=crypto.crc,
        )
        if not crypto.crc_valid:
            return self._drop(
                "login_crc_mismatch", operation=Operation.LOGIN,
                endpoint=endpoint, priority=4, comparison=crypto.crc)
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="account_frozen",
            outcome="failure" if account.frozen else "pass",
            priority=5,
            frozen=account.frozen,
        )
        if account.frozen:
            return self._login_failure(
                AuthErrorCode.ACCOUNT_FROZEN,
                endpoint=endpoint,
                reason="account_frozen",
                priority=5)
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="account_balance",
            outcome="pass" if account.balance_cents > 0 else "failure",
            priority=6,
            balance_cents=account.balance_cents,
        )
        if account.balance_cents <= 0:
            return self._login_failure(
                AuthErrorCode.INSUFFICIENT_FUNDS,
                endpoint=endpoint,
                reason="insufficient_funds",
                priority=6)
        jlu_fields_valid = self._has_expected_jlu_fields(request)
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="jlu_fields",
            outcome="pass" if jlu_fields_valid else "failure",
            priority=7,
            control_status={
                "received": request.control_status,
                "expected": self.config.server.control_check_status[0],
            },
            adapter_num={"received": request.adapter_num, "expected": 1},
            ipdog={
                "received": request.ipdog,
                "expected": self.config.server.ipdog[0],
            },
            auth_version={
                "received": request.auth_version,
                "expected": self.config.server.auth_version,
            },
        )
        if not jlu_fields_valid:
            return self._login_failure(
                AuthErrorCode.WRONG_VERSION,
                endpoint=endpoint,
                reason="jlu_fields_mismatch",
                priority=7)

        ip_matches = (account.expected_ipv4 is None or
                      request.reported_ipv4 == account.expected_ipv4)
        mac_matches = (account.expected_mac is None or
                       crypto.recovered_mac == account.expected_mac)
        binding_valid = not account.bind_ipv4_and_mac or (ip_matches and mac_matches)
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="ip_mac_binding",
            outcome="pass" if binding_valid else "failure",
            priority=8,
            required=account.bind_ipv4_and_mac,
            ip_matches=ip_matches,
            mac_matches=mac_matches,
            reported_ipv4=request.reported_ipv4,
            expected_ipv4=account.expected_ipv4,
            recovered_mac=crypto.recovered_mac,
            expected_mac=account.expected_mac,
        )
        if account.bind_ipv4_and_mac and not (ip_matches and mac_matches):
            return self._login_failure(
                AuthErrorCode.WRONG_IP_MAC_BIND,
                endpoint=endpoint,
                reason="ip_mac_binding_mismatch",
                priority=8)
        ip_valid = crypto.md5_c_valid and ip_matches
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="ip_address",
            outcome="pass" if ip_valid else "failure",
            priority=9,
            md5_c=crypto.md5_c,
            reported_ipv4=request.reported_ipv4,
            expected_ipv4=account.expected_ipv4,
            ip_matches=ip_matches,
        )
        if not ip_valid:
            return self._login_failure(
                AuthErrorCode.WRONG_IP,
                endpoint=endpoint,
                reason="ip_validation_failed",
                priority=9)
        mac_valid = crypto.repeated_mac_valid and mac_matches
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="mac_address",
            outcome="pass" if mac_valid else "failure",
            priority=10,
            repeated_mac=crypto.repeated_mac,
            recovered_mac=crypto.recovered_mac,
            expected_mac=account.expected_mac,
            mac_matches=mac_matches,
        )
        if not mac_valid:
            return self._login_failure(
                AuthErrorCode.WRONG_MAC,
                endpoint=endpoint,
                reason="mac_validation_failed",
                priority=10)
        dhcp_valid = not account.require_dhcp or not request.dhcp.is_unspecified
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="dhcp",
            outcome="pass" if dhcp_valid else "failure",
            priority=11,
            required=account.require_dhcp,
            received=request.dhcp,
        )
        if account.require_dhcp and request.dhcp.is_unspecified:
            return self._login_failure(
                AuthErrorCode.FORCE_DHCP,
                endpoint=endpoint,
                reason="dhcp_required",
                priority=11)

        other_endpoints = self.account_sessions.get(account.username, set()) - {endpoint}
        max_sessions = account.max_sessions or self.config.server.max_sessions_per_account
        session_limit_valid = len(other_endpoints) < max_sessions
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="session_limit",
            outcome="pass" if session_limit_valid else "failure",
            priority=12,
            other_endpoints=other_endpoints,
            active_other_sessions=len(other_endpoints),
            max_sessions=max_sessions,
        )
        if max_sessions == 1 and other_endpoints:
            return self._login_failure(
                AuthErrorCode.IN_USE_WIRED,
                endpoint=endpoint,
                reason="account_in_use_wired",
                priority=12)
        if len(other_endpoints) >= max_sessions:
            return self._login_failure(
                AuthErrorCode.TOO_MANY_IP,
                endpoint=endpoint,
                reason="too_many_ip_sessions",
                priority=12)

        before = self.state_snapshot()
        self._remove_session(
            endpoint,
            operation=Operation.LOGIN,
            reason="login_replace_session",
        )
        self._session_number += 1
        derived = derive_auth_info_detailed(
            self.secret, endpoint, account.username, challenge.salt, self._session_number)
        auth_info = derived.output
        self._trace(
            "auth_info_derived",
            operation=Operation.LOGIN,
            endpoint=endpoint,
            secret=self.secret,
            input=derived.input,
            output=derived.output,
            username=account.username,
            salt=challenge.salt,
            session_number=self._session_number,
        )
        self.sessions[endpoint] = AuthenticatedSession(
            endpoint=endpoint,
            username=account.username,
            login_salt=challenge.salt,
            auth_info=auth_info,
            reported_ipv4=request.reported_ipv4,
            mac=crypto.recovered_mac,
            last_activity=now,
            billing=BillingState(
                month_time_minutes=0,
                traffic_kib=self.config.server.initial_month_traffic_kib,
                balance_ten_thousandths=account.balance_cents * 100,
                remaining_minutes=0,
            ),
        )
        self.account_sessions.setdefault(account.username, set()).add(endpoint)
        self.challenges.pop(endpoint, None)
        self.metrics.logins_succeeded += 1
        self._trace(
            "state_changed",
            operation=Operation.LOGIN,
            endpoint=endpoint,
            reason="login_success",
            before=before,
            after=self.state_snapshot(),
        )
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="login_success",
            outcome="success",
            priority=13,
            username=account.username,
            auth_info=auth_info,
        )
        return build_login_success_response(
            auth_info, self.config.server.initial_month_traffic_kib, account.balance_cents)

    def _handle_ka1(self, data: bytes, endpoint: tuple[str, int], *,
                    now: float) -> bytes | None:
        try:
            request = parse_ka1_request(data)
        except PacketFormatError as error:
            self._trace(
                "packet_parse_failed",
                operation=Operation.KA1,
                endpoint=endpoint,
                reason="ka1_parse_failed",
                parse_error=str(error),
                packet=data,
            )
            return self._drop(
                "ka1_parse_failed", operation=Operation.KA1,
                endpoint=endpoint, parse_error=str(error))

        self._trace(
            "packet_parsed",
            operation=Operation.KA1,
            endpoint=endpoint,
            request=request,
            packet=data,
        )
        session = self.sessions.get(endpoint)
        self._trace(
            "session_lookup",
            operation=Operation.KA1,
            endpoint=endpoint,
            found=session is not None,
            session=asdict(session) if session is not None else None,
        )
        if session is None:
            return self._drop(
                "ka1_session_missing", operation=Operation.KA1,
                endpoint=endpoint)
        account = self.config.accounts.get(session.username)
        if account is None:
            return self._drop(
                "ka1_account_missing", operation=Operation.KA1,
                endpoint=endpoint, username=session.username)

        digest = verify_ka1_detailed(
            request, account.password, session.login_salt)
        auth_info_valid = hmac.compare_digest(
            request.auth_info, session.auth_info)
        self._trace(
            "crypto_check",
            operation=Operation.KA1,
            endpoint=endpoint,
            username=session.username,
            password=account.password,
            salt=session.login_salt,
            digest=digest,
            received_auth_info=request.auth_info,
            expected_auth_info=session.auth_info,
            auth_info_valid=auth_info_valid,
        )
        self._decision(
            Operation.KA1,
            endpoint,
            decision="ka1_digest",
            outcome="pass" if digest.valid else "drop",
            digest=digest,
        )
        self._decision(
            Operation.KA1,
            endpoint,
            decision="ka1_auth_info",
            outcome="pass" if auth_info_valid else "drop",
            received=request.auth_info,
            expected=session.auth_info,
            valid=auth_info_valid,
        )
        if not digest.valid or not auth_info_valid:
            return self._drop(
                "ka1_authentication_failed", operation=Operation.KA1,
                endpoint=endpoint, digest=digest,
                received_auth_info=request.auth_info,
                expected_auth_info=session.auth_info,
                auth_info_valid=auth_info_valid)

        before = self.state_snapshot()
        last_activity_before = session.last_activity
        session.last_activity = now
        self.metrics.keepalives_accepted += 1
        response = build_ka1_response()
        self._trace(
            "state_changed",
            operation=Operation.KA1,
            endpoint=endpoint,
            reason="ka1_accepted",
            before=before,
            after=self.state_snapshot(),
            last_activity_before=last_activity_before,
            last_activity_after=session.last_activity,
        )
        self._decision(
            Operation.KA1,
            endpoint,
            decision="ka1_accepted",
            outcome="success",
            response=response,
        )
        self._trace(
            "response_created",
            operation=Operation.KA1,
            endpoint=endpoint,
            request=data,
            response=response,
        )
        return response

    def _handle_ka2(self, data: bytes, endpoint: tuple[str, int], *,
                    now: float) -> bytes | None:
        session = self.sessions.get(endpoint)
        self._trace(
            "session_lookup",
            operation=Operation.KA2,
            endpoint=endpoint,
            found=session is not None,
            session=asdict(session) if session is not None else None,
        )
        if session is None:
            return self._drop(
                "ka2_session_missing", operation=Operation.KA2,
                endpoint=endpoint)
        if data == session.last_ka2_request:
            self._trace(
                "ka2_retransmission",
                operation=Operation.KA2,
                endpoint=endpoint,
                request=data,
                cached_request=session.last_ka2_request,
                cached_response=session.last_ka2_response,
                state_advanced=False,
                session=asdict(session),
            )
            return session.last_ka2_response
        try:
            request = parse_ka2_request(data)
        except PacketFormatError as error:
            self._trace(
                "packet_parse_failed",
                operation=Operation.KA2,
                endpoint=endpoint,
                reason="ka2_parse_failed",
                parse_error=str(error),
                packet=data,
            )
            return self._drop(
                "ka2_parse_failed", operation=Operation.KA2,
                endpoint=endpoint, parse_error=str(error))
        self._trace(
            "packet_parsed",
            operation=Operation.KA2,
            endpoint=endpoint,
            request=request,
            packet=data,
        )

        serial_valid = request.serial == session.expected_ka2_serial
        self._decision(
            Operation.KA2,
            endpoint,
            decision="ka2_serial",
            outcome="pass" if serial_valid else "drop",
            received=request.serial,
            expected=session.expected_ka2_serial,
        )
        if not serial_valid:
            return self._drop(
                "ka2_serial_mismatch", operation=Operation.KA2,
                endpoint=endpoint, received=request.serial,
                expected=session.expected_ka2_serial)
        tail_valid = hmac.compare_digest(request.tail, session.ka2_tail)
        self._decision(
            Operation.KA2,
            endpoint,
            decision="ka2_tail",
            outcome="pass" if tail_valid else "drop",
            received=request.tail,
            expected=session.ka2_tail,
            valid=tail_valid,
        )
        if not tail_valid:
            return self._drop(
                "ka2_tail_mismatch", operation=Operation.KA2,
                endpoint=endpoint, received=request.tail,
                expected=session.ka2_tail)

        if not session.first_ka2_seen:
            bootstrap_path = "initial_1"
            valid_type_and_version = (
                request.packet_type == 1 and request.version == b"\x0f\x27")
        elif session.last_ka2_type == 1 and session.second_initial_type1_allowed:
            bootstrap_path = (
                "1-1-3" if request.packet_type == 1 else "1-3"
                if request.packet_type == 3 else "invalid")
            valid_type_and_version = (
                request.packet_type in (1, 3) and
                request.version == self.config.server.keep_alive_version)
        else:
            bootstrap_path = "alternating"
            valid_type_and_version = (
                request.packet_type != session.last_ka2_type and
                request.version == self.config.server.keep_alive_version)
        self._decision(
            Operation.KA2,
            endpoint,
            decision="ka2_type_and_version",
            outcome="pass" if valid_type_and_version else "drop",
            serial=request.serial,
            packet_type=request.packet_type,
            version=request.version,
            bootstrap_path=bootstrap_path,
            first_ka2_seen=session.first_ka2_seen,
            last_ka2_type=session.last_ka2_type,
            second_initial_type1_allowed=session.second_initial_type1_allowed,
            keep_alive_version=self.config.server.keep_alive_version,
        )
        if not valid_type_and_version:
            return self._drop(
                "ka2_type_or_version_invalid", operation=Operation.KA2,
                endpoint=endpoint, packet_type=request.packet_type,
                version=request.version)
        ip_valid = (
            request.packet_type != 3 or
            request.reported_ipv4 == session.reported_ipv4)
        if request.packet_type == 3:
            self._decision(
                Operation.KA2,
                endpoint,
                decision="ka2_reported_ip",
                outcome="pass" if ip_valid else "drop",
                received=request.reported_ipv4,
                expected=session.reported_ipv4,
            )
        if not ip_valid:
            return self._drop(
                "ka2_reported_ip_mismatch", operation=Operation.KA2,
                endpoint=endpoint, received=request.reported_ipv4,
                expected=session.reported_ipv4)

        before = self.state_snapshot()
        old_tail = session.ka2_tail
        derived = derive_ka2_tail_detailed(
            self.secret, session.auth_info, old_tail, request.serial, request.packet_type)
        new_tail = derived.output
        self._trace(
            "ka2_tail_derived",
            operation=Operation.KA2,
            endpoint=endpoint,
            secret=self.secret,
            input=derived.input,
            old_tail=old_tail,
            new_tail=new_tail,
            auth_info=session.auth_info,
            serial=request.serial,
            packet_type=request.packet_type,
        )
        session.billing.traffic_kib += 1
        response = build_ka2_response(
            request.serial, request.packet_type, new_tail,
            session.billing.month_time_minutes, session.billing.traffic_kib,
            session.billing.balance_ten_thousandths, session.billing.remaining_minutes)
        session.ka2_tail = new_tail
        session.expected_ka2_serial = (request.serial + 1) & 0xFF
        session.first_ka2_seen = True
        if session.last_ka2_type == 1 and session.second_initial_type1_allowed:
            session.second_initial_type1_allowed = False
        session.last_ka2_type = request.packet_type
        session.last_ka2_request = data
        session.last_ka2_response = response
        session.last_activity = now
        self.metrics.keepalives_accepted += 1
        self._trace(
            "response_created",
            operation=Operation.KA2,
            endpoint=endpoint,
            request=data,
            response=response,
            canonical=True,
        )
        self._trace(
            "state_changed",
            operation=Operation.KA2,
            endpoint=endpoint,
            reason="ka2_accepted",
            before=before,
            after=self.state_snapshot(),
            response=response,
        )
        self._decision(
            Operation.KA2,
            endpoint,
            decision="ka2_accepted",
            outcome="success",
            serial=request.serial,
            packet_type=request.packet_type,
            response=response,
        )
        return response

    def _handle_logout(self, data: bytes, endpoint: tuple[str, int], *,
                       now: float) -> bytes | None:
        digest = hashlib.sha256(data).digest()
        tombstone = self.logout_tombstones.get(endpoint)
        if tombstone is not None and hmac.compare_digest(tombstone[1], digest):
            self._trace(
                "logout_retransmission",
                operation=Operation.LOGOUT,
                endpoint=endpoint,
                request=data,
                request_digest=digest,
                expires_at=tombstone[0],
                cached_ack=tombstone[2],
                state_advanced=False,
            )
            return tombstone[2]
        try:
            request = parse_logout_request(data)
        except PacketFormatError as error:
            self._trace(
                "packet_parse_failed",
                operation=Operation.LOGOUT,
                endpoint=endpoint,
                reason="logout_parse_failed",
                parse_error=str(error),
                packet=data,
            )
            return self._drop(
                "logout_parse_failed", operation=Operation.LOGOUT,
                endpoint=endpoint, parse_error=str(error))

        self._trace(
            "packet_parsed",
            operation=Operation.LOGOUT,
            endpoint=endpoint,
            request=request,
            packet=data,
            request_digest=digest,
        )
        session = self.sessions.get(endpoint)
        self._trace(
            "session_lookup",
            operation=Operation.LOGOUT,
            endpoint=endpoint,
            found=session is not None,
            session=asdict(session) if session is not None else None,
        )
        if session is None or request.username != session.username:
            return self._drop(
                "logout_session_mismatch", operation=Operation.LOGOUT,
                endpoint=endpoint, username=request.username)
        account = self.config.accounts.get(session.username)
        if account is None:
            return self._drop(
                "logout_account_missing", operation=Operation.LOGOUT,
                endpoint=endpoint, username=session.username)
        challenge = self.challenges.get(endpoint)
        salts = [session.login_salt]
        if challenge is not None:
            salts.append(challenge.salt)
        verifications = [
            (salt, verify_logout_crypto_detailed(
                request, account.password, salt))
            for salt in salts
        ]
        salt_matches = any(result.valid for _, result in verifications)
        mac_valid = hmac.compare_digest(request.recovered_mac, session.mac)
        auth_info_valid = hmac.compare_digest(
            request.auth_info, session.auth_info)
        for salt, verification in verifications:
            self._trace(
                "crypto_check",
                operation=Operation.LOGOUT,
                endpoint=endpoint,
                username=session.username,
                password=account.password,
                salt=salt,
                digest=verification,
                received_mac=request.recovered_mac,
                expected_mac=session.mac,
                mac_valid=mac_valid,
                received_auth_info=request.auth_info,
                expected_auth_info=session.auth_info,
                auth_info_valid=auth_info_valid,
            )
        self._decision(
            Operation.LOGOUT,
            endpoint,
            decision="logout_crypto",
            outcome=("pass" if salt_matches and mac_valid and auth_info_valid
                     else "drop"),
            candidates=verifications,
            salt_matches=salt_matches,
            mac_valid=mac_valid,
            auth_info_valid=auth_info_valid,
        )
        if not salt_matches or not mac_valid or not auth_info_valid:
            return self._drop(
                "logout_authentication_failed", operation=Operation.LOGOUT,
                endpoint=endpoint, candidates=verifications,
                salt_matches=salt_matches, mac_valid=mac_valid,
                auth_info_valid=auth_info_valid)

        response = build_logout_response()
        before = self.state_snapshot()
        self._remove_session(
            endpoint,
            operation=Operation.LOGOUT,
            reason="logout_success",
        )
        self.challenges.pop(endpoint, None)
        expires_in_seconds = 2
        expires_at = now + expires_in_seconds
        tombstone = (expires_at, digest, response)
        self.logout_tombstones[endpoint] = tombstone
        self._trace(
            "logout_tombstone_created",
            operation=Operation.LOGOUT,
            endpoint=endpoint,
            request=data,
            request_digest=digest,
            ack=response,
            expires_at=expires_at,
            expires_in_seconds=expires_in_seconds,
            tombstone=tombstone,
            before=before,
            after=self.state_snapshot(),
        )
        self._trace(
            "response_created",
            operation=Operation.LOGOUT,
            endpoint=endpoint,
            request=data,
            response=response,
        )
        self._decision(
            Operation.LOGOUT,
            endpoint,
            decision="logout_accepted",
            outcome="success",
            request_digest=digest,
            response=response,
            expires_at=expires_at,
        )
        return response

    def _has_expected_jlu_fields(self, request) -> bool:
        return (request.control_status == self.config.server.control_check_status[0] and
                request.adapter_num == 1 and
                request.ipdog == self.config.server.ipdog[0] and
                request.auth_version == self.config.server.auth_version)

    def _trace(
        self,
        event: str,
        *,
        operation: Operation | None = None,
        endpoint: tuple[str, int] | None = None,
        detail_fields: dict[str, Any] | None = None,
        **details: Any,
    ) -> None:
        merged_details = dict(detail_fields or {})
        merged_details.update(details)
        self.trace.emit(
            event,
            operation=operation.value if operation is not None else None,
            endpoint=endpoint,
            details=merged_details,
        )

    @staticmethod
    def _endpoint_key(endpoint: tuple[str, int]) -> str:
        return f"{endpoint[0]}:{endpoint[1]}"

    def _account_sessions_snapshot(self) -> dict[str, set[str]]:
        return {
            username: {
                self._endpoint_key(endpoint) for endpoint in endpoints
            }
            for username, endpoints in self.account_sessions.items()
        }

    def state_snapshot(self) -> dict[str, Any]:
        return {
            "challenges": {
                self._endpoint_key(endpoint): asdict(challenge)
                for endpoint, challenge in self.challenges.items()
            },
            "sessions": {
                self._endpoint_key(endpoint): asdict(session)
                for endpoint, session in self.sessions.items()
            },
            "account_sessions": self._account_sessions_snapshot(),
            "logout_tombstones": {
                self._endpoint_key(endpoint): tombstone
                for endpoint, tombstone in self.logout_tombstones.items()
            },
            "metrics": asdict(self.metrics),
            "session_number": self._session_number,
        }

    def _decision(
        self,
        operation: Operation,
        endpoint: tuple[str, int],
        *,
        decision: str,
        outcome: str,
        wire_error_code: int | None = None,
        drop_reason: str | None = None,
        reason: str | None = None,
        **details: Any,
    ) -> None:
        record = decision_record(
            operation=operation.value,
            outcome=outcome,
            wire_error_code=wire_error_code,
            drop_reason=drop_reason,
        )
        self._trace(
            "validation_decision",
            operation=operation,
            endpoint=endpoint,
            detail_fields=record,
            decision=decision,
            reason=reason,
            **details,
        )

    def _login_failure(
        self,
        code: int,
        *,
        endpoint: tuple[str, int],
        reason: str,
        **details: Any,
    ) -> bytes:
        self.metrics.logins_rejected += 1
        self._decision(
            Operation.LOGIN,
            endpoint,
            decision="login_failure",
            outcome="failure",
            wire_error_code=int(code),
            reason=reason,
            **details,
        )
        return build_login_failure_response(code)

    def _drop(
        self,
        reason: str,
        *,
        operation: Operation | None,
        endpoint: tuple[str, int],
        **details: Any,
    ) -> None:
        self.metrics.datagrams_dropped += 1
        if operation is not None:
            self._decision(
                operation,
                endpoint,
                decision=reason,
                outcome="drop",
                drop_reason=reason,
                reason=reason,
                **details,
            )
        else:
            record = decision_record(
                operation="unknown",
                outcome="drop",
                wire_error_code=None,
                drop_reason=reason,
            )
            self._trace(
                "validation_decision",
                operation=None,
                endpoint=endpoint,
                detail_fields=record,
                decision=reason,
                reason=reason,
                **details,
            )
        self._trace(
            "datagram_dropped",
            operation=operation,
            endpoint=endpoint,
            reason=reason,
            **details,
        )
        return None

    @staticmethod
    def _is_expired(timestamp: float, ttl: float, now: float) -> bool:
        return now - timestamp > ttl

    def _remove_expired_state(
        self,
        now: float,
        *,
        operation: Operation | None = None,
    ) -> None:
        for endpoint, challenge in tuple(self.challenges.items()):
            if self._is_expired(
                    challenge.issued_at,
                    self.config.server.challenge_ttl_seconds,
                    now):
                del self.challenges[endpoint]
                self._trace(
                    "state_expired",
                    operation=operation,
                    endpoint=endpoint,
                    detail_fields={"endpoint": endpoint},
                    state_type="challenge",
                    timestamp=challenge.issued_at,
                    ttl_seconds=self.config.server.challenge_ttl_seconds,
                    expired_at=(challenge.issued_at +
                                self.config.server.challenge_ttl_seconds),
                    now=now,
                    removed_object=challenge,
                )
        for endpoint, session in tuple(self.sessions.items()):
            if self._is_expired(
                    session.last_activity,
                    self.config.server.session_ttl_seconds,
                    now):
                self._trace(
                    "state_expired",
                    operation=operation,
                    endpoint=endpoint,
                    detail_fields={"endpoint": endpoint},
                    state_type="session",
                    timestamp=session.last_activity,
                    ttl_seconds=self.config.server.session_ttl_seconds,
                    expired_at=(session.last_activity +
                                self.config.server.session_ttl_seconds),
                    now=now,
                    removed_object=session,
                )
                self._remove_session(
                    endpoint,
                    operation=operation,
                    reason="session_expired",
                )
        for endpoint, (expires_at, _, _) in tuple(self.logout_tombstones.items()):
            if now >= expires_at:
                tombstone = self.logout_tombstones[endpoint]
                del self.logout_tombstones[endpoint]
                self._trace(
                    "state_expired",
                    operation=operation,
                    endpoint=endpoint,
                    detail_fields={"endpoint": endpoint},
                    state_type="logout_tombstone",
                    expired_at=expires_at,
                    now=now,
                    removed_object=tombstone,
                )

    def _remove_session(
        self,
        endpoint: tuple[str, int],
        *,
        operation: Operation | None = None,
        reason: str = "session_removed",
    ) -> None:
        account_index_before = self._account_sessions_snapshot()
        session = self.sessions.pop(endpoint, None)
        if session is None:
            return
        endpoints = self.account_sessions.get(session.username)
        if endpoints is not None:
            endpoints.discard(endpoint)
            if not endpoints:
                del self.account_sessions[session.username]
        account_index_after = self._account_sessions_snapshot()
        self._trace(
            "session_removed",
            operation=operation,
            endpoint=endpoint,
            reason=reason,
            session=session,
            account_index_before=account_index_before,
            account_index_after=account_index_after,
        )

    @staticmethod
    def _mutate_response(action: ScenarioAction,
                         response: bytes | None) -> bytes | None:
        if response is None:
            return None
        if action.kind is ActionKind.TRUNCATE:
            return response[:action.truncate_length]
        if action.kind is ActionKind.WRONG_OPCODE:
            return bytes((action.wrong_opcode,)) + response[1:]
        if action.kind is ActionKind.WRONG_TAIL and len(response) >= 20:
            return response[:19] + bytes((response[19] ^ 0xFF,)) + response[20:]
        return response
