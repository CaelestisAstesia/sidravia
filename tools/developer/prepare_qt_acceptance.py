"""Create a pytest lifecycle request from two verified Windows Qt releases."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

from release_artifacts import (
    QT_MANIFEST, QT_SIGNATURE, WINDOWS_ARCHITECTURES, plain_path, public_check,
    read_public, require, unique_object,
)
from verify_distribution import matches_pe


ROOT = Path(__file__).resolve().parents[2]


def prepare_command_fixture(directory, request):
    directory = directory.resolve()
    directory.mkdir()
    feed = directory / 'feed'
    manifest_path = Path(request['manifest'])
    manifest = json.loads(manifest_path.read_bytes())
    version = manifest['version']
    files = feed / version
    files.mkdir(parents=True)
    shutil.copyfile(manifest_path, files / QT_MANIFEST)
    shutil.copyfile(request['signature'], files / QT_SIGNATURE)
    assets = []
    for asset in manifest['assets']:
        if asset['arch'] != request['arch'] or asset['kind'] not in ('qt-core-repository', 'qt-gui-repository'):
            continue
        name = asset['name']
        shutil.copyfile(manifest_path.parent / name, files / name)
        assets.append(dict(id=len(assets) + 1, name=name, size=asset['size'], state='uploaded',
                           digest='sha256:' + asset['sha256'],
                           browser_download_url=f'https://github.com/CaelestisAstesia/sidravia/releases/download/v{version}/{name}'))
    require(len(assets) == 2, 'qt_command_fixture_requires_both_profiles')
    (feed / 'api.json').write_text(
        json.dumps([dict(id=1, tag_name='v' + version, prerelease='-' in version, assets=assets)]),
        encoding='utf-8',
    )
    with Path(request['maintenanceImage']).open('rb') as image:
        maintenance_digest = hashlib.file_digest(image, 'sha256').hexdigest()
    build_request = directory / 'commands.json'
    build_request.write_text(json.dumps(dict(
        repo=str(ROOT), feed=str(feed), sourceVersion=request['sourceVersion'],
        sourceBuildID=request['sourceBuildId'], publicKey=request['publicKey'],
        maintenanceSHA256=maintenance_digest,
    )), encoding='utf-8')
    subprocess.run([request['testProgram'], '-test.run=^TestQtBuildCommandFixture$', '-test.v'],
                   env=dict(os.environ, SIDRAVIA_TEST_QT_COMMAND_FIXTURE=str(build_request)),
                   stdout=sys.stderr, check=True, timeout=180)
    commands = directory / 'commands'
    for name in ('sidraviactl.exe', 'sidraviaupdater.exe'):
        require((commands / name).is_file(), 'qt_command_fixture_missing_binary')
    request['productCommands'] = str(commands)


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
    parser.add_argument('--command-fixture', type=Path,
                        help='new isolated directory for testing discovery, download and apply through the product commands')
    args = parser.parse_args()
    try:
        request = prepare(args.previous, args.target, args.maintenance, args.test_program,
                          args.public_key, args.arch, args.go)
        if args.command_fixture is not None:
            prepare_command_fixture(args.command_fixture, request)
        print(json.dumps(request, indent=2))
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        parser.exit(1, f'Qt acceptance request failed: {error}\n')


if __name__ == '__main__':
    main()
