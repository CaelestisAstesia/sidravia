#!/usr/bin/python3 -s
"""Local publisher key custody. Never signs, uploads, or changes product roots."""
from __future__ import annotations

import argparse
import base64
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import resource
import stat
import subprocess
import sys

REPO = Path(__file__).resolve().parents[2]
ATTRIBUTES = {'application': 'org.sidravia.publisher', 'purpose': 'release-signing', 'format': 'ed25519-seed-v1'}
BACKUP = 'publisher-ed25519-seed.gpg'
PUBLIC = 'publisher-public.txt'
RECORD = 'publisher-key.json'
GPG = '/usr/bin/gpg'


class CustodyError(Exception):
    """A stable error code, never an underlying exception containing a secret."""


def require(condition, code):
    if not condition:
        raise CustodyError(code)


def public_identity(seed):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    require(len(seed) == 32, 'invalid_seed')
    public = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes_raw()
    return base64.b64encode(public).decode('ascii'), hashlib.sha256(public).hexdigest()


def backup_document(seed):
    public, key_id = public_identity(seed)
    return json.dumps({'schemaVersion': 1, 'algorithm': 'Ed25519', 'seed': base64.b64encode(seed).decode('ascii'),
                       'publicKey': public, 'keyId': key_id}, separators=(',', ':')).encode('ascii')


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'invalid_backup_document')
        result[key] = value
    return result


def check_backup(plaintext, expected_public):
    require(0 < len(plaintext) <= 4096, 'invalid_backup_size')
    try:
        doc = json.loads(plaintext, object_pairs_hook=unique_object)
        require(isinstance(doc, dict) and set(doc) == {'schemaVersion', 'algorithm', 'seed', 'publicKey', 'keyId'}, 'invalid_backup_document')
        require(type(doc['schemaVersion']) is int and doc['schemaVersion'] == 1 and doc['algorithm'] == 'Ed25519', 'invalid_backup_document')
        seed = base64.b64decode(doc['seed'], validate=True)
        public, key_id = public_identity(seed)
        require(doc['publicKey'] == public == expected_public and doc['keyId'] == key_id, 'backup_key_mismatch')
        return key_id
    except (ValueError, TypeError, KeyError, UnicodeError):
        raise CustodyError('invalid_backup_document') from None


def plain_path(path):
    path = Path(os.path.abspath(path))
    for part in (path, *path.parents):
        require(not part.is_symlink(), 'linked_path_refused')
    return path


def private_directory(path, *, create=False):
    path = plain_path(path)
    require(not path.is_relative_to(REPO), 'key_directory_inside_repository')
    if create:
        path.mkdir(mode=0o700, exist_ok=True)
    if path.exists():
        info = path.stat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and info.st_mode & 0o077 == 0,
                'key_directory_not_private')
    else:
        require(path.parent.is_dir(), 'key_directory_parent_missing')
    return path


def read_file(path, limit):
    path = plain_path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= limit, 'invalid_input_file')
        result = handle.read(limit + 1)
        require(len(result) == info.st_size, 'input_changed')
    return result


def write_new(directory, name, data):
    fd = os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def gpg(data, *, decrypt=False):
    command = [GPG, '--no-options', '--no-tty', '--no-symkey-cache', '--pinentry-mode', 'ask',
               '--max-output', '8192', '--output', '-']
    command += ['--decrypt'] if decrypt else ['--symmetric', '--cipher-algo', 'AES256']
    try:
        result = subprocess.run(command, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=900)
    except subprocess.TimeoutExpired:
        raise CustodyError('gpg_timeout_preserve_existing_files') from None
    require(result.returncode == 0, 'gpg_cancelled_or_failed')
    require(0 < len(result.stdout) <= 8192, 'invalid_gpg_output')
    return result.stdout


class KWallet:
    """libsecret access to the existing KDE provider; no secret-list/export API."""

    def __init__(self):
        import gi
        gi.require_version('Secret', '1')
        from gi.repository import Secret, Gio, GLib
        self.Secret, self.Gio, self.GLib = Secret, Gio, GLib
        self.service = Secret.Service.get_sync(Secret.ServiceFlags.NONE, None)
        self.owner = self.service.get_name_owner()
        pid = self.service.get_connection().call_sync(
            'org.freedesktop.DBus', '/org/freedesktop/DBus', 'org.freedesktop.DBus', 'GetConnectionUnixProcessID',
            GLib.Variant('(s)', (self.owner,)), GLib.VariantType.new('(u)'), Gio.DBusCallFlags.NONE, 5000, None).unpack()[0]
        executable = str(Path(f'/proc/{pid}/exe').readlink())
        require(executable in ('/usr/bin/ksecretd', '/usr/bin/kwalletd6', '/usr/bin/kwalletd5'), 'secret_service_is_not_kwallet')
        self.provider = executable
        self.schema = Secret.Schema.new('org.sidravia.PublisherKey', Secret.SchemaFlags.DONT_MATCH_NAME,
                                        {key: Secret.SchemaAttributeType.STRING for key in ATTRIBUTES})

    def check_owner(self):
        require(self.service.get_name_owner() == self.owner, 'wallet_provider_changed')

    def matches(self):
        self.check_owner()
        items = self.service.search_sync(self.schema, ATTRIBUTES, self.Secret.SearchFlags.ALL, None)
        self.check_owner()
        return items

    def store(self, seed):
        require(not self.matches(), 'publisher_key_already_in_wallet')
        collection = self.Secret.Collection.for_alias_sync(self.service, 'default', self.Secret.CollectionFlags.NONE, None)
        require(collection is not None, 'default_wallet_missing')
        if collection.get_locked():
            self.service.unlock_sync([collection], None)
        require(not collection.get_locked(), 'wallet_unlock_cancelled')
        self.check_owner()
        self.service.ensure_session_sync(None)
        value = self.Secret.Value.new(base64.b64encode(seed).decode('ascii'), -1, 'text/plain')
        # NONE deliberately excludes REPLACE; never overwrite a matching entry.
        item = self.Secret.Item.create_sync(collection, self.schema, ATTRIBUTES, 'Sidravia publisher signing key (Ed25519)',
                                            value, self.Secret.ItemCreateFlags.NONE, None)
        self.check_owner()
        items = self.matches()
        require(len(items) == 1 and items[0].get_object_path() == item.get_object_path(), 'wallet_write_ambiguous')
        require(item.load_secret_sync(None), 'wallet_readback_failed')
        stored = item.get_secret()
        require(stored is not None and stored.get() == base64.b64encode(seed), 'wallet_readback_mismatch')
        self.check_owner()


