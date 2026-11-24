"""Compose existing checks once; share the VM command report and log runner."""
from __future__ import annotations

import datetime
import contextlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import validation_run as runner


def plan(root, args):
    scopes = list(dict.fromkeys(args.scope or ['go', 'python']))
    if (args.go_package and 'go' not in scopes) or (args.python_test and 'python' not in scopes):
        raise ValueError('selected tests require the matching scope')
    if (args.gui_test or args.preview_build or args.linux_build) and 'gui' not in scopes:
        raise ValueError('GUI tests/builds require --scope gui')
    if args.go_package and args.go_checks != 'native':
        raise ValueError('targeted Go packages use native checks; request cross separately')
    stages = []
    def add(name, argv, cwd=root):
        stages.append({'name':name, 'argv':list(map(str, argv)), 'cwd':str(cwd), 'timeout_seconds':args.stage_timeout})
    if 'go' in scopes:
        if args.go_package:
            packages = list(dict.fromkeys(args.go_package))
            if any(not p or p.startswith('-') for p in packages):
                raise ValueError('Go packages must not be options')
            add('go-test', [args.go, 'test', '-count=1', *packages])
            add('go-vet', [args.go, 'vet', *packages])
            add('diff-check', ['git', 'diff', '--check'])
        else:
            add('go', [sys.executable, root/'tools/developer/verify_repository.py', '--scope', 'go', '--go', args.go, '--go-checks', args.go_checks, '--jobs', args.jobs])
    if 'python' in scopes:
        if args.python_test:
            for index, pattern in enumerate(dict.fromkeys(args.python_test)):
                if '/' in pattern or '\\' in pattern or not pattern.endswith('.py'):
                    raise ValueError('Python test patterns are filenames below tools/developer/tests')
                if not any(path.is_file() for path in (root/'tools/developer/tests').glob(pattern)):
                    raise ValueError('Python test selection matches no files: ' + pattern)
                add('python-' + str(index+1), [sys.executable,'-m','unittest','discover','-s',root/'tools/developer/tests','-p',pattern,'-v'])
        else:
            add('python', [sys.executable, root/'tools/developer/verify_repository.py', '--scope', 'python'])
    if 'gui' in scopes:
        flutter = str(Path(args.flutter).resolve())
        gui = root/'gui'
        add('gui-sdk', [sys.executable, root/'tools/developer/verification_commands.py', '--sdk', flutter, root/'gui/.flutter-version'], root)
        add('gui-dependencies', [flutter,'pub','get','--enforce-lockfile'], gui)
        add('gui-analyze', [flutter,'analyze','--no-pub'], gui)
        tests = list(dict.fromkeys(args.gui_test or []))
        for test in tests:
            p = Path(test)
            if p.is_absolute() or '..' in p.parts or not test.startswith('test/') or not test.endswith('.dart'):
                raise ValueError('GUI tests must be relative test/*.dart paths')
        add('gui-tests', [flutter,'test','--no-pub',*tests], gui)
        if args.preview_build:
            add('preview-web', [flutter,'build','web','--no-pub','--no-web-resources-cdn','--target','lib/dev/interactive_demo_main.dart'], gui)
        if args.linux_build:
            # Existing builder owns sibling binaries and platform build rules.
            # Its dependency validation remains intact; do not rerun GUI tests.
            add('linux-bundle', [sys.executable, root/'tools/developer/dev.py','linux','--go',args.go,'--flutter',flutter], root)
    return runner.validate_request({'schema':1,'stages':stages})


@contextlib.contextmanager
def worktree_lock(root):
    # One owner per worktree: Flutter tools touch the same generated state.
    lock = root/'.local'/'verification.lock'
    lock.parent.mkdir(parents=True, exist_ok=True)
    with lock.open('a+b') as handle:
        if os.name == 'nt':
            import msvcrt
            handle.seek(0);handle.write(b'0');handle.flush();handle.seek(0)
            try:
                msvcrt.locking(handle.fileno(),msvcrt.LK_NBLCK,1)
            except OSError:
                raise ValueError('another check owns this worktree') from None
        else:
            import fcntl
            try:
                fcntl.flock(handle,fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError:
                raise ValueError('another check owns this worktree') from None
        yield


def run(root, args, identity):
    request = plan(root, args)
    request['source'] = identity()
    if args.plan:
        print(json.dumps(request, ensure_ascii=False, indent=2))
        return 0
    directory = Path(args.reports).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    with worktree_lock(root):
        output = Path(tempfile.mkdtemp(prefix=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ-check-'), dir=directory))
        runner.write_json(output/'request.json',request)
        try:
            request['environment'] = {}
            runner.write_json(output/'request.json',request)
            sdk_index = next((i for i,s in enumerate(request['stages']) if s['name']=='gui-sdk'),None)
            if sdk_index is None:
                report = runner.run_request(request,output)
            else:
                prefix = dict(request,stages=request['stages'][:sdk_index+1])
                report = runner.run_request(prefix,output)
                remaining = request['stages'][sdk_index+1:]
                if report['status']=='passed':
                    sdk = runner.read_json(output/'gui-sdk.stdout.log')
                    path = str(Path(sdk['flutterRoot'])/'bin')+os.pathsep+os.environ.get('PATH','')
                    for stage in remaining:
                        stage.setdefault('environment',{})['PATH'] = path
                runner.write_json(output/'request.json',request)
                if remaining:
                    report = runner.run_request(dict(request,stages=remaining),output,initial_report=report)
        except Exception as error:
            report = runner.read_json(output/'report.json') if (output/'report.json').exists() else {'schema':1,'kind':'verification','source':request['source'],'stages':[]}
            report.update(status='failed',error=type(error).__name__+': '+str(error))
        report['kind'] = 'verification'
        try:
            report['source_after'] = identity()
            report['source_unchanged'] = report['source'] == report['source_after']
        except Exception as error:
            report.update(source_unchanged=False, source_error=str(error))
        if not report['source_unchanged']:
            report.update(status='failed',error='source changed during checks')
        runner.write_json(output/'report.json',report)
    runner.present(report,output/'report.json',args.format)
    return 0 if report['status']=='passed' else 1


def resolve_flutter_sdk(flutter, version_file, *, cwd, env):
    executable = Path(flutter).resolve()
    sdk = json.loads(subprocess.check_output([str(executable), '--version', '--machine'],
                                              cwd=cwd, env=env, text=True))
    if sdk['frameworkVersion'] != version_file.read_text().strip():
        raise ValueError('Flutter SDK does not match .flutter-version')
    return executable, sdk


def validate_sdk(flutter, version_file):
    # This validator runs INSIDE the normal stage group/Job. The outer stage
    # deadline owns the complete SDK process tree and its result/log files.
    result = subprocess.run([flutter, '--version', '--machine'], capture_output=True)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    if result.returncode:
        return result.returncode
    sdk = json.loads(result.stdout.decode('utf-8'))
    if sdk['frameworkVersion'] != Path(version_file).read_text().strip():
        print('Flutter SDK does not match .flutter-version', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    if len(sys.argv) != 4 or sys.argv[1] != '--sdk':
        raise SystemExit('internal SDK validator: --sdk FLUTTER VERSION_FILE')
    raise SystemExit(validate_sdk(sys.argv[2], sys.argv[3]))
