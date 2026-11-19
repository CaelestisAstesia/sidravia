"""Harmless ordinary-user probe for explicit Windows native update acceptance."""
import ctypes as c
from ctypes import wintypes as w
import json
import os
from pathlib import Path
import subprocess
import sys


def identity():
    kernel = c.WinDLL('kernel32', use_last_error=True)
    security = c.WinDLL('advapi32', use_last_error=True)
    kernel.GetCurrentProcess.restype = w.HANDLE
    kernel.IsProcessInJob.argtypes = [w.HANDLE, w.HANDLE, c.POINTER(w.BOOL)]
    kernel.IsProcessInJob.restype = w.BOOL
    kernel.ProcessIdToSessionId.argtypes = [w.DWORD, c.POINTER(w.DWORD)]
    kernel.CloseHandle.argtypes = [w.HANDLE]
    kernel.LocalFree.argtypes = [c.c_void_p]
    security.OpenProcessToken.argtypes = [w.HANDLE, w.DWORD, c.POINTER(w.HANDLE)]
    security.GetTokenInformation.argtypes = [w.HANDLE, c.c_int, c.c_void_p, w.DWORD, c.POINTER(w.DWORD)]
    security.ConvertSidToStringSidW.argtypes = [c.c_void_p, c.POINTER(c.c_void_p)]
    security.GetSidSubAuthorityCount.argtypes = [c.c_void_p]
    security.GetSidSubAuthorityCount.restype = c.POINTER(c.c_ubyte)
    security.GetSidSubAuthority.argtypes = [c.c_void_p, w.DWORD]
    security.GetSidSubAuthority.restype = c.POINTER(w.DWORD)
    token = w.HANDLE()
    inside = w.BOOL()
    session = w.DWORD()
    if not kernel.IsProcessInJob(kernel.GetCurrentProcess(), None, c.byref(inside)) or not kernel.ProcessIdToSessionId(os.getpid(), c.byref(session)) or not security.OpenProcessToken(kernel.GetCurrentProcess(), 8, c.byref(token)):
        raise c.WinError(c.get_last_error())
    try:
        def info(kind):
            size = w.DWORD()
            security.GetTokenInformation(token, kind, None, 0, c.byref(size))
            data = c.create_string_buffer(size.value)
            if not security.GetTokenInformation(token, kind, data, size, c.byref(size)):
                raise c.WinError(c.get_last_error())
            return data
        user = info(1)
        sid = c.cast(user, c.POINTER(c.c_void_p))[0]
        text = c.c_void_p()
        if not security.ConvertSidToStringSidW(sid, c.byref(text)):
            raise c.WinError(c.get_last_error())
        try:
            user_sid = c.wstring_at(text.value)
        finally:
            kernel.LocalFree(text)
        integrity = info(25)
        label = c.cast(integrity, c.POINTER(c.c_void_p))[0]
        count = security.GetSidSubAuthorityCount(label)[0]
        rid = security.GetSidSubAuthority(label, count - 1)[0]
        return dict(pid=os.getpid(), sid=user_sid, integrity=rid, session=session.value, in_job=bool(inside.value))
    finally:
        kernel.CloseHandle(token)


def main():
    if os.name != 'nt':
        raise RuntimeError('Windows probe only')
    current = identity()
    if '--child' in sys.argv:
        print(json.dumps(current))
        return
    if not current['in_job'] or ('--expect-blocked' not in sys.argv and current['integrity'] != 8192):
        raise RuntimeError('expected contained, non-elevated ordinary-user stage')
    try:
        result = subprocess.run([sys.executable, __file__, '--child'], creationflags=0x01000000,
                                capture_output=True, text=True, timeout=15, check=True)
    except OSError as error:
        if '--expect-blocked' not in sys.argv or error.winerror != 5:
            raise
        print(json.dumps(dict(parent=current, breakaway='blocked_as_expected')))
        return
    if '--expect-blocked' in sys.argv:
        raise RuntimeError('default runner unexpectedly allowed breakaway')
    child = json.loads(result.stdout)
    if child['in_job'] or any(child[k] != current[k] for k in ('sid', 'integrity', 'session')):
        raise RuntimeError('breakaway changed user/session/integrity or remained contained')
    print(json.dumps(dict(parent=current, child=child, breakaway='explicitly_allowed')))
    if '--leak-probe' in sys.argv:
        root = Path(os.environ['SIDRAVIA_INNO_TEST_ROOT'])
        if not (root / '.sidravia-native-owner').is_file():
            raise RuntimeError('owned native fixture root required')
        source = root / 'cleanup_sentinel.go'
        source.write_text('package main\nimport "time"\nfunc main(){time.Sleep(5*time.Minute)}\n')
        executable = root / 'cleanup_sentinel.exe'
        subprocess.run(['go', 'build', '-o', str(executable), str(source)], check=True, timeout=90)
        process = subprocess.Popen([str(executable)], creationflags=0x01000000,
                                   stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print(json.dumps(dict(expected_cleanup_failure=True, escaped_pid=process.pid, executable=str(executable))), flush=True)


if __name__ == '__main__':
    main()
