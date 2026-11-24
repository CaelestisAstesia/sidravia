import contextlib
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import subprocess
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import dev
import verification_commands as checks


class CheckPlanning(unittest.TestCase):
    def test_gui_combination_has_one_analysis_and_one_test_stage(self):
        args=dev.parse_args(['check','--scope','gui','--scope','gui','--gui-test','test/preview/offline_demo_test.dart','--gui-test','test/preview/offline_demo_test.dart','--preview-build','--linux-build'])
        request=checks.plan(Path('/repo'),args)
        names=[s['name'] for s in request['stages']]
        self.assertEqual(names,['gui-sdk','gui-dependencies','gui-analyze','gui-tests','preview-web','linux-bundle'])
        self.assertEqual(request['stages'][3]['argv'].count('test/preview/offline_demo_test.dart'),1)
        self.assertNotIn('--check-gui',request['stages'][-1]['argv'])

    def test_targeted_checks_preserve_test_rerun_and_avoid_full_matrix(self):
        args=dev.parse_args(['check','--scope','go','--go-package','./internal/update/...','--go-package','./internal/update/...'])
        stages=checks.plan(Path('/repo'),args)['stages']
        self.assertEqual(stages[0]['argv'],['go','test','-count=1','./internal/update/...'])
        self.assertEqual([s['name'] for s in stages],['go-test','go-vet','diff-check'])

    def test_options_cannot_be_hidden_inside_test_selections(self):
        for options in (['--scope','go','--go-package','-count=0'],['--scope','gui','--gui-test','../outside.dart'],['--scope','go','--gui-test','test/example.dart']):
            with self.subTest(options=options):
                try:
                    args=dev.parse_args(['check',*options])
                except SystemExit:
                    continue
                with self.assertRaises(ValueError):checks.plan(Path('/repo'),args)

    def test_plan_is_json_and_does_not_create_a_report(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'tools/developer/tests').mkdir(parents=True)
            (root/'tools/developer/tests/test_dev.py').write_text('')
            args=dev.parse_args(['check','--scope','python','--python-test','test_dev.py','--plan','--reports',str(root/'evidence')])
            stdout=io.StringIO()
            with contextlib.redirect_stdout(stdout):self.assertEqual(checks.run(root,args,lambda:{'commit':'fixture'}),0)
            self.assertEqual(json.loads(stdout.getvalue())['stages'][0]['name'],'python-1')
            self.assertFalse((root/'evidence').exists())

    def test_misspelled_selection_cannot_pass_with_zero_tests(self):
        with tempfile.TemporaryDirectory() as folder:
            args=dev.parse_args(['check','--scope','python','--python-test','test_typo.py'])
            with self.assertRaisesRegex(ValueError,'matches no files'):
                checks.plan(Path(folder),args)

    def test_real_failure_produces_one_json_result_and_complete_logs(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            args=dev.parse_args(['check','--scope','python','--format','json','--reports',str(root/'evidence')])
            request={'schema':1,'stages':[{'name':'failure','argv':[sys.executable,'-c','import sys;print("诊断");sys.exit(9)'],'cwd':str(root)}]}
            stdout=io.StringIO()
            with mock.patch.object(checks,'plan',return_value=request),contextlib.redirect_stdout(stdout):
                code=checks.run(root,args,lambda:{'commit':'fixture'})
            result=json.loads(stdout.getvalue())
            self.assertEqual(code,1)
            self.assertEqual(result['stages'][0]['exit_code'],9)
            self.assertEqual((Path(result['report']).parent/'failure.stdout.log').read_text().strip(),'诊断')

    def test_normal_linux_and_preview_entries_reject_a_busy_worktree(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            developer = Path(dev.__file__).parent
            entry = (
                'import sys; from pathlib import Path; sys.path.insert(0, sys.argv[1]); '
                'import dev; dev.ROOT = Path(sys.argv[2]); raise SystemExit(dev.main(sys.argv[3:]))'
            )
            with checks.worktree_lock(root):
                for command in ('linux', 'preview'):
                    with self.subTest(command=command):
                        child = subprocess.run(
                            [sys.executable, '-c', entry, str(developer), str(root), command],
                            capture_output=True, text=True, timeout=10,
                        )
                        self.assertEqual(child.returncode, 1, child.stderr)
                        self.assertIn('another developer command owns this worktree', child.stderr)
                        self.assertFalse((root/'gui').exists())

    def test_python_check_can_run_while_a_flutter_writer_owns_the_worktree(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            suite = root / 'tools/developer/tests'
            suite.mkdir(parents=True)
            suite.joinpath('test_example.py').write_text(
                'import unittest\n'
                'class Example(unittest.TestCase):\n'
                '    def test_example(self):\n'
                '        self.assertTrue(True)\n'
            )
            args = dev.parse_args([
                'check', '--scope', 'python', '--python-test', 'test_example.py',
                '--format', 'json', '--reports', str(root / 'evidence'),
            ])
            stdout = io.StringIO()
            with checks.worktree_lock(root), contextlib.redirect_stdout(stdout):
                code = checks.run(root, args, lambda: {'commit': 'fixture'})
            result = json.loads(stdout.getvalue())
            self.assertEqual(code, 0)
            self.assertTrue(result['source_unchanged'])
            self.assertEqual(result['stages'][0]['status'], 'passed')

    def test_linux_bundle_stage_can_take_ownership_after_prior_checks(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            args = dev.parse_args([
                'check', '--scope', 'gui', '--linux-build', '--format', 'json',
                '--reports', str(root / 'evidence'),
            ])
            developer = Path(dev.__file__).parent
            child = (
                'import sys; from pathlib import Path; sys.path.insert(0, sys.argv[1]); '
                'from verification_commands import worktree_lock\n'
                'with worktree_lock(Path(sys.argv[2])): print("child owns the worktree")'
            )
            request = {'schema': 1, 'stages': [
                {'name': 'gui-tests', 'argv': [sys.executable, '-c', 'pass'], 'cwd': str(root)},
                {'name': 'linux-bundle', 'argv': [sys.executable, '-c', child, str(developer), str(root)], 'cwd': str(root)},
            ]}
            stdout = io.StringIO()
            with mock.patch.object(checks, 'plan', return_value=request), contextlib.redirect_stdout(stdout):
                code = checks.run(root, args, lambda: {'commit': 'fixture'})
            result = json.loads(stdout.getvalue())
            self.assertEqual(code, 0)
            self.assertTrue(all(stage['status'] == 'passed' for stage in result['stages']))
            log = Path(result['report']).parent / 'linux-bundle.stdout.log'
            self.assertIn('child owns the worktree', log.read_text())

    def test_source_edit_invalidates_a_successful_process(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            args=dev.parse_args(['check','--scope','python','--format','json','--reports',str(root/'evidence')])
            request={'schema':1,'stages':[{'name':'success','argv':[sys.executable,'-c','pass'],'cwd':str(root)}]}
            with mock.patch.object(checks,'plan',return_value=request),contextlib.redirect_stdout(io.StringIO()):
                identities=iter([{'fingerprint':'before'},{'fingerprint':'after'}])
                code=checks.run(root,args,lambda:next(identities))
            report=json.loads(next((root/'evidence').glob('*/report.json')).read_text())
            self.assertEqual(code,1)
            self.assertFalse(report['source_unchanged'])
            self.assertEqual(report['stages'][0]['status'],'passed')

    @unittest.skipIf(os.name == 'nt', 'Unix fake SDK; Windows Job lifecycle is covered by runner contracts')
    def test_sdk_deadline_stops_descendants_and_retains_stage_report(self):
        import time
        actual_root=Path(__file__).resolve().parents[3]
        with tempfile.TemporaryDirectory() as folder:
            temp=Path(folder)
            (temp/'tools').mkdir()
            (temp/'tools/developer').symlink_to(actual_root/'tools/developer',target_is_directory=True)
            (temp/'gui').mkdir()
            (temp/'gui/.flutter-version').write_text('fixture')
            sentinel=temp/'late-writer'
            sdk=temp/'fake-flutter'
            child=f'import time;from pathlib import Path;time.sleep(1.3);Path({str(sentinel)!r}).write_text("escaped")'
            sdk.write_text('#!'+sys.executable+'\nimport subprocess,sys,time\nsubprocess.Popen([sys.executable,"-c",'+repr(child)+'])\ntime.sleep(30)\n')
            sdk.chmod(0o700)
            args=dev.parse_args(['check','--scope','gui','--flutter',str(sdk),'--stage-timeout','1','--format','json','--reports',str(temp/'evidence')])
            stdout=io.StringIO()
            # Real SDK validator subprocess, real group, real timeout.
            with contextlib.redirect_stdout(stdout):
                code=checks.run(temp,args,lambda:{'commit':'fixture'})
            result=json.loads(stdout.getvalue())
            self.assertEqual(code,1)
            self.assertEqual(result['stages'][0]['name'],'gui-sdk')
            self.assertEqual(result['stages'][0]['status'],'timed_out')
            self.assertTrue(all(s['status']=='not_run' for s in result['stages'][1:]))
            time.sleep(1.5)
            self.assertFalse(sentinel.exists())

    @unittest.skipIf(os.name == 'nt', 'Unix executable fixture; Windows uses the same metadata binding')
    def test_sdk_metadata_binds_fixed_dart_for_subprocess_checks(self):
        actual_root=Path(__file__).resolve().parents[3]
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'tools').mkdir();(root/'tools/developer').symlink_to(actual_root/'tools/developer',target_is_directory=True)
            (root/'gui').mkdir();(root/'gui/.flutter-version').write_text('fixture')
            sdk=root/'sdk';(sdk/'bin').mkdir(parents=True)
            dart=sdk/'bin/dart';dart.write_text('#!'+sys.executable+'\nprint("fixed-dart")\n');dart.chmod(0o700)
            flutter=root/'wrapper-flutter'
            script='#!'+sys.executable+'\nimport sys,json,shutil,subprocess,os\n'
            script+='if sys.argv[1]=="--version":print(json.dumps('+repr({'frameworkVersion':'fixture','flutterRoot':str(sdk)})+'))\n'
            script+='else:\n assert os.path.realpath(shutil.which("dart"))=='+repr(str(dart))+'\n subprocess.run(["dart","--version"],check=True)\n'
            flutter.write_text(script);flutter.chmod(0o700)
            args=dev.parse_args(['check','--scope','gui','--flutter',str(flutter),'--format','json','--reports',str(root/'evidence')])
            stdout=io.StringIO()
            with contextlib.redirect_stdout(stdout):code=checks.run(root,args,lambda:{'commit':'fixture'})
            result=json.loads(stdout.getvalue())
            self.assertEqual(code,0)
            self.assertEqual([s['name'] for s in result['stages']],['gui-sdk','gui-dependencies','gui-analyze','gui-tests'])
            request=json.loads((Path(result['report']).parent/'request.json').read_text())
            self.assertNotIn('PATH',request['stages'][0].get('environment',{}))
            self.assertTrue(all(s['environment']['PATH'].startswith(str(sdk/'bin')) for s in request['stages'][1:]))
