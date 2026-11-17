"""Opt-in real producer/consumer/public-signature acceptance, with test keys only.

No product or installer is started, no formal key is generated, and no release
is uploaded. The Windows ARM64 case proves ZIP/cross-build consumption only.
"""
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


@unittest.skipUnless(os.environ.get('SIDRAVIA_RELEASE_ARTIFACT_FIXTURE') == '1', 'explicit real-package release fixture required')
class ReleasePackageFixture(unittest.TestCase):
    def test_exact_local_versions_and_both_archive_formats(self):
        go = os.environ['SIDRAVIA_TEST_GO']
        commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        self.assertEqual(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT, text=True), '')
        owned = Path(tempfile.mkdtemp(prefix='release-tools-fixture-', dir=ROOT / '.local'))
        print('owned evidence:', owned)
        # Public key of the universally known all-zero synthetic seed, also used
        # by the Go signer tests. This must never become a product trust root.
        public = owned / 'synthetic-public.txt'
        public.write_bytes(base64.b64encode(bytes.fromhex('3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29')))
        seed = base64.b64encode(bytes(32))
        count = 0

        def invoke(argv, *, good=True, input=None):
            nonlocal count
            count += 1
            result = subprocess.run(argv, cwd=ROOT, env=dict(os.environ, GOTOOLCHAIN='local'), input=input,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
            (owned / f'{count:02d}.stdout.log').write_bytes(result.stdout)
            (owned / f'{count:02d}.stderr.log').write_bytes(result.stderr)
            self.assertEqual(result.returncode == 0, good, result.stderr.decode(errors='replace')[-2000:])
            return result

        results = []
        for version, target in [('1.2.3-preview.1', 'linux-amd64'), ('1.2.3-preview.2', 'linux-amd64'), ('1.2.3-preview.2', 'windows-arm64')]:
            case = owned / (target + '-' + version)
            case.mkdir()
            output = case / 'products'
            invoke([sys.executable, 'tools/developer/dev.py', 'package', '--version', version, '--target', target,
                    '--output', str(output), '--go', go])
            names = sorted(p.name for p in output.iterdir() if p.name != 'SHA256SUMS.txt')
            request = case / 'request.json'
            request.write_text(json.dumps({'schemaVersion': 1, 'version': version, 'buildId': commit,
                                           'expectedAssets': names, 'distributions': [{'directory': str(output), 'target': target}]}))
            manifest, signature = case / 'sidravia-release.json', case / 'sidravia-release.sig'
            common = ['--request', str(request), '--public-key', str(public), '--go', go, '--format', 'json']
            prepare = [sys.executable, 'tools/developer/dev.py', 'release', 'prepare', *common, '--output', str(manifest)]
            prepared = json.loads(invoke(prepare).stdout)
            self.assertEqual(prepared['signature'], 'not_checked')
            self.assertTrue(prepared['source_unchanged'])
            before = manifest.read_bytes()
            invoke(prepare, good=False)
            invoke([go, 'run', '-mod=readonly', './tools/signrelease', '--manifest', str(manifest), '--output', str(signature)], input=seed)
            verify = [sys.executable, 'tools/developer/dev.py', 'release', 'verify', *common, '--manifest', str(manifest), '--signature', str(signature)]
            verified = json.loads(invoke(verify).stdout)
            self.assertEqual(verified['signature'], 'passed')
            self.assertEqual(verified['artifactBytes'], 'passed')
            self.assertEqual(verified['nativeUpgrade'], 'not_run')
            self.assertEqual(verified['publicDownload'], 'not_run')
            manifest.write_bytes(before + b'\n')
            invoke(verify, good=False)
            manifest.write_bytes(before)
            product = output / names[0]
            with product.open('ab') as handle:
                handle.write(b'fixture changed after signing')
            invoke(verify, good=False)
            results.append({'version': version, 'target': target, 'producerConsumer': 'passed', 'syntheticSignature': 'passed',
                            'overwriteRefused': True, 'changedSignedBytesRefused': True, 'changedArtifactRefused': True})
        (owned / 'result.json').write_text(json.dumps({'schemaVersion': 1, 'sourceCommit': commit, 'syntheticKeyOnly': True,
            'cases': results, 'nativeUpgrade': 'not_run', 'publicDownload': 'not_run', 'publication': 'not_run'}, indent=2))
