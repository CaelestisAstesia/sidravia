"""Deterministic real-page captures and conservative visual difference triage."""
from __future__ import annotations

import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys

import validation_run as runner
from verification_commands import worktree_lock

SCENARIO_VERSION = 1
DEFAULT_FRAMES = [
    {'id': f'{width}-{theme}', 'width': width, 'height': height, 'scale': 1.0, 'dark': theme == 'dark'}
    for width, height in ((400, 800), (960, 700)) for theme in ('light', 'dark')
]


def pillow():
    try:
        from PIL import Image, ImageChops
    except ImportError:
        raise ValueError('Pillow is required for PNG inspection/comparison (Fedora python3-pillow)') from None
    return Image, ImageChops


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def capture_input(path):
    request = runner.read_json(path) if path else {}
    if not isinstance(request, dict) or set(request) - {'schema', 'fixedTime', 'platform', 'frames'} or request.get('schema', 1) != 1:
        raise ValueError('capture request fields: schema=1, fixedTime, platform, frames')
    fixed = request.get('fixedTime', '2026-01-02T12:34:56.000Z')
    if not isinstance(fixed, str):
        raise ValueError('fixedTime must be an ISO timestamp with timezone')
    moment = datetime.datetime.fromisoformat(fixed.replace('Z', '+00:00'))
    if moment.tzinfo is None:
        raise ValueError('fixedTime requires an explicit timezone')
    fixed = moment.astimezone(datetime.timezone.utc).isoformat(timespec='milliseconds').replace('+00:00', 'Z')
    target = request.get('platform', 'linux')
    if target not in ('linux', 'windows'):
        raise ValueError('capture platform is a simulated linux/windows theme, not native acceptance')
    frames = request.get('frames', DEFAULT_FRAMES)
    validate_frames(frames)
    return {'schema': 1, 'fixedTime': fixed, 'platform': target, 'frames': frames}



def validate_frames(frames):
    if not isinstance(frames, list) or not 1 <= len(frames) <= 16:
        raise ValueError('request requires 1..16 frames')
    seen = set()
    for frame in frames:
        if not isinstance(frame, dict) or set(frame) != {'id', 'width', 'height', 'scale', 'dark'}:
            raise ValueError('each frame requires id,width,height,scale,dark')
        name = frame['id']
        if not isinstance(name, str) or not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}', name) or name in seen:
            raise ValueError('frame ids must be unique short identifiers')
        seen.add(name)
        if any(type(frame[k]) is not int or not 280 <= frame[k] <= 1600 for k in ('width', 'height')):
            raise ValueError('frame dimensions must be integers in 280..1600')
        if type(frame['scale']) not in (int, float) or not .8 <= frame['scale'] <= 3 or type(frame['dark']) is not bool:
            raise ValueError('frame requires scale in .8..3 and boolean dark')


def font_input(path):
    fonts = runner.read_json(path)
    if not isinstance(fonts, dict) or not fonts:
        raise ValueError('font manifest must map font families to existing font file arrays')
    metadata = {}
    resolved = {}
    for family, files in fonts.items():
        if not isinstance(family, str) or not family or not isinstance(files, list) or not files:
            raise ValueError('invalid font family or empty font list')
        paths = []
        for file in files:
            if not isinstance(file, str) or not Path(file).is_absolute():
                raise ValueError('font paths must be explicit absolute paths')
            p = Path(file).resolve(strict=True)
            if not p.is_file():
                raise ValueError('font is not a regular file')
            paths.append(str(p))
        resolved[family] = paths
        metadata[family] = [sha(p) for p in paths]
    return resolved, metadata



def validate_cjk_coverage(fonts, families):
    from PIL import ImageFont
    paths = {p for family in families for p in fonts.get(family, [])}
    if not paths:
        raise ValueError('no explicitly loaded ordinary font families')
    loaded = [ImageFont.truetype(path,20) for path in paths]
    def signature(font, text):
        mask = font.getmask(text)
        return mask.size, bytes(mask)
    for character in '校园网':
        if not any(signature(font,character) != signature(font,'\U0010ffff') for font in loaded):
            raise ValueError('selected ordinary font families lack Chinese capture glyphs')

