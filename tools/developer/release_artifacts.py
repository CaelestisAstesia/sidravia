"""Prepare or verify exact release metadata using existing distribution checks.

Public material only: no key generation/unlocking/signing, network, Git writes,
product execution or installation. Preparation is not a release approval.
"""
from __future__ import annotations

import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile

import verify_distribution as distribution


class ReleaseError(ValueError):
    pass


def require(condition, reason):
    if not condition:
        raise ReleaseError(reason)


def plain_path(path, directory=False):
    path = Path(os.path.abspath(path))
    for item in (path, *path.parents):
        info = item.lstat()
        require(not stat.S_ISLNK(info.st_mode) and not getattr(info, 'st_file_attributes', 0) & 0x400,
                'linked_input_path')
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), 'invalid_input_type')
    if not directory:
        require(info.st_nlink == 1, 'hard_linked_input')
    return path


def read_public(path, limit):
    path = plain_path(path)
    before = path.stat()
    require(0 < before.st_size <= limit, 'public_input_size')
    with path.open('rb') as handle:
        current = os.fstat(handle.fileno())
        require(os.path.samestat(before, current), 'input_identity_changed')
        data = handle.read(limit + 1)
    require(len(data) == before.st_size and os.path.samestat(current, path.stat()), 'input_changed')
    return data


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate_json_field')
        result[key] = value
    return result


def document(path):
    try:
        return json.loads(read_public(path, 256 << 10), object_pairs_hook=unique_object)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ReleaseError('invalid_public_json') from error


def public_identity(path):
    return public_identity_bytes(read_public(path,256))


def public_identity_bytes(data):
    try:
        key = base64.b64decode(data.strip(), validate=True)
    except ValueError as error:
        raise ReleaseError('invalid_public_key') from error
    require(len(key) == 32, 'invalid_public_key')
    return hashlib.sha256(key).hexdigest()


def request_document(path):
    request = document(path)
    require(isinstance(request, dict) and set(request) == {'schemaVersion', 'version', 'buildId', 'expectedAssets', 'distributions'},
            'request_fields')
    require(type(request['schemaVersion']) is int and request['schemaVersion'] == 1, 'request_schema')
    require(isinstance(request['version'], str) and isinstance(request['buildId'], str), 'request_identity_type')
    require(re.fullmatch('[0-9a-f]{40}', request['buildId']) is not None, 'request_build_commit')
    expected = request['expectedAssets']
    require(isinstance(expected, list) and 0 < len(expected) <= 32 and all(isinstance(n, str) and n and '/' not in n and '\\' not in n for n in expected), 'expected_asset_list')
    require(len(set(n.casefold() for n in expected)) == len(expected), 'duplicate_expected_asset')
    groups = request['distributions']
    require(isinstance(groups, list) and 0 < len(groups) <= 16, 'distribution_list')
    for group in groups:
        require(isinstance(group, dict) and {'directory', 'target'} <= set(group) <= {'directory', 'target', 'guiDirectory'}, 'distribution_fields')
        require(all(isinstance(value, str) and value for value in group.values()), 'distribution_value_type')
    return request


def collect(repo, request_path, public_key, go, *, request_snapshot=None):
    request_path = plain_path(request_path)
    request = request_document(request_path) if request_snapshot is None else request_snapshot
    key_id = public_identity(public_key)
    assets, receipts = [], []
    seen = set()
    for group in request['distributions']:
        directory = plain_path(request_path.parent / group['directory'], directory=True)
        gui = plain_path(request_path.parent / group['guiDirectory'], directory=True) if 'guiDirectory' in group else None
        # This is the existing independent source/content/ownership consumer.
        # No daemon, GUI, installer or help smoke is executed by this operation.
        receipt = distribution.verify(directory, repo, group['target'], request['version'], request['buildId'], gui=gui, go=go, smoke=False)
        os_name, arch = group['target'].split('-')
        for name, expected_hash in receipt['artifactHashes'].items():
            require(name.casefold() not in seen, 'duplicate_asset')
            seen.add(name.casefold())
            path = plain_path(directory / name)
            info = path.stat()
            digest = hashlib.sha256()
            with path.open('rb') as handle:
                require(os.path.samestat(info, os.fstat(handle.fileno())), 'artifact_identity_changed')
                for chunk in iter(lambda: handle.read(1 << 20), b''):
                    digest.update(chunk)
            require(os.path.samestat(info, path.stat()) and info.st_size == path.stat().st_size and digest.hexdigest() == expected_hash,
                    'artifact_changed_after_consumption')
            kind = 'setup' if name.endswith('-setup.exe') else receipt['payloadKind'] + '-portable'
            assets.append({'os': os_name, 'arch': arch, 'kind': kind, 'name': name, 'size': info.st_size,
                           'sha256': expected_hash, 'filesSha256': receipt['installerOwnershipSha256'] if kind == 'setup' else receipt['productOwnershipSha256'], 'payloadKind': receipt['payloadKind']})
        receipts.append(dict(receipt, directory=str(directory)))
    require(set(request['expectedAssets']) == {a['name'] for a in assets}, 'missing_or_unexpected_asset')
    result = {'schemaVersion': 2, 'keyId': key_id, 'version': request['version'], 'buildId': request['buildId'],
              'minimumProtocol': 1, 'assets': sorted(assets, key=lambda a: a['name'])}
    return result, receipts


