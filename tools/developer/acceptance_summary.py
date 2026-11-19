"""Summarize recorded acceptance facts; never run fixtures or infer cleanup."""
from __future__ import annotations

from collections import Counter
import json
from pathlib import Path
import re


NATIVE_KINDS = ('linux-desktop', 'linux-announcement', 'linux-rpm', 'windows-field')
BOUNDARIES = [
    'Receipt inspection does not rerun or revalidate the candidate, artifacts or current system.',
    'No supplied receipt proves campus authentication or long-term stability.',
    'Native GUI appearance, notification delivery, DPI and actual login/autostart require explicit field evidence.',
    'Signing, public download and release acceptance are not established by these receipts.',
]


def native(path, kind):
    if kind not in NATIVE_KINDS:
        raise ValueError('unsupported native receipt kind: ' + kind)
    path = Path(path).resolve()
    if path.is_dir():
        path /= 'result.json'
    with path.open('rb') as stream:
        raw = stream.read(4 * 1024 * 1024 + 1)
    if len(raw) > 4 * 1024 * 1024:
        raise ValueError('native receipt exceeds 4 MiB')
    report = json.loads(raw.decode('utf-8-sig'))
    if not isinstance(report, dict):
        raise ValueError('native receipt must be an object')
    result = {'schema': 1, 'kind': 'native-inspection', 'report': str(path),
              'report_kind': kind, 'checks': [], 'facts': {}, 'errors': {}}
    if kind == 'windows-field':
        if (type(report.get('Schema')) is not int or report['Schema'] != 1
                or report.get('Stage') not in ('Before', 'Installed', 'AfterKeep', 'AfterDelete')
                or not isinstance(report.get('ExpectedBuildID'), str)
                or not re.fullmatch('[a-fA-F0-9]{40}', report['ExpectedBuildID'])
                or not isinstance(report.get('ManifestHash'), str)
                or not re.fullmatch('[a-fA-F0-9]{64}', report['ManifestHash'])
                or not isinstance(report.get('Results'), list)):
            raise ValueError('expected offline_acceptance.ps1 snapshot')
        seen = set()
        for row in report['Results']:
            if (not isinstance(row, dict) or not isinstance(row.get('Check'), str)
                    or not row['Check'] or row['Check'] in seen
                    or not isinstance(row.get('Status'), str)
                    or row['Status'] not in ('PASS', 'FAIL', 'BLOCK', 'SKIP', 'MANUAL')):
                raise ValueError('invalid Windows acceptance check')
            seen.add(row['Check'])
            # Deliberately omit Detail, user PATH and file inventories.
            result['checks'].append({'name': row['Check'], 'status': row['Status']})
        counts = Counter(row['status'] for row in result['checks'])
        result.update(status='failed' if counts['FAIL'] or counts['BLOCK'] else 'incomplete' if not counts or counts['SKIP'] or counts['MANUAL'] else 'passed',
                      checkpoint=report['Stage'], candidate_build=report['ExpectedBuildID'].lower(),
                      candidate_manifest_sha256=report['ManifestHash'].lower())
    else:
        required = {'linux-desktop': ('namespace', 'events', 'campusAuthentication', 'realAccountAutostart'),
                    'linux-announcement': ('nativeLog', 'realSchoolRequests', 'realAccountIntegration'),
                    'linux-rpm': ('rpmSHA256', 'events', 'hostInstallation', 'campusTraffic')}[kind]
        if (type(report.get('passed')) is not bool or not isinstance(report.get('source'), dict)
                or type(report.get('source_unchanged')) is not bool
                or any(key not in report for key in required)):
            raise ValueError('receipt does not match ' + kind)
        source = report['source']
        result['source'] = {key: source[key] for key in ('commit', 'dirty', 'source_fingerprint') if key in source}
        result['facts']['source_unchanged'] = report['source_unchanged']
        result['status'] = 'passed' if report['passed'] else 'failed'
        # Preserve explicit fixture boundaries without dumping events/logs/paths.
        keys = ('campusAuthentication', 'realAccountAutostart', 'realSchoolRequests',
                'realAccountIntegration', 'hostInstallation', 'campusTraffic',
                'rpmPlugins', 'trayAcceptance', 'signatures')
        for key in keys[:6]:
            if key in report and type(report[key]) is not bool:
                raise ValueError('invalid native boundary: ' + key)
        for key in keys[6:]:
            if key in report and (not isinstance(report[key], str) or len(report[key]) > 2000):
                raise ValueError('invalid native boundary: ' + key)
        if kind in ('linux-desktop', 'linux-rpm') and not isinstance(report['events'], list):
            raise ValueError('native events must be an array')
        if kind == 'linux-desktop' and not isinstance(report['namespace'], str):
            raise ValueError('native namespace must be a string')
        if kind == 'linux-announcement' and not isinstance(report['nativeLog'], str):
            raise ValueError('nativeLog must be a string')
        if kind == 'linux-rpm' and (not isinstance(report['rpmSHA256'], list) or not report['rpmSHA256']
                or any(not isinstance(value, str) or not re.fullmatch('[a-fA-F0-9]{64}', value) for value in report['rpmSHA256'])):
            raise ValueError('invalid RPM hashes')
        if kind == 'linux-rpm':
            result['rpm_sha256'] = report['rpmSHA256']
        result['limits'] = {key: report[key] for key in keys if key in report}
    return result