def capture(root, args, identity):
    Image, _ = pillow()
    config = capture_input(args.request)
    fonts, font_hashes = font_input(args.font_manifest)
    flutter = str(Path(args.flutter).resolve())
    output = Path(args.output).resolve()
    if output.exists():
        raise ValueError('capture output must not exist; baselines are never overwritten')
    with worktree_lock(root):
        before = identity()
        output.mkdir(parents=True)
        images = output/'images'
        images.mkdir()
        runner.write_json(output/'fonts.json', fonts)
        # The same stage runner owns SDK version command, deadlines and logs.
        request_path = output/'capture-request.json'
        runner.write_json(request_path, dict(config, output=str(images), sdkInfo=str(output/'gui-sdk.stdout.log')))
        commands = [
            {'name': 'gui-sdk', 'argv': [sys.executable, str(root/'tools/developer/verification_commands.py'), '--sdk', flutter, str(root/'gui/.flutter-version')]},
            {'name': 'capture-inputs', 'argv': [sys.executable, str(root/'tools/developer/visual_checks.py'), '--record-inputs', str(output/'gui-sdk.stdout.log'), str(output/'fonts.json'), str(output/'inputs.json')]},
            {'name': 'gui-dependencies', 'argv': [flutter, 'pub', 'get', '--enforce-lockfile']},
            {'name': 'gui-capture', 'argv': [flutter, 'test', '--no-pub', '--concurrency=1', '--dart-define=GUI_CAPTURE_REQUEST='+str(request_path), '--dart-define=SYSTEM_FONT_MANIFEST='+str(output/'fonts.json'), 'test/visual/gui_capture_test.dart']},
        ]
        request = {'schema': 1, 'source': before, 'environment': {'TZ': 'UTC'}, 'stages': [dict(c, cwd=str(root/'gui'), timeout_seconds=args.stage_timeout) for c in commands]}
        runner.write_json(output/'request.json', request)
        report = runner.run_request(request, output)
        report.update(kind='visual-capture', cases=[])
        try:
            if report['status'] == 'passed':
                sdk = json.loads((output/'gui-sdk.stdout.log').read_text(encoding='utf-8'))
                inputs = runner.read_json(output/'inputs.json')
                sdk_license = Path(inputs['licensePath'])
                if inputs['fonts'] != font_hashes or font_input(args.font_manifest)[1] != font_hashes or sha(sdk_license) != inputs['license']:
                    raise ValueError('external font/SDK license inputs changed during capture')
                inventory = runner.read_json(images/'cases.json')['cases']
                if len(inventory) != 27 * len(config['frames']):
                    raise ValueError('actual-page capture inventory is incomplete (27 cases per frame)')
                for families in {tuple(c.get('fontFamilies', [])) for c in inventory}:
                    validate_cjk_coverage(fonts, families)
                frames = {f['id']: f for f in config['frames']}
                seen = set()
                for case in inventory:
                    name = case['file']
                    if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9_-]+\.png', name) or case['id'] in seen or case['frame'] not in frames or name != case['id']+'.png' or not case['id'].startswith(case['frame']+'-'):
                        raise ValueError('invalid or duplicate capture inventory')
                    seen.add(case['id'])
                    path = images/name
                    with Image.open(path) as image:
                        size = list(image.size)
                    frame = frames[case['frame']]
                    if size != [frame['width'], frame['height']]:
                        raise ValueError('capture dimensions do not match frame')
                    report['cases'].append(dict(case, file='images/'+name, sha256=sha(path), size=size, frame=frame))
                if any(sum(c['frame']['id'] == frame_id for c in report['cases']) != 27 for frame_id in frames):
                    raise ValueError('capture matrix frame coverage is incomplete')
                report['context'] = {
                    'scenarioVersion': SCENARIO_VERSION, 'fixedTime': config['fixedTime'], 'timezone': 'UTC',
                    'targetPlatform': config['platform'], 'renderer': 'flutter-widget-dpr1',
                    'host': [sys.platform, platform.machine()],
                    'flutter': {k: sdk[k] for k in ('frameworkVersion', 'engineRevision')},
                    'fonts': font_hashes, 'diagnosticFont': sha(root/'gui/assets/fonts/SourceCodePro-Regular.otf'),
                    'license': inputs['license'],
                }
        except Exception as error:
            report.update(status='failed', error=type(error).__name__+': '+str(error))
        report['source_after'] = identity()
        report['source_unchanged'] = before == report['source_after']
        if not report['source_unchanged']:
            report.update(status='failed', source_error='source changed during capture')
        runner.write_json(output/'report.json', report)
    if args.format == 'json':
        # Full image inventory belongs in the report; keep stdout compact.
        summary = {k:v for k,v in report.items() if k not in ('cases', 'context')}
        summary['image_count'] = len(report['cases'])
        runner.present(summary, output/'report.json', 'json')
    else:
        runner.present(report, output/'report.json')
        print('Images: '+str(len(report['cases'])))
    return 0 if report['status'] == 'passed' else 1


