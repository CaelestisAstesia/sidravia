from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from distribution_fixture import DistributionFixture, SHA, VERSION, elf, pe

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('verify_distribution', ROOT / 'tools/developer/verify_distribution.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class DistributionContract(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.fixture = DistributionFixture(Path(temp.name))

    def test_source_and_archive_agreement_does_not_admit_build_junk(self):
        for name in ('debug.pdb', 'data/build.log', 'data/flutter_assets/AssetManifest.bin.json', 'data/flutter_assets/NOTICES', 'data/flutter_assets/assets/preview-font.ttf', 'dartjni.dll', 'unexpected.txt'):
            with self.subTest(name=name):
                path = self.fixture.gui / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'unused')
                self.fixture.clear_output()
                self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64'))
                with self.assertRaises(module.VerificationError):
                    self.verify('windows-amd64')
                path.unlink()

    def test_gui_requires_application_snapshot_not_just_engine(self):
        (self.fixture.gui / 'data/app.so').unlink()
        self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64'))
        with self.assertRaisesRegex(module.VerificationError, 'gui_source_incomplete'):
            self.verify('windows-amd64')

    def test_rejects_missing_or_changed_third_party_notices(self):
        target = 'linux-amd64'
        name = 'THIRD-PARTY-NOTICES.txt'
        for replacement, reason in ((None, 'archive_file_set_mismatch'),
                                    (b'incomplete notices', 'archive_source_mismatch')):
            with self.subTest(replacement=replacement):
                self.fixture.clear_output()
                payload = self.fixture.payload(target, gui=False)
                if replacement is None:
                    del payload[name]
                else:
                    payload[name] = replacement
                self.fixture.package(target, payload, gui=False)
                with self.assertRaisesRegex(module.VerificationError, reason):
                    self.verify(target, gui=False)

    def test_gui_rejects_missing_declared_font(self):
        (self.fixture.gui / 'data/flutter_assets/FontManifest.json').write_text('[{"fonts":[{"asset":"assets/fonts/missing.otf"}]}]')
        self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64'))
        with self.assertRaisesRegex(module.VerificationError, 'gui_declared_font_missing'):
            self.verify('windows-amd64')

    def verify(self, target='linux-amd64', smoke=False, gui=True):
        return module.verify(self.fixture.out, self.fixture.repo, target, VERSION, SHA, self.fixture.gui if target.startswith('windows-') and gui else None, smoke=smoke)

    def test_xz_consumes_the_same_exact_payload_and_rejects_ambiguous_formats(self):
        import gzip, lzma
        self.fixture.package('linux-amd64', self.fixture.payload('linux-amd64', gui=False), gui=False)
        original = next(self.fixture.out.glob('*.tar.gz'))
        encoded = original.with_name(original.name[:-3] + '.xz')
        encoded.write_bytes(lzma.compress(gzip.decompress(original.read_bytes()), preset=6))
        with mock.patch.object(module, 'binary_identity'), self.assertRaisesRegex(ValueError, 'ambiguous_archive_encoding'):
            self.verify('linux-amd64', gui=False)
        original.unlink()
        (self.fixture.out / 'SHA256SUMS.txt').write_text(module.digest(encoded.read_bytes()) + '  ' + encoded.name + '\n')
        with mock.patch.object(module, 'binary_identity'):
            result = self.verify('linux-amd64', gui=False)
        self.assertEqual(result['productOwnership'], 'passed')

    def test_exact_windows_and_linux_contract(self):
        for target in ('windows-amd64', 'linux-amd64', 'windows-arm64', 'linux-arm64'):
            with self.subTest(target=target):
                self.fixture.clear_output()
                self.fixture.package(target, self.fixture.payload(target, gui=False), gui=False)
                with mock.patch.object(module, 'binary_identity') as identity:
                    result = self.verify(target, gui=False)
                self.assertEqual(identity.call_count, 3 if target.startswith('windows') else 2)
                self.assertEqual(result['helpSmoke'], 'not_run')
                self.assertEqual(result['installation'], 'not_run')

    def test_windows_core_has_exact_payload_and_help_only_smoke(self):
        self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64', gui=False), gui=False)
        with mock.patch.object(module, 'binary_identity'), mock.patch.object(module.platform, 'system', return_value='Windows'), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=b'help')) as run:
            result = self.verify('windows-amd64', smoke=True, gui=False)
        self.assertEqual(result['helpSmoke'], 'passed')
        self.assertEqual([Path(call.args[0][0]).name for call in run.call_args_list], ['sidraviactl.exe', 'sidraviamgr.exe'])
        self.assertTrue(all(call.args[0][1:] == ['--help'] for call in run.call_args_list))
        for call in run.call_args_list:
            cwd, env = call.kwargs['cwd'], call.kwargs['env']
            self.assertEqual(Path(env['LOCALAPPDATA']), cwd.parent / 'local')
            self.assertEqual(Path(env['APPDATA']), cwd.parent / 'roaming')

    def test_windows_core_rejects_gui_missing_manager_and_installer(self):
        for change in ('gui', 'missing-manager', 'installer'):
            with self.subTest(change=change):
                self.fixture.clear_output()
                contents = self.fixture.payload('windows-amd64', gui=False)
                if change == 'gui': contents['Sidravia.exe'] = b'unexpected GUI'
                if change == 'missing-manager': del contents['sidraviamgr.exe']
                self.fixture.package('windows-amd64', contents, gui=(change == 'installer'))
                with self.assertRaises(module.VerificationError):
                    self.verify('windows-amd64', gui=False)

    def test_archive_missing_extra_alias_traversal_and_wrong_source(self):
        for change in ('missing', 'extra', 'alias', 'traversal', 'metadata', 'profile'):
            with self.subTest(change=change):
                payload = self.fixture.payload('linux-amd64')
                if change == 'missing': del payload['sidraviad']
                elif change == 'extra': payload['config/configurations.json'] = b'not allowed'
                elif change == 'alias': payload['SIDRAVIACTL'] = b'alias'
                elif change == 'traversal': payload['../escape'] = b'escape'
                elif change == 'metadata': payload['BUILD-INFO.txt'] = b'wrong build'
                elif change == 'profile': payload['institution-profiles/jlu.json'] = b'changed profile'
                self.fixture.package('linux-amd64', payload)
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaises(module.VerificationError): self.verify()

    def test_stale_gui_bytes_rejected(self):
        self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64'))
        (self.fixture.gui / 'Sidravia.exe').write_bytes(pe('amd64', b'different candidate'))
        with self.assertRaisesRegex(module.VerificationError, 'archive_source_mismatch'): self.verify('windows-amd64')

    def test_replaced_binary_rejected_by_producer_manifest(self):
        payload = self.fixture.payload('linux-amd64')
        payload['sidraviactl'] = b'stale binary from another candidate'
        self.fixture.package('linux-amd64', payload)
        with self.assertRaisesRegex(module.VerificationError, 'BUILD-INFO'): self.verify()

    def test_trimpath_metadata_does_not_claim_embedded_identity(self):
        info = {'Path': 'sidravia/cmd/sidraviactl', 'Settings': [{'Key': k, 'Value': v} for k, v in {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}.items()]}
        with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
            self.assertFalse(module.binary_identity(Path('inert'), 'sidraviactl', 'linux-amd64', VERSION, SHA, 'go'))

    def test_checksum_duplicate_missing_malformed_and_tampered(self):
        for change in ('duplicate', 'missing', 'malformed', 'tampered'):
            with self.subTest(change=change):
                self.fixture.package('linux-amd64', self.fixture.payload('linux-amd64'))
                sums = self.fixture.out / 'SHA256SUMS.txt'
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

    def test_linux_gui_launcher_package_is_distinct_from_artifact_name(self):
        info = {
            'Path': 'sidravia/cmd/sidravia-gui-launcher',
            'Settings': [
                {'Key': 'GOOS', 'Value': 'linux'},
                {'Key': 'GOARCH', 'Value': 'amd64'},
                {'Key': 'CGO_ENABLED', 'Value': '0'},
                {'Key': '-ldflags', 'Value': f'-X main.ProductVersion={VERSION} -X main.BuildID={SHA}'},
            ],
        }
        artifact = Path('sidravia_gui')
        with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
            self.assertTrue(module.binary_identity(artifact, artifact.name, 'linux-amd64', VERSION, SHA, 'go'))
        info['Path'] = 'sidravia/cmd/sidraviactl'
        with mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=json.dumps(info))):
            with self.assertRaisesRegex(module.VerificationError, 'binary_package_mismatch'):
                module.binary_identity(artifact, artifact.name, 'linux-amd64', VERSION, SHA, 'go')

    def test_smoke_executes_help_only_and_never_daemon(self):
        self.fixture.package('windows-amd64', self.fixture.payload('windows-amd64'))
        with mock.patch.object(module, 'binary_identity'), mock.patch.object(module.platform, 'system', return_value='Windows'), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run', return_value=mock.Mock(stdout=b'help')) as run:
            self.assertEqual(self.verify('windows-amd64', True)['helpSmoke'], 'passed')
        self.assertEqual([Path(call.args[0][0]).name for call in run.call_args_list], ['sidraviactl.exe', 'sidraviamgr.exe'])
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

    def test_windows_arm_gui_requires_arm_runner_engine_plugins_and_snapshot(self):
        names = ('Sidravia.exe', 'flutter_windows.dll', 'msvcp140.dll', 'vcruntime140.dll', 'vcruntime140_1.dll', 'valid_plugin.dll', 'data/app.so')
        for name in names:
            (self.fixture.gui / name).write_bytes((elf if name.endswith('.so') else pe)('arm64'))
        target = 'windows-arm64'
        self.fixture.package(target, self.fixture.payload(target))
        with mock.patch.object(module, 'binary_identity'):
            self.assertEqual(self.verify(target)['installerArchitecture'], 'amd64')
        for name in names:
            with self.subTest(name=name):
                self.fixture.clear_output()
                image = elf if name.endswith('.so') else pe
                (self.fixture.gui / name).write_bytes(image('amd64'))
                self.fixture.package(target, self.fixture.payload(target))
                with self.assertRaisesRegex(module.VerificationError, 'gui_(pe|elf)_target_mismatch'):
                    self.verify(target)
                (self.fixture.gui / name).write_bytes(image('arm64'))

    def test_arm_smoke_rejects_amd64_host_before_execution(self):
        for target in ('windows-arm64', 'linux-arm64'):
            with self.subTest(target=target):
                self.fixture.clear_output()
                self.fixture.package(target, self.fixture.payload(target, gui=False), gui=False)
                host = 'Windows' if target.startswith('windows-') else 'Linux'
                with mock.patch.object(module.platform, 'system', return_value=host), mock.patch.object(module.platform, 'machine', return_value='AMD64'), mock.patch.object(module.subprocess, 'run') as run:
                    with self.assertRaisesRegex(module.VerificationError, 'smoke_requires_matching_native_host'):
                        self.verify(target, smoke=True, gui=False)
                run.assert_not_called()

    def test_linux_gui_exact_release_and_stale_bytes(self):
        for p in list(self.fixture.gui.rglob('*')):
            if p.is_file(): p.unlink()
        names = ('sidravia_gui', 'lib/libflutter_linux_gtk.so', 'lib/libapp.so', 'data/icudtl.dat', 'data/flutter_assets/NOTICES.Z', 'data/icons/sidravia.png', 'data/flutter_assets/version.json', 'FLUTTER-LICENSE')
        payload = self.fixture.payload('linux-amd64')
        for name in names:
            path = self.fixture.gui / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(json.dumps({'version': VERSION, 'app_name': 'sidravia_gui'}).encode() if name.endswith('version.json') else name.encode())
            if name == 'sidravia_gui' or name.endswith('.so'):
                path.write_bytes(b'\x7fELF\x02\x01' + b'\0' * 12 + b'\x3e\0')
            payload['gui-runtime/' + name] = path.read_bytes()
        payload['sidravia_gui'] = elf('amd64', b'static launcher')
        payload['BUILD-INFO.txt'] += f'BinarySHA256: {module.digest(payload["sidravia_gui"])}  sidravia_gui\n'.encode()
        payload['sidravia-files.json'] = self.fixture.ownership('linux-amd64', payload, 'gui')
        self.fixture.package('linux-amd64', payload)
        with mock.patch.object(module, 'binary_identity'):
            result = module.verify(self.fixture.out,self.fixture.repo,'linux-amd64',VERSION,SHA,self.fixture.gui)
        self.assertEqual(result['sourceContents'], 'passed')
        (self.fixture.gui / 'lib/libapp.so').write_bytes(payload['gui-runtime/lib/libapp.so'] + b'stale')
        with self.assertRaisesRegex(module.VerificationError, 'archive_source_mismatch'):
            module.verify(self.fixture.out,self.fixture.repo,'linux-amd64',VERSION,SHA,self.fixture.gui)

    def test_product_ownership_rejects_tampering_and_ambiguous_fields(self):
        for change in ('build', 'missing', 'user-data', 'hash', 'mode', 'boolean', 'duplicate'):
            with self.subTest(change=change):
                self.fixture.clear_output()
                payload = self.fixture.payload('linux-amd64', gui=False)
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
                self.fixture.package('linux-amd64', payload, gui=False)
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaises(module.VerificationError): self.verify('linux-amd64', gui=False)

    def test_matching_source_archive_and_index_cannot_own_user_data(self):
        for name in ('config/private.json', 'logs/history', 'CON .txt', 'CONOUT$.txt'):
            with self.subTest(name=name):
                self.fixture.clear_output()
                injected = self.fixture.gui / name
                injected.parent.mkdir(parents=True, exist_ok=True)
                injected.write_bytes(b'not product data')
                payload = self.fixture.payload('windows-amd64')
                self.fixture.package('windows-amd64', payload)
                with self.assertRaisesRegex(module.VerificationError,'ownership_(protected|unsafe)_path'):
                    module.validate_product_ownership(json.loads(payload['sidravia-files.json']))
                with mock.patch.object(module, 'binary_identity'):
                    with self.assertRaisesRegex(module.VerificationError, 'ownership_(protected|unsafe)_path|unexpected_gui_runtime_path'):
                        self.verify('windows-amd64')
                injected.unlink()

    def test_ownership_limits_and_parent_aliases(self):
        item = {'path': 'lib', 'size': 1, 'sha256': 'a' * 64, 'mode': 0o644}
        for files in ([item] * 20001, [dict(item, size=3 << 30)],
                      [item, dict(item, path='LIB/a')],
                      [dict(item, path=str(i), size=2 << 30) for i in range(3)]):
            with self.assertRaises(module.VerificationError):
                module.validate_product_ownership({'schemaVersion': 1, 'files': files})