def public_check(repo, go, mode, manifest_data, public_data, signature_data=None, *,
                 published_names=None, qt_artifacts=None, qt_maintenance=None):
    # Go verifies these exact captured bytes, never a second lookup of mutable
    # source paths. The private temporary directory contains public data only.
    with tempfile.TemporaryDirectory(prefix='sidravia-release-public-') as temporary:
        root = Path(temporary)
        manifest, public_key = root / 'manifest.json', root / 'public.txt'
        signature = root / 'signature.bin'
        if published_names is not None:
            manifest_name, signature_name = published_names
            (root / 'manifest').mkdir()
            (root / 'signature').mkdir()
            manifest = root / 'manifest' / Path(manifest_name).name
            signature = root / 'signature' / Path(signature_name).name
        manifest.write_bytes(manifest_data)
        public_key.write_bytes(public_data)
        command = [go, 'run', '-mod=readonly', './tools/signrelease', mode,
                   '--manifest', str(manifest), '--public-key', str(public_key), '--json']
        if signature_data is not None:
            signature.write_bytes(signature_data)
            command += ['--signature', str(signature)]
            if published_names is not None:
                command.append('--published')
        if qt_artifacts is not None:
            command += ['--qt-artifacts', str(qt_artifacts), '--qt-maintenance', str(qt_maintenance)]
        result = subprocess.run(command, cwd=repo, env=dict(os.environ, GOTOOLCHAIN='local'), capture_output=True, encoding='utf-8', timeout=120)
    require(result.returncode == 0, 'manifest_structure_or_signature_rejected')
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise ReleaseError('invalid_public_verifier_result') from error
    require(report.get('ok') is True and report.get('signature') == ('passed' if mode == 'verify' else 'not_checked'), 'public_verifier_result_mismatch')
    return report


def encoding_compatibility(manifest, path):
    extended_name = 'sidravia-release-extended.json'
    extended = [a for a in manifest['assets'] if a['arch'] == 'arm64' and a['kind'] in ('gui-portable', 'setup')]
    if extended or path.name == extended_name:
        require(len(extended) == len(manifest['assets']), 'mixed_legacy_and_extended_manifest')
        require(path.name == extended_name, 'extended_manifest_requires_separate_name')
        legacy_path = path.parent / 'sidravia-release.json'
        if os.path.lexists(legacy_path):
            legacy = document(legacy_path)
            require(isinstance(legacy, dict) and all(legacy.get(k) == manifest[k] for k in
                    ('schemaVersion', 'version', 'buildId', 'keyId', 'minimumProtocol')), 'extended_legacy_identity_mismatch')
        return
    xz_assets = [a for a in manifest['assets'] if a['name'].endswith('.tar.xz')]
    if not xz_assets:
        require(path.name != 'sidravia-release-xz.json', 'xz_manifest_requires_xz_assets')
        return
    require(len(xz_assets) == len(manifest['assets']), 'mixed_legacy_and_xz_manifest')
    require(path.name == 'sidravia-release-xz.json', 'xz_manifest_requires_separate_name')
    legacy = document(path.parent / 'sidravia-release.json')
    require(isinstance(legacy, dict), 'invalid_legacy_manifest')
    require(all(legacy.get(k) == manifest[k] for k in ('schemaVersion', 'version', 'buildId', 'keyId', 'minimumProtocol')), 'xz_legacy_identity_mismatch')
    require(isinstance(legacy.get('assets'), list) and all(isinstance(a, dict) and isinstance(a.get('name'), str) for a in legacy['assets']) and not any(a['name'].endswith('.tar.xz') for a in legacy['assets']), 'xz_in_legacy_manifest')
    for asset in xz_assets:
        matches = [a for a in legacy['assets'] if all(a.get(k) == asset[k] for k in ('os', 'arch', 'kind'))]
        require(len(matches) == 1 and matches[0]['name'] == asset['name'][:-3] + '.gz' and all(matches[0].get(k) == asset[k] for k in ('filesSha256', 'payloadKind')), 'xz_legacy_ownership_mismatch')