def record_inputs(sdk_log, font_manifest, output):
    sdk = runner.read_json(sdk_log)
    license_path = Path(sdk['flutterRoot'])/'LICENSE'
    _, fonts = font_input(font_manifest)
    runner.write_json(output, {'fonts': fonts, 'license': sha(license_path), 'licensePath': str(license_path)})




def load_capture(directory):
    Image, _ = pillow()
    root = Path(directory).resolve()
    report = runner.read_json(root/'report.json')
    if not isinstance(report, dict) or type(report.get('schema')) is not int or report.get('schema') != 1 or report.get('kind') != 'visual-capture' or report.get('status') != 'passed' or report.get('source_unchanged') is not True:
        raise ValueError('only successful, source-stable visual capture receipts can be compared')
    context = report.get('context')
    required = {'scenarioVersion','fixedTime','timezone','targetPlatform','renderer','host','flutter','fonts','diagnosticFont','license'}
    if not isinstance(context, dict) or set(context) != required:
        raise ValueError('capture environment metadata is missing or unsupported')
    source = report.get('source')
    if not isinstance(source, dict) or not re.fullmatch(r'[0-9a-f]{40}', str(source.get('commit',''))) or not re.fullmatch(r'[0-9a-f]{64}', str(source.get('source_fingerprint',''))):
        raise ValueError('capture source identity is missing or invalid')
    cases = report.get('cases')
    if not isinstance(cases, list) or not 1 <= len(cases) <= 432:
        raise ValueError('invalid capture inventory')
    index = {}
    for case in cases:
        if not isinstance(case, dict):
            raise ValueError('capture case must be an object')
        validate_frames([case.get('frame')])
        name = case.get('id')
        if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,159}', name) or name in index or case.get('file') != 'images/'+name+'.png' or not name.startswith(case['frame']['id']+'-'):
            raise ValueError('invalid or duplicate image path/id')
        if type(case.get('timezoneOffsetSeconds')) is not int or case['timezoneOffsetSeconds'] != 0:
            raise ValueError('capture local-time offset is missing or not UTC')
        path = root/case['file']
        if path.is_symlink() or (root/'images').is_symlink() or sha(path) != case.get('sha256'):
            raise ValueError('capture artifact changed after capture: '+name)
        with Image.open(path) as image:
            if image.format != 'PNG' or list(image.size) != case.get('size') or not all(1 <= n <= 1600 for n in image.size):
                raise ValueError('invalid image format/dimensions: '+name)
            image.verify()
        index[name] = dict(case, path=path)
    return root, report, index


def image_difference(before, after, before_sha=None, after_sha=None):
    Image, ImageChops = pillow()
    before_bytes, after_bytes = Path(before).read_bytes(), Path(after).read_bytes()
    if (before_sha and hashlib.sha256(before_bytes).hexdigest() != before_sha) or (after_sha and hashlib.sha256(after_bytes).hexdigest() != after_sha):
        raise ValueError('capture artifact changed while comparing')
    with Image.open(io.BytesIO(before_bytes)) as a, Image.open(io.BytesIO(after_bytes)) as b:
        render_metadata_changed = any(a.info.get(k) != b.info.get(k) for k in ('icc_profile','gamma','srgb','chromaticity','exif'))
        a = a.convert('RGBA')
        b = b.convert('RGBA')
        if a.size != b.size:
            return {'kind':'resized','before_size':list(a.size),'after_size':list(b.size)}, None
        # getbbox() on an RGBA diff may inspect only alpha. Include ALL four
        # channels, or an opaque text/color change would disappear entirely.
        channels = ImageChops.difference(a,b).split()
        mask = ImageChops.lighter(ImageChops.lighter(channels[0],channels[1]),ImageChops.lighter(channels[2],channels[3]))
        box = mask.getbbox()
        if box is None:
            return {'kind':'metadata_changed' if render_metadata_changed else 'unchanged','changed_pixels':0}, None
        count = sum(mask.histogram()[1:])
        binary = mask.point(lambda x: 255 if x else 0)
        zero = Image.new('L',a.size)
        highlight = Image.merge('RGB',(binary,zero,zero))
        return {'kind':'changed','changed_pixels':count,'pixel_ratio':count/(a.width*a.height),'bbox':list(box)}, highlight


