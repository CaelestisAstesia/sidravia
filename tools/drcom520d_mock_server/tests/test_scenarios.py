import tempfile
import unittest
from pathlib import Path

from drcom520d_mock_server.models import Operation
from drcom520d_mock_server.scenarios import (
    ActionKind,
    ScenarioAction,
    ScenarioError,
    ScenarioProgram,
    builtin_scenario,
    load_scenario,
    parse_scenario,
)


class ScenarioTests(unittest.TestCase):
    def test_busy_then_success_is_deterministic(self):
        program = builtin_scenario("busy-then-success")
        self.assertEqual(program.next_action(Operation.LOGIN).reject_code, 0x02)
        self.assertEqual(program.next_action(Operation.LOGIN).reject_code, 0x02)
        self.assertEqual(program.next_action(Operation.LOGIN).kind, ActionKind.NORMAL)

    def test_repeat_last_keeps_dropping_keepalive(self):
        program = ScenarioProgram(
            actions={Operation.KA1: (ScenarioAction(ActionKind.NORMAL),
                                     ScenarioAction(ActionKind.DROP))},
            repeat_last=True,
        )
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.NORMAL)
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.DROP)
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.DROP)

    def test_operations_have_independent_deterministic_counters(self):
        program = ScenarioProgram(
            actions={
                Operation.LOGIN: (ScenarioAction(ActionKind.DROP),),
                Operation.KA1: (ScenarioAction(ActionKind.DELAY, delay_milliseconds=1),),
            },
        )
        self.assertEqual(program.next_action(Operation.LOGIN).kind, ActionKind.DROP)
        self.assertEqual(program.next_action(Operation.KA1).delay_milliseconds, 1)
        self.assertEqual(program.next_action(Operation.LOGIN).kind, ActionKind.NORMAL)
        self.assertEqual(program.counters, {Operation.LOGIN: 2, Operation.KA1: 1})

    def test_parses_all_action_kinds(self):
        program = parse_scenario({
            "operations": {
                "challenge": [{"action": "wrong_opcode", "opcode": 1}],
                "login": [
                    {"action": "reject", "code": 2},
                    {"action": "truncate", "length": 32},
                ],
                "ka1": [{"action": "delay", "milliseconds": 0}],
                "ka2": [
                    {"action": "wrong_tail"},
                    {"action": "expire_session"},
                ],
                "logout": [{"action": "drop"}],
            },
            "repeat_last": False,
        })
        self.assertEqual(program.actions[Operation.CHALLENGE][0].wrong_opcode, 1)
        self.assertEqual(program.actions[Operation.LOGIN][0].reject_code, 2)
        self.assertEqual(program.actions[Operation.LOGIN][1].truncate_length, 32)
        self.assertEqual(program.actions[Operation.KA1][0].delay_milliseconds, 0)
        self.assertEqual(program.actions[Operation.KA2][0].kind, ActionKind.WRONG_TAIL)
        self.assertEqual(program.actions[Operation.KA2][1].kind, ActionKind.EXPIRE_SESSION)

    def test_rejects_invalid_action_parameters(self):
        cases = (
            ({"operations": {"ka2": [{"action": "reject", "code": 3}]}},
             "reject is only valid for login"),
            ({"operations": {"login": [{"action": "truncate"}]}},
             "truncate requires length"),
            ({"operations": {"login": [{"action": "reject", "code": True}]}},
             "reject.code must be an integer"),
            ({"operations": {"login": [{"action": "reject", "code": 256}]}},
             "reject.code must be an integer from 0 to 255"),
            ({"operations": {"ka1": [{"action": "delay", "milliseconds": -1}]}},
             "delay.milliseconds must be a non-negative integer"),
            ({"operations": {"login": [{"action": "truncate", "length": 65}]}},
             "truncate.length must be an integer from 0 to 64"),
            ({"operations": {"ka2": [{"action": "wrong_tail", "length": 1}]}},
             "unknown wrong_tail fields: length"),
            ({"operations": {"login": [{"action": "normal", "code": 2}]}},
             "unknown normal fields: code"),
            ({"operations": {"ka1": [{"action": "wrong_tail"}]}},
             "wrong_tail is only valid for ka2"),
        )
        for payload, message in cases:
            with self.subTest(payload=payload):
                with self.assertRaisesRegex(ScenarioError, message):
                    parse_scenario(payload)

    def test_rejects_unknown_or_malformed_schema_fields(self):
        cases = (
            ({"operations": {}, "surprise": True}, "unknown root fields: surprise"),
            ({"operations": {"unknown": []}}, "unknown operation: unknown"),
            ({"operations": {"login": [{"action": "unknown"}]}}, "unknown action: unknown"),
            ({"operations": {"login": []}}, "operations.login must not be empty"),
            ({"operations": {}, "repeat_last": 1}, "repeat_last must be a boolean"),
        )
        for payload, message in cases:
            with self.subTest(payload=payload):
                with self.assertRaisesRegex(ScenarioError, message):
                    parse_scenario(payload)

    def test_loads_busy_then_success_example_strictly(self):
        path = Path("tools/drcom520d_mock_server/examples/busy_then_success.json")
        program = load_scenario(path)
        self.assertTrue(program.repeat_last)
        self.assertEqual(program.next_action(Operation.LOGIN).reject_code, 2)
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.NORMAL)
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.DROP)
        self.assertEqual(program.next_action(Operation.KA1).kind, ActionKind.DROP)

    def test_load_scenario_wraps_json_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "invalid.json"
            path.write_text("{", encoding="utf-8")
            with self.assertRaisesRegex(ScenarioError, "cannot read scenario"):
                load_scenario(path)

    def test_load_scenario_wraps_invalid_utf8(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "invalid.json"
            path.write_bytes(b"\xff")
            with self.assertRaisesRegex(ScenarioError, "cannot read scenario"):
                load_scenario(path)

    def test_all_builtin_names_have_expected_faults(self):
        self.assertEqual(builtin_scenario("normal").next_action(Operation.LOGIN).kind,
                         ActionKind.NORMAL)
        self.assertEqual(builtin_scenario("flaky-login").next_action(Operation.LOGIN).kind,
                         ActionKind.DROP)
        self.assertEqual(builtin_scenario("keepalive-timeout").next_action(Operation.KA1).kind,
                         ActionKind.DROP)
        self.assertEqual(builtin_scenario("session-expired").next_action(Operation.KA1).kind,
                         ActionKind.EXPIRE_SESSION)
        self.assertEqual(builtin_scenario("malformed-response").next_action(Operation.KA2).kind,
                         ActionKind.WRONG_TAIL)
        with self.assertRaisesRegex(ScenarioError, "unknown built-in scenario"):
            builtin_scenario("made-up")


if __name__ == "__main__":
    unittest.main()
