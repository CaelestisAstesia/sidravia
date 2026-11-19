from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import dev
import report_inspection as inspection
import validation_run as runner


class ReportInspection(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)

    def save(self, **changes):
        report = {'schema': 1, 'kind': 'verification', 'status': 'failed',
                  'source': {'commit': 'a' * 40, 'files': ['large inventory']},
                  'source_unchanged': False,
                  'stages': [{'name': 'tests', 'status': 'failed', 'exit_code': 1,
                              'duration_seconds': 2, 'stdout': 'tests.stdout.log',
                              'stderr': 'tests.stderr.log', 'argv': ['private argument']},
                             {'name': 'build', 'status': 'not_run'}]}
        report.update(changes)
        runner.write_json(self.directory / 'report.json', report)
        return report

    def test_real_runner_receipt_failure_and_not_run_without_reexecution(self):
        request = {'schema': 1, 'stages': [
            {'name': 'tests', 'argv': [sys.executable, '-c', "import sys; print('诊断'); sys.exit(3)"]},
            {'name': 'never', 'argv': ['must-not-execute']}]}
        runner.run_request(request, self.directory)
        before = {p.name: p.read_bytes() for p in self.directory.iterdir()}
        with mock.patch('subprocess.Popen', side_effect=AssertionError('must only read')):
            summary = inspection.inspect(self.directory)
        self.assertEqual(summary['counts'], {'failed': 1, 'not_run': 1})
        self.assertEqual(summary['stages'][0]['exit_code'], 3)
        self.assertIn('诊断', summary['stages'][0]['logs']['stdout']['tail'])
        self.assertEqual(before, {p.name: p.read_bytes() for p in self.directory.iterdir()})

    def test_bounded_tails_preserve_raw_bytes_and_mark_missing(self):
        self.save()
        data = b'x' * 100000 + b'\xfffailure'
        (self.directory / 'tests.stdout.log').write_bytes(data)
        summary = inspection.inspect(self.directory, tail_bytes=8)
        logs = summary['stages'][0]['logs']
        self.assertEqual(logs['stderr']['status'], 'missing')
        self.assertTrue(logs['stdout']['truncated'])
        self.assertEqual(logs['stdout']['tail'], '\ufffdfailure')
        self.assertEqual((self.directory / 'tests.stdout.log').read_bytes(), data)
        self.assertNotIn('files', summary['source'])
        self.assertNotIn('argv', summary['stages'][0])
        self.assertFalse(summary['facts']['source_unchanged'])

    def test_explicit_stage_and_zero_tail(self):
        report = self.save(status='passed')
        report['stages'][0]['status'] = 'passed'
        runner.write_json(self.directory / 'report.json', report)
        (self.directory / 'tests.stderr.log').write_text('successful diagnostic')
        self.assertNotIn('logs', inspection.inspect(self.directory)['stages'][0])
        selected = inspection.inspect(self.directory, stage='tests')
        self.assertEqual(len(selected['stages']), 1)
        self.assertIn('successful', selected['stages'][0]['logs']['stderr']['tail'])
        with mock.patch.object(inspection, 'log_tail', side_effect=AssertionError('no log access')):
            self.assertNotIn('logs', inspection.inspect(self.directory, stage='tests', tail_bytes=0)['stages'][0])
        with self.assertRaisesRegex(ValueError, 'stage not found'):
            inspection.inspect(self.directory, stage='missing')

    def test_vm_evidence_and_cleanup_errors_remain_separate(self):
        self.save(kind='windows-vm', evidence='incomplete', cleanup='unknown',
                  evidence_error='transfer failed', cleanup_error='task uncertain',
                  host_source_unchanged=True, guest_source_unchanged=False)
        summary = inspection.inspect(self.directory)
        self.assertEqual(summary['errors'], {'evidence_error': 'transfer failed', 'cleanup_error': 'task uncertain'})
        self.assertEqual(summary['facts']['cleanup'], 'unknown')
        self.assertFalse(summary['facts']['guest_source_unchanged'])

    def test_log_traversal_and_symlinks_rejected(self):
        for name in ('../outside.stderr.log', r'C:\outside.stderr.log', r'..\outside.stderr.log', '/outside.stderr.log'):
            report = self.save()
            report['stages'][0]['stderr'] = name
            runner.write_json(self.directory / 'report.json', report)
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'sibling'):
                inspection.inspect(self.directory)
        self.save()
        target = self.directory / 'target'
        target.write_text('do not read')
        link = self.directory / 'tests.stderr.log'
        try:
            link.symlink_to(target)
        except OSError:
            self.skipTest('symlinks unavailable on this host')
        with self.assertRaisesRegex(ValueError, 'linked'):
            inspection.inspect(self.directory)

    def test_bad_receipts_fail_explicitly(self):
        for change in ({'schema': True}, {'kind': []}, {'status': []},
                       {'stages': [{'name': 'a', 'status': []}]},
                       {'kind': 'visual-comparison'}, {'stages': None},
                       {'status': 'mystery'}, {'duration_seconds': -1},
                       {'stages': [{'name': 'a', 'status': 'passed'}, {'name': 'a', 'status': 'failed'}]}):
            self.save(**change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                inspection.inspect(self.directory)

    def test_cli_json_is_one_object_and_failed_receipt_is_not_execution(self):
        self.save()
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = dev.main(['report', str(self.directory), '--format', 'json'])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(output.getvalue())['status'], 'failed')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = dev.main(['report', str(self.directory), '--stage', 'missing', '--format', 'json'])
        self.assertEqual(code, 1)
        self.assertIn('stage not found', json.loads(output.getvalue())['error'])

    def test_text_sanitizes_terminal_controls(self):
        self.save(error='\x1b[31mfailure\x1b[0m')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            dev.main(['report', str(self.directory)])
        self.assertNotIn('\x1b', output.getvalue())
        self.assertIn('source_unchanged: False', output.getvalue())

    def test_partial_download_receipt_and_malformed_fields_keep_json_contract(self):
        self.save(kind='release-download', stages=[], status='interrupted')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(dev.main(['report', str(self.directory), '--format', 'json']), 0)
        self.assertEqual(json.loads(output.getvalue())['status'], 'interrupted')
        for change in ({'not_covered': None}, {'not_covered': [None]}, {'download': None},
                       {'download': {'signature': []}}, {'download': {'signedAssets': [None]}}):
            self.save(kind='release-download', **change)
            output = io.StringIO()
            with self.subTest(change=change), contextlib.redirect_stdout(output):
                self.assertEqual(dev.main(['report', str(self.directory), '--format', 'json']), 1)
            self.assertIn('invalid download', json.loads(output.getvalue())['error'])


if __name__ == '__main__':
    unittest.main()
