"""Sign temporary ARM64 acceptance fixtures with a well-known test vector."""

import argparse
import base64
from pathlib import Path
import shutil
import subprocess
import sys


SEED = base64.b64encode(bytes(32))
PUBLIC = base64.b64encode(bytes.fromhex('3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29'))


def run(*args, input=None):
    return subprocess.run(args, input=input, check=True, stdout=subprocess.PIPE, timeout=210).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workspace', type=Path, required=True)
    parser.add_argument('--toolkit', type=Path, required=True)
    parser.add_argument('--build', required=True)
    args = parser.parse_args()
    root = args.workspace.resolve()
    key = root / 'fixture-public-key'
    key.write_bytes(PUBLIC)
    signer = root / 'signrelease.exe'
    image = root / 'references/windows-arm64/installerbase.exe'
    image.parent.mkdir(parents=True)
    shutil.copyfile(args.toolkit / 'installerbase.exe', image)
    releases = []
    for version in ('0.6.0-rc.1', '0.6.0'):
        source = root / version
        output = root / (version + '-signed')
        output.mkdir()
        base = f'sidravia-v{version}-windows-arm64-qt'
        setup = base + '-setup.exe'
        shutil.copyfile(source / setup, output / setup)
        archives = []
        for profile in ('core', 'gui'):
            name = f'{base}-{profile}-repository.zip'
            archive = output / name
            run(signer, 'qt-repository', '--input', source / name, '--output', archive, input=SEED)
            archives.append(archive)
        manifest = output / 'sidravia-release-qt.json'
        manifest.write_bytes(run(signer, 'qt-manifest', '--version', version, '--build-id', args.build,
                                 '--public-key', key, '--qt-installers', output,
                                 '--qt-maintenance', image.parent.parent, *archives))
        run(signer, '--manifest', manifest, '--output', output / 'sidravia-release-qt.sig', input=SEED)
        releases.append(output)
    request = run(sys.executable, 'tools/developer/prepare_qt_acceptance.py',
                  '--arch', 'arm64', '--previous', releases[0], '--target', releases[1],
                  '--maintenance', image.parent.parent, '--public-key', key,
                  '--test-program', root / 'qt-handoff.test.exe',
                  '--command-fixture', root / 'product-command-fixture')
    (root / 'qt-acceptance.json').write_bytes(request)


if __name__ == '__main__':
    main()
