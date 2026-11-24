from ipaddress import IPv4Address

from drcom520d_mock_server.models import Account, ApplicationConfig, ServerSettings


class FakeClock:
    def __init__(self, value: float = 1000.0):
        self.value = value

    def __call__(self) -> float:
        return self.value

    def advance(self, seconds: float) -> None:
        self.value += seconds


def fictional_account() -> Account:
    return Account(
        username="student-test",
        password="local-test-password",
        enabled=True,
        frozen=False,
        require_dhcp=False,
        expected_ipv4=IPv4Address("10.0.0.2"),
        expected_mac=bytes.fromhex("020000000001"),
        bind_ipv4_and_mac=True,
        max_sessions=1,
        balance_cents=10000,
    )


def server_config(account: Account) -> ApplicationConfig:
    return ApplicationConfig(
        server=ServerSettings(
            challenge_ttl_seconds=10,
            session_ttl_seconds=90,
            max_sessions_per_account=1,
            auth_version=bytes.fromhex("2c00"),
            keep_alive_version=bytes.fromhex("dc02"),
            control_check_status=bytes.fromhex("20"),
            ipdog=bytes.fromhex("01"),
            initial_month_traffic_kib=14208,
            initial_balance_cents=10000,
            server_secret=b"s" * 32,
        ),
        accounts={account.username: account},
    )
