"""Conservative, read-only scope suggestions; reuse the normal check planner."""
from __future__ import annotations

import copy
import json
import os
from pathlib import PurePosixPath
import subprocess
import sys

import verification_commands as checks


SCOPES = ('go', 'python', 'gui')


def changed_paths(root, base):
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=root)
    # Resolve one commit first. An invalid/missing base cannot become an empty
    # diff or an option. Compare that exact tree with the current worktree.
    commit = git('rev-parse', '--verify', '--end-of-options', base + '^{commit}').decode('ascii').strip()
    tracked = git('diff', '--name-only', '-z', '--no-renames', commit, '--')
    untracked = git('ls-files', '--others', '--exclude-standard', '-z')
    return commit, sorted({os.fsdecode(path) for path in (tracked + untracked).split(b'\0') if path})


def classify(path):
    """Return scopes, a reason and any extra manual acceptance requirement."""
    p = PurePosixPath(path)
    if path.startswith('.github/workflows/'):
        return SCOPES, 'CI configuration can affect all verification jobs', 'Hosted workflow validation'
    if path.startswith('gui/assets/'):
        return ('gui',), 'bundled GUI resource, including runtime-loaded license text', 'Affected-page visual/interaction review'
    if path.startswith('docs/') or p.suffix.lower() == '.md' or path in ('LICENSE', 'NOTICE', '.gitignore'):
        return (), 'documentation or repository metadata; no executable check inferred', None
    if path in ('go.mod', 'go.sum'):
        return ('go',), 'Go dependency/toolchain change', None
    if path.startswith(('packaging/', 'scripts/', 'tools/build/')):
        return SCOPES, 'build, integration or packaging inputs affect delivery contracts', 'Relevant native fixture and exact-package acceptance'
    if p.suffix == '.go':
        return ('go',), 'Go source/test: full native scope includes reverse dependents', None
    if path.startswith('gui/'):
        if (path.startswith(('gui/native/', 'gui/test/native/'))
                or (path.startswith('gui/test/') and p.suffix in ('.cc', '.cpp', '.h'))):
            return ('gui',), 'shared native GUI code or contract test', 'Native CTest and affected GUI platform acceptance'
        native = path.startswith(('gui/linux/', 'gui/windows/', 'gui/macos/'))
        return ('gui',), 'GUI source, assets, dependencies or platform project', ('Native GUI build and platform acceptance' if native else 'Affected-page visual/interaction review')
    if path.startswith(('tools/drcom520d_mock_server/', 'tools/windows_drcom_acceptance/')):
        return ('python',), 'existing Python verifier covers these tool suites', None
    if path in (
        'tools/developer/linux_development.py',
        'tools/developer/linux_rpm.py',
        'tools/developer/linux_rpm_fixture.py',
        'tools/developer/verify_distribution.py',
    ):
        return ('python',), 'delivery builder or package consumer verifier', 'Relevant native fixture and exact-package acceptance'
    if path.startswith('tools/developer/') and p.suffix == '.py':
        return ('python',), 'developer Python tool/test: run the existing Python scope', None
    if path.startswith('tools/developer/'):
        return SCOPES, 'build, integration or acceptance inputs affect delivery contracts', 'Relevant native fixture and exact-package acceptance'
    if path.startswith('tools/wireshark/'):
        return ('python',), 'acceptance-tool suite includes Lua contract checks', 'Packet/decoder scenario review'
    return SCOPES, 'unrecognized path: conservative scopes, manual scope review required', 'Unrecognized path; decide additional checks'


def recommend(root, args, identity):
    if (args.scope or args.go_package or args.python_test or args.gui_test
            or args.preview_build or args.linux_build or args.go_checks != 'native'):
        raise ValueError('--recommend cannot combine explicit scope/test/build selections; use the emitted command to customize')
    before = identity()
    base, paths = changed_paths(root, args.base or 'HEAD')
    decisions = []
    selected = set()
    cross = False
    for path in paths:
        scopes, reason, manual = classify(path)
        selected.update(scopes)
        # Platform-specific source and module changes warrant compile contracts.
        cross |= path in ('go.mod', 'go.sum') or (
            path.endswith('.go') and any(token in PurePosixPath(path).stem
                                       for token in ('_windows', '_linux', '_darwin', '_unix', '_amd64', '_arm64')))
        decisions.append({'path': path, 'scopes': list(scopes), 'reason': reason, 'manual': manual})
    scopes = [scope for scope in SCOPES if scope in selected]
    options = copy.copy(args)
    options.scope = scopes
    options.go_checks = 'full' if cross else 'native'
    request = checks.plan(root, options) if scopes else {'schema': 1, 'stages': []}
    request['source'] = before
    command = None
    if scopes:
        command = [sys.executable, str(root/'tools/developer/dev.py'), 'check']
        for scope in scopes:
            command.extend(['--scope', scope])
        command.extend(['--go', args.go, '--flutter', args.flutter, '--go-checks', options.go_checks,
                        '--jobs', str(args.jobs), '--stage-timeout', str(args.stage_timeout),
                        '--reports', args.reports, '--format', args.format])
    if identity() != before:
        raise ValueError('source changed while recommending checks; inspect the new diff')
    return {'schema': 1, 'kind': 'check-recommendation', 'status': 'planned',
            'base_commit': base, 'source': before, 'source_unchanged': True,
            'changes': decisions, 'scopes': scopes, 'request': request,
            'command': command, 'command_cwd': str(root),
            'not_covered': [
                'No checks have run; this is a recommendation, not acceptance.',
                'No import/dependency graph inference; scopes do not prove every affected contract.',
                'Native execution on other platforms, GUI delivery, installation/update, campus and release acceptance.',
            ]}


def run(root, args, identity):
    # Like check --plan, emit one JSON plan even with the default text format.
    print(json.dumps(recommend(root, args, identity), ensure_ascii=True, indent=2))
    return 0
