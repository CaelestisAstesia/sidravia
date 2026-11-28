#!/usr/bin/env python3
"""Exercise verified Windows ARM64 portable distributions in a disposable Windows account."""

import argparse
import contextlib
import ctypes
from ctypes import wintypes
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unicodedata
import uuid
import zipfile

from verify_distribution import require


ARM64 = 0xAA64


class WindowsProcesses:
    def __init__(self):
        kernel = ctypes.WinDLL('kernel32', use_last_error=True)
        self.open = kernel.OpenProcess
        self.open.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
        self.open.restype = wintypes.HANDLE
        self.close = kernel.CloseHandle
        self.close.argtypes = (wintypes.HANDLE,)
        self.close.restype = wintypes.BOOL
        self.machine = kernel.IsWow64Process2
        self.machine.argtypes = (wintypes.HANDLE, ctypes.POINTER(wintypes.USHORT), ctypes.POINTER(wintypes.USHORT))
        self.machine.restype = wintypes.BOOL
        self.image = kernel.QueryFullProcessImageNameW
        self.image.argtypes = (wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD))
        self.image.restype = wintypes.BOOL

        self.wait = kernel.WaitForSingleObject
        self.wait.argtypes = (wintypes.HANDLE, wintypes.DWORD)
        self.wait.restype = wintypes.DWORD

    @contextlib.contextmanager
    def process_handle(self, pid, *, synchronized=False):
        access = 0x1000 | (0x100000 if synchronized else 0)  # query information and optional SYNCHRONIZE
        handle = self.open(access, False, pid)
        if not handle:
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            yield handle
        finally:
            self.close(handle)

    def inspect(self, pid, executable=None):
        with self.process_handle(pid) as handle:
            return self.inspect_handle(handle, pid, executable)

    def inspect_handle(self, handle, pid, executable=None):
        process, host = wintypes.USHORT(), wintypes.USHORT()
        if not self.machine(handle, ctypes.byref(process), ctypes.byref(host)):
            raise ctypes.WinError(ctypes.get_last_error())
        require(host.value == ARM64, 'smoke_requires_windows_arm64_host')
        if executable is not None:
            name = ctypes.create_unicode_buffer(32768)
            size = wintypes.DWORD(len(name))
            if not self.image(handle, 0, name, ctypes.byref(size)):
                raise ctypes.WinError(ctypes.get_last_error())
            require(os.path.samefile(name.value, executable), 'process_executable_mismatch')
            # IsWow64Process2 returns UNKNOWN (0) for native processes.
            require(process.value == 0, 'process_uses_emulation')
        return {'pid': pid, 'processMachine': process.value, 'nativeMachine': host.value}

    def wait_for_exit(self, handle):
        status = self.wait(handle, 20000)
        if status == 0xFFFFFFFF:
            raise ctypes.WinError(ctypes.get_last_error())
        require(status == 0, 'desktop_daemon_did_not_exit_after_its_owner')


def invoke(executable, argv, log, *, timeout=30, env=None, check=True):
    with log.open('wb') as output:
        result = subprocess.run([str(executable), *argv], cwd=executable.parent, env=env,
                                stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
                                timeout=timeout, check=False)
    if check and result.returncode:
        raise RuntimeError(f'{executable.name} exited {result.returncode}, see {log.name}')
    return result.returncode


def wait_runtime(path, gui):
    for _ in range(120):
        if gui is not None and gui.poll() is not None:
            raise RuntimeError(f'GUI exited before startup, code {gui.returncode}')
        try:
            return json.loads(path.read_bytes())
        except (FileNotFoundError, json.JSONDecodeError):
            time.sleep(.25)
    raise RuntimeError('daemon runtime record did not appear')


