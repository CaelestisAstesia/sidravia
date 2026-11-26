import json
import pathlib
import tempfile
import unittest
import sys
import shutil
from itertools import product
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import linux_rpm
import dev


class RPMContracts(unittest.TestCase):
    @unittest.skipUnless(sys.platform == 'linux', 'requires Linux directory replacement semantics')
    def test_publication_preserves_the_previous_output_and_keeps_failed_copies_private(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            package = root / 'candidate.rpm'
            package.write_bytes(b'synthetic RPM')
            receipt = {'sha256': linux_rpm.digest(package)}
            output = root / 'candidate'
            check_source = mock.Mock()
            with mock.patch.object(linux_rpm.shutil, 'copy2', side_effect=OSError('disk full')):
                with self.assertRaisesRegex(OSError, 'disk full'):
                    linux_rpm.publish_rpm(package, receipt, output, check_source)
            check_source.assert_not_called()
            self.assertFalse(output.exists())
            self.assertEqual(list(root.glob('.sidravia-rpm-*')), [])
            def changed_during_copy():
                copied, = root.glob('.sidravia-rpm-*/complete/candidate.rpm')
                self.assertEqual(copied.read_bytes(), package.read_bytes())
                raise ValueError('source changed during RPM build')
            with self.assertRaisesRegex(ValueError, 'source changed'):
                linux_rpm.publish_rpm(package, receipt, output, changed_during_copy)
            self.assertFalse(output.exists())
            linux_rpm.publish_rpm(package, receipt, output, check_source)
            self.assertEqual((output / package.name).read_bytes(), package.read_bytes())
            self.assertEqual(json.loads((output / 'candidate.rpm.json').read_bytes()), receipt)
            self.assertEqual((output / 'SHA256SUMS').read_text(), receipt['sha256'] + '  candidate.rpm\n')
            with self.assertRaises(FileExistsError):
                linux_rpm.publish_rpm(package, receipt, output, check_source)
            self.assertEqual((output / package.name).read_bytes(), package.read_bytes())

    def test_acceptance_uses_explicit_tools_and_only_gui_probes_flutter(self):
        architectures = (('amd64', 'x86_64'), ('arm64', 'aarch64'))
        for kind, (host_arch, rpm_arch) in product(('gui', 'core'), architectures):
            with self.subTest(kind=kind, host_arch=host_arch), tempfile.TemporaryDirectory() as tmp:
                workspace = pathlib.Path(tmp)
                root = workspace / 'checkout'
                root.mkdir()
                sdk = workspace / 'shared-sdk'
                dart = sdk / 'bin/dart'
                flutter = workspace / 'wrapper/flutter'
                go = str(workspace / 'shared-go/bin/go')
                env = {}
                if kind == 'gui':
                    (root / 'gui').mkdir()
                    (root / 'gui/.flutter-version').write_text('3.47.0\n')
                    dart.parent.mkdir(parents=True)
                    dart.touch()
                    env = {'XDG_RUNTIME_DIR': str(workspace), 'WAYLAND_DISPLAY': 'wayland-0'}
                source = dict(commit='fixture', dirty=False, status='', source_fingerprint='fixture')
                packages = [root / 'old.rpm', root / 'new.rpm']
                for release, pkg in enumerate(packages, start=1):
                    pkg.write_text(f'synthetic RPM {release}')
                    pkg.with_suffix('.rpm.json').write_text(json.dumps({
                        'kind': kind, 'version': '1.0.0', 'release': str(release),
                        'architecture': rpm_arch, 'source': source,
                    }))
                probe_result = json.dumps({'frameworkVersion': '3.47.0', 'flutterRoot': str(sdk)})
                args = dev.parse_args(['linux', '--go', go, '--flutter', str(flutter),
                                       '--verify-rpm-upgrade', *map(str, packages)])
                with (mock.patch.object(dev, 'ROOT', root),
                      mock.patch.object(dev.platform, 'system', return_value='Linux'),
                      mock.patch.object(linux_rpm, 'native_arch', return_value=host_arch),
                      mock.patch.object(dev, 'metadata', return_value=source) as metadata,
                      mock.patch.object(linux_rpm, 'verify') as verify,
                      mock.patch.object(dev.subprocess, 'check_output', return_value=probe_result) as probe,
                      mock.patch.object(dev.subprocess, 'run', return_value=mock.Mock(returncode=19)) as run):
                    self.assertEqual(dev.linux_command(args, env), 19)
                self.assertTrue((root / '.local').is_dir())
                metadata.assert_called_once_with(go, env)
                argv = run.call_args.args[0]
                index = argv.index(str(root / 'tools/developer/linux_rpm_fixture.py'))
                expected = ['--go', go]
                if kind == 'gui':
                    expected += ['--dart', str(dart)]
                    probe.assert_called_once_with([str(flutter.resolve()), '--version', '--machine'],
                                                 cwd=root, env=env, text=True)
                    self.assertEqual(run.call_args.kwargs['env']['WAYLAND_DISPLAY'], str(workspace / 'wayland-0'))
                else:
                    probe.assert_not_called()
                    self.assertNotIn('WAYLAND_DISPLAY', run.call_args.kwargs['env'])
                self.assertEqual(argv[index + 1:-2], [*expected, str(root)])
                staged = list(map(pathlib.Path, argv[-2:]))
                self.assertEqual([p.name for p in staged], [p.name for p in packages])
                self.assertEqual([call.args[0] for call in verify.call_args_list], staged)
                self.assertTrue(all((root / '.local') in p.parents for p in staged))
                self.assertTrue(all(not p.exists() for p in staged))

    def test_query_retains_empty_file_inventory_columns(self):
        with mock.patch.object(linux_rpm.subprocess, 'check_output', return_value='/usr/share/licenses/sidravia/LICENSE\t100644\tabc\t\n'):
            self.assertEqual(len(linux_rpm.query('candidate.rpm', '--qf', 'format').split('\t')), 4)

    @unittest.skipUnless(shutil.which('rpm'), 'requires native RPM tool')
    def test_prerelease_mapping_preserves_rpm_ordering(self):
        import subprocess
        before = linux_rpm.rpm_version('0.4.0-dev.linux.5')
        self.assertEqual(before, '0.4.0~dev.linux.5')
        result = subprocess.check_output(['rpm', '--eval', '%{lua:print(rpm.vercmp("' + before + '", "0.4.0"))}'], text=True).strip()
        self.assertEqual(result, '-1')
        for invalid in ('v0.4.0', '0.4', '01.4.0', '0.4.0+meta', '0.4.0\n%post'):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                linux_rpm.rpm_version(invalid)

    @unittest.skipUnless(sys.platform == 'linux', 'requires Linux permissions and symbolic links')
    def test_inventory_preserves_links_and_permissions_without_owning_shared_dirs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            app = root / 'usr/lib64/sidravia'
            app.mkdir(parents=True)
            binary = app / 'sidravia_gui'
            binary.write_bytes(b'executable')
            binary.chmod(0o755)
            binpath = root / 'usr/bin'
            binpath.mkdir()
            (binpath / 'sidravia_gui').symlink_to('../lib64/sidravia/sidravia_gui')
            manifest = linux_rpm.tree_manifest(root)
            self.assertNotIn('/usr', manifest)
            self.assertNotIn('/usr/bin', manifest)
            self.assertIn('/usr/lib64/sidravia', manifest)
            self.assertEqual(manifest['/usr/bin/sidravia_gui']['link'], '../lib64/sidravia/sidravia_gui')
            self.assertEqual(manifest['/usr/lib64/sidravia/sidravia_gui']['mode'] & 0o777, 0o755)

    def test_consumer_rejects_changed_rpm_before_reading_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            rpm = pathlib.Path(tmp) / 'candidate.rpm'
            rpm.write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'producer hash'):
                linux_rpm.verify(rpm, {'sha256': '0' * 64})