def prepare(repo, go, request_path, public_key, output, *, request_snapshot=None, before_publish=None):
    output = Path(os.path.abspath(output))
    require(not output.exists() and not output.is_symlink(), 'output_already_exists')
    plain_path(output.parent, directory=True)
    public_data = read_public(public_key,256)
    expected, receipts = collect(repo, request_path, public_key, go, request_snapshot=request_snapshot)
    require(expected['keyId'] == public_identity_bytes(public_data), 'public_key_changed')
    for receipt in receipts:
        require(not output.absolute().is_relative_to(Path(receipt['directory'])), 'manifest_output_inside_distribution')
    encoding_compatibility(expected, output)
    data = (json.dumps(expected, ensure_ascii=True, indent=2) + '\n').encode('utf-8')
    public_check(repo, go, 'inspect', data, public_data)
    with tempfile.TemporaryDirectory(prefix='.sidravia-manifest-', dir=output.parent) as temporary:
        complete = Path(temporary) / 'complete.json'
        with complete.open('xb') as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        if before_publish is not None:
            before_publish()
        # Reserve without overwriting a previous candidate, then publish
        # only the fully written bytes. An interrupted reservation is empty.
        with output.open('xb') as reservation:
            reserved = os.fstat(reservation.fileno())
        try:
            complete.replace(output)
        except OSError:
            with contextlib.suppress(OSError):
                if os.path.samestat(output.lstat(), reserved):
                    output.unlink()
            raise
    return summary(expected, receipts, 'not_checked', output)


def verify(repo, go, request_path, public_key, manifest, signature, *, request_snapshot=None):
    data = read_public(manifest, 256 << 10)
    public_data = read_public(public_key,256)
    signature_data = read_public(signature,64)
    if Path(manifest).name == 'sidravia-release-extended.json':
        require(Path(signature).name == 'sidravia-release-extended.sig', 'extended_signature_requires_separate_name')
    expected, receipts = collect(repo, request_path, public_key, go, request_snapshot=request_snapshot)
    encoding_compatibility(expected, Path(manifest))
    require(expected['keyId'] == public_identity_bytes(public_data), 'public_key_changed')
    try:
        parsed = json.loads(data,object_pairs_hook=unique_object)
    except (UnicodeError,json.JSONDecodeError) as error:
        raise ReleaseError('invalid_manifest_json') from error
    require(parsed == expected, 'manifest_does_not_match_artifacts')
    public_check(repo, go, 'verify', data, public_data, signature_data)
    require(read_public(manifest, 256 << 10) == data and read_public(public_key,256) == public_data and read_public(signature,64) == signature_data,
            'public_input_changed_during_verification')
    return summary(expected, receipts, 'passed', manifest)


def summary(manifest, receipts, signature, output):
    return {'schema': 1, 'status': 'passed', 'version': manifest['version'], 'buildId': manifest['buildId'],
            'keyId': manifest['keyId'], 'manifest': str(output), 'assets': len(manifest['assets']),
            'artifactBytes': 'passed', 'signature': signature, 'distributions': receipts,
            'nativeUpgrade': 'not_run', 'publicDownload': 'not_run', 'publication': 'not_run'}


def run(args, repo, source, *, before_publish=None):
    request_path = plain_path(args.request)
    request = request_document(request_path)
    require(not source['dirty'] and source['commit'] == request['buildId'], 'clean_source_commit_required')
    public_key = plain_path(args.public_key)
    if args.mode == 'prepare':
        def recheck():
            if before_publish is not None:
                before_publish()
            require(request_document(request_path) == request, 'request_changed_during_validation')
        return prepare(repo, args.go, request_path, public_key, Path(args.output),
                       request_snapshot=request, before_publish=recheck)
    result = verify(repo, args.go, request_path, public_key, Path(args.manifest), Path(args.signature), request_snapshot=request)
    require(result['buildId'] == source['commit'], 'release_source_commit_mismatch')
    require(request_document(request_path) == request, 'request_changed_during_validation')
    return result
