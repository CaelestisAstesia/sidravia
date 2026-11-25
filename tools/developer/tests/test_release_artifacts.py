import base64
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/developer'))
import release_artifacts as release
from distribution_fixture import DistributionFixture, SHA, VERSION


class ReleaseArtifactContracts(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.fixture = DistributionFixture(self.root)
        self.key = self.root / 'public.txt'
        self.key.write_bytes(base64.b64encode(bytes(range(32))))
        self.request = self.root / 'request.json'
        self.write_group('linux-amd64', gui=False)
        self.identity = mock.patch.object(release.distribution, 'binary_identity', return_value=False)
        self.identity.start()
        self.addCleanup(self.identity.stop)

    def write_group(self, target, gui=False):
        contents = self.fixture.payload(target, gui=gui)
        self.fixture.package(target, contents, gui=gui)
        names = sorted(p.name for p in self.fixture.out.iterdir() if p.name != 'SHA256SUMS.txt')
        group = {'target': target, 'directory': str(self.fixture.out)}
        if gui:
            group['guiDirectory'] = str(self.fixture.gui)
        self.data = {'schemaVersion': 1, 'version': VERSION, 'buildId': SHA,
                     'expectedAssets': names, 'distributions': [group]}
        self.save()

    def save(self):
        self.request.write_text(json.dumps(self.data), encoding='utf-8')

    def collect(self):
        return release.collect(self.fixture.repo, self.request, self.key, 'go')

    def test_xz_preparation_preserves_primary_manifest_and_ownership(self):
        import gzip, lzma
        legacy, _ = self.collect()
        primary = self.root / 'sidravia-release.json'
        primary.write_text(json.dumps(legacy))
        original = next(self.fixture.out.glob('*.tar.gz'))
        encoded = original.with_name(original.name[:-3] + '.xz')
        encoded.write_bytes(lzma.compress(gzip.decompress(original.read_bytes()), preset=6))
        original.unlink()
        (self.fixture.out / 'SHA256SUMS.txt').write_text(hashlib.sha256(encoded.read_bytes()).hexdigest() + '  ' + encoded.name + '\n')
        self.data['expectedAssets'] = [encoded.name]
        self.save()
        output = self.root / 'sidravia-release-xz.json'
        before = primary.read_bytes()
        with mock.patch.object(release, 'public_check'):
            result = release.prepare(self.fixture.repo, 'go', self.request, self.key, output)
        self.assertEqual(primary.read_bytes(), before)
        sidecar = json.loads(output.read_text())
        self.assertEqual(sidecar['assets'][0]['filesSha256'], legacy['assets'][0]['filesSha256'])
        self.assertEqual(result['signature'], 'not_checked')

    def test_xz_sidecar_requires_matching_legacy_ownership(self):
        import copy
        legacy, _ = self.collect()
        (self.root / 'sidravia-release.json').write_text(json.dumps(legacy))
        sidecar = copy.deepcopy(legacy)
        sidecar['assets'][0]['name'] = sidecar['assets'][0]['name'][:-3] + '.xz'
        path = self.root / 'sidravia-release-xz.json'
        release.encoding_compatibility(sidecar, path)
        with self.assertRaisesRegex(ValueError, 'separate_name'):
            release.encoding_compatibility(sidecar, self.root / 'sidravia-release.json')
        sidecar['assets'][0]['filesSha256'] = 'a' * 64
        with self.assertRaisesRegex(ValueError, 'ownership_mismatch'):
            release.encoding_compatibility(sidecar, path)

    def test_existing_consumer_binds_exact_archive_and_ownership(self):
        manifest, receipts = self.collect()
        self.assertEqual(manifest['keyId'], hashlib.sha256(bytes(range(32))).hexdigest())
        asset, = manifest['assets']
        archive = self.fixture.out / asset['name']
        self.assertEqual(asset['sha256'], hashlib.sha256(archive.read_bytes()).hexdigest())
        self.assertEqual(asset['filesSha256'], receipts[0]['productOwnershipSha256'])
        self.assertEqual((asset['os'], asset['arch'], asset['kind'], asset['payloadKind']), ('linux', 'amd64', 'core-portable', 'core'))
        self.assertEqual(receipts[0]['productOwnership'], 'passed')
        self.assertEqual(receipts[0]['embeddedGoIdentity'], 'not_recorded')
        self.assertEqual(receipts[0]['helpSmoke'], 'not_run')

    def test_setup_binds_its_own_icon_inventory_without_claiming_native_acceptance(self):
        # Start a separate normal producer directory; a release may combine it
        # with the core directory but may not silently overwrite duplicate assets.
        for path in self.fixture.out.iterdir():
            path.unlink()
        self.write_group('windows-amd64', gui=True)
        manifest, receipts = self.collect()
        self.assertEqual({a['kind'] for a in manifest['assets']}, {'gui-portable', 'setup'})
        self.assertEqual(len({a['filesSha256'] for a in manifest['assets']}), 2)
        setup,= [a for a in manifest['assets'] if a['kind']=='setup']
        self.assertEqual(setup['filesSha256'],receipts[0]['installerOwnershipSha256'])
        self.assertEqual(receipts[0]['installerPayload'], 'requires_native_acceptance')
        self.assertEqual(receipts[0]['installation'], 'not_run')

    def test_rejects_changed_source_archive_and_checksums(self):
        manifest, _ = self.collect()
        path = self.fixture.out / manifest['assets'][0]['name']
        path.write_bytes(path.read_bytes() + b'changed')
        with self.assertRaisesRegex(ValueError, 'output_checksum_mismatch'):
            self.collect()

    def test_explicit_expected_set_and_duplicate_distribution_are_checked(self):
        self.data['expectedAssets'].append('missing.zip')
        self.save()
        with self.assertRaisesRegex(ValueError, 'missing_or_unexpected_asset'):
            self.collect()
        self.data['expectedAssets'].pop()
        self.data['distributions'] *= 2
        self.save()
        with self.assertRaisesRegex(ValueError, 'duplicate_asset'):
            self.collect()

    def test_mixed_build_and_public_key_rejected(self):
        self.data['buildId'] = 'b' * 40
        self.save()
        with self.assertRaisesRegex(ValueError, 'archive_source_mismatch'):
            self.collect()
        self.data['buildId'] = SHA
        self.save()
        self.key.write_text('private seeds are not accepted here', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'invalid_public_key'):
            self.collect()

    def test_duplicate_request_fields_and_linked_input_rejected(self):
        self.request.write_text(self.request.read_text().replace('"schemaVersion": 1', '"schemaVersion": 1,"schemaVersion": 1'))
        with self.assertRaisesRegex(ValueError, 'duplicate_json_field'):
            self.collect()
        self.save()
        alias = self.root / 'request-alias.json'
        try:
            alias.symlink_to(self.request)
        except OSError:
            self.skipTest('symlink unavailable')
        with self.assertRaisesRegex(ValueError, 'linked_input_path'):
            release.request_document(alias)

    def test_verification_rejects_rewritten_manifest_before_signature_check(self):
        manifest, _ = self.collect()
        manifest['buildId'] = 'b' * 40
        path = self.root / 'sidravia-release.json'
        path.write_text(json.dumps(manifest))
        signature=self.root / 'signature'
        signature.write_bytes(bytes(64))
        with self.assertRaisesRegex(ValueError, 'manifest_does_not_match_artifacts'):
            release.verify(self.fixture.repo, 'go', self.request, self.key, path, signature)

    def test_preparation_refuses_overwrite_and_output_inside_distribution(self):
        output = self.root / 'existing.json'
        output.write_text('keep')
        with self.assertRaisesRegex(ValueError, 'output_already_exists'):
            release.prepare(self.fixture.repo, 'go', self.request, self.key, output)
        self.assertEqual(output.read_text(), 'keep')
        with self.assertRaisesRegex(ValueError, 'manifest_output_inside_distribution'):
            release.prepare(self.fixture.repo, 'go', self.request, self.key, self.fixture.out / 'sidravia-release.json')
        with self.assertRaisesRegex(ValueError, 'manifest_output_inside_distribution'):
            release.prepare(self.fixture.repo, 'go', self.request, self.key, self.root / 'unused' / '..' / 'out' / 'sidravia-release.json')

    def test_run_requires_clean_exact_build_commit(self):
        args = type('Args', (), {'request': str(self.request)})()
        for source in ({'dirty': True, 'commit': SHA}, {'dirty': False, 'commit': 'b' * 40}):
            with self.assertRaisesRegex(ValueError, 'clean_source_commit_required'):
                release.run(args, ROOT, source)

    def test_changed_request_or_source_does_not_publish_manifest(self):
        output = self.root / 'candidate.json'
        args = mock.Mock(mode='prepare', request=self.request, public_key=self.key, output=output, go='go')
        source = {'dirty': False, 'commit': SHA}
        changed = json.dumps(dict(self.data, version='9.8.7'))
        with mock.patch.object(release, 'public_check', side_effect=lambda *_: self.request.write_text(changed)):
            with self.assertRaisesRegex(ValueError, 'request_changed_during_validation'):
                release.run(args, self.fixture.repo, source)
        self.assertFalse(output.exists())
        self.assertEqual(list(self.root.glob('.sidravia-manifest-*')), [])
        self.save()
        with mock.patch.object(release, 'public_check'):
            with self.assertRaisesRegex(ValueError, 'source changed'):
                release.run(args, self.fixture.repo, source,
                            before_publish=mock.Mock(side_effect=ValueError('source changed')))
        self.assertFalse(output.exists())
        with mock.patch.object(release, 'public_check'):
            with self.assertRaisesRegex(ValueError, 'request_changed_during_validation'):
                release.run(args, self.fixture.repo, source,
                            before_publish=lambda: self.request.write_text(changed))
        self.assertFalse(output.exists())

    def test_public_verifier_uses_exact_owned_byte_snapshots(self):
        captured = {}
        def invoke(argv, **kwargs):
            for flag in ('--manifest','--public-key','--signature'):
                path=Path(argv[argv.index(flag)+1])
                captured[flag]=path.read_bytes()
                self.assertIn('sidravia-release-public-',str(path.parent))
            return type('Result',(),{'returncode':0,'stdout':'{"ok":true,"signature":"passed"}'})()
        with mock.patch.object(release.subprocess,'run',side_effect=invoke):
            release.public_check(ROOT,'go','verify',b'exact signed bytes',b'exact public key',b'exact signature')
        self.assertEqual(captured,{'--manifest':b'exact signed bytes','--public-key':b'exact public key','--signature':b'exact signature'})

    def test_linked_signature_is_rejected_before_public_verification(self):
        manifest,_=self.collect()
        path=self.root/'manifest.json';path.write_text(json.dumps(manifest))
        signature=self.root/'signature';signature.write_bytes(bytes(64))
        alias=self.root/'alias-signature'
        try:
            alias.hardlink_to(signature)
        except OSError:
            self.skipTest('hard links unavailable')
        with self.assertRaisesRegex(ValueError,'hard_linked_input'):
            release.verify(self.fixture.repo,'go',self.request,self.key,path,signature)

    def test_checked_request_snapshot_cannot_switch_build_after_entry_validation(self):
        args=type('Args',(),{'request':str(self.request),'public_key':str(self.key),'mode':'prepare','go':'go','output':str(self.root/'manifest.json')})()
        original=json.loads(self.request.read_text())
        seen={}
        def prepare(repo,go,path,public_key,output,*,request_snapshot,before_publish):
            changed=dict(original,buildId='b'*40)
            self.request.write_text(json.dumps(changed))
            seen.update(request_snapshot)
            # The actual collector must continue to consume original A's files,
            # not the changed B request from the ignored local path.
            manifest,_=release.collect(repo,path,public_key,go,request_snapshot=request_snapshot)
            self.assertEqual(manifest['buildId'],SHA)
            before_publish()
            return {'buildId':manifest['buildId']}
        with mock.patch.object(release,'prepare',side_effect=prepare):
            with self.assertRaisesRegex(ValueError,'request_changed_during_validation'):
                release.run(args,self.fixture.repo,{'dirty':False,'commit':SHA})
        self.assertEqual(seen,original)
