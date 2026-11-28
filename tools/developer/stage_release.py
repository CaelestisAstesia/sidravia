"""Assemble a bridge release from the verified Package workflow artifacts."""

import argparse
import hashlib
import json
import re
import shutil
import tempfile
from pathlib import Path

from release_artifacts import (
    QT_MANIFEST, QT_SIGNATURE, WINDOWS_ARCHITECTURES, document, plain_path,
    public_check, public_identity_bytes, read_public, require, unique_object,
)


SOURCE = Path(__file__).resolve().parents[2]


def add_qt_release(lane, assets, version, build_id, architectures, publisher_key, go, add_package):
    source = plain_path(lane / 'assets', directory=True)
    toolchains = plain_path(lane / 'toolchains', directory=True)
    manifest_data = read_public(source / QT_MANIFEST, 256 << 10)
    signature_data = read_public(source / QT_SIGNATURE, 64)
    public_data = read_public(publisher_key, 256)
    publisher_id = public_identity_bytes(public_data)
    for arch in architectures:
        target = f'windows-{arch}'
        receipt_path = lane.parent / f'sidravia-v{version}-{target}' / 'qt-toolkit.json'
        receipt = document(receipt_path)
        require(isinstance(receipt, dict), f'invalid_qt_toolkit_receipt: {target}')
        image = plain_path(toolchains / target / 'installerbase.exe')
        key = receipt.get('publisherPublicKey', '')
        require(receipt.get('target') == target and isinstance(key, str) and re.fullmatch(r'[0-9a-f]{64}', key)
                and hashlib.sha256(bytes.fromhex(key)).hexdigest() == publisher_id,
                f'qt_toolkit_publisher_mismatch: {target}')
        with image.open('rb') as handle:
            require(hashlib.file_digest(handle, 'sha256').hexdigest() == receipt.get('maintenanceSHA256'),
                    f'qt_toolkit_maintenance_mismatch: {target}')
    manifest = json.loads(manifest_data, object_pairs_hook=unique_object)
    require((manifest['version'], manifest['buildId']) == (version, build_id), 'qt_release_identity_mismatch')
    expected = {
        f'sidravia-v{version}-windows-{arch}-{suffix}'
        for arch in architectures
        for suffix in ('qt-setup.exe', 'qt-core-repository.zip', 'qt-gui-repository.zip')
    }
    signed = manifest['assets']
    require(len(signed) == len(expected) and {item['name'] for item in signed} == expected,
            'qt_release_assets_mismatch')
    for item in signed:
        add_package(source / item['name'], item['sha256'])
    report = public_check(SOURCE, go, 'verify', manifest_data, public_data, signature_data,
                          published_names=(QT_MANIFEST, QT_SIGNATURE),
                          qt_artifacts=assets, qt_maintenance=toolchains)
    require(report.get('artifactBytes') == report.get('installerBytes') == 'passed',
            'qt_release_artifact_verification_incomplete')
    (assets / QT_MANIFEST).write_bytes(manifest_data)
    (assets / QT_SIGNATURE).write_bytes(signature_data)


