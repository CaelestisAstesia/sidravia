import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from release_artifacts import QT_MANIFEST, QT_SIGNATURE, request_document
from stage_release import stage


class StageReleaseTests(unittest.TestCase):
    version = '0.6.0-alpha.1'
    commit = 'a' * 40

    def setUp(self):
        sandbox = tempfile.TemporaryDirectory()
        self.addCleanup(sandbox.cleanup)
        self.root = Path(sandbox.name)
        self.expected_payloads = set()
        for arch in ('amd64', 'arm64'):
            windows = f'windows-{arch}'
            linux = f'linux-{arch}'
            for kind in ('gui', 'core'):
                self.distribution(windows, kind, 'zip')
                for encoding in ('gz', 'xz'):
                    self.distribution(linux, kind, encoding)
                for extension in ('rpm', 'deb'):
                    self.system_package(linux, arch, kind, extension)

    def write_package(self, path, *, public_name=None):
        self.expected_payloads.add(public_name or path.name)
        path.write_bytes(path.name.encode('ascii'))
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def distribution(self, target, kind, encoding):
        base = f'sidravia-v{self.version}-{target}'
        lane = self.root / 'inputs' / (base if encoding == 'zip' else base + '-portable')
        group = lane / (kind + '-xz' if encoding == 'xz' else kind)
        group.mkdir(parents=True)
        extension = 'zip' if encoding == 'zip' else 'tar.' + encoding
        names = [f'{base}-{kind}-portable.{extension}']
        if kind == 'gui':
            (lane / 'gui-input').mkdir(exist_ok=True)
            if encoding == 'zip':
                names.append(f'{base}-setup.exe')
        hashes = {name: self.write_package(group / name) for name in names}
        (group / 'SHA256SUMS.txt').write_text(
            ''.join(f'{digest}  {name}\n' for name, digest in hashes.items()))
        receipt = (kind + '-xz-verification.json' if encoding == 'xz' else
                   'core-verification.json' if kind == 'core' else 'verification.json')
        (lane / receipt).write_text(json.dumps(dict(
            version=self.version, buildId=self.commit, target=target, payloadKind=kind,
            artifactHashes=hashes, productOwnershipSha256='1' * 64)))

    def system_package(self, target, arch, kind, extension):
        base = f'sidravia-v{self.version}-{target}'
        group = self.root / 'inputs' / f'{base}-{extension}' / kind
        group.mkdir(parents=True)
        architecture = {'amd64': 'x86_64', 'arm64': 'aarch64'}[arch] if extension == 'rpm' else arch
        product = 'sidravia' if kind == 'gui' else 'sidravia-core'
        package_version = self.version.replace('-alpha.', '~alpha.')
        if extension == 'rpm':
            name = f'{product}-{package_version}-1.{architecture}.rpm'
            public_name = f'{product}-{self.version}-1.{architecture}.rpm'
        else:
            name = f'{product}_{package_version}-1_{architecture}.deb'
            public_name = f'{product}_{self.version}-1_{architecture}.deb'
        receipt = dict(version=self.version, source=dict(commit=self.commit, dirty=False),
                       source_unchanged=True, architecture=architecture, kind=kind, package=name,
                       sha256=self.write_package(group / name, public_name=public_name))
        (group / (name + '.json')).write_text(json.dumps(receipt))

    def test_assembles_exact_bridge_assets_and_valid_request_paths(self):
        self.assertEqual(stage(self.root, self.version, self.commit), 22)
        published = {p.name for p in (self.root / 'assets').iterdir()}
        self.assertEqual(published, self.expected_payloads | {'SHA256SUMS.txt'})
        listed = (self.root / 'assets/SHA256SUMS.txt').read_text().splitlines()
        self.assertEqual(len(listed), 22)
        self.assertEqual({line.split('  ')[1] for line in listed}, self.expected_payloads)
        for line in listed:
            digest, name = line.split('  ')
            self.assertEqual(hashlib.sha256((self.root / 'assets' / name).read_bytes()).hexdigest(), digest)
        for source in (self.root / 'inputs').glob('*/*/*~*'):
            if source.suffix in {'.rpm', '.deb'}:
                alias = self.root / 'assets' / source.name.replace('~', '-')
                self.assertEqual(alias.read_bytes(), source.read_bytes())
        for manifest, asset_count in (('sidravia-release', 7), ('sidravia-release-extended', 4),
                                      ('sidravia-release-xz', 3)):
            path = self.root / 'requests' / f'{manifest}.request.json'
            request = request_document(path)
            groups = request['distributions']
            self.assertEqual(len(request['expectedAssets']), asset_count)
            for group in groups:
                for key in ('directory', 'guiDirectory'):
                    if key in group:
                        self.assertTrue((path.parent / group[key]).is_dir())
            if manifest.endswith('extended'):
                assets = set(request['expectedAssets'])
                prefix = f'sidravia-v{self.version}-linux-arm64-gui-portable.tar.'
                self.assertTrue({prefix + 'xz', prefix + 'gz'} <= assets)

    def qt_lane(self):
        lane = self.root / 'inputs/qt-release'
        assets = lane / 'assets'
        assets.mkdir(parents=True)
        manifest = dict(version=self.version, buildId=self.commit, assets=[])
        for arch in ('amd64', 'arm64'):
            base = f'sidravia-v{self.version}-windows-{arch}'
            distribution = self.root / 'inputs' / base
            legacy_setup = f'{base}-setup.exe'
            (distribution / 'gui' / legacy_setup).unlink()
            self.expected_payloads.remove(legacy_setup)
            receipt_path = distribution / 'verification.json'
            receipt = json.loads(receipt_path.read_text())
            del receipt['artifactHashes'][legacy_setup]
            receipt_path.write_text(json.dumps(receipt))
            maintenance = lane / 'toolchains' / f'windows-{arch}' / 'installerbase.exe'
            maintenance.parent.mkdir(parents=True)
            maintenance.write_bytes(b'reference image')
            for suffix in ('qt-setup.exe', 'qt-core-repository.zip', 'qt-gui-repository.zip'):
                name = f'{base}-{suffix}'
                manifest['assets'].append(dict(name=name, sha256=self.write_package(assets / name)))
        (assets / QT_MANIFEST).write_text(json.dumps(manifest))
        (assets / QT_SIGNATURE).write_bytes(b'signature')
        self.publisher_key = self.root / 'trusted-public-key'
        self.publisher_key.write_bytes(b'trusted key')
        return assets

    def test_qt_release_stages_only_verified_payloads_and_signed_metadata(self):
        source = self.qt_lane()
        with patch('stage_release.public_check', return_value=dict(artifactBytes='passed', installerBytes='passed')) as verify:
            self.assertEqual(stage(self.root, self.version, self.commit, qt_publisher_key=self.publisher_key), 26)
        args, kwargs = verify.call_args
        self.assertEqual(args[3:], ((source / QT_MANIFEST).read_bytes(), b'trusted key', b'signature'))
        self.assertEqual(kwargs['published_names'], (QT_MANIFEST, QT_SIGNATURE))
        self.assertNotEqual(kwargs['qt_artifacts'], source)
        self.assertEqual(kwargs['qt_maintenance'], source.parent / 'toolchains')
        output = self.root / 'assets'
        self.assertEqual({path.name for path in output.iterdir()},
                         self.expected_payloads | {'SHA256SUMS.txt', QT_MANIFEST, QT_SIGNATURE})
        for line in (output / 'SHA256SUMS.txt').read_text().splitlines():
            digest, name = line.split('  ')
            self.assertIn(name, self.expected_payloads)
            self.assertEqual(hashlib.sha256((output / name).read_bytes()).hexdigest(), digest)
        for metadata in (QT_MANIFEST, QT_SIGNATURE):
            self.assertEqual((output / metadata).read_bytes(), (source / metadata).read_bytes())

        for name in ('sidravia-release', 'sidravia-release-extended'):
            request = request_document(self.root / 'requests' / (name + '.request.json'))
            for group in request['distributions']:
                self.assertEqual(group.get('qtWindowsGUI', False),
                                 group['target'].startswith('windows-') and 'guiDirectory' in group)

    def test_qt_release_requires_both_architectures_and_complete_verification(self):
        source = self.qt_lane()
        path = source / QT_MANIFEST
        complete = path.read_bytes()
        incomplete = json.loads(complete)
        incomplete['assets'] = [asset for asset in incomplete['assets'] if '-amd64-' in asset['name']]
        path.write_text(json.dumps(incomplete))
        with patch('stage_release.public_check') as verify:
            with self.assertRaisesRegex(ValueError, 'qt_release_assets_mismatch'):
                stage(self.root, self.version, self.commit, qt_publisher_key=self.publisher_key)
            verify.assert_not_called()
        path.write_bytes(complete)
        with patch('stage_release.public_check', return_value=dict(artifactBytes='passed')):
            with self.assertRaisesRegex(ValueError, 'qt_release_artifact_verification_incomplete'):
                stage(self.root, self.version, self.commit, qt_publisher_key=self.publisher_key)
        self.assertFalse((self.root / 'assets').exists())
        self.assertFalse((self.root / 'requests').exists())

    def test_refuses_ownership_mismatch_without_partial_candidate(self):
        lane = self.root / 'inputs' / f'sidravia-v{self.version}-linux-arm64-portable'
        receipt_path = lane / 'gui-xz-verification.json'
        receipt = json.loads(receipt_path.read_text())
        receipt['productOwnershipSha256'] = '2' * 64
        receipt_path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, 'portable_ownership_mismatch'):
            stage(self.root, self.version, self.commit)
        self.assertFalse((self.root / 'assets').exists())
        self.assertFalse((self.root / 'requests').exists())
