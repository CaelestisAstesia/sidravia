import contextlib
import importlib.util
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

if sys.platform == 'linux':
    import fcntl

ROOT = Path(__file__).resolve().parents[3]
SOURCE = ROOT / 'packaging/linux/rpm_maintenance.py'


def load():
    spec = importlib.util.spec_from_file_location('rpm_maintenance', SOURCE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@unittest.skipUnless(sys.platform == 'linux' and hasattr(os, 'pidfd_open'), 'requires Linux pidfds')
class MaintenanceContracts(unittest.TestCase):
    def test_guard_setup_failure_cannot_acknowledge_success(self):
        m = load()
        with tempfile.TemporaryDirectory(prefix='rpm-guard-failure-', dir=ROOT / '.local') as tmp:
            path = Path(tmp) / 'gate'
            lock = os.open(path, os.O_CREAT | os.O_RDONLY, 0o644)
            parent = os.pidfd_open(os.getpid())
            transaction = os.open(Path(tmp) / 'transaction', os.O_CREAT | os.O_RDWR, 0o644)
            try:
                fcntl.flock(lock, fcntl.LOCK_EX)
                with mock.patch.object(m.os, 'setsid', side_effect=OSError('synthetic setup failure')):
                    with self.assertRaisesRegex(RuntimeError, 'did not become ready'):
                        m.hold_until_transaction_exits(lock, parent, transaction)
            finally:
                for fd in (lock, parent, transaction): os.close(fd)
            with path.open() as check:
                fcntl.flock(check, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_exact_installation_signals_only_its_process(self):
        if os.geteuid() != 0:
            child = subprocess.run(['unshare', '--user', '--map-root-user', '--pid', '--fork', '--mount-proc',
                sys.executable, '-m', 'unittest', 'test_rpm_maintenance.MaintenanceContracts.test_exact_installation_signals_only_its_process'],
                cwd=Path(__file__).parent, capture_output=True, text=True, timeout=10)
            self.assertEqual(child.returncode, 0, child.stdout + child.stderr)
            return
        m = load()
        with tempfile.TemporaryDirectory(prefix='rpm-maintenance-', dir=ROOT / '.local') as tmp:
            app = Path(tmp) / 'app'
            app.mkdir()
            (app / 'sidravia.rpm').write_text('sidravia-rpm-v1\n')
            for name in m.NAMES:
                shutil.copy2('/usr/bin/sleep', app / name)
            other = Path(tmp) / 'other'
            other.mkdir()
            shutil.copy2('/usr/bin/sleep', other / 'sidraviad')
            own = subprocess.Popen([str(app / 'sidraviad'), '60'])
            bystander = subprocess.Popen([str(other / 'sidraviad'), '60'])
            try:
                m.APP = app
                with self.assertRaisesRegex(RuntimeError, 'legacy installation'):
                    m.require_cooperative_installation()
                self.assertIsNone(own.poll())
                (app / 'sidravia.rpm-lifecycle').write_text('sidravia-rpm-lifecycle-v1\n')
                m.require_cooperative_installation()
                m.stop_group(False, 2)
                self.assertEqual(own.wait(timeout=2), -signal.SIGTERM)
                self.assertIsNone(bystander.poll())
            finally:
                for process in (own, bystander):
                    if process.poll() is None: process.terminate()
                    process.wait(timeout=3)

    def test_guard_releases_when_transaction_unlocks_while_host_stays_alive(self):
        with tempfile.TemporaryDirectory(prefix='rpm-guard-', dir=ROOT / '.local') as tmp:
            gate = Path(tmp) / 'lock'
            code = f'''import importlib.util,os,fcntl,sys
s=importlib.util.spec_from_file_location('m',{str(SOURCE)!r});m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
fd=os.open({str(gate)!r},os.O_CREAT|os.O_RDONLY,0o644);fcntl.flock(fd,fcntl.LOCK_EX)
tx=os.open({str(gate)+'.transaction'!r},os.O_CREAT|os.O_RDWR,0o644);fcntl.lockf(tx,fcntl.LOCK_EX)
parent=os.pidfd_open(os.getpid());m.hold_until_transaction_exits(fd,parent,tx);os.close(fd)
print('ready',flush=True);sys.stdin.readline();fcntl.lockf(tx,fcntl.LOCK_UN)
print('unlocked',flush=True);sys.stdin.readline()
'''
            supervisor = subprocess.Popen([sys.executable, '-c', code], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
            try:
                self.assertEqual(supervisor.stdout.readline().strip(), 'ready')
                with gate.open() as lock:
                    with self.assertRaises(BlockingIOError): fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
                    supervisor.stdin.write('\n');supervisor.stdin.flush()
                    self.assertEqual(supervisor.stdout.readline().strip(),'unlocked')
                    deadline = time.monotonic() + 3
                    while True:
                        try:
                            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
                            break
                        except BlockingIOError:
                            if time.monotonic() >= deadline: self.fail('orphaned maintenance gate')
                            time.sleep(.02)
                    self.assertIsNone(supervisor.poll())
                    supervisor.communicate('\n', timeout=5)
            finally:
                if supervisor.poll() is None: supervisor.kill()
                supervisor.wait(timeout=3)
