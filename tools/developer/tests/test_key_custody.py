"""Custody boundaries using public synthetic seed; no real wallet access."""
import base64
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('key_custody', Path(__file__).resolve().parents[2] / 'signrelease/key_custody.py')
custody = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(custody)
SEED = bytes(32)  # Universally known test seed; never a product root.
PUBLIC, KEY_ID = custody.public_identity(SEED)


class TestWallet:
    provider = 'synthetic-only'

    def __init__(self, existing=False, fail=False):
        self.existing, self.fail, self.written = existing, fail, False

    def matches(self):
        return [object()] if self.existing else []

    def store(self, seed):
        if self.fail:
            raise custody.CustodyError('wallet_write_failed')
        if seed != SEED:
            raise AssertionError('Only the public test seed may reach this fixture')
        self.written = True


class CustodyTests(unittest.TestCase):
    def test_backup_derives_and_checks_the_entire_identity(self):
        document = custody.backup_document(SEED)
        self.assertEqual(custody.check_backup(document, PUBLIC), KEY_ID)
        for field, value in [('schemaVersion', True), ('algorithm', 'RSA'), ('seed', 'invalid!'),
                             ('publicKey', 'invalid'), ('keyId', 'invalid')]:
            with self.subTest(field=field):
                altered = json.loads(document)
                altered[field] = value
                with self.assertRaises(custody.CustodyError):
                    custody.check_backup(json.dumps(altered).encode(), PUBLIC)
        with self.assertRaises(custody.CustodyError):
            custody.check_backup(document[:-1] + b',"schemaVersion":1}', PUBLIC)

    def test_other_key_is_not_an_acceptable_backup(self):
        other, _ = custody.public_identity(bytes([1]) * 32)
        with self.assertRaisesRegex(custody.CustodyError, 'backup_key_mismatch'):
            custody.check_backup(custody.backup_document(SEED), other)

    def test_preflight_does_not_generate_or_access_secrets(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(custody.os, 'urandom', side_effect=AssertionError):
            directory = Path(folder) / 'custody'
            result = custody.preflight(directory, TestWallet())
            self.assertFalse(directory.exists())
            self.assertFalse(result['walletSecretRead'])
            self.assertFalse(result['keyGenerated'])

    def test_existing_wallet_or_output_refuses_before_generation(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(custody.os, 'urandom', side_effect=AssertionError):
            directory = Path(folder)
            with self.assertRaisesRegex(custody.CustodyError, 'already_in_wallet'):
                custody.create(directory, TestWallet(existing=True))
            (directory / custody.BACKUP).write_bytes(b'original')
            with self.assertRaisesRegex(custody.CustodyError, 'files_already_exist'):
                custody.create(directory, TestWallet())
            self.assertEqual((directory / custody.BACKUP).read_bytes(), b'original')

    def test_repository_shared_and_linked_directories_refused(self):
        with self.assertRaises(custody.CustodyError):
            custody.private_directory(custody.REPO / '.local/signing')
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder) / 'public'
            directory.mkdir(mode=0o755)
            with self.assertRaises(custody.CustodyError):
                custody.private_directory(directory)
            link = Path(folder) / 'link'
            link.symlink_to(directory)
            with self.assertRaises(custody.CustodyError):
                custody.private_directory(link)

    def test_cancelling_encryption_does_not_write_wallet_or_key_files(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(custody.os, 'urandom', return_value=SEED), \
             patch.object(custody, 'gpg', side_effect=custody.CustodyError('gpg_cancelled_or_failed')), contextlib.redirect_stderr(io.StringIO()):
            wallet = TestWallet()
            with self.assertRaises(custody.CustodyError):
                custody.create(Path(folder), wallet)
            self.assertFalse(wallet.written)
            self.assertEqual({p.name for p in Path(folder).iterdir()}, {'.custody.lock'})

    def test_wallet_failure_preserves_only_encrypted_backup_and_public_material(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(custody.os, 'urandom', return_value=SEED), \
             patch.object(custody, 'gpg', return_value=b'encrypted-fixture'), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaisesRegex(custody.CustodyError, 'wallet_write_failed'):
                custody.create(Path(folder), TestWallet(fail=True))
            self.assertEqual((Path(folder) / custody.BACKUP).read_bytes(), b'encrypted-fixture')
            self.assertEqual((Path(folder) / custody.PUBLIC).read_text().strip(), PUBLIC)
            self.assertFalse((Path(folder) / custody.RECORD).exists())

    def test_creation_requires_recovery_check_and_records_no_seed(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(custody.os, 'urandom', return_value=SEED), \
             patch.object(custody, 'gpg', side_effect=[b'encrypted-fixture', custody.backup_document(SEED)]), \
             contextlib.redirect_stderr(io.StringIO()):
            wallet = TestWallet()
            result = custody.create(Path(folder), wallet)
            self.assertTrue(wallet.written)
            record = (Path(folder) / custody.RECORD).read_text()
            self.assertNotIn(base64.b64encode(SEED).decode(), record + json.dumps(result))
            self.assertEqual(json.loads(record)['backupRecovery'], 'passed')
            for name in (custody.BACKUP, custody.PUBLIC, custody.RECORD):
                self.assertEqual((Path(folder) / name).stat().st_mode & 0o777, 0o600)

    def test_changed_persisted_backup_or_public_file_never_claims_success(self):
        for name in (custody.BACKUP, custody.PUBLIC):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as folder, \
                 patch.object(custody.os, 'urandom', return_value=SEED), \
                 patch.object(custody, 'gpg', return_value=b'encrypted-fixture') as encrypt, \
                 contextlib.redirect_stderr(io.StringIO()):
                wallet = TestWallet()
                original_store = wallet.store

                def store_and_damage(seed):
                    original_store(seed)
                    (Path(folder) / name).write_bytes(b'changed-after-persistence')

                wallet.store = store_and_damage
                with self.assertRaisesRegex(custody.CustodyError, 'persisted_.*_mismatch'):
                    custody.create(Path(folder), wallet)
                self.assertTrue(wallet.written)
                self.assertEqual((Path(folder) / name).read_bytes(), b'changed-after-persistence')
                self.assertFalse((Path(folder) / custody.RECORD).exists())
                self.assertEqual(encrypt.call_count, 1)

    @unittest.skipUnless(Path('/usr/bin/gpg').exists(), 'GPG required for isolated ciphertext fixture')
    def test_real_gpg_backup_roundtrip_and_corruption_refusal(self):
        # The explicit test-only passphrase is public, not a production secret.
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            common = ['/usr/bin/gpg', '--no-options', '--homedir', folder, '--batch', '--no-symkey-cache',
                      '--pinentry-mode', 'loopback', '--passphrase', 'public-fixture-passphrase', '--output', '-']
            try:
                enc = subprocess.run(common + ['--symmetric', '--cipher-algo', 'AES256'],
                                     input=custody.backup_document(SEED), capture_output=True, timeout=30)
                self.assertEqual(enc.returncode, 0)
                self.assertNotIn(base64.b64encode(SEED), enc.stdout)
                dec = subprocess.run(common + ['--decrypt'], input=enc.stdout, capture_output=True, timeout=30)
                self.assertEqual(dec.returncode, 0)
                self.assertEqual(custody.check_backup(dec.stdout, PUBLIC), KEY_ID)
                damaged = enc.stdout[:-10]
                bad = subprocess.run(common + ['--decrypt'], input=damaged, capture_output=True, timeout=30)
                self.assertNotEqual(bad.returncode, 0)
                (home / 'backup.gpg').write_bytes(enc.stdout)
                (home / 'public.txt').write_text(PUBLIC)
            finally:
                subprocess.run(['/usr/bin/gpgconf', '--homedir', folder, '--kill', 'gpg-agent'], capture_output=True, timeout=10)


if __name__ == '__main__':
    unittest.main()
