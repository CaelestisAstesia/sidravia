"""Opt-in native Inno/Go handoff fixture; no installed product or real data.

Uses the existing pinned compiler/scratch environment and dev.py python scope.
The native fixture EXEs share internal/update, but are not production CTL/GUI.
"""
import contextlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest
import uuid
import zipfile

REPO = Path(__file__).resolve().parents[3]


def windows_path(path):
    if os.name == 'nt':
        return str(path)
    return subprocess.check_output(['wslpath', '-w', str(path)], text=True).strip()


@contextlib.contextmanager
def fixture_directory():
    root = Path(tempfile.mkdtemp(prefix='update-handoff-', dir=os.environ['SIDRAVIA_INNO_TEST_ROOT']))
    try:
        yield root
    except BaseException:
        # Do not remove a fixture whose detached process outcome is uncertain.
        # Its owner marker and logs allow exact read-only diagnosis/recovery.
        print('failed update fixture preserved:', root)
        raise
    else:
        shutil.rmtree(root)


@unittest.skipUnless(os.environ.get('SIDRAVIA_ISCC') and os.environ.get('SIDRAVIA_INNO_TEST_ROOT'),
                     'native Inno compiler/scratch root not configured')
class UpdateHandoff(unittest.TestCase):
    def test_native_handoff_short_host_and_recovery(self):
        with fixture_directory() as root:
            host = root / 'package-host.exe'
            env = dict(os.environ, GOOS='windows', GOARCH='amd64', CGO_ENABLED='0')
            built = subprocess.run(['go', 'build', '-mod=readonly', '-o', str(host), './internal/update/setup/testdata/update_host'],
                                   cwd=REPO, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
            self.assertEqual(built.returncode, 0, built.stdout.decode(errors='replace'))
            for package in ('./internal/update', './internal/process/winproc', './internal/update/setup/internal/innopeer', './internal/release', './internal/installation/resume', './internal/installation/startgate'):
                test_exe = root / (package.rsplit('/', 1)[-1] + '.test.exe')
                compiled_tests = subprocess.run(['go', 'test', '-c', '-o', str(test_exe), package], cwd=REPO, env=env,
                                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
                self.assertEqual(compiled_tests.returncode, 0, compiled_tests.stdout.decode(errors='replace'))
                native_tests = subprocess.run([str(test_exe), '-test.v'], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
                self.assertEqual(native_tests.returncode, 0, native_tests.stdout.decode(errors='replace'))
                print(native_tests.stdout.decode(errors='replace'))
            # Exercise the real CTL -> sibling updater process boundary without
            # network, cache writes, application startup or installation.
            for filename, package in (('sidraviactl.exe', './cmd/sidraviactl'), ('sidraviaupdater.exe', './cmd/sidraviaupdater'), ('sidraviad.exe', './cmd/sidraviad')):
                built = subprocess.run(['go', 'build', '-mod=readonly', '-ldflags',
                                        '-X main.ProductVersion=0.4.0-alpha.1 -X main.BuildID=fixture',
                                        '-o', str(root / filename), package], cwd=REPO, env=env,
                                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
                self.assertEqual(built.returncode, 0, built.stdout.decode(errors='replace'))
            # Desktop builds pin publisher trust; apply must stop at explicit
            # confirmation before touching layout, cache or network.
            for operation, code, expected_exit in (('version', 'ok', 0), ('apply', 'confirmation_required', 1)):
                invoked = subprocess.run([str(root / 'sidraviactl.exe'), 'update', operation],
                                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
                self.assertEqual(invoked.returncode, expected_exit, invoked.stderr.decode(errors='replace'))
                result = json.loads(invoked.stdout)
                self.assertEqual(result['schemaVersion'], 1)
                self.assertEqual(result['code'], code)
                if operation == 'version':
                    self.assertEqual(result['result'], dict(productVersion='0.4.0-alpha.1', buildId='fixture'))
                print('update-native-host:', operation, 'PASS')
            real = root / 'real-lifecycle'
            (real / 'target').mkdir(parents=True)
            operation = uuid.uuid4().hex
            (real / 'fixture-owner').write_text(operation)
            (real / 'target/sidravia.portable').write_bytes(b'')
            for name in ('sidraviactl.exe', 'sidraviad.exe', 'sidraviaupdater.exe'):
                shutil.copyfile(root / name, real / 'target' / name)
            checked = subprocess.run([str(host), 'real-lifecycle', windows_path(real), operation],
                                     stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
            self.assertEqual(checked.returncode, 0, checked.stdout.decode(errors='replace') +
                             (real / 'daemon.log').read_text(errors='replace')[-2000:] if (real / 'daemon.log').exists() else checked.stdout.decode(errors='replace'))
            self.assertEqual(json.loads((real / 'real-lifecycle.json').read_text())['coldResumeConfirmed'], True)
            print('update-native-real-lifecycle: prepare/abort/commit/cold-resume PASS')
            gui_source = os.environ.get('SIDRAVIA_GUI_TEST_DIRECTORY')
            if gui_source:
                gui_case = root / 'real-gui-lifecycle'
                gui_target = gui_case / 'target'
                gui_target.mkdir(parents=True)
                gui_operation = uuid.uuid4().hex
                (gui_case / 'fixture-owner').write_text(gui_operation)
                (gui_target / 'sidravia.portable').write_bytes(b'')
                for name in ('sidraviactl.exe', 'sidraviad.exe', 'sidraviaupdater.exe'):
                    shutil.copyfile(root / name, gui_target / name)
                source = Path(gui_source)
                self.assertTrue((source / 'Sidravia.exe').is_file())
                for item in source.iterdir():
                    if item.name == 'data' and item.is_dir():
                        shutil.copytree(item, gui_target / item.name)
                    elif item.name == 'Sidravia.exe' or item.suffix.lower() == '.dll':
                        shutil.copyfile(item, gui_target / item.name)
                checked = subprocess.run([str(host), 'real-lifecycle', windows_path(gui_case), gui_operation, 'gui'],
                                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=100)
                self.assertEqual(checked.returncode, 0, checked.stdout.decode(errors='replace') +
                                 (gui_case / 'daemon.log').read_text(errors='replace')[-3000:] if (gui_case / 'daemon.log').exists() else checked.stdout.decode(errors='replace'))
                self.assertEqual(json.loads((gui_case / 'real-lifecycle.json').read_text())['desktop'], True)
                print('update-native-real-GUI-lifecycle: admission/prepare/abort/exit/cold-resume PASS')
            archive = os.environ.get('SIDRAVIA_CANDIDATE_ARCHIVE')
            if archive:
                candidate = root / 'exact-candidate'
                candidate_target = candidate / 'target'
                candidate_target.mkdir(parents=True)
                operation = uuid.uuid4().hex
                (candidate / 'fixture-owner').write_text(operation)
                with zipfile.ZipFile(archive) as package:
                    for item in package.infolist():
                        parts = item.filename.replace('\\', '/').split('/')
                        self.assertFalse(item.filename.startswith(('/', '\\')) or '..' in parts or ':' in item.filename)
                        self.assertNotEqual((item.external_attr >> 16) & 0o170000, 0o120000)
                    package.extractall(candidate_target)
                self.assertTrue((candidate_target / 'sidravia.portable').is_file())
                gui = (candidate_target / 'Sidravia.exe').is_file()
                arguments = [str(host), 'real-lifecycle', windows_path(candidate), operation]
                if gui:
                    arguments.append('gui')
                checked = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=100)
                self.assertEqual(checked.returncode, 0, checked.stdout.decode(errors='replace') +
                                 (candidate / 'daemon.log').read_text(errors='replace')[-3000:] if (candidate / 'daemon.log').exists() else checked.stdout.decode(errors='replace'))
                info = dict(line.split(': ', 1) for line in (candidate_target / 'BUILD-INFO.txt').read_text().splitlines() if ': ' in line)
                receipt = json.loads((candidate / 'real-lifecycle.json').read_text())
                self.assertEqual(receipt['productVersion'], info['ProductVersion'])
                self.assertEqual(receipt['buildId'], info['BuildID'])
                print('update-native-exact-candidate:', info['ProductVersion'], info['BuildID'], 'PASS')
            payload = root / 'new-product.bin'
            payload.write_bytes(b'new synthetic product; never execute this payload\n')
            compiler = os.environ['SIDRAVIA_ISCC']
            version = subprocess.run([compiler, '--version'], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
            self.assertIn(b'7.1.0', version.stdout, 'fixed Inno compiler required')
            compiled = subprocess.run([compiler, '/Q', '/DHost=' + windows_path(host), '/DPayload=' + windows_path(payload),
                                       '/O' + windows_path(root), windows_path(REPO / 'tools/developer/testdata/update_handoff.iss')],
                                      stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
            self.assertEqual(compiled.returncode, 0, compiled.stdout.decode(errors='replace'))
            installer = root / 'handoff-fixture.exe'
            cases = [
                ('visible 中文 space', dict(mode='desktop', visible=True, maintain=False)),
                ('tray', dict(mode='desktop', visible=False, maintain=True, configurationId='synthetic-saved')),
                ('headless-offline', dict(mode='headless', visible=False, maintain=False)),
                ('headless-maintain', dict(mode='headless', visible=False, maintain=True, configurationId='synthetic-saved')),
                ('not-running', dict(mode='none', visible=False, maintain=False)),
                ('stuck', dict(mode='headless', visible=False, maintain=False)),
                ('no-breakaway', dict(mode='none', visible=False, maintain=False)),
            ]
            for name, intent in cases:
                state = root / name
                target = state / 'target'
                target.mkdir(parents=True)
                op = uuid.uuid4().hex
                (state / 'fixture-owner').write_text(op)
                (state / 'intent.json').write_text(json.dumps(intent))
                (target / 'sidravia.portable').write_bytes(b'')
                (target / 'config').mkdir()
                sentinel = target / 'config/configurations.json'
                sentinel.write_bytes(b'{"fixture":"no credentials"}')
                updater = target / 'updater.exe'
                shutil.copyfile(host, updater)
                old = updater.read_bytes()
                # Launcher assigns updater to a kill-on-close job before it
                # runs. The accepted installer must break away and survive.
                launched = subprocess.run([str(host), 'job', windows_path(state), op, windows_path(installer), name],
                                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
                self.assertEqual(launched.returncode, 0, launched.stdout.decode(errors='replace'))
                if name == 'no-breakaway':
                    self.assertFalse((state / 'handoff.json').exists())
                    self.assertEqual(json.loads((state / 'launch-rejected.json').read_text())['reason'], 'job_breakaway_denied')
                    self.assertEqual(updater.read_bytes(), old)
                elif name == 'stuck':
                    self.assertTrue((state / 'handoff.json').exists())
                    self.assertEqual(json.loads((state / 'installer-exit.json').read_text())['exitCode'], 7)
                    self.assertFalse((state / 'complete.txt').exists())
                    self.assertEqual(updater.read_bytes(), old)
                else:
                    deadline = time.monotonic() + 20
                    while not (state / 'complete.txt').exists() and time.monotonic() < deadline:
                        time.sleep(.05)
                    self.assertTrue((state / 'complete.txt').exists(), (state / 'setup.log').read_text(errors='replace')[-3000:] if (state / 'setup.log').exists() else name)
                    self.assertEqual(json.loads((state / 'installer-exit.json').read_text())['exitCode'], 0)
                    self.assertEqual(updater.read_bytes(), payload.read_bytes())
                    plan = json.loads((state / 'restored.json').read_text())
                    self.assertEqual(plan['mode'], intent['mode'])
                    self.assertEqual(plan['visible'], intent['visible'])
                    self.assertEqual(plan['skipAutoLogin'], intent['mode'] != 'none')
                    self.assertEqual(plan.get('configurationId', ''), intent.get('configurationId', ''))
                    self.assertEqual((state / (op + '.claimed')).read_text(), 'claimed\n')
                self.assertEqual(sentinel.read_bytes(), b'{"fixture":"no credentials"}')
                print('update-native-case:', name, 'PASS')
