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
import acceptance_summary as summary
import dev
import report_inspection
import validation_run as runner


class AcceptanceSummary(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def save(self, name, data):
        path = self.root / name
        runner.write_json(path, data)
        return path

    def desktop(self, **changes):
        receipt = {'passed': True, 'source': {'commit': 'a' * 40}, 'source_unchanged': True,
                   'namespace': 'synthetic', 'events': ['settings-saved', 'normal-exit'],
                   'campusAuthentication': False, 'realAccountAutostart': False}
        receipt.update(changes)
        return receipt

    def stage(self, **changes):
        receipt = {'schema': 1, 'kind': 'windows-vm', 'status': 'passed',
                   'source': {'commit': 'a' * 40}, 'source_unchanged': True,
                   'evidence': 'collected', 'cleanup': 'task_removed',
                   'stages': [{'name': 'native', 'status': 'passed', 'exit_code': 0}]}
        receipt.update(changes)
        return report_inspection.inspect(self.save('report.json', receipt), tail_bytes=0)

    def test_vm_execution_evidence_cleanup_are_independent(self):
        receipt = self.stage(status='failed', cleanup='unknown', cleanup_error='owned task uncertain')
        row = summary.dimensions(receipt)
        self.assertEqual(row['execution']['stage_status'], 'passed')
        self.assertEqual(row['execution']['reported_status'], 'failed')
        self.assertEqual(row['evidence']['collection'], 'collected')
        self.assertEqual(row['cleanup']['status'], 'unknown')
        self.assertEqual(row['errors']['cleanup_error'], 'owned task uncertain')

    def test_failed_execution_and_incomplete_evidence_are_not_fixed_by_cleanup(self):
        receipt = self.stage(status='failed', evidence='incomplete', evidence_error='transfer failed',
                             stages=[{'name': 'install', 'status': 'timed_out'}, {'name': 'uninstall', 'status': 'not_run'}])
        row = summary.dimensions(receipt)
        self.assertEqual(row['execution']['stage_status'], 'failed')
        self.assertEqual(row['execution']['stage_counts']['not_run'], 1)
        self.assertEqual(row['evidence']['collection'], 'incomplete')
        self.assertEqual(row['cleanup']['status'], 'task_removed')
        self.assertTrue(any('scheduled-task removal only' in gap for gap in row['not_covered']))

    def test_absent_facts_are_not_inferred_from_exit_zero(self):
        row = summary.dimensions(self.stage(evidence=None, cleanup=None, source_unchanged=False))
        self.assertIsNone(row['cleanup']['status'])
        self.assertTrue(any('Stable source' in gap for gap in row['not_covered']))
        row = summary.dimensions(report_inspection.inspect(self.save('local.json',
            {'schema': 1, 'kind': 'command-sequence', 'status': 'passed', 'stages': []}), tail_bytes=0))
        self.assertEqual(row['cleanup']['status'], 'not_recorded')
        self.assertEqual(row['execution']['stage_status'], 'incomplete')

    def test_native_pass_does_not_prove_finally_cleanup(self):
        path = self.save('desktop.json', self.desktop())
        row = summary.dimensions(summary.native(path, 'linux-desktop'))
        self.assertEqual(row['execution']['reported_status'], 'passed')
        self.assertEqual(row['cleanup']['status'], 'not_recorded')
        self.assertFalse(row['limits']['campusAuthentication'])
        self.assertTrue(any('finally' in gap for gap in row['not_covered']))

    def test_rpm_and_announcement_keep_their_boundaries(self):
        rpm = self.desktop(rpmSHA256=['b' * 64], hostInstallation=False, campusTraffic=False,
                           trayAcceptance='private bus has no tray host', signatures='unsigned local candidate')
        row = summary.dimensions(summary.native(self.save('rpm.json', rpm), 'linux-rpm'))
        self.assertEqual(row['limits']['trayAcceptance'], rpm['trayAcceptance'])
        self.assertFalse(row['limits']['hostInstallation'])
        announcement = {'passed': True, 'source': {}, 'source_unchanged': True,
                        'nativeLog': '/unavailable/copied-from-other-host.log',
                        'realSchoolRequests': False, 'realAccountIntegration': False}
        row = summary.dimensions(summary.native(self.save('announcement.json', announcement), 'linux-announcement'))
        self.assertFalse(row['limits']['realSchoolRequests'])
        self.assertEqual(row['evidence']['status'], 'receipt_available')
        self.assertEqual(row['evidence']['collection'], 'not_recorded')

    def test_windows_skips_manual_fail_and_checkpoint_coverage(self):
        receipt = {'Schema': 1, 'ExpectedBuildID': 'c' * 40, 'ManifestHash': 'e' * 64, 'Stage': 'Installed',
                   'Results': [{'Check': 'BuildID', 'Status': 'PASS'},
                               {'Check': 'KeepData', 'Status': 'SKIP'},
                               {'Check': 'IntegrationChoices', 'Status': 'MANUAL'}],
                   'UserPath': 'sensitive', 'Data': ['private'], 'User': 'private'}
        path = self.save('windows.json', receipt)
        parsed = summary.native(path, 'windows-field')
        self.assertEqual(parsed['status'], 'incomplete')
        self.assertNotIn('sensitive', json.dumps(parsed))
        row = summary.dimensions(parsed)
        self.assertIn('SKIP: KeepData', row['not_covered'])
        self.assertIn('MANUAL: IntegrationChoices', row['not_covered'])
        self.assertEqual(row['candidate_build'], 'c' * 40)
        self.assertEqual(row['candidate_manifest_sha256'], 'e' * 64)
        receipt['Results'].append({'Check': 'Payload', 'Status': 'FAIL', 'Detail': 'private detail'})
        parsed = summary.native(self.save('windows.json', receipt), 'windows-field')
        self.assertEqual(parsed['status'], 'failed')
        self.assertNotIn('private detail', json.dumps(parsed))

    def test_multiple_receipts_do_not_merge_source_or_candidate_identity(self):
        first = self.stage()
        path = self.save('native.json', self.desktop(source={'commit': 'd' * 40}))
        result = summary.combine(first, ['linux-desktop=' + str(path)])
        self.assertEqual(result['source_comparison'], 'different_commits')
        self.assertEqual(result['review_status'], 'attention_required')
        self.assertEqual(len(result['receipts']), 2)
        self.assertNotIn('status', result)
        self.save('native.json', self.desktop())
        self.assertEqual(summary.combine(first, ['linux-desktop=' + str(path)])['source_comparison'], 'same_commit_only')
        self.save('native.json', self.desktop(source={}))
        self.assertEqual(summary.combine(first, ['linux-desktop=' + str(path)])['source_comparison'], 'unknown')

    def test_review_status_never_claims_full_acceptance(self):
        self.assertEqual(summary.combine(self.stage(), [])['review_status'], 'recorded_checks_only')
        self.assertEqual(summary.combine(self.stage(source={}), [])['review_status'], 'incomplete')
        for changes in ({'cleanup': 'unknown'}, {'evidence': 'incomplete'}, {'source_unchanged': False}):
            with self.subTest(changes=changes):
                self.assertEqual(summary.combine(self.stage(**changes), [])['review_status'], 'attention_required')
        path = self.save('native.json', self.desktop())
        self.assertEqual(summary.combine(summary.native(path, 'linux-desktop'), [])['review_status'], 'incomplete')

    def test_bad_or_duplicate_attachments_fail_explicitly(self):
        first = self.stage()
        path = self.save('native.json', self.desktop())
        for attachments in (['missing-separator'], ['other=' + str(path)],
                            ['linux-desktop=' + str(path)] * 2):
            with self.subTest(attachments=attachments), self.assertRaises(ValueError):
                summary.combine(first, attachments)
        for receipt in ({'passed': True}, self.desktop(campusAuthentication='false'), self.desktop(events='events')):
            with self.subTest(receipt=receipt), self.assertRaises(ValueError):
                summary.native(self.save('bad.json', receipt), 'linux-desktop')

    def test_cli_reads_without_running_or_mutating_and_outputs_one_json(self):
        self.stage()
        native = self.save('native.json', self.desktop())
        before = {path.name: path.read_bytes() for path in self.root.iterdir()}
        output = io.StringIO()
        with mock.patch('subprocess.Popen', side_effect=AssertionError('read only')), contextlib.redirect_stdout(output):
            code = dev.main(['report', str(self.root), '--native', 'linux-desktop=' + str(native), '--format', 'json'])
        self.assertEqual(code, 0)
        self.assertEqual(len(json.loads(output.getvalue())['acceptance']['receipts']), 2)
        self.assertEqual(before, {path.name: path.read_bytes() for path in self.root.iterdir()})

    def test_standalone_native_text_and_invalid_stage_option(self):
        path = self.save('native.json', self.desktop())
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(dev.main(['report', str(path), '--kind', 'linux-desktop']), 0)
        self.assertIn('NOT COVERED:', output.getvalue())
        self.assertIn('cleanup:', output.getvalue())
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(dev.main(['report', str(path), '--kind', 'linux-desktop', '--stage', 'x', '--format', 'json']), 1)
        self.assertIn('only applies', json.loads(output.getvalue())['error'])


if __name__ == '__main__':
    unittest.main()
