"""Small JSON command runner shared by local verification and Windows VM runs."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time


def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def write_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + ".pending")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def configure_output():
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def human_text(value):
    # Keep raw child bytes in logs and escaped data in JSON; terminal summaries
    # must not execute child-provided colors, cursor moves or OSC sequences.
    text = re.sub(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", "", str(value))
    text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", text)
    return re.sub(r"[\x00-\x08\x0b-\x1f\x7f]", "", text)


def validate_request(request):
    if not isinstance(request, dict) or set(request) - {"schema", "stages", "source", "environment"}:
        raise ValueError("unknown request fields")
    if request.get("schema") != 1 or not isinstance(request.get("stages"), list) or not 1 <= len(request["stages"]) <= 64:
        raise ValueError("request requires schema=1 and 1..64 stages")
    validate_environment(request.get("environment", {}))
    names = set()
    for stage in request["stages"]:
        if not isinstance(stage, dict) or set(stage) - {"name", "argv", "cwd", "timeout_seconds", "environment"}:
            raise ValueError("unknown stage fields")
        name = stage.get("name")
        if not isinstance(name, str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}", name) or name in names:
            raise ValueError("stage names must be unique short identifiers")
        names.add(name)
        argv = stage.get("argv")
        if not isinstance(argv, list) or not argv or not all(isinstance(a, str) and "\0" not in a for a in argv) or not argv[0]:
            raise ValueError("argv must be a nonempty string array, never a shell command")
        if not isinstance(stage.get("cwd", "."), str) or "\0" in stage.get("cwd", "."):
            raise ValueError("invalid working directory")
        timeout = stage.get("timeout_seconds", 900)
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not 0 < timeout <= 3600:
            raise ValueError("stage timeout must be in (0,3600]")
        validate_environment(stage.get("environment", {}))
    return request


def validate_environment(value):
    if not isinstance(value, dict) or len({k.casefold() for k in value if isinstance(k, str)}) != len(value):
        raise ValueError("invalid or duplicate environment names")
    for key, item in value.items():
        if not isinstance(key, str) or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or not isinstance(item, str) or "\0" in item:
            raise ValueError("environment must contain string names and values")


def terminate_tree(process):
    if os.name == "nt":
        # Killing the dedicated worker closes its Job and every descendant.
        process.kill()
    else:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    process.wait(timeout=5)


def own_windows_job(native_update_fixtures=False):
    """The dedicated worker owns a Job before spawning the actual command."""
    import ctypes as c
    from ctypes import wintypes as w
    class Basic(c.Structure):
        _fields_ = [("process_time", c.c_int64), ("job_time", c.c_int64),
                    ("flags", w.DWORD), ("min_ws", c.c_size_t), ("max_ws", c.c_size_t),
                    ("active", w.DWORD), ("affinity", c.c_size_t),
                    ("priority", w.DWORD), ("scheduling", w.DWORD)]
    class IO(c.Structure):
        _fields_ = [(name, c.c_uint64) for name in ("read_ops", "write_ops", "other_ops", "read_bytes", "write_bytes", "other_bytes")]
    class Limits(c.Structure):
        _fields_ = [("basic", Basic), ("io", IO), ("process_memory", c.c_size_t),
                    ("job_memory", c.c_size_t), ("peak_process", c.c_size_t), ("peak_job", c.c_size_t)]
    api = c.WinDLL("kernel32", use_last_error=True)
    api.CreateJobObjectW.argtypes = [c.c_void_p, w.LPCWSTR]
    api.CreateJobObjectW.restype = w.HANDLE
    api.GetCurrentProcess.argtypes = []
    api.GetCurrentProcess.restype = w.HANDLE
    api.SetInformationJobObject.argtypes = [w.HANDLE, c.c_int, c.c_void_p, w.DWORD]
    api.SetInformationJobObject.restype = w.BOOL
    api.AssignProcessToJobObject.argtypes = [w.HANDLE, w.HANDLE]
    api.AssignProcessToJobObject.restype = w.BOOL
    api.CloseHandle.argtypes = [w.HANDLE]
    api.CloseHandle.restype = w.BOOL
    name = None
    if native_update_fixtures:
        import uuid
        name = 'Local\\Sidravia-Native-Worker-' + uuid.uuid4().hex
        os.environ['SIDRAVIA_NATIVE_WORKER_JOB'] = name
    job = api.CreateJobObjectW(None, name)
    if not job:
        raise c.WinError(c.get_last_error())
    limits = Limits()
    limits.basic.flags = 0x2000  # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
    if native_update_fixtures:
        limits.basic.flags |= 0x0800  # explicit developer-only BREAKAWAY_OK
    if not api.SetInformationJobObject(job, 9, c.byref(limits), c.sizeof(limits)) or not api.AssignProcessToJobObject(job, api.GetCurrentProcess()):
        error = c.get_last_error()
        api.CloseHandle(job)
        raise c.WinError(error)
    # Deliberately keep the non-inheritable handle until worker process exit.
    # Closing it earlier would terminate the worker before it returns its code.
    return job


def stage_command(stage, directory, native_update_fixtures=False):
    if os.name != "nt":
        return stage["argv"]
    request = Path(directory).resolve() / (stage["name"] + ".command.json")
    worker = {"argv": stage["argv"]}
    if native_update_fixtures:
        worker["native_update_fixtures"] = True
    write_json(request, worker)
    return [sys.executable, str(Path(__file__).resolve()), "--worker", str(request)]


def run_request(request, directory, initial_report=None, *, native_update_fixtures=False):
    validate_request(request)
    if native_update_fixtures and os.name != "nt":
        raise ValueError("native update fixtures require a Windows worker")
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    report_path = directory / "report.json"
    if initial_report is None:
        if report_path.exists():
            raise ValueError("report already exists; use a new run directory")
        report = {"schema": 1, "kind": "command-sequence", "status": "running", "source": request.get("source"), "stages": []}
        prior_seconds = 0
        failed = False
    else:
        # Explicit continuation lets a validated SDK stage bind the following
        # commands' environment without repeating it or losing its logs.
        if not isinstance(initial_report, dict) or initial_report.get("kind") != "command-sequence" or initial_report.get("status") not in ("passed", "failed") or not report_path.is_file() or read_json(report_path) != initial_report or initial_report.get("source") != request.get("source"):
            raise ValueError("continuation must match the existing owned report and source")
        if bool(initial_report.get("native_update_fixtures", False)) != native_update_fixtures:
            raise ValueError("continuation cannot change native update mode")
        previous = {s["name"] for s in initial_report["stages"]}
        if any(s["name"] in previous for s in request["stages"]):
            raise ValueError("continuation cannot overwrite a completed stage")
        report = initial_report
        prior_seconds = report.get("duration_seconds", 0)
        failed = report["status"] != "passed"
        report["status"] = "running"
    if native_update_fixtures:
        report["native_update_fixtures"] = True
    started = time.monotonic()
    try:
        for stage in request["stages"]:
            row = {"name": stage["name"], "argv": stage["argv"], "cwd": stage.get("cwd", "."), "status": "not_run", "exit_code": None, "duration_seconds": 0}
            report["stages"].append(row)
            if failed:
                continue
            print("Running " + row["name"], file=sys.stderr, flush=True)
            timer = time.monotonic()
            row.update(stdout=row["name"] + ".stdout.log", stderr=row["name"] + ".stderr.log", status="running")
            write_json(report_path, report)
            env = dict(os.environ, PYTHONUTF8="1", PYTHONDONTWRITEBYTECODE="1")
            env.update(request.get("environment", {}))
            env.update(stage.get("environment", {}))
            process = None
            try:
                with (directory / row["stdout"]).open("wb") as stdout, (directory / row["stderr"]).open("wb") as stderr:
                    process = subprocess.Popen(stage_command(stage, directory, native_update_fixtures), cwd=row["cwd"], env=env, stdout=stdout, stderr=stderr, start_new_session=os.name != "nt")
                    try:
                        row["exit_code"] = process.wait(timeout=stage.get("timeout_seconds", 900))
                        row["status"] = "passed" if row["exit_code"] == 0 else "failed"
                    except subprocess.TimeoutExpired:
                        row["status"] = "timed_out"
                        terminate_tree(process)
            except BaseException as error:
                if process is not None and process.poll() is None:
                    terminate_tree(process)
                row.update(status="failed", error=type(error).__name__ + ": " + str(error))
                if not isinstance(error, Exception):
                    raise
            finally:
                if process is not None and os.name != "nt":
                    # The session belongs to this stage even when its leader
                    # already exited. Do not leave detached writers behind.
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                row["duration_seconds"] = round(time.monotonic() - timer, 3)
                failed = row["status"] != "passed"
                write_json(report_path, report)
        report["status"] = "failed" if failed else "passed"
    finally:
        if report["status"] == "running":
            report["status"] = "interrupted"
        report["duration_seconds"] = round(prior_seconds + time.monotonic() - started, 3)
        write_json(report_path, report)
    return report


def present(report, report_path, output_format="text"):
    if output_format == "json":
        print(json.dumps(dict(report, report=str(report_path)), ensure_ascii=False))
        return
    print(f"{report['status'].upper()} {report.get('duration_seconds', 0):.3f}s")
    for stage in report.get("stages", []):
        print(f"  {stage['status']:10} {stage['name']} {stage.get('duration_seconds', 0):.3f}s")
        if stage["status"] in ("failed", "timed_out"):
            if stage.get("error"):
                print("    " + human_text(stage["error"])[:500])
            for stream in ("stderr", "stdout"):
                if stage.get(stream):
                    path = Path(report_path).parent / stage[stream]
                    if path.is_file():
                        with path.open("rb") as log:
                            log.seek(max(0, path.stat().st_size - 1200))
                            tail = log.read().decode("utf-8", errors="replace").strip()
                        if tail:
                            print("    " + human_text(tail).replace("\n", "\n    "))
                            break
    for key in ("error", "evidence_error", "cleanup_error", "source_error"):
        if report.get(key):
            print(key + ": " + human_text(report[key])[:500])
    print("Report: " + str(report_path))


def main():
    configure_output()
    if len(sys.argv) == 3 and sys.argv[1] == "--worker":
        worker = read_json(sys.argv[2])
        job = own_windows_job(worker.get("native_update_fixtures", False)) if os.name == "nt" else None
        return subprocess.call(worker["argv"])
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--request", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--format", choices=("text", "json"), default="text")
    parser.add_argument("--native-update-fixtures", action="store_true", help="explicit isolated Windows update acceptance; ordinary runs stay contained")
    args = parser.parse_args()
    report = run_request(read_json(args.request), args.output, native_update_fixtures=args.native_update_fixtures)
    present(report, Path(args.output) / "report.json", args.format)
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
