import json
import tempfile
import unittest
from pathlib import Path

from sidravia_drcom_acceptance.preflight import (
    CheckStatus,
    Preflight,
    render_preflight_zh,
)
from sidravia_drcom_acceptance.processes import CommandResult


class FakeRunner:
    def __init__(self, responses: dict[str, CommandResult] | None = None):
        self.responses = responses or {}
        self.calls: list[tuple[str, ...]] = []

    def run(self, argv, *, input_text=None, timeout=10.0):
        call = tuple(str(part) for part in argv)
        self.calls.append(call)
        key = self._key(call)
        return self.responses.get(key, CommandResult(call, 1, "", f"no fixture for {key}"))

    @staticmethod
    def _key(call: tuple[str, ...]) -> str:
        # Normalize both / and \ to find the final component, then lowercase.
        executable = call[0].replace("\\", "/").rstrip("/").split("/")[-1].lower()
        if executable == "tshark.exe" and "-X" in call:
            return "tshark-lua"
        if executable == "dumpcap.exe" and "-D" in call:
            return "dumpcap-devices"
        if executable == "sidraviactl.exe" and "contract" in call:
            return "sidravia-contract"
        if executable == "powershell.exe":
            return "powershell-adapters"
        if executable == "sc.exe":
            return "npcap-service"
        return executable


def result(argv: tuple[str, ...], code: int = 0, stdout: str = "", stderr: str = ""):
    return CommandResult(argv, code, stdout, stderr)


class PreflightTests(unittest.TestCase):
    def make_repository(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        lua = root / "tools" / "wireshark" / "drcom.lua"
        lua.parent.mkdir(parents=True)
        lua.write_text("-- fixture", encoding="utf-8")
        return root

    def test_missing_wireshark_is_actionable_and_dependent_checks_skip(self):
        runner = FakeRunner({
            "npcap-service": result(("sc.exe",), 1060, "", "service missing"),
        })
        report = Preflight(
            repository_root=self.make_repository(),
            runner=runner,
            resolver=lambda _name: None,
            environ={},
            platform_name="win32",
        ).run()
        statuses = {check.code: check.status for check in report.checks}
        text = render_preflight_zh(report)

        self.assertEqual(statuses["wireshark"], CheckStatus.FAIL)
        self.assertEqual(statuses["npcap"], CheckStatus.FAIL)
        self.assertEqual(statuses["lua_load"], CheckStatus.SKIP)
        self.assertEqual(statuses["capture_devices"], CheckStatus.SKIP)
        self.assertEqual(statuses["adapter_mapping"], CheckStatus.SKIP)
        self.assertEqual(statuses["capture_permission"], CheckStatus.SKIP)
        self.assertIn("安装 Wireshark 并选择 Npcap", text)
        self.assertIn("不会自动安装", text)
        self.assertEqual(report.exit_code, 2)
        self.assertFalse(any("-Verb" in call for call in runner.calls))

    def test_fully_ready_environment_loads_lua_and_maps_adapter(self):
        guid = "11111111-1111-1111-1111-111111111111"
        responses = {
            "wireshark.exe": result(("Wireshark.exe",), 0, "Wireshark 4.4.0"),
            "tshark.exe": result(("tshark.exe",), 0, "TShark 4.4.0 with Lua 5.4"),
            "dumpcap.exe": result(("dumpcap.exe",), 0, "Dumpcap 4.4.0"),
            "npcap-service": result(("sc.exe",), 0, "STATE              : 4  RUNNING"),
            "tshark-lua": result(("tshark.exe",), 0,
                                  "challenge_request\tclient_to_server\tTrue\n"),
            "dumpcap-devices": result(("dumpcap.exe",), 0,
                                      f"1. \\Device\\NPF_{{{guid}}} (Intel Ethernet)\n"),
            "powershell-adapters": result(("powershell.exe",), 0, json.dumps([{
                "Name": "以太网",
                "InterfaceDescription": "Intel Ethernet",
                "InterfaceGuid": guid,
                "ifIndex": 7,
                "MacAddress": "02-00-00-00-00-01",
                "Status": "Up",
                "IPv4": ["10.0.0.2"],
            }], ensure_ascii=False)),
            "sidraviactl.exe": result(("sidraviactl.exe",), 0, "sidravia 0.1.0"),
            "sidraviad.exe": result(("sidraviad.exe",), 0, "sidraviad 0.1.0"),
            "sidravia-contract": result(("sidraviactl.exe",), 0, json.dumps({
                "schema_version": 1,
                "capabilities": [
                    "stdin_credentials", "application_transcript_v1",
                    "transport_observation_v1", "session_snapshot_v1",
                    "idempotent_cleanup",
                ],
            })),
        }
        runner = FakeRunner(responses)
        paths = {
            name: f"C:\\Program Files\\Wireshark\\{name}"
            for name in ("Wireshark.exe", "tshark.exe", "dumpcap.exe")
        }
        paths.update({
            "sidraviactl.exe": "D:\\Sidravia\\sidraviactl.exe",
            "sidraviad.exe": "D:\\Sidravia\\sidraviad.exe",
            "powershell.exe": "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
            "sc.exe": "C:\\Windows\\System32\\sc.exe",
        })
        report = Preflight(
            repository_root=self.make_repository(),
            runner=runner,
            resolver=paths.get,
            environ={"ProgramFiles": "C:\\Program Files"},
            platform_name="win32",
        ).run()

        failures = [check for check in report.checks if check.status is not CheckStatus.PASS]
        self.assertEqual(failures, [])
        self.assertEqual(report.exit_code, 0)
        lua_call = next(call for call in runner.calls if "-X" in call)
        self.assertIn("-T", lua_call)
        self.assertIn("fields", lua_call)
        self.assertNotIn("cmd.exe", lua_call)
        self.assertTrue(any("Get-NetAdapter" in " ".join(call) for call in runner.calls))

    def test_lua_failure_is_reported_without_attempting_capture(self):
        runner = FakeRunner({
            "wireshark.exe": result(("Wireshark.exe",), 0, "Wireshark"),
            "tshark.exe": result(("tshark.exe",), 0, "TShark with Lua"),
            "dumpcap.exe": result(("dumpcap.exe",), 0, "Dumpcap"),
            "npcap-service": result(("sc.exe",), 0, "RUNNING"),
            "tshark-lua": result(("tshark.exe",), 1, "", "Lua: syntax error"),
            "dumpcap-devices": result(("dumpcap.exe",), 0, ""),
        })
        resolver = lambda name: f"C:\\fixture\\{name}"
        report = Preflight(
            repository_root=self.make_repository(), runner=runner, resolver=resolver,
            environ={}, platform_name="win32",
        ).run()
        check = next(item for item in report.checks if item.code == "lua_load")
        self.assertEqual(check.status, CheckStatus.FAIL)
        self.assertIn("Lua", check.summary)
        self.assertEqual(report.exit_code, 2)

    def test_non_windows_platform_stops_with_actionable_failure(self):
        report = Preflight(
            repository_root=self.make_repository(), runner=FakeRunner(),
            resolver=lambda _: None, environ={}, platform_name="linux",
        ).run()
        self.assertEqual(report.exit_code, 2)
        self.assertEqual(report.checks[0].code, "platform")
        self.assertEqual(report.checks[0].status, CheckStatus.FAIL)
        self.assertTrue(all(check.status is CheckStatus.SKIP for check in report.checks[1:]))


if __name__ == "__main__":
    unittest.main()
