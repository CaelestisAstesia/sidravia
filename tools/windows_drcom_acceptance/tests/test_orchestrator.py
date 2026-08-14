import json
import tempfile
import unittest
from dataclasses import replace
from pathlib import Path

from sidravia_drcom_acceptance.artifacts import SensitiveArtifacts
from sidravia_drcom_acceptance.orchestrator import (
    AcceptanceOrchestrator,
    CaptureBrokerDriver,
    RunDependencies,
    RunFailed,
    RunRequest,
    SidraviaSessionDriver,
    build_broker_command,
    write_capture_manifest,
)
from sidravia_drcom_acceptance.processes import CommandResult


class RecordingPreflight:
    def __init__(self, events):
        self.events = events

    def require_ready(self, request):
        self.events.append("preflight")


class RecordingArtifacts:
    def __init__(self, events, root):
        self.events = events
        self.value = SensitiveArtifacts(root)
        root.mkdir(parents=True)

    def create(self, request):
        self.events.append("artifacts_create")
        return self

    def cleanup(self):
        self.events.append("artifacts_cleanup")

    def __getattr__(self, name):
        return getattr(self.value, name)


class RecordingCapture:
    def __init__(self, events):
        self.events = events
        self.started = False

    def start(self, request, artifacts):
        self.events.append("capture_start")
        self.started = True

    def stop(self):
        if self.started:
            self.events.append("capture_stop")
            self.started = False


class RecordingSession:
    def __init__(self, events, start_error=None, wait_error=None):
        self.events = events
        self.start_error = start_error
        self.wait_error = wait_error

    def probe_contract(self):
        self.events.append("contract_probe")

    def create_from_stdin(self, request, artifacts):
        self.events.append("session_create")
        return "acceptance-session-1"

    def start(self, session_id, artifacts):
        self.events.append("session_start")
        if self.start_error:
            raise self.start_error

    def wait_terminal(self, session_id, timeout_seconds):
        self.events.append("session_wait")
        if self.wait_error:
            raise self.wait_error

    def snapshot(self, session_id, destination):
        self.events.append("session_snapshot")

    def stop(self, session_id):
        self.events.append("session_stop")

    def delete(self, session_id):
        self.events.append("session_delete")


class RecordingTshark:
    def __init__(self, events):
        self.events = events

    def extract(self, request, artifacts):
        self.events.append("tshark")

    def close_processes(self):
        self.events.append("processes_close")


class RecordingReporter:
    def __init__(self, events):
        self.events = events

    def compare_and_export(self, request, artifacts, session_id):
        self.events.append("compare")
        return ("identity", "lifecycle", "wire")


def request(root: Path) -> RunRequest:
    return RunRequest(
        local_app_data=root,
        run_id="11111111-1111-1111-1111-111111111111",
        broker_script=root / "capture_broker.ps1",
        dumpcap_path=Path(r"C:\Program Files\Wireshark\dumpcap.exe"),
        interface_id=r"\Device\NPF_{11111111-1111-1111-1111-111111111111}",
        duration_seconds=30,
    )


class OrchestratorLifecycleTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.events = []

    def dependencies(self, *, start_error=None, wait_error=None):
        artifacts = RecordingArtifacts(self.events, self.root / "run")
        return RunDependencies(
            preflight=RecordingPreflight(self.events),
            artifacts=artifacts,
            capture=RecordingCapture(self.events),
            session=RecordingSession(self.events, start_error, wait_error),
            tshark=RecordingTshark(self.events),
            reporter=RecordingReporter(self.events),
        )

    def test_success_follows_evidence_order_then_cleans_every_owned_resource(self):
        summary = AcceptanceOrchestrator(self.dependencies()).run(request(self.root))
        self.assertTrue(summary.passed)
        self.assertEqual(summary.checks, ("identity", "lifecycle", "wire"))
        self.assertEqual(self.events, [
            "preflight", "contract_probe", "artifacts_create", "session_create",
            "capture_start", "session_start", "session_wait", "session_snapshot",
            "capture_stop", "tshark", "compare", "session_stop",
            "session_delete", "processes_close", "artifacts_cleanup",
        ])

    def test_failure_after_capture_start_still_cleans_session_processes_and_files(self):
        dependencies = self.dependencies(start_error=RuntimeError("SENTINEL-PASSWORD"))
        with self.assertRaises(RunFailed) as captured:
            AcceptanceOrchestrator(dependencies).run(request(self.root))
        self.assertNotIn("SENTINEL-PASSWORD", str(captured.exception))
        self.assertEqual(self.events, [
            "preflight", "contract_probe", "artifacts_create", "session_create",
            "capture_start", "session_start", "capture_stop", "session_stop",
            "session_delete", "processes_close", "artifacts_cleanup",
        ])

    def test_keyboard_interrupt_runs_cleanup_and_returns_safe_failure(self):
        dependencies = self.dependencies(wait_error=KeyboardInterrupt())
        with self.assertRaisesRegex(RunFailed, "取消"):
            AcceptanceOrchestrator(dependencies).run(request(self.root))
        self.assertEqual(self.events[-5:], [
            "capture_stop", "session_stop", "session_delete",
            "processes_close", "artifacts_cleanup",
        ])

    def test_session_timeout_runs_the_same_complete_cleanup(self):
        dependencies = self.dependencies(wait_error=TimeoutError("network detail"))
        with self.assertRaisesRegex(RunFailed, "TimeoutError"):
            AcceptanceOrchestrator(dependencies).run(request(self.root))
        self.assertEqual(self.events[-5:], [
            "capture_stop", "session_stop", "session_delete",
            "processes_close", "artifacts_cleanup",
        ])

    def test_cleanup_failures_do_not_skip_later_steps_and_hide_exception_text(self):
        dependencies = self.dependencies(start_error=RuntimeError("primary secret"))
        dependencies.capture.stop = self._failure("capture_stop", "pcap secret")
        dependencies.session.stop = self._failure("session_stop", "session secret")
        dependencies.session.delete = self._failure("session_delete", "delete secret")
        dependencies.tshark.close_processes = self._failure("processes_close", "proc secret")
        dependencies.artifacts.cleanup = self._failure("artifacts_cleanup", "path secret")

        with self.assertRaises(RunFailed) as captured:
            AcceptanceOrchestrator(dependencies).run(request(self.root))
        message = str(captured.exception)
        self.assertIn("capture_stop", message)
        self.assertIn("artifacts_cleanup", message)
        self.assertNotIn("secret", message)
        self.assertEqual(self.events[-5:], [
            "capture_stop", "session_stop", "session_delete",
            "processes_close", "artifacts_cleanup",
        ])

    def _failure(self, event, message):
        def fail(*_args, **_kwargs):
            self.events.append(event)
            raise RuntimeError(message)
        return fail


class ManifestAndCredentialTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name).resolve()
        self.artifacts = SensitiveArtifacts(self.root / "run")
        self.artifacts.root.mkdir()

    def test_manifest_is_strict_contains_no_secret_and_normal_command_has_no_runas(self):
        run_request = request(self.root)
        fields = write_capture_manifest(self.artifacts, run_request)
        manifest = json.loads(self.artifacts.manifest.read_text(encoding="utf-8"))
        self.assertEqual(set(manifest), {
            "schema_version", "run_root", "dumpcap_path", "interface_id",
            "bpf", "pcap_path", "ready_path", "stop_path", "duration_seconds",
        })
        self.assertEqual(fields, manifest)
        self.assertNotIn("SENTINEL-ACCEPTANCE-PASSWORD", json.dumps(manifest))
        command = build_broker_command(run_request.broker_script, self.artifacts.manifest)
        self.assertNotIn("runas", " ".join(command).lower())
        self.assertNotIn("SENTINEL-ACCEPTANCE-PASSWORD", " ".join(command))

    def test_manifest_rejects_out_of_range_duration_or_unsafe_interface(self):
        base = request(self.root)
        with self.assertRaises(ValueError):
            write_capture_manifest(
                self.artifacts,
                replace(base, duration_seconds=901),
            )
        with self.assertRaises(ValueError):
            write_capture_manifest(
                self.artifacts,
                replace(base, interface_id="bad\nargument"),
            )

    def test_session_create_passes_secret_only_via_stdin_and_clears_reference(self):
        secret = "SENTINEL-ACCEPTANCE-PASSWORD"

        class Runner:
            def __init__(self):
                self.calls = []

            def run(self, argv, *, input_text=None, timeout=10.0):
                self.calls.append((tuple(argv), input_text))
                return CommandResult(tuple(argv), 0,
                                     '{"session_id":"acceptance-session-1"}', "")

        values = iter(("fixture-user", secret))
        runner = Runner()
        driver = SidraviaSessionDriver(
            Path("sidraviactl.exe"), runner, credential_reader=lambda _label: next(values)
        )
        session_id = driver.create_from_stdin(request(self.root), self.artifacts)
        argv, stdin_text = runner.calls[0]
        self.assertEqual(session_id, "acceptance-session-1")
        self.assertNotIn(secret, " ".join(argv))
        self.assertIn(secret, stdin_text)
        self.assertNotIn(secret, repr(driver.__dict__))
        self.assertTrue(driver.credential_payload_cleared)

    def test_broker_script_has_one_self_elevation_and_no_shell_evaluation(self):
        script = (Path(__file__).parents[1] / "capture_broker.ps1").read_text(
            encoding="utf-8"
        )
        lowered = script.lower()
        self.assertEqual(lowered.count("-verb runas"), 1)
        self.assertNotIn("invoke-expression", lowered)
        self.assertNotIn("cmd.exe", lowered)
        self.assertIn('"udp port 61440"', lowered)
        self.assertIn("finally", lowered)
        self.assertIn("hklm:", lowered)
        self.assertIn("reparsepoint", lowered)

    def test_capture_driver_starts_one_plain_broker_and_stops_with_empty_sentinel(self):
        processes = []

        class Process:
            def __init__(self, argv, **kwargs):
                self.argv = tuple(argv)
                self.kwargs = kwargs
                self.returncode = None
                self.wait_calls = []
                processes.append(self)
                self_artifacts.ready.write_text(
                    '{"schema_version":1,"status":"ready","dumpcap_pid":123}',
                    encoding="utf-8",
                )

            def poll(self):
                return self.returncode

            def wait(self, timeout):
                self.wait_calls.append(timeout)
                self.returncode = 0
                return 0

            def terminate(self):
                self.returncode = 1

            def kill(self):
                self.returncode = 1

        self_artifacts = self.artifacts
        driver = CaptureBrokerDriver(
            process_factory=Process,
            sleep=lambda _seconds: None,
            monotonic=self._clock(),
        )
        driver.start(request(self.root), self.artifacts)
        driver.stop()
        self.assertEqual(len(processes), 1)
        self.assertNotIn("runas", " ".join(processes[0].argv).lower())
        self.assertEqual(self.artifacts.stop_signal.read_bytes(), b"")
        self.assertEqual(processes[0].wait_calls, [15.0])

    def test_capture_driver_ready_timeout_terminates_owned_broker(self):
        process = None

        class Process:
            returncode = None

            def __init__(self, argv, **kwargs):
                nonlocal process
                process = self
                self.terminated = False

            def poll(self):
                return self.returncode

            def wait(self, timeout):
                if not self.terminated:
                    raise TimeoutError()
                return 1

            def terminate(self):
                self.terminated = True
                self.returncode = 1

            def kill(self):
                self.terminated = True
                self.returncode = 1

        driver = CaptureBrokerDriver(
            process_factory=Process,
            sleep=lambda _seconds: None,
            monotonic=self._clock(step=1.0),
            ready_timeout_seconds=0.5,
        )
        with self.assertRaisesRegex(RunFailed, "ready"):
            driver.start(request(self.root), self.artifacts)
        self.assertTrue(process.terminated)

    @staticmethod
    def _clock(step=0.1):
        current = -step

        def monotonic():
            nonlocal current
            current += step
            return current

        return monotonic


if __name__ == "__main__":
    unittest.main()