def preflight(directory, wallet):
    directory = private_directory(directory)
    require(not wallet.matches(), 'publisher_key_already_in_wallet')
    require(all(not (directory / name).exists() and not (directory / name).is_symlink()
                for name in (BACKUP, PUBLIC, RECORD)), 'custody_files_already_exist')
    require(Path(GPG).is_file(), 'gpg_not_available')
    return {'status': 'ready', 'directory': str(directory), 'provider': wallet.provider,
            'walletSecretRead': False, 'keyGenerated': False, 'signature': 'not_run', 'upload': 'not_run'}


def create(directory, wallet):
    preflight(directory, wallet)
    directory = private_directory(directory, create=True)
    lock_fd = os.open(directory / '.custody.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        lock_info = os.fstat(lock_fd)
        require(stat.S_ISREG(lock_info.st_mode) and lock_info.st_uid == os.getuid()
                and lock_info.st_nlink == 1 and lock_info.st_mode & 0o077 == 0, 'invalid_lock_file')
        try:
            fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise CustodyError('custody_operation_already_running') from None
        preflight(directory, wallet)
        seed = os.urandom(32)
        public, key_id = public_identity(seed)
        print('请在 GPG 本机弹窗中设置备份口令，不要把口令发到聊天', file=sys.stderr, flush=True)
        encrypted = gpg(backup_document(seed))
        # Persist recovery material before modifying the wallet. On any later
        # failure preserve it and stop; never retry with a different new key.
        write_new(directory, BACKUP, encrypted)
        write_new(directory, PUBLIC, (public + '\n').encode('ascii'))
        wallet.store(seed)
        persisted = read_file(directory / BACKUP, 8192)
        require(persisted == encrypted, 'persisted_backup_mismatch')
        require(read_file(directory / PUBLIC, 256) == (public + '\n').encode('ascii'), 'persisted_public_key_mismatch')
        print('钱包写入并读回一致；请再次输入备份口令，核对解密恢复', file=sys.stderr, flush=True)
        check_backup(gpg(persisted, decrypt=True), public)
        record = {'schemaVersion': 1, 'algorithm': 'Ed25519', 'publicKey': public, 'keyId': key_id,
                  'walletAttributes': ATTRIBUTES, 'backupFile': BACKUP, 'backupSHA256': hashlib.sha256(persisted).hexdigest(),
                  'walletReadback': 'passed', 'backupRecovery': 'passed', 'oneDriveUpload': 'not_run',
                  'createdAt': datetime.datetime.now(datetime.timezone.utc).isoformat()}
        write_new(directory, RECORD, (json.dumps(record, indent=2) + '\n').encode('ascii'))
        return {'status': 'created', 'keyId': key_id, 'directory': str(directory), 'walletReadback': 'passed',
                'backupRecovery': 'passed', 'oneDriveUpload': 'not_run', 'signature': 'not_run', 'publication': 'not_run'}
    finally:
        os.close(lock_fd)


def verify_backup(backup, public_file):
    public = read_file(public_file, 256).decode('ascii').strip()
    require(len(base64.b64decode(public, validate=True)) == 32, 'invalid_public_key')
    encrypted = read_file(backup, 8192)
    key_id = check_backup(gpg(encrypted, decrypt=True), public)
    return {'status': 'verified', 'keyId': key_id, 'backupSHA256': hashlib.sha256(encrypted).hexdigest(),
            'walletAccess': 'not_run', 'signature': 'not_run', 'upload': 'not_run'}


def main():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='mode', required=True)
    for mode in ('preflight', 'create'):
        sub.add_parser(mode)
    verify = sub.add_parser('verify-backup')
    verify.add_argument('--backup', type=Path, required=True)
    verify.add_argument('--public-key', type=Path, required=True)
    args = parser.parse_args()
    directory = Path.home() / '.local/share/sidravia-signing'
    try:
        if args.mode == 'verify-backup':
            result = verify_backup(args.backup, args.public_key)
        else:
            wallet = KWallet()
            result = preflight(directory, wallet) if args.mode == 'preflight' else create(directory, wallet)
        print(json.dumps(result))
        return 0
    except Exception as error:
        # Do not echo libsecret, GPG or parsing exceptions: they may include
        # arguments or plaintext. Existing ciphertext/wallet entries survive.
        code = str(error) if type(error) is CustodyError else 'custody_failed_preserve_existing_state'
        print(json.dumps({'status': 'failed', 'code': code, 'preserveExistingState': True}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
