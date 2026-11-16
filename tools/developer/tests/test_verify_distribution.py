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

    def payload(self, target, gui=True):
        contents = {'LICENSE': b'LICENSE', 'sidravia.portable': b'',
                    'institution-profiles/jlu.json': b'internal/daemon/configuration/profiles/jlu.json',
                    'BUILD-INFO.txt': f'ProductVersion: {VERSION}\nBuildID: {SHA}\nTarget: {target}\nArtifactKind: distribution\n'.encode(),
                    'sidraviactl': b'ctl', 'sidraviad': b'daemon', 'sidraviaupdater': b'updater'}
        if target.startswith('windows-'):
            contents['sidraviactl.exe'] = contents.pop('sidraviactl')
            contents['sidraviad.exe'] = contents.pop('sidraviad')
            contents['sidraviaupdater.exe'] = contents.pop('sidraviaupdater')
            contents['sidraviamgr.exe'] = b'manager'
            contents['app_icon.ico'] = b'gui/windows/runner/resources/app_icon.ico'
            if gui and target == 'windows-amd64':
                contents.update({p.relative_to(self.gui).as_posix(): p.read_bytes() for p in self.gui.rglob('*') if p.is_file()})
        names = ['sidraviactl.exe', 'sidraviad.exe', 'sidraviaupdater.exe', 'sidraviamgr.exe'] if target.startswith('windows-') else ['sidraviactl', 'sidraviad', 'sidraviaupdater']
        contents['BUILD-INFO.txt'] += ''.join(f'BinarySHA256: {module.digest(contents[name])}  {name}\n' for name in names).encode()
        contents['sidravia-files.json'] = self.ownership(target, contents, 'gui' if gui and target == 'windows-amd64' else 'core')
        return contents

    def ownership(self, target, contents, kind):
        product = {name: data for name, data in contents.items() if name not in ('sidravia.portable', 'sidravia-files.json')}
        return json.dumps({'schemaVersion': 1, 'version': VERSION, 'buildId': SHA,
            'os': target.split('-')[0], 'arch': target.split('-')[1], 'payloadKind': kind,
            'files': [{'path': name, 'size': len(data), 'sha256': module.digest(data),
                       'mode': 0o755 if not target.startswith('windows') and name in ('sidraviactl', 'sidraviad', 'sidraviaupdater', 'sidravia_gui') else 0o644}
                      for name, data in sorted(product.items())]}).encode()

    def package(self, target, contents, gui=True):
        base = f'sidravia-v{VERSION}-{target}'
        kind = 'gui-portable' if 'Sidravia.exe' in contents or 'sidravia_gui' in contents else 'core-portable'
        archive = self.out / (base + '-' + kind + ('.zip' if target.startswith('windows-') else '.tar.gz'))
        if target.startswith('windows-'):
            with zipfile.ZipFile(archive, 'w') as z:
                for name, data in contents.items():
                    z.writestr(name, data)
            if gui and target == 'windows-amd64':
                (self.out / (base + '-setup.exe')).write_bytes(b'MZinert installer')
        else:
            with tarfile.open(archive, 'w:gz') as tar:
                for name, data in contents.items():
                    entry = tarfile.TarInfo(base + '/' + name)
                    entry.size = len(data)
                    entry.mode = 0o755 if name in ('sidravia_gui', 'sidraviactl', 'sidraviad', 'sidraviaupdater') else 0o644
                    tar.addfile(entry, io.BytesIO(data))
        sums = '\n'.join(module.digest(p.read_bytes()) + '  ' + p.name for p in sorted(self.out.iterdir()) if p.name != 'SHA256SUMS.txt')
        (self.out / 'SHA256SUMS.txt').write_text(sums + '\n')

    def verify(self, target='linux-amd64', smoke=False, gui=True):
        return module.verify(self.out, self.repo, target, VERSION, SHA, self.gui if target == 'windows-amd64' and gui else None, smoke=smoke)

    def test_exact_windows_and_linux_contract(self):
        for target in ('windows-amd64', 'linux-amd64', 'windows-arm64', 'linux-arm64'):
            with self.subTest(target=target):
                for p in self.out.iterdir():
                    p.unlink()
                self.package(target, self.payload(target))
                with mock.patch.object(module, 'binary_identity') as identity:
                    result = self.verify(target)
                self.assertEqual(identity.call_count, 4 if target.startswith('windows') else 3)
                self.assertEqual(result['helpSmoke'], 'not_run')
                self.assertEqual(result['installation'], 'not_run')

    def test_windows_core_has_exact_payload_and_help_only_smoke(self):
        self.package('windows-amd64', self.payload('windows-amd64', gui=False), gui=False)
        with mock.patch.object(module, 'binary_identity'), mock.patch.object(module.platform, 'system', return_value='Windows'), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=b'help')) as run:
            result = self.verify('windows-amd64', smoke=True, gui=False)
        self.assertEqual(result['helpSmoke'], 'passed')
        self.assertEqual([Path(call.args[0][0]).name for call in run.call_args_list], ['sidraviactl.exe', 'sidraviaupdater.exe', 'sidraviamgr.exe'])
        self.assertTrue(all(call.args[0][1:] == ['--help'] for call in run.call_args_list))

    def test_windows_core_rejects_gui_missing_manager_and_installer(self):
        for change in ('gui', 'missing-manager', 'installer'):
            with self.subTest(change=change):
                for p in self.out.iterdir():
                    p.unlink()
                contents = self.payload('windows-amd64', gui=False)
                if change == 'gui': contents['Sidravia.exe'] = b'unexpected GUI'
                if change == 'missing-manager': del contents['sidraviamgr.exe']
                self.package('windows-amd64', contents, gui=(change == 'installer'))
                with self.assertRaises(module.VerificationError):
                    self.verify('windows-amd64', gui=False)

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

    def test_replaced_binary_rejected_by_producer_manifest(self):
        payload = self.payload('linux-amd64')
        payload['sidraviactl'] = b'stale binary from another candidate'
        self.package('linux-amd64', payload)
        with self.assertRaisesRegex(module.VerificationError, 'BUILD-INFO'): self.verify()

    def test_trimpath_metadata_does_not_claim_embedded_identity(self):
        info = {'Path': 'sidravia/cmd/sidraviactl', 'Settings': [{'Key': k, 'Value': v} for k, v in {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}.items()]}
        with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
            self.assertFalse(module.binary_identity(Path('inert'), 'sidraviactl', 'linux-amd64', VERSION, SHA, 'go'))

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
            info = {'Path': 'sidravia/cmd/sidraviactl', 'Settings': [{'Key': k, 'Value': v} for k, v in settings.items()]}
            with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
                with self.assertRaises(module.VerificationError): module.binary_identity(Path('inert'), 'sidraviactl', 'linux-amd64', VERSION, SHA, 'go')

    def test_smoke_executes_help_only_and_never_daemon(self):
        self.package('windows-amd64', self.payload('windows-amd64'))
        with mock.patch.object(module, 'binary_identity'), mock.patch.object(module.platform, 'system', return_value='Windows'), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=b'help')) as run:
            self.assertEqual(self.verify('windows-amd64', True)['helpSmoke'], 'passed')
        self.assertEqual([Path(call.args[0][0]).name for call in run.call_args_list], ['sidraviactl.exe', 'sidraviaupdater.exe', 'sidraviamgr.exe'])
        self.assertTrue(all(call.args[0][1:] == ['--help'] for call in run.call_args_list))

    def test_arm_metadata_rejects_amd64_binary(self):
        for target in ('windows-arm64', 'linux-arm64'):
            for arch in ('amd64', 'arm64'):
                with self.subTest(target=target, arch=arch):
                    settings = {'GOOS': target.split('-')[0], 'GOARCH': arch, 'CGO_ENABLED': '0'}
                    info = {'Path': 'sidravia/cmd/sidraviactl', 'Settings': [{'Key': k, 'Value': v} for k, v in settings.items()]}
                    with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
                        if arch == 'amd64':
                            with self.assertRaisesRegex(module.VerificationError, 'binary_target_mismatch'):
                                module.binary_identity(Path('inert'), 'sidraviactl', target, VERSION, SHA, 'go')
                        else:
                            self.assertFalse(module.binary_identity(Path('inert'), 'sidraviactl', target, VERSION, SHA, 'go'))

    def test_windows_arm_gui_rejected(self):
        with self.assertRaisesRegex(module.VerificationError, 'gui_target_mismatch'):
            module.verify(self.out, self.repo, 'windows-arm64', VERSION, SHA, self.gui)

    def test_arm_smoke_rejects_amd64_host_before_execution(self):
        for target in ('windows-arm64', 'linux-arm64'):
            with self.subTest(target=target):
                for path in self.out.iterdir():
                    path.unlink()
                self.package(target, self.payload(target, gui=False), gui=False)
                host = 'Windows' if target.startswith('windows-') else 'Linux'
                with mock.patch.object(module.platform, 'system', return_value=host), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run') as run:
                    with self.assertRaisesRegex(module.VerificationError, 'smoke_requires_matching_native_host'):
                        self.verify(target, smoke=True, gui=False)
                run.assert_not_called()

    def test_linux_gui_exact_release_and_stale_bytes(self):
        for p in list(self.gui.rglob('*')):
            if p.is_file(): p.unlink()
        names = ('sidravia_gui', 'lib/libflutter_linux_gtk.so', 'lib/libapp.so', 'data/icudtl.dat', 'data/flutter_assets/NOTICES.Z', 'data/icons/sidravia.png', 'data/flutter_assets/version.json', 'FLUTTER-LICENSE')
        payload = self.payload('linux-amd64')
        for name in names:
            path = self.gui / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(json.dumps({'version': VERSION, 'app_name': 'sidravia_gui'}).encode() if name.endswith('version.json') else name.encode())
            if name == 'sidravia_gui' or name.endswith('.so'):
                path.write_bytes(b'\x7fELF\x02\x01' + b'\0' * 12 + b'\x3e\0')
            payload[name] = path.read_bytes()
        payload['sidravia-files.json'] = self.ownership('linux-amd64', payload, 'gui')
        self.package('linux-amd64', payload)
        with mock.patch.object(module, 'binary_identity'):
            result = module.verify(self.out,self.repo,'linux-amd64',VERSION,SHA,self.gui)
        self.assertEqual(result['sourceContents'], 'passed')
        (self.gui / 'lib/libapp.so').write_bytes(payload['lib/libapp.so'] + b'stale')
        with self.assertRaisesRegex(module.VerificationError, 'archive_source_mismatch'):
            module.verify(self.out,self.repo,'linux-amd64',VERSION,SHA,self.gui)

    def test_product_ownership_rejects_tampering_and_ambiguous_fields(self):
        for change in ('build', 'missing', 'user-data', 'hash', 'mode', 'boolean', 'duplicate'):
            with self.subTest(change=change):
                for p in self.out.iterdir(): p.unlink()
                payload = self.payload('linux-amd64', gui=False)
                document = json.loads(payload['sidravia-files.json'])
                if change == 'build': document['buildId'] = 'b' * 40
                if change == 'missing': document['files'].pop()
                if change == 'user-data': document['files'][0]['path'] = 'config/private.json'
                if change == 'hash': document['files'][0]['sha256'] = 'b' * 64
                if change == 'mode': document['files'][0]['mode'] = 0o777
                if change == 'boolean': document['schemaVersion'] = True
                encoded = json.dumps(document).encode()
                if change == 'duplicate': encoded = encoded.replace(b'"schemaVersion": 1', b'"schemaVersion": 1, "schemaVersion": 1')
                payload['sidravia-files.json'] = encoded
                self.package('linux-amd64', payload, gui=False)
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaises(module.VerificationError): self.verify('linux-amd64', gui=False)

    def test_matching_source_archive_and_index_cannot_own_user_data(self):
        for name in ('config/private.json', 'logs/history', 'CON .txt', 'CONOUT$.txt'):
            with self.subTest(name=name):
                for p in self.out.iterdir(): p.unlink()
                injected = self.gui / name
                injected.parent.mkdir(parents=True, exist_ok=True)
                injected.write_bytes(b'not product data')
                payload = self.payload('windows-amd64')
                self.package('windows-amd64', payload)
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaisesRegex(module.VerificationError, 'ownership_(protected|unsafe)_path'):
                        self.verify('windows-amd64')
                injected.unlink()

    def test_ownership_limits_and_parent_aliases(self):
        item = {'path': 'lib', 'size': 1, 'sha256': 'a' * 64, 'mode': 0o644}
        for files in ([item] * 20001, [dict(item, size=3 << 30)],
                      [item, dict(item, path='LIB/a')],
                      [dict(item, path=str(i), size=2 << 30) for i in range(3)]):
            with self.assertRaises(module.VerificationError):
                module.validate_product_ownership({'schemaVersion': 1, 'files': files})
