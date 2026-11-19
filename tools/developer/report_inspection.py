"""Read existing stage receipts without rerunning commands or expanding inventories."""
from __future__ import annotations

from collections import Counter
import json
import math
from pathlib import Path, PureWindowsPath

import validation_run as runner


KINDS = {'verification', 'windows-vm', 'command-sequence', 'visual-capture'}
STATUSES = {'passed', 'failed', 'timed_out', 'not_run', 'running', 'interrupted'}
ERRORS = ('error', 'evidence_error', 'cleanup_error', 'source_error')
FACTS = ('source_unchanged', 'host_source_unchanged', 'guest_source_unchanged',
         'evidence', 'cleanup')


def seconds(value):
    if type(value) not in (int, float) or not math.isfinite(value) or value < 0:
        raise ValueError('invalid stage/report duration_seconds')
    return value


def log_tail(directory, name, limit):
    # Receipts may be copied from Windows. Accept only collected sibling logs;
    # never follow paths embedded in argv/cwd or links out of the report folder.
    if (not isinstance(name, str) or not name or Path(name).name != name
            or PureWindowsPath(name).name != name or ':' in name
            or not name.endswith(('.stdout.log', '.stderr.log'))):
        raise ValueError('log must name a sibling stdout/stderr log')
    path = directory / name
    if path.is_symlink():
        raise ValueError('linked logs are unsupported')
    result = {'path': str(path), 'status': 'missing'}
    if not path.exists():
        return result
    if not path.is_file():
        raise ValueError('log is not a regular file')
    with path.open('rb') as stream:
        size = stream.seek(0, 2)
        stream.seek(max(0, size - limit))
        data = stream.read(limit)
    result.update(status='available', bytes=size, truncated=size > len(data),
                  tail=data.decode('utf-8', errors='replace'))
    return result


def inspect(path, stage=None, tail_bytes=2000):
    path = Path(path).resolve()
    if path.is_dir():
        path = path / 'report.json'
    # Report size is bounded separately from arbitrarily large raw log files.
    with path.open('rb') as stream:
        raw = stream.read(4 * 1024 * 1024 + 1)
    if len(raw) > 4 * 1024 * 1024:
        raise ValueError('stage report exceeds 4 MiB')
    report = json.loads(raw.decode('utf-8-sig'))
    if (not isinstance(report, dict) or type(report.get('schema')) is not int
            or report['schema'] != 1 or not isinstance(report.get('kind'), str)
            or report['kind'] not in KINDS or not isinstance(report.get('status'), str)
            or report['status'] not in STATUSES or not isinstance(report.get('stages'), list)):
        raise ValueError('expected schema-1 check/vm/command-sequence/visual-capture stage report')
    if type(tail_bytes) is not int or not 0 <= tail_bytes <= 16384:
        raise ValueError('tail_bytes must be 0..16384')
    rows = []
    names = set()
    for row in report['stages']:
        if (not isinstance(row, dict) or not isinstance(row.get('name'), str)
                or not row['name'] or row['name'] in names
                or not isinstance(row.get('status'), str) or row['status'] not in STATUSES):
            raise ValueError('invalid or duplicate stage')
        names.add(row['name'])
        item = {key: row.get(key) for key in ('name', 'status', 'exit_code')}
        item['duration_seconds'] = seconds(row.get('duration_seconds', 0))
        if row.get('error'):
            item['error'] = str(row['error'])[:2000]
        rows.append(item)
    if stage is not None and stage not in names:
        raise ValueError('stage not found: ' + stage)
    result = {'schema': 1, 'kind': 'report-inspection', 'report': str(path),
              'report_kind': report['kind'], 'status': report['status'],
              'duration_seconds': seconds(report.get('duration_seconds', 0)),
              'counts': dict(Counter(row['status'] for row in rows)),
              'stages': [], 'facts': {key: report[key] for key in FACTS if key in report},
              'errors': {key: str(report[key])[:2000] for key in ERRORS if report.get(key)}}
    source = report.get('source')
    if isinstance(source, dict):
        result['source'] = {key: source[key] for key in
                            ('commit', 'dirty', 'source_fingerprint', 'manifest_sha256') if key in source}
    for original, row in zip(report['stages'], rows):
        if stage is not None and row['name'] != stage:
            continue
        if tail_bytes and (stage is not None or row['status'] in ('failed', 'timed_out', 'interrupted')):
            row['logs'] = {key: log_tail(path.parent, original[key], tail_bytes)
                           for key in ('stderr', 'stdout') if key in original}
        result['stages'].append(row)
    return result


def run(args):
    result = inspect(args.path, args.stage, args.tail_bytes)
    if args.format == 'json':
        print(json.dumps(result, ensure_ascii=False))
    else:
        print(f"{result['status'].upper()} {result['report_kind']} {result['duration_seconds']:.3f}s")
        for key, value in result.get('source', {}).items():
            print(runner.human_text(f"source.{key}: {value}"))
        for key, value in {**result['facts'], **result['errors']}.items():
            print(runner.human_text(f"{key}: {value}"))
        for row in result['stages']:
            print(runner.human_text(f"  {row['status']:11} {row['name']} {row['duration_seconds']:.3f}s"))
            if row.get('error'):
                print('    ' + runner.human_text(row['error']))
            for name, log in row.get('logs', {}).items():
                detail = log['status']
                if log['status'] == 'available':
                    detail += f", {log['bytes']} bytes, truncated={log['truncated']}"
                print(runner.human_text(f"    {name}: {log['path']} ({detail})"))
                if log.get('tail'):
                    print('    ' + runner.human_text(log['tail']).replace('\n', '\n    '))
        print('Report: ' + runner.human_text(result['report']))
    # Reading a failed receipt successfully is distinct from test acceptance.
    return 0
