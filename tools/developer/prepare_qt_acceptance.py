"""Create a pytest lifecycle request from two verified Windows Qt releases."""

import argparse
import json
from pathlib import Path
import subprocess

from release_artifacts import (
    QT_MANIFEST, QT_SIGNATURE, WINDOWS_ARCHITECTURES, plain_path, public_check,
    read_public, require, unique_object,
)
from verify_distribution import matches_pe


ROOT = Path(__file__).resolve().parents[2]


def prepare(previous, target, maintenance, test_program, public_key, arch, go):
    require(arch in WINDOWS_ARCHITECTURES, 'unsupported_qt_acceptance_architecture')
    maintenance = plain_path(maintenance, directory=True)
    image = plain_path(maintenance / f'windows-{arch}' / 'installerbase.exe')
    test_program = plain_path(test_program)
    with test_program.open('rb') as handle:
        require(matches_pe(handle.read(4096), arch), 'qt_acceptance_test_program_architecture_mismatch')
    public_data = read_public(public_key, 256)

    def verified_release(directory, *, after_version=None):
        directory = plain_path(directory, directory=True)
        manifest = directory / QT_MANIFEST
        signature = directory / QT_SIGNATURE
        manifest_data = read_public(manifest, 256 << 10)
        signature_data = read_public(signature, 64)
        report = public_check(ROOT, go, 'verify', manifest_data, public_data, signature_data,
                              published_names=(QT_MANIFEST, QT_SIGNATURE),
                              qt_artifacts=directory, qt_maintenance=maintenance, after_version=after_version)
        require(report.get('artifactBytes') == report.get('installerBytes') == 'passed',
                'qt_acceptance_release_verification_incomplete')
        assets = json.loads(manifest_data, object_pairs_hook=unique_object)['assets']
        setups = [asset for asset in assets if asset['arch'] == arch and asset['kind'] == 'qt-setup']
        require(len(setups) == 1, 'qt_acceptance_target_architecture_missing')
        return report, manifest, signature, setups[0]

    source, source_manifest, _, installer = verified_release(previous)
    _, manifest, signature, _ = verified_release(target, after_version=source['version'])
    setup = plain_path(source_manifest.parent / installer['name'])
    return dict(arch=arch, sourceVersion=source['version'], sourceBuildId=source['buildId'],
                setup=str(setup), setupSHA256=installer['sha256'], setupSize=installer['size'],
                maintenanceImage=str(image), testProgram=str(test_program),
                repositories=str(manifest.parent), manifest=str(manifest), signature=str(signature),
                publicKey=public_data.decode('ascii').strip())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--previous', type=Path, required=True, help='signed older Qt release directory')
    parser.add_argument('--target', type=Path, required=True, help='signed target Qt release directory')
    parser.add_argument('--maintenance', type=Path, required=True, help='directory of Qt maintenance reference images')
    parser.add_argument('--test-program', type=Path, required=True)
    parser.add_argument('--public-key', type=Path, required=True, help='trusted disposable fixture public key file')
    parser.add_argument('--arch', choices=WINDOWS_ARCHITECTURES, required=True)
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    try:
        print(json.dumps(prepare(args.previous, args.target, args.maintenance, args.test_program,
                                 args.public_key, args.arch, args.go), indent=2))
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        parser.exit(1, f'Qt acceptance request failed: {error}\n')


if __name__ == '__main__':
    main()
