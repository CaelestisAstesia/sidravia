import pathlib
import tempfile
import unittest
import sys
import shutil
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import linux_rpm


class RPMContracts(unittest.TestCase):
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
