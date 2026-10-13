from __future__ import annotations

import importlib.util
import io
import json
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('verify_distribution', ROOT / 'tools/developer/verify_distribution.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
VERSION = '0.3.0-alpha.1'
SHA = 'a' * 40


class DistributionContract(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / 'repo'
        self.out = self.root / 'out'
        self.gui = self.root / 'gui'
        for path in (self.repo, self.out, self.gui):
            path.mkdir()
        for name in ('LICENSE', 'internal/daemon/configuration/profiles/jlu.json', 'gui/windows/runner/resources/app_icon.ico'):
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(name.encode())
        for name in ('Sidravia.exe', 'flutter_windows.dll', 'data/icudtl.dat'):
            path = self.gui / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(name.encode())

    def payload(self, target):
        contents = {'LICENSE': b'LICENSE', 'sidravia.portable': b'',
                    'institution-profiles/jlu.json': b'internal/daemon/configuration/profiles/jlu.json',
                    'BUILD-INFO.txt': f'ProductVersion: {VERSION}\nBuildID: {SHA}\nTarget: {target}\nArtifactKind: distribution\n'.encode(),
                    'sidraviactl': b'ctl', 'sidraviad': b'daemon'}
        if target == 'windows-amd64':
            contents['sidraviactl.exe'] = contents.pop('sidraviactl')
            contents['sidraviad.exe'] = contents.pop('sidraviad')
            contents['sidraviamgr.exe'] = b'manager'
            contents['app_icon.ico'] = b'gui/windows/runner/resources/app_icon.ico'
            contents.update({p.relative_to(self.gui).as_posix(): p.read_bytes() for p in self.gui.rglob('*') if p.is_file()})
        return contents

    def package(self, target, contents):
        base = f'sidravia-v{VERSION}-{target}'
        archive = self.out / (base + ('-portable.zip' if target == 'windows-amd64' else '-portable.tar.gz'))
        if target == 'windows-amd64':
            with zipfile.ZipFile(archive, 'w') as z:
                for name, data in contents.items():
                    z.writestr(name, data)
            (self.out / (base + '-setup.exe')).write_bytes(b'MZinert installer')
        else:
            with tarfile.open(archive, 'w:gz') as tar:
                for name, data in contents.items():
                    entry = tarfile.TarInfo(base + '/' + name)
                    entry.size = len(data)
                    tar.addfile(entry, io.BytesIO(data))
        sums = '\n'.join(module.digest(p.read_bytes()) + '  ' + p.name for p in sorted(self.out.iterdir()) if p.name != 'SHA256SUMS.txt')
        (self.out / 'SHA256SUMS.txt').write_text(sums + '\n')

    def verify(self, target='linux-amd64', smoke=False):
        return module.verify(self.out, self.repo, target, VERSION, SHA, self.gui if target == 'windows-amd64' else None, smoke=smoke)

    def test_exact_windows_and_linux_contract(self):
        for target in ('windows-amd64', 'linux-amd64'):
            with self.subTest(target=target):
                for p in self.out.iterdir():
                    p.unlink()
                self.package(target, self.payload(target))
                with mock.patch.object(module, 'binary_identity') as identity:
                    result = self.verify(target)
                self.assertEqual(identity.call_count, 3 if target.startswith('windows') else 2)
                self.assertEqual(result['helpSmoke'], 'not_run')
                self.assertEqual(result['installation'], 'not_run')

    def test_archive_missing_extra_alias_traversal_and_wrong_source(self):
        for change in ('missing', 'extra', 'alias', 'traversal', 'metadata', 'profile'):
            with self.subTest(change=change):
                payload = self.payload('linux-amd64')
                if change == 'missing': del payload['sidraviad']
                elif change == 'extra': payload['config/configurations.json'] = b'not allowed'
                elif change == 'alias': payload['SIDRAVIACTL'] = b'alias'
                elif change == 'traversal': payload['../escape'] = b'escape'
                elif change == 'metadata': payload['BUILD-INFO.txt'] = b'wrong build'
                elif change == 'profile': payload['institution-profiles/jlu.json'] = b'changed profile'
                self.package('linux-amd64', payload)
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaises(module.VerificationError): self.verify()

    def test_stale_gui_bytes_rejected(self):
        self.package('windows-amd64', self.payload('windows-amd64'))
        (self.gui / 'Sidravia.exe').write_bytes(b'different candidate')
        with self.assertRaisesRegex(module.VerificationError, 'archive_source_mismatch'): self.verify('windows-amd64')

    def test_checksum_duplicate_missing_malformed_and_tampered(self):
        for change in ('duplicate', 'missing', 'malformed', 'tampered'):
            with self.subTest(change=change):
                self.package('linux-amd64', self.payload('linux-amd64'))
                sums = self.out / 'SHA256SUMS.txt'
                if change == 'duplicate': sums.write_text(sums.read_text() * 2)
                elif change == 'missing': sums.write_text('')
                elif change == 'malformed': sums.write_text('malformed\n')
                else: sums.write_text('0' * 64 + sums.read_text()[64:])
                with self.assertRaises(module.VerificationError): self.verify()

    def test_wrong_embedded_identity_and_target(self):
        for key, value in (('GOOS', 'windows'), ('GOARCH', 'arm64'), ('CGO_ENABLED', '1'), ('-ldflags', '-X main.ProductVersion=other -X main.BuildID=' + SHA)):
            settings = {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', '-ldflags': '-X main.ProductVersion=' + VERSION + ' -X main.BuildID=' + SHA}
            settings[key] = value
            info = {'Path': 'sidravia/cmd/sidravia', 'Settings': [{'Key': k, 'Value': v} for k, v in settings.items()]}
            with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
                with self.assertRaises(module.VerificationError): module.binary_identity(Path('inert'), 'sidraviactl', 'linux-amd64', VERSION, SHA, 'go')

    def test_smoke_executes_help_only_and_never_daemon(self):
        self.package('windows-amd64', self.payload('windows-amd64'))
        with mock.patch.object(module, 'binary_identity'), mock.patch.object(module.platform, 'system', return_value='Windows'), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=b'help')) as run:
            self.assertEqual(self.verify('windows-amd64', True)['helpSmoke'], 'passed')
        self.assertEqual([Path(call.args[0][0]).name for call in run.call_args_list], ['sidraviactl.exe', 'sidraviamgr.exe'])
        self.assertTrue(all(call.args[0][1:] == ['--help'] for call in run.call_args_list))