def stage(directory, version, build_id, *, qt_publisher_key, go='go'):
    require(re.fullmatch(r'[0-9][0-9A-Za-z.+-]*', version), 'invalid_version')
    require(re.fullmatch(r'[0-9a-f]{40}', build_id), 'invalid_build_commit')
    inputs = plain_path(directory / 'inputs', directory=True)
    require(not any((directory / name).exists() for name in ('assets', 'requests')),
            'candidate_output_already_exists')
    hashes = {}
    requests = {
        name: dict(schemaVersion=1, version=version, buildId=build_id,
                   expectedAssets=[], distributions=[])
        for name in ('sidravia-release', 'sidravia-release-xz', 'sidravia-release-extended')
    }

    def add_package(source, expected_hash, *, public_name=None):
        source = plain_path(source)
        name = public_name or source.name
        require(name not in hashes and source.stat().st_size > 0, f'duplicate_or_empty_asset: {name}')
        staged = assets / name
        shutil.copyfile(source, staged)
        with staged.open('rb') as handle:
            actual_hash = hashlib.file_digest(handle, 'sha256').hexdigest()
        require(actual_hash == expected_hash, f'asset_checksum_mismatch: {name}')
        hashes[name] = actual_hash

    def add_distribution(lane, target, kind, names, manifest, *, group=None, receipt=None):
        source = plain_path(lane / (group or kind), directory=True)
        receipt = document(lane / (receipt or ('verification.json' if kind == 'gui' else 'core-verification.json')))
        require((receipt['version'], receipt['buildId'], receipt['target'], receipt['payloadKind'])
                == (version, build_id, target, kind), f'distribution_identity_mismatch: {source}')
        require(set(receipt['artifactHashes']) == set(names), f'distribution_names_mismatch: {source}')
        require({item.name for item in source.iterdir()} == set(names) | {'SHA256SUMS.txt'},
                f'distribution_files_mismatch: {source}')
        ownership = receipt['productOwnershipSha256']
        require(re.fullmatch(r'[0-9a-f]{64}', ownership), f'invalid_ownership: {source}')
        for name in names:
            add_package(source / name, receipt['artifactHashes'][name])
        request = requests[manifest]
        entry = {'directory': '../inputs/' + source.relative_to(inputs).as_posix(), 'target': target}
        if kind == 'gui':
            gui = plain_path(lane / 'gui-input', directory=True)
            entry['guiDirectory'] = '../inputs/' + gui.relative_to(inputs).as_posix()
        request['expectedAssets'].extend(names)
        request['distributions'].append(entry)
        return ownership

    def add_system_package(source, arch, kind, extension):
        package, = source.glob('*.' + extension)
        receipt = document(package.with_name(package.name + '.json'))
        architecture = {'amd64': 'x86_64', 'arm64': 'aarch64'}[arch] if extension == 'rpm' else arch
        require((receipt['version'], receipt['source']['commit'], receipt['kind'],
                 receipt['architecture'], receipt['package'])
                == (version, build_id, kind, architecture, package.name),
                f'system_package_identity_mismatch: {package.name}')
        require(receipt['source_unchanged'] is True and receipt['source']['dirty'] is False,
                f'system_package_source_changed: {package.name}')
        # GitHub rewrites tildes in release asset names. Use the public alias
        # when staging checksums, while validating the original producer name.
        add_package(package, receipt['sha256'], public_name=package.name.replace('~', '-'))

    with tempfile.TemporaryDirectory(prefix='.candidate-', dir=directory) as temporary:
        assets = Path(temporary) / 'assets'
        assets.mkdir()
        request_dir = Path(temporary) / 'requests'
        request_dir.mkdir()
        add_qt_release(inputs / 'qt-release', assets, version, build_id, WINDOWS_ARCHITECTURES,
                       qt_publisher_key, go, add_package)
        for arch in WINDOWS_ARCHITECTURES:
            target = f'windows-{arch}'
            base = f'sidravia-v{version}-{target}'
            lane = inputs / base
            add_distribution(lane, target, 'gui', [f'{base}-gui-portable.zip'],
                             'sidravia-release-extended' if arch == 'arm64' else 'sidravia-release')
            add_distribution(lane, target, 'core', [f'{base}-core-portable.zip'], 'sidravia-release')
            target = f'linux-{arch}'
            base = f'sidravia-v{version}-{target}'
            lane = inputs / (base + '-portable')
            for kind in ('gui', 'core'):
                extended = arch == 'arm64' and kind == 'gui'
                prefix = f'{base}-{kind}-portable.tar.'
                gzip = add_distribution(lane, target, kind, [prefix + 'gz'],
                                        'sidravia-release-extended' if extended else 'sidravia-release')
                xz = add_distribution(lane, target, kind, [prefix + 'xz'],
                                      'sidravia-release-extended' if extended else 'sidravia-release-xz',
                                      group=kind + '-xz', receipt=kind + '-xz-verification.json')
                require(gzip == xz, f'portable_ownership_mismatch: {prefix}')
                for extension in ('rpm', 'deb'):
                    add_system_package(inputs / (base + '-' + extension) / kind, arch, kind, extension)
        (assets / 'SHA256SUMS.txt').write_text(
            ''.join(f'{digest}  {name}\n' for name, digest in sorted(hashes.items())), encoding='ascii')
        for name, request in requests.items():
            (request_dir / (name + '.request.json')).write_text(
                json.dumps(request, indent=2) + '\n', encoding='utf-8')
        assets.rename(directory / 'assets')
        request_dir.rename(directory / 'requests')
    return len(hashes)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--build-id', required=True)
    parser.add_argument('--qt-publisher-key', type=Path, required=True,
                        help='verify and stage the signed Qt lane using this trusted public key')
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    try:
        count = stage(args.directory, args.version, args.build_id, qt_publisher_key=args.qt_publisher_key, go=args.go)
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f'release candidate assembly failed: {error}\n')
    print(f'Assembled {count} verified packages and three unsigned manifest requests.')
    print('Included a verified Qt manifest and signature.')


if __name__ == '__main__':
    main()
