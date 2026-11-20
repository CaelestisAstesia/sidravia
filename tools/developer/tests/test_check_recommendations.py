from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import check_recommendations as suggestions
import dev


class Recommendations(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git('init', '-q')
        self.write('.gitignore', '.local/\n.project/\nignored*\n')
        self.write('README.md', 'initial')
        self.write('internal/sample/sample.go', 'package sample\n')
        self.write('old file.py', 'original')
        self.git('add', '.')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'initial')
        self.base = self.git('rev-parse', 'HEAD').strip()

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, encoding='utf-8')

    def write(self, path, text):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding='utf-8')

    def recommend(self, *args):
        return suggestions.recommend(self.root, dev.parse_args(['check', '--recommend', *args]), lambda: {'commit': 'fixture'})

    def test_tracks_staged_unstaged_rename_deletion_untracked_and_ignores_private(self):
        self.git('mv', 'old file.py', 'new file.py')
        self.write('README.md', 'edited')
        (self.root / 'internal/sample/sample.go').unlink()
        special = 'tools/developer/中文 file.py'
        self.write(special, '# new')
        self.write('.project/private.md', 'private')
        self.write('ignored-secret', 'private')
        base, paths = suggestions.changed_paths(self.root, 'HEAD')
        self.assertEqual(base, self.base)
        self.assertEqual(set(paths), {'old file.py', 'new file.py', 'README.md', 'internal/sample/sample.go', special})

    @unittest.skipIf(sys.platform == 'win32', 'Windows filenames cannot contain newlines')
    def test_newline_filename_is_not_split(self):
        path = 'tools/developer/line\nbreak.py'
        self.write(path, '# new')
        self.assertEqual(suggestions.changed_paths(self.root, 'HEAD')[1], [path])

    def test_explicit_base_includes_branch_commits_and_local_edits(self):
        self.write('tools/developer/helper.py', '# tool')
        self.git('add', 'tools/developer/helper.py')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'tool')
        self.write('gui/lib/example.dart', '// gui')
        result = self.recommend('--base', self.base)
        self.assertEqual(result['scopes'], ['python', 'gui'])
        self.assertEqual({row['path'] for row in result['changes']}, {'tools/developer/helper.py', 'gui/lib/example.dart'})
        current = self.recommend()
        self.assertEqual(current['scopes'], ['gui'])

    def test_python_only_uses_existing_planner_without_execution_or_evidence(self):
        self.write('tools/developer/helper.py', '# tool')
        with mock.patch('verification_commands.run', side_effect=AssertionError('must not execute')):
            result = self.recommend()
        self.assertEqual(result['scopes'], ['python'])
        self.assertEqual([stage['name'] for stage in result['request']['stages']], ['python'])
        self.assertEqual(result['status'], 'planned')
        self.assertNotIn('--recommend', result['command'])
        parsed = dev.parse_args(result['command'][2:])
        self.assertEqual(parsed.scope, ['python'])
        self.assertFalse(parsed.plan)
        self.assertFalse(parsed.recommend)
        self.assertEqual(result['command_cwd'], str(self.root))
        self.assertFalse((self.root / '.project').exists())
        self.assertFalse((self.root / '.local').exists())

    def test_go_avoids_narrow_package_guess_and_platform_adds_cross_contracts(self):
        self.write('internal/sample/sample.go', 'package changed\n')
        result = self.recommend()
        command = result['request']['stages'][0]['argv']
        self.assertIn('--scope', command)
        self.assertIn('native', command)
        self.assertNotIn('--go-package', result['command'])
        self.write('internal/sample/sample_windows.go', 'package sample\n')
        self.assertIn('full', self.recommend()['command'])

    def test_gui_and_native_changes_show_manual_acceptance(self):
        self.write('gui/linux/runner.cc', '// native')
        result = self.recommend()
        self.assertEqual(result['scopes'], ['gui'])
        self.assertIn('Native GUI', result['changes'][0]['manual'])
        self.assertEqual([s['name'] for s in result['request']['stages']],
                         ['gui-sdk', 'gui-dependencies', 'gui-analyze', 'gui-tests'])
        self.assertTrue(any('campus' in item for item in result['not_covered']))

    def test_runtime_markdown_resource_is_not_treated_as_documentation(self):
        self.write('gui/assets/fonts/SourceCodePro-LICENSE.md', 'runtime license text')
        self.write('README.md', 'ordinary documentation')
        result = self.recommend()
        self.assertEqual(result['scopes'], ['gui'])
        decisions = {row['path']: row for row in result['changes']}
        self.assertEqual(decisions['README.md']['scopes'], [])
        self.assertEqual(decisions['gui/assets/fonts/SourceCodePro-LICENSE.md']['scopes'], ['gui'])

    def test_unknown_paths_are_visible_and_fall_back_conservatively(self):
        self.write('unexpected/config.blob', 'data')
        result = self.recommend()
        self.assertEqual(result['scopes'], ['go', 'python', 'gui'])
        self.assertIn('Unrecognized', result['changes'][0]['manual'])

    def test_clean_and_docs_only_do_not_fall_back_to_default_go_python(self):
        for dirty in (False, True):
            if dirty:
                self.write('README.md', 'updated')
            result = self.recommend()
            self.assertEqual(result['scopes'], [])
            self.assertEqual(result['request']['stages'], [])
            self.assertIsNone(result['command'])
            self.assertEqual(result['status'], 'planned')

    def test_bad_base_and_incompatible_flags_fail_explicitly(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.recommend('--base', 'missing-branch')
        for flags in (['--scope', 'python'], ['--gui-test', 'test/x.dart'], ['--go-checks', 'full'], ['--linux-build']):
            with self.subTest(flags=flags), self.assertRaisesRegex(ValueError, 'explicit'):
                self.recommend(*flags)

    def test_source_changes_during_planning_invalidate_recommendation(self):
        identities = iter([{'commit': 'before'}, {'commit': 'after'}])
        with self.assertRaisesRegex(ValueError, 'source changed'):
            suggestions.recommend(self.root, dev.parse_args(['check', '--recommend']), lambda: next(identities))

    def test_cli_emits_one_json_plan_and_rejects_base_on_execution(self):
        self.write('tools/developer/helper.py', '# tool')
        output = io.StringIO()
        with mock.patch.object(dev, 'ROOT', self.root), mock.patch.object(dev, 'source_fingerprint', return_value='fixture'), contextlib.redirect_stdout(output):
            self.assertEqual(dev.main(['check', '--recommend', '--plan']), 0)
        self.assertEqual(json.loads(output.getvalue())['scopes'], ['python'])
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(dev.main(['check', '--base', 'HEAD', '--format', 'json']), 1)
        self.assertIn('requires --recommend', json.loads(output.getvalue())['error'])


if __name__ == '__main__':
    unittest.main()
