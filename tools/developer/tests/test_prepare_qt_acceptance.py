import base64
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/developer'))
from distribution_fixture import pe
from prepare_qt_acceptance import prepare
from release_artifacts import QT_MANIFEST, QT_SIGNATURE


class PrepareQtAcceptanceTest(unittest.TestCase):
    def test_uses_verified_release_inputs_and_rejects_a_modified_setup(self):
        # Known Ed25519 test vector. It never represents product trust.
        seed = base64.b64encode(bytes(32))
        public = bytes.fromhex('3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29')
        arch = 'arm64'
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            signer = root / 'signrelease.exe'
            subprocess.run(['go', 'build', '-mod=readonly', '-o', signer, './tools/signrelease'],
                           cwd=ROOT, check=True, timeout=120)
            key = root / 'public-key'
            key.write_bytes(base64.b64encode(public))
            test_program = root / 'handoff.exe'
            test_program.write_bytes(pe(arch))
            maintenance = root / 'toolchains'
            image = maintenance / f'windows-{arch}' / 'installerbase.exe'
            image.parent.mkdir(parents=True)
            image_bytes = pe(arch, b'inert reference')
            image.write_bytes(image_bytes)

            def sign(*args, input=None):
                return subprocess.run([signer, *args], input=input, capture_output=True,
                                      check=True, timeout=30).stdout

            def signed_release(version, build):
                directory = root / version
                directory.mkdir()
                stem = f'sidravia-v{version}-windows-{arch}-qt'
                setup = directory / (stem + '-setup.exe')
                setup.write_bytes(image_bytes + version.encode())
                unsigned = root / 'unsigned.zip'
                repositories = []
                for profile in ('core', 'gui'):
                    updates = b'<Updates/>'
                    index = dict(schemaVersion=1, product='Sidravia', target=f'windows-{arch}',
                                 profile=profile, version=version, buildId=build,
                                 maintenanceSha256=hashlib.sha256(image_bytes).hexdigest(),
                                 files={'Updates.xml': hashlib.sha256(updates).hexdigest()})
                    with zipfile.ZipFile(unsigned, 'w') as archive:
                        archive.writestr('sidravia-ifw-inventory.json', json.dumps(index))
                        archive.writestr('Updates.xml', updates)
                    repository = directory / (stem + f'-{profile}-repository.zip')
                    sign('qt-repository', '--input', unsigned, '--output', repository, input=seed)
                    repositories.append(repository)
                manifest = directory / QT_MANIFEST
                manifest.write_bytes(sign('qt-manifest', '--version', version, '--build-id', build,
                                          '--public-key', key, '--qt-installers', directory,
                                          '--qt-maintenance', maintenance, *repositories))
                sign('--manifest', manifest, '--output', directory / QT_SIGNATURE, input=seed)
                return directory, setup

            previous, setup = signed_release('1.2.3', 'a' * 40)
            target, _ = signed_release('1.2.4', 'b' * 40)
            options = previous, target, maintenance, test_program, key, arch, 'go'
            request = prepare(*options)
            self.assertEqual(request['setup'], str(setup))
            self.assertEqual(request['setupSize'], setup.stat().st_size)
            self.assertEqual(request['setupSHA256'], hashlib.sha256(setup.read_bytes()).hexdigest())
            self.assertEqual(request['sourceVersion'], '1.2.3')
            self.assertEqual(request['sourceBuildId'], 'a' * 40)
            self.assertEqual(request['repositories'], str(target))
            self.assertEqual(request['manifest'], str(target / QT_MANIFEST))
            self.assertEqual(request['signature'], str(target / QT_SIGNATURE))
            self.assertEqual(request['publicKey'], key.read_text())
            with self.assertRaisesRegex(ValueError, 'manifest_structure_or_signature_rejected'):
                prepare(target, previous, *options[2:])
            setup.write_bytes(setup.read_bytes() + b'changed')
            with self.assertRaisesRegex(ValueError, 'manifest_structure_or_signature_rejected'):
                prepare(*options)
