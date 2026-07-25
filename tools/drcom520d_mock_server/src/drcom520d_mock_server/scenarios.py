from __future__ import annotations

import json
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Any

from .models import Operation


class ScenarioError(ValueError):
    """Raised when a fault-scenario definition is not strictly valid."""


class ActionKind(str, Enum):
    NORMAL = "normal"
    REJECT = "reject"
    DROP = "drop"
    DELAY = "delay"
    TRUNCATE = "truncate"
    WRONG_OPCODE = "wrong_opcode"
    EXPIRE_SESSION = "expire_session"
    WRONG_TAIL = "wrong_tail"


@dataclass(frozen=True, slots=True)
class ScenarioAction:
    kind: ActionKind
    reject_code: int | None = None
    delay_milliseconds: int | None = None
    truncate_length: int | None = None
    wrong_opcode: int | None = None


@dataclass(slots=True)
class ScenarioProgram:
    actions: dict[Operation, tuple[ScenarioAction, ...]]
    repeat_last: bool = False
    counters: dict[Operation, int] = field(default_factory=dict)

    def next_action(self, operation: Operation) -> ScenarioAction:
        sequence = self.actions.get(operation, ())
        index = self.counters.get(operation, 0)
        self.counters[operation] = index + 1
        if not sequence:
            return ScenarioAction(ActionKind.NORMAL)
        if index < len(sequence):
            return sequence[index]
        return sequence[-1] if self.repeat_last else ScenarioAction(ActionKind.NORMAL)


_ROOT_FIELDS = frozenset({"operations", "repeat_last"})
_ACTION_FIELDS = {
    ActionKind.NORMAL: frozenset({"action"}),
    ActionKind.REJECT: frozenset({"action", "code"}),
    ActionKind.DROP: frozenset({"action"}),
    ActionKind.DELAY: frozenset({"action", "milliseconds"}),
    ActionKind.TRUNCATE: frozenset({"action", "length"}),
    ActionKind.WRONG_OPCODE: frozenset({"action", "opcode"}),
    ActionKind.EXPIRE_SESSION: frozenset({"action"}),
    ActionKind.WRONG_TAIL: frozenset({"action"}),
}
_BUILTINS: dict[str, dict[str, Any]] = {
    "normal": {"operations": {}},
    "busy-then-success": {
        "operations": {
            "login": [
                {"action": "reject", "code": 2},
                {"action": "reject", "code": 2},
                {"action": "normal"},
            ],
            "ka1": [
                {"action": "normal"},
                {"action": "drop"},
            ],
        },
        "repeat_last": True,
    },
    "flaky-login": {
        "operations": {"login": [{"action": "drop"}, {"action": "normal"}]},
    },
    "keepalive-timeout": {
        "operations": {"ka1": [{"action": "drop"}]},
        "repeat_last": True,
    },
    "session-expired": {
        "operations": {"ka1": [{"action": "expire_session"}]},
        "repeat_last": True,
    },
    "malformed-response": {
        "operations": {"ka2": [{"action": "wrong_tail"}]},
        "repeat_last": True,
    },
}


def load_scenario(path: Path) -> ScenarioProgram:
    """Load a strictly validated scenario JSON file."""
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ScenarioError(f"cannot read scenario: {error}") from error
    return parse_scenario(payload)


def builtin_scenario(name: str) -> ScenarioProgram:
    """Return a fresh deterministic program for one documented built-in name."""
    try:
        payload = _BUILTINS[name]
    except KeyError as error:
        raise ScenarioError(f"unknown built-in scenario: {name}") from error
    return parse_scenario(payload)


def parse_scenario(payload: Any) -> ScenarioProgram:
    root = _mapping(payload, "root")
    _check_keys(root, _ROOT_FIELDS, "root")
    if "operations" not in root:
        raise ScenarioError("missing root fields: operations")
    operations = _mapping(root["operations"], "operations")
    repeat_last = root.get("repeat_last", False)
    if not isinstance(repeat_last, bool):
        raise ScenarioError("repeat_last must be a boolean")

    parsed_actions: dict[Operation, tuple[ScenarioAction, ...]] = {}
    for operation_name, raw_actions in operations.items():
        operation = _operation(operation_name)
        parsed_actions[operation] = _actions(raw_actions, operation)
    return ScenarioProgram(actions=parsed_actions, repeat_last=repeat_last)


def _mapping(value: Any, name: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ScenarioError(f"{name} must be an object")
    return value


def _check_keys(raw: dict[str, Any], allowed: frozenset[str], name: str) -> None:
    unknown = sorted(set(raw) - allowed)
    if unknown:
        raise ScenarioError(f"unknown {name} fields: {', '.join(unknown)}")


def _operation(value: Any) -> Operation:
    if not isinstance(value, str):
        raise ScenarioError("operation names must be strings")
    try:
        return Operation(value)
    except ValueError as error:
        raise ScenarioError(f"unknown operation: {value}") from error


def _actions(value: Any, operation: Operation) -> tuple[ScenarioAction, ...]:
    if not isinstance(value, list):
        raise ScenarioError(f"operations.{operation.value} must be an array")
    if not value:
        raise ScenarioError(f"operations.{operation.value} must not be empty")
    return tuple(_action(raw, operation) for raw in value)


def _action(value: Any, operation: Operation) -> ScenarioAction:
    raw = _mapping(value, "action")
    kind_name = raw.get("action")
    if not isinstance(kind_name, str):
        raise ScenarioError("action requires an action string")
    try:
        kind = ActionKind(kind_name)
    except ValueError as error:
        raise ScenarioError(f"unknown action: {kind_name}") from error
    _check_keys(raw, _ACTION_FIELDS[kind], kind.value)

    if kind is ActionKind.REJECT:
        if operation is not Operation.LOGIN:
            raise ScenarioError("reject is only valid for login")
        return ScenarioAction(kind, reject_code=_integer(raw, "code", kind.value, 0, 255))
    if kind is ActionKind.DELAY:
        return ScenarioAction(
            kind,
            delay_milliseconds=_integer(raw, "milliseconds", kind.value, 0, None),
        )
    if kind is ActionKind.TRUNCATE:
        return ScenarioAction(
            kind,
            truncate_length=_integer(raw, "length", kind.value, 0, 64),
        )
    if kind is ActionKind.WRONG_OPCODE:
        return ScenarioAction(kind, wrong_opcode=_integer(raw, "opcode", kind.value, 0, 255))
    if kind is ActionKind.WRONG_TAIL:
        if operation is not Operation.KA2:
            raise ScenarioError("wrong_tail is only valid for ka2")
        return ScenarioAction(kind)
    return ScenarioAction(kind)


def _integer(raw: dict[str, Any], field_name: str, action_name: str,
             minimum: int, maximum: int | None) -> int:
    if field_name not in raw:
        raise ScenarioError(f"{action_name} requires {field_name}")
    value = raw[field_name]
    if isinstance(value, bool) or not isinstance(value, int):
        raise ScenarioError(f"{action_name}.{field_name} must be an integer")
    if value < minimum or (maximum is not None and value > maximum):
        if maximum is None:
            raise ScenarioError(f"{action_name}.{field_name} must be a non-negative integer")
        raise ScenarioError(
            f"{action_name}.{field_name} must be an integer from {minimum} to {maximum}")
    return value