def gallery(output, title, summary, cards):
    import html
    body = ''.join(cards)
    document = '''<!doctype html><html lang="en"><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>'''+html.escape(title)+'''</title><style>
body{font:15px system-ui,sans-serif;margin:24px;background:#f4f5f7;color:#18212b}
article{background:white;border:1px solid #ccd2d9;border-radius:10px;padding:16px;margin:20px 0}
.images{display:flex;gap:12px;align-items:flex-start;overflow:auto}figure{margin:0;min-width:0}
img{max-width:480px;height:auto;border:1px solid #ccd2d9}figcaption{padding:5px}
pre{white-space:pre-wrap;overflow-wrap:anywhere}summary{cursor:pointer}a{color:#235f98}
</style><h1>'''+html.escape(title)+'</h1><pre>'+html.escape(summary)+'</pre>'+body+'</html>'
    (Path(output)/'index.html').write_text(document,encoding='utf-8')


def image_link(path, output, label):
    import html
    from urllib.parse import quote
    relative = os.path.relpath(path,output).replace(os.sep,'/')
    return '<figure><a href="'+html.escape(quote(relative),quote=True)+'"><img loading="lazy" src="'+html.escape(quote(relative),quote=True)+'"></a><figcaption>'+html.escape(label)+'</figcaption></figure>'


def compare(args):
    import html
    import time
    before_root, before_report, before = load_capture(args.baseline)
    after_root, after_report, after = load_capture(args.candidate)
    output = Path(args.output).resolve()
    if output.exists() or output.is_relative_to(before_root) or output.is_relative_to(after_root):
        raise ValueError('comparison output must be new and outside both captures')
    output.mkdir(parents=True)
    started = time.monotonic()
    report = {'schema':1,'kind':'visual-comparison','status':'failed','baseline':str(before_root),'candidate':str(after_root),'sources':[before_report['source'],after_report['source']],'cases':[],'needs_review':[]}
    cards = []
    context = before_report['context']
    incompatible = [k for k in context if context[k] != after_report['context'][k]]
    report['incompatible_fields'] = incompatible
    if incompatible:
        report['status'] = 'incompatible'
        report['summary'] = {'before':len(before),'after':len(after),'review':0,'unchanged':0}
        cards.append('<p>Environment/input mismatch: '+html.escape(', '.join(incompatible))+'. No visual pass is claimed.</p>')
    else:
        (output/'changes').mkdir()
        unchanged = []
        for name in sorted(set(before)|set(after)):
            a,b = before.get(name),after.get(name)
            image = None
            if a is None:
                row = {'kind':'added'}
            elif b is None:
                row = {'kind':'removed'}
            else:
                row,image = image_difference(a['path'],b['path'],a['sha256'],b['sha256'])
                if a.get('frame') != b.get('frame') and row['kind'] == 'unchanged':
                    row['kind'] = 'frame_changed'
            row['id'] = name
            report['cases'].append(row)
            if row['kind'] == 'unchanged':
                unchanged.append(name)
                continue
            report['needs_review'].append(name)
            figures = ''
            if a: figures += image_link(a['path'],output,'Before '+str(a['size']))
            if b: figures += image_link(b['path'],output,'After '+str(b['size']))
            if image is not None:
                target = output/'changes'/(name+'.png')
                image.save(target)
                image.close()
                row['difference'] = 'changes/'+name+'.png'
                figures += image_link(target,output,'Exact changed pixels (red)')
            cards.append('<article><h2>'+html.escape(name)+' — '+row['kind']+'</h2><pre>'+html.escape(json.dumps(row,ensure_ascii=False))+'</pre><div class="images">'+figures+'</div></article>')
        report['status'] = 'changes' if report['needs_review'] else 'passed'
        counts = {kind:sum(c['kind']==kind for c in report['cases']) for kind in ('unchanged','changed','resized','added','removed','frame_changed','metadata_changed')}
        report['summary'] = dict(counts,review=len(report['needs_review']))
        cards.append('<details><summary>Unchanged: '+str(len(unchanged))+'</summary><pre>'+html.escape('\n'.join(unchanged))+'</pre></details>')
    report['duration_seconds'] = round(time.monotonic()-started,3)
    gallery(output,'Visual comparison',json.dumps(report['summary'],ensure_ascii=False,indent=2),cards)
    runner.write_json(output/'report.json',report)
    summary = {k:v for k,v in report.items() if k not in ('cases','needs_review','sources')}
    summary['index'] = str(output/'index.html')
    if args.format == 'json':
        runner.present(summary,output/'report.json','json')
    else:
        runner.present(summary,output/'report.json')
        print('Summary: '+json.dumps(report['summary'],ensure_ascii=False))
        print('Index: '+str(output/'index.html'))
    return 2 if incompatible else (1 if report['needs_review'] else 0)


if __name__ == '__main__':
    if len(sys.argv) == 5 and sys.argv[1] == '--record-inputs':
        record_inputs(*sys.argv[2:])
    else:
        raise SystemExit('internal capture input recorder')