def dimensions(result):
    kind = result['report_kind']
    facts = result.get('facts', {})
    gaps = list(BOUNDARIES)
    row = {'report': result['report'], 'kind': kind,
           'source': result.get('source', {}),
           'execution': {'reported_status': result['status']},
           'evidence': {'status': 'receipt_available', 'collection': facts.get('evidence', 'not_recorded')},
           'cleanup': {'status': facts.get('cleanup', 'not_recorded')},
           'not_covered': gaps}
    if result.get('errors'):
        row['errors'] = result['errors']
    for key in ('source_unchanged', 'host_source_unchanged', 'guest_source_unchanged'):
        if key in facts:
            row[key] = facts[key]
    if 'candidate_build' in result:
        row['candidate_build'] = result['candidate_build']
        row['candidate_manifest_sha256'] = result['candidate_manifest_sha256']
        row['checkpoint'] = result['checkpoint']
    if 'rpm_sha256' in result:
        row['rpm_sha256'] = result['rpm_sha256']
    if result.get('counts') is not None:
        counts = result['counts']
        row['execution']['stage_counts'] = counts
        row['execution']['stage_status'] = (
            'failed' if any(counts.get(key) for key in ('failed', 'timed_out', 'interrupted'))
            else 'incomplete' if not counts or counts.get('not_run') or counts.get('running') else 'passed')
        if not counts or counts.get('not_run') or counts.get('running'):
            gaps.append('Stages are absent, still running or not_run; the requested sequence is not complete.')
    if kind == 'windows-vm':
        gaps.append('VM command success does not identify which native/UI/install scenarios were accepted; supply their receipts.')
        if facts.get('evidence') != 'collected':
            gaps.append('VM evidence collection is incomplete or not recorded.')
        if facts.get('cleanup') == 'task_removed':
            gaps.append('task_removed proves owned scheduled-task removal only; product/fixture cleanup remains separate.')
        else:
            gaps.append('Owned VM task cleanup is unknown, incomplete or not started.')
    if kind in NATIVE_KINDS:
        row['execution']['checks'] = result['checks']
        if result.get('limits'):
            row['limits'] = result['limits']
        if kind != 'windows-field':
            gaps.append('Final fixture/process cleanup is not recorded by this legacy receipt; passed may precede finally cleanup.')
        if kind == 'linux-rpm':
            gaps.append('Isolated RPM acceptance does not prove host installation, host plugins or desktop tray delivery.')
        if kind == 'windows-field':
            if not result['checks']:
                gaps.append('No Windows checkpoint checks were recorded.')
            for check in result['checks']:
                if check['status'] != 'PASS':
                    gaps.append(check['status'] + ': ' + check['name'])
            gaps.append('One Windows checkpoint does not prove a full install/uninstall cycle or installer component choices.')
    if facts.get('source_unchanged') is not True:
        gaps.append('Stable source identity is not proved by this receipt.')
    if row['cleanup']['status'] == 'not_recorded':
        gaps.append('No explicit final cleanup outcome is available; cleanup is not inferred from command exit.')
    return row


def combine(primary, attachments):
    receipts = [dimensions(primary)]
    seen = {primary['report']}
    for value in attachments:
        kind, separator, path = value.partition('=')
        if not separator or not path:
            raise ValueError('--native expects KIND=PATH')
        result = native(path, kind)
        if result['report'] in seen:
            raise ValueError('duplicate receipt: ' + result['report'])
        seen.add(result['report'])
        receipts.append(dimensions(result))
    commits = [row['source'].get('commit') for row in receipts]
    comparison = 'unknown'
    if all(isinstance(commit, str) and re.fullmatch('[a-fA-F0-9]{40}', commit) for commit in commits):
        comparison = 'different_commits' if len({commit.lower() for commit in commits}) > 1 else 'same_commit_only'
    attention = comparison == 'different_commits' or any(
        row['execution']['reported_status'] in ('failed', 'timed_out', 'interrupted')
        or row['execution'].get('stage_status') == 'failed' or row.get('errors')
        or any(row.get(key) is False for key in ('source_unchanged', 'host_source_unchanged', 'guest_source_unchanged'))
        or row['evidence']['collection'] == 'incomplete'
        or row['cleanup']['status'] == 'unknown' for row in receipts)
    incomplete = comparison == 'unknown' or any(
        row['execution']['reported_status'] != 'passed'
        or row['execution'].get('stage_status') == 'incomplete'
        or row.get('source_unchanged') is not True
        or row['evidence']['collection'] != 'collected'
        or row['cleanup']['status'] != 'task_removed' for row in receipts)
    return {'receipts': receipts, 'source_comparison': comparison,
            'review_status': 'attention_required' if attention else 'incomplete' if incomplete else 'recorded_checks_only',
            'not_covered': ['Receipts are independent observations, not a merged acceptance PASS.',
                            'Equal commit IDs alone do not bind dirty source bytes, package hashes or fixture instances.']}
