"""Assemble a bridge release from the verified Package workflow artifacts."""

import argparse
import hashlib
import json
import re
import shutil
import tempfile
from pathlib import Path

from release_artifacts import document, plain_path, require


def stage(directory, version, build_id):
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

    def add_package(source, expected_hash):
        source = plain_path(source)
        name = source.name
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
        add_package(package, receipt['sha256'])

    with tempfile.TemporaryDirectory(prefix='.candidate-', dir=directory) as temporary:
        assets = Path(temporary) / 'assets'
        assets.mkdir()
        request_dir = Path(temporary) / 'requests'
        request_dir.mkdir()
        for arch in ('amd64', 'arm64'):
            target = f'windows-{arch}'
            base = f'sidravia-v{version}-{target}'
            lane = inputs / base
            add_distribution(lane, target, 'gui', [f'{base}-gui-portable.zip', f'{base}-setup.exe'],
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
    args = parser.parse_args()
    try:
        count = stage(args.directory, args.version, args.build_id)
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f'release candidate assembly failed: {error}\n')
    print(f'Assembled {count} verified packages and three unsigned manifest requests.')


if __name__ == '__main__':
    main()
