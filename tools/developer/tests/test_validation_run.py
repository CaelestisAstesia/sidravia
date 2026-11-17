import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import validation_run as runner
import windows_vm


class ValidationContracts(unittest.TestCase):
    def stage(self, name, script, **kwargs):
        return dict(name=name, argv=[sys.executable, '-c', script], timeout_seconds=10, **kwargs)

    def test_unicode_argv_logs_and_machine_output(self):
        stage = self.stage('unicode', 'import sys;print(repr(sys.argv[1:]));print("错误",file=sys.stderr)')
        stage['argv'] += ['中文 空格', 'a"b', 'tail\\']
        with tempfile.TemporaryDirectory() as folder:
            report = runner.run_request({'schema': 1, 'stages': [stage]}, folder)
            self.assertEqual(report['status'], 'passed')
            self.assertIn('中文 空格', (Path(folder) / 'unicode.stdout.log').read_text())
            self.assertIn('错误', (Path(folder) / 'unicode.stderr.log').read_text())
            stdout = io.StringIO()
            with contextlib.redirect_stdout(stdout):
                runner.present(report, Path(folder) / 'report.json', 'json')
            self.assertEqual(json.loads(stdout.getvalue())['status'], 'passed')
            with self.assertRaisesRegex(ValueError, 'already exists'):
                runner.run_request({'schema': 1, 'stages': [stage]}, folder)

    def test_nonzero_stops_sequence_and_keeps_both_logs(self):
        request = {'schema': 1, 'stages': [self.stage('fail', 'import sys;print("diagnostic");sys.exit(7)'), self.stage('later', 'raise Exception("must not run")')]}
        with tempfile.TemporaryDirectory() as folder:
            report = runner.run_request(request, folder)
            self.assertEqual(report['status'], 'failed')
            self.assertEqual(report['stages'][0]['exit_code'], 7)
            self.assertEqual(report['stages'][1]['status'], 'not_run')
            self.assertIn('diagnostic', (Path(folder) / 'fail.stdout.log').read_text())

    def test_timeout_terminates_descendant_and_retains_partial_output(self):
        with tempfile.TemporaryDirectory() as folder:
            sentinel = Path(folder) / 'escaped'
            child = f'import time;from pathlib import Path;time.sleep(3);Path({str(sentinel)!r}).write_text("escaped")'
            script = f'import subprocess,sys,time;print("started",flush=True);subprocess.Popen([sys.executable,"-c",{child!r}]);time.sleep(5)'
            stage = self.stage('timeout', script)
            stage['timeout_seconds'] = 2 if os.name == 'nt' else .2
            report = runner.run_request({'schema': 1, 'stages': [stage]}, folder)
            self.assertEqual(report['stages'][0]['status'], 'timed_out')
            self.assertIn('started', (Path(folder) / 'timeout.stdout.log').read_text())
            time.sleep(3.1)
            self.assertFalse(sentinel.exists())

    def test_normal_parent_exit_cannot_leave_a_writer(self):
        with tempfile.TemporaryDirectory() as folder:
            sentinel = Path(folder) / 'escaped'
            child = f'import time;from pathlib import Path;time.sleep(1);Path({str(sentinel)!r}).write_text("escaped")'
            script = f'import subprocess,sys;subprocess.Popen([sys.executable,"-c",{child!r}])'
            report = runner.run_request({'schema':1,'stages':[self.stage('parent',script)]},folder)
            self.assertEqual(report['status'],'passed')
            time.sleep(1.1)
            self.assertFalse(sentinel.exists())

    def test_rejects_ambiguous_requests_before_commands(self):
        good = self.stage('one', 'pass')
        for request in ({'schema': 2, 'stages': [good]}, {'schema': 1, 'stages': [good, good]}, {'schema': 1, 'stages': [dict(good, argv='echo unsafe')]}, {'schema': 1, 'stages': [dict(good, timeout_seconds=False)]}, {'schema': 1, 'stages': [dict(good, environment={'PATH':'one','Path':'two'})]}):
            with self.subTest(request=request), self.assertRaises(ValueError):
                runner.validate_request(request)

    def test_vm_expands_data_without_interpreting_braces_or_shell(self):
        request = {'schema': 1, 'stages': [dict(name='native', argv=['{source}\\runner.exe', '{"literal":1}', 'a;$(x)'], cwd='tools', environment={'OUT':'{run}\\out'})]}
        result = windows_vm.expand_request(request, {'source': r'C:\owned\source', 'run': r'C:\owned\run', 'workspace': r'C:\build'})
        self.assertEqual(result['stages'][0]['argv'][1:], ['{"literal":1}', 'a;$(x)'])
        self.assertEqual(result['stages'][0]['cwd'], r'C:\owned\source\tools')
        for cwd in ('../foreign', r'C:\foreign', r'\foreign', 'C:foreign'):
            with self.subTest(cwd=cwd), self.assertRaises(ValueError):
                windows_vm.expand_request({'schema':1,'stages':[dict(request['stages'][0],cwd=cwd)]},{'source':r'C:\source'})

    def test_windows_snapshot_rejects_aliases_links_and_changed_bytes(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'a.txt').write_bytes(b'a')
            digest = __import__('hashlib').sha256(b'a').hexdigest()
            manifest = {'files': [{'path':'a.txt','sha256':digest}]}
            self.assertGreater(len(windows_vm.snapshot(root, manifest)), 0)
            (root / 'a.txt').write_bytes(b'b')
            with self.assertRaisesRegex(ValueError, 'changed'):
                windows_vm.snapshot(root, manifest)
            for name in ('../a.txt', 'C:/a.txt', 'NUL.txt', 'a.txt.', 'a\\b', '.project/secret'):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    windows_vm.snapshot(root, {'files':[{'path':name,'sha256':digest}]})