def smoke_runtime(processes, directory, evidence, label, version, build, *, gui):
    env = dict(os.environ, SIDRAVIA_NAMESPACE='native-arm64-' + uuid.uuid4().hex)
    namespace = directory / 'namespaces' / env['SIDRAVIA_NAMESPACE']
    namespace.mkdir(parents=True)
    # Namespaces select their own profile directory. Preserve the real bundle.
    source_profiles = directory / 'institution-profiles'
    target_profiles = namespace / 'institution-profiles'
    target_profiles.mkdir()
    for profile in source_profiles.glob('*.json'):
        (target_profiles / profile.name).write_bytes(profile.read_bytes())
    ctl = directory / 'sidraviactl.exe'
    runtime = namespace / 'runtime.json'
    report = {'label': label, 'layout': 'portable'}
    child = None
    desktop_daemon = None
    with contextlib.ExitStack() as handles:
        try:
            if gui:
                exe = directory / 'Sidravia.exe'
                with (evidence / f'{label}-gui.log').open('wb') as output:
                    child = subprocess.Popen([str(exe), '--skip-auto-login', '--start-visible'],
                                             cwd=directory, env=env, stdin=subprocess.DEVNULL,
                                             stdout=output, stderr=subprocess.STDOUT)
                report['gui'] = processes.inspect(child.pid, exe)
            else:
                invoke(ctl, ['daemon', 'start'], evidence / f'{label}-start.log', env=env)
            record = wait_runtime(runtime, child)
            require(record.get('buildId') == build and record.get('productVersion') == version, 'daemon_runtime_identity_mismatch')
            daemon = directory / 'sidraviad.exe'
            if gui:
                desktop_daemon = handles.enter_context(processes.process_handle(record['pid'], synchronized=True))
                report['daemon'] = processes.inspect_handle(desktop_daemon, record['pid'], daemon)
            else:
                report['daemon'] = processes.inspect(record['pid'], daemon)
            status = evidence / f'{label}-ipc.log'
            mode = 'desktop' if gui else 'headless'
            for _ in range(20):
                code = invoke(ctl, ['daemon', 'status'], status, timeout=8, env=env, check=False)
                output = status.read_text(encoding='utf-8', errors='replace')
                running = '(running)' in unicodedata.normalize('NFKC', output)
                if not code and running and all(token in output for token in (version, build, mode)):
                    break
                if child is not None and child.poll() is not None:
                    raise RuntimeError('GUI exited before live Build ID and mode were confirmed')
                time.sleep(.25)
            else:
                raise RuntimeError(f'IPC did not report the expected build and mode, see {status.name}')
            report['ipc'] = {'buildId': build, 'version': version, 'mode': mode}
        finally:
            if child is not None and child.poll() is None:
                # Close only the GUI launched by this smoke. The desktop daemon
                # observes the owner's exit and shuts down on its own.
                child.terminate()
                child.wait(timeout=15)
            if desktop_daemon is not None:
                processes.wait_for_exit(desktop_daemon)
                report['shutdown'] = 'desktop_owner_exit'
            elif not gui:
                invoke(ctl, ['daemon', 'stop'], evidence / f'{label}-stop.log', env=env)
            require(not runtime.exists(), 'daemon_runtime_record_remains')
    return report


def check_build(directory, version, build):
    info = (directory / 'BUILD-INFO.txt').read_text(encoding='utf-8').splitlines()
    require({f'ProductVersion: {version}', f'BuildID: {build}', 'Target: windows-arm64'} <= set(info),
            'windows_arm64_candidate_identity_mismatch')


@contextlib.contextmanager
def fixture_directory(summary):
    root = Path(tempfile.mkdtemp(prefix='sdra64-'))
    try:
        yield root
        shutil.rmtree(root)
    except Exception:
        summary['fixtureDirectory'] = str(root)
        raise


def exercise(args, summary):
    e = args.evidence
    processes = WindowsProcesses()
    summary['host'] = processes.inspect(os.getpid())
    base = f'sidravia-v{args.version}-windows-arm64'
    with fixture_directory(summary) as root:
        for kind, source in (('core', args.core_distribution), ('gui', args.distribution)):
            directory = root / f'portable-{kind}'
            with zipfile.ZipFile(source / f'{base}-{kind}-portable.zip') as archive:
                archive.extractall(directory)
            require((directory / 'sidravia.portable').is_file(), 'portable_marker_missing')
            require(not (directory / 'sidraviaupdater.exe').exists(), 'portable_updater_unexpected')
            check_build(directory, args.version, args.build_id)
            summary['checks'].append(smoke_runtime(processes, directory, e, f'portable-{kind}',
                                                   args.version, args.build_id, gui=kind == 'gui'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--distribution', type=Path, required=True)
    parser.add_argument('--core-distribution', type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--build-id', required=True)
    args = parser.parse_args()
    require(os.name == 'nt', 'smoke_requires_windows')
    for field in ('distribution', 'core_distribution', 'evidence'):
        setattr(args, field, getattr(args, field).resolve())
    args.evidence.mkdir()
    summary = {'buildId': args.build_id, 'version': args.version, 'checks': [], 'status': 'failed'}
    try:
        exercise(args, summary)
        summary['status'] = 'passed'
    except Exception as exc:
        summary['error'] = str(exc)
        raise
    finally:
        (args.evidence / 'receipt.json').write_text(json.dumps(summary, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
