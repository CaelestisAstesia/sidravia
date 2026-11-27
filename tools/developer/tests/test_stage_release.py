import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from release_artifacts import request_document
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

    def write_package(self, path):
        self.expected_payloads.add(path.name)
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
        name = f'{base}-{kind}.{extension}'
        receipt = dict(version=self.version, source=dict(commit=self.commit, dirty=False),
                       source_unchanged=True, architecture=architecture, kind=kind, package=name,
                       sha256=self.write_package(group / name))
        (group / (name + '.json')).write_text(json.dumps(receipt))

    def test_assembles_exact_bridge_assets_and_valid_request_paths(self):
        self.assertEqual(stage(self.root, self.version, self.commit), 22)
        published = {p.name for p in (self.root / 'assets').iterdir()}
        self.assertEqual(published, self.expected_payloads | {'SHA256SUMS.txt'})
        listed = {line.split('  ')[1] for line in (self.root / 'assets/SHA256SUMS.txt').read_text().splitlines()}
        self.assertEqual(listed, self.expected_payloads)
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
