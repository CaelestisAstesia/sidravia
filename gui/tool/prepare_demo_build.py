"""Copy only GUI sources into a new isolated native/web demo build directory."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('destination', type=Path)
args = parser.parse_args()
root = Path(__file__).resolve().parents[2]
destination = args.destination.resolve()
if destination.exists() or destination.is_relative_to(root):
    raise SystemExit('Destination must be new and outside the repository')
paths = set(subprocess.check_output(['git', 'ls-files', 'gui'], cwd=root, text=True).splitlines())
paths.update([
    'gui/lib/dev/interactive_demo_main.dart', 'gui/lib/dev/offline_demo_client.dart',
])
manifest = {}
for name in sorted(paths):
    if name.startswith(('gui/archive/', 'gui/test/', 'gui/tool/')):
        continue
    source = root / name
    if not source.is_file():
        continue
    target = destination / Path(name).relative_to('gui')
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)
    manifest[name] = hashlib.sha256(source.read_bytes()).hexdigest()
(destination / 'DEMO-SOURCE-MANIFEST.json').write_text(json.dumps({
    'base': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
    'workingTree': subprocess.check_output(['git', 'status', '--short'], cwd=root, text=True),
    'sourceSha256': manifest,
}, indent=2), encoding='utf-8')
print(destination)
