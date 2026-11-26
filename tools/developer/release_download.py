"""Public asset consumer, using the existing runner and Go signature verifier."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request

import release_artifacts as release
import validation_run as runner


def url(value, redirect=False):
    release.require(isinstance(value, str), 'public_url_type')
    parsed = urllib.parse.urlsplit(value)
    release.require(parsed.scheme == 'https' and parsed.hostname and not parsed.username
                    and not parsed.password and not parsed.fragment
                    and (redirect or not parsed.query), 'public_https_url_required')
    return value


class PublicRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        url(newurl, redirect=True)
        return super().redirect_request(request, fp, code, msg, headers, newurl)


def request(path):
    data = release.document(path)
    release.require(isinstance(data, dict) and set(data) == {'schemaVersion', 'version', 'assets', 'manifest', 'signature'}, 'download_request_fields')
    release.require(type(data['schemaVersion']) is int and data['schemaVersion'] == 1
                    and isinstance(data['version'], str) and data['version'], 'download_request_identity')
    release.require(isinstance(data['assets'], list) and 1 <= len(data['assets']) <= 32, 'download_asset_list')
    seen = set()
    total = 0
    for asset in data['assets']:
        release.require(isinstance(asset, dict) and set(asset) == {'name', 'url', 'size', 'sha256'}, 'download_asset_fields')
        name = asset['name']
        release.require(isinstance(name, str) and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._+~\-]{0,180}', name)
                        and not name.endswith('.') and name.split('.')[0].upper() not in
                        {'CON', 'PRN', 'AUX', 'NUL', *('COM'+str(i) for i in range(1,10)), *('LPT'+str(i) for i in range(1,10))}, 'download_asset_name')
        release.require(name.casefold() not in seen, 'duplicate_download_asset')
        seen.add(name.casefold())
        url(asset['url'])
        release.require(type(asset['size']) is int and 0 < asset['size'] <= 2 << 30, 'download_asset_size')
        release.require(isinstance(asset['sha256'], str) and re.fullmatch('[0-9a-f]{64}', asset['sha256']), 'download_asset_hash')
        total += asset['size']
    release.require(total <= 8 << 30, 'download_total_size')
    names = {asset['name'] for asset in data['assets']}
    release.require(isinstance(data['manifest'], str) and isinstance(data['signature'], str)
                    and data['manifest'] != data['signature'] and {data['manifest'], data['signature']} <= names, 'download_metadata_names')
    return data


def consume(repo, go, request_path, key_path, output):
    inventory = request(request_path)
    public = release.read_public(key_path, 256)
    release.public_identity_bytes(public)
    files, partial = output/'files', output/'partial'
    files.mkdir(); partial.mkdir()
    result = {'assets': [], 'signature': 'not_run', 'signedAssets': [], 'hashOnlyAssets': [], 'verifiedMetadata': [],
              'nativeUpgrade': 'not_run', 'installation': 'not_run', 'publication': 'not_run'}
    result_path = output/'download-result.json'
    opener = urllib.request.build_opener(PublicRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context()))
    try:
        for index, asset in enumerate(inventory['assets']):
            row = {'name': asset['name'], 'size': asset['size'], 'sha256': asset['sha256'], 'status': 'downloading'}
            result['assets'].append(row)
            runner.write_json(result_path, result)
            temporary = partial/str(index)
            digest, count = hashlib.sha256(), 0
            req = urllib.request.Request(asset['url'], headers={'User-Agent': 'Sidravia-release-verifier', 'Accept-Encoding': 'identity'})
            with opener.open(req, timeout=30) as response, temporary.open('xb') as stream:
                release.require(response.status == 200, 'public_http_status')
                length = response.headers.get('Content-Length')
                release.require(length is None or length == str(asset['size']), 'public_content_length_mismatch')
                while True:
                    chunk = response.read(min(1 << 20, asset['size'] - count + 1))
                    if not chunk:
                        break
                    count += len(chunk)
                    release.require(count <= asset['size'], 'public_asset_oversize')
                    stream.write(chunk); digest.update(chunk)
            release.require(count == asset['size'] and digest.hexdigest() == asset['sha256'], 'public_asset_size_or_hash_mismatch')
            temporary.rename(files/asset['name'])
            row.update(status='passed', path=str(files/asset['name']))
            runner.write_json(result_path, result)
        manifest = release.read_public(files/inventory['manifest'], 256 << 10)
        signature = release.read_public(files/inventory['signature'], 64)
        result['signature'] = 'checking'
        release.public_check(repo, go, 'verify', manifest, public, signature,
                             published_names=(inventory['manifest'], inventory['signature']))
        result['signature'] = 'passed'
        parsed = json.loads(manifest, object_pairs_hook=release.unique_object)
        release.require(parsed['version'] == inventory['version'], 'download_manifest_version_mismatch')
        expected = {asset['name']: asset for asset in inventory['assets']}
        for asset in parsed['assets']:
            release.require(asset['name'] in expected and all(asset[key] == expected[asset['name']][key] for key in ('size','sha256')), 'signed_asset_inventory_mismatch')
        for asset in inventory['assets']:
            path = release.plain_path(files/asset['name'])
            before = path.stat()
            digest = hashlib.sha256()
            with path.open('rb') as stream:
                release.require(os.path.samestat(before, os.fstat(stream.fileno())), 'download_identity_changed')
                for chunk in iter(lambda: stream.read(1 << 20), b''):
                    digest.update(chunk)
            after = path.stat()
            release.require(os.path.samestat(before, after) and before.st_size == after.st_size == asset['size']
                            and digest.hexdigest() == asset['sha256'], 'download_changed_after_verification')
        metadata = {inventory['manifest'], inventory['signature']}
        result.update(signature='passed', version=parsed['version'], buildId=parsed['buildId'],
                      signedAssets=[asset['name'] for asset in parsed['assets']],
                      hashOnlyAssets=sorted(set(expected) - {asset['name'] for asset in parsed['assets']} - metadata),
                      verifiedMetadata=sorted(metadata),
                      status='passed')
    except Exception as error:
        if result['signature'] == 'checking':
            result['signature'] = 'failed'
        if result['assets'] and result['assets'][-1]['status'] == 'downloading':
            result['assets'][-1]['status'] = 'failed'
        # Never copy redirect URLs (which may contain signed query parameters)
        # or network exception text into the public report.
        reason = str(error) if isinstance(error, release.ReleaseError) else type(error).__name__
        result.update(status='failed', error=reason)
    runner.write_json(result_path, result)
    return 0 if result['status'] == 'passed' else 1


def run(repo, args, identity):
    inventory = request(args.request)
    public = release.read_public(args.public_key, 256)
    release.public_identity_bytes(public)
    output = Path(os.path.abspath(args.output))
    release.plain_path(output.parent, directory=True)
    before = identity()
    output.mkdir()  # Exclusive: preserve every earlier run, including failures.
    runner.write_json(output/'download-request.json', inventory)
    (output/'publisher-public.txt').write_bytes(public)
    command = [sys.executable, str(Path(__file__).resolve()), '--worker', str(repo), args.go, str(output)]
    spec = {'schema':1, 'source':before, 'stages':[{'name':'public-download', 'argv':command, 'cwd':str(repo), 'timeout_seconds':args.stage_timeout}]}
    runner.write_json(output/'request.json', spec)
    report = runner.run_request(spec, output)
    report.update(kind='release-download', not_covered=[
        'No archive extraction, payload/native/installer/update/campus acceptance.',
        'Signature covers signedAssets only; hashOnlyAssets use the supplied inventory trust.',
        'This checks the explicit URLs and inventory, not completeness/discovery of the GitHub Release asset listing.',
        'No signing, upload, publication or cleanup of retained download evidence.'])
    result_path = output/'download-result.json'
    if result_path.exists():
        report['download'] = runner.read_json(result_path)
    try:
        report['source_after'] = identity()
        report['source_unchanged'] = before == report['source_after']
        release.require(report['source_unchanged'], 'source_changed_during_download')
        release.require(request(args.request) == inventory and release.read_public(args.public_key,256) == public, 'download_inputs_changed')
        release.require(request(output/'download-request.json') == inventory and release.read_public(output/'publisher-public.txt',256) == public, 'download_snapshot_changed')
    except Exception as error:
        report.update(status='failed', error=str(error))
    runner.write_json(output/'report.json', report)
    if args.format == 'json':
        compact = {key:value for key,value in report.items() if key != 'download'}
        download = report.get('download', {})
        compact.update(report=str(output/'report.json'), assets=len(download.get('assets', [])), signature=download.get('signature','not_run'))
        print(json.dumps(compact, ensure_ascii=False))
    else:
        runner.present(report, output/'report.json')
        print('Signature: ' + report.get('download',{}).get('signature','not_run'))
    return 0 if report['status'] == 'passed' else 1


if __name__ == '__main__':
    runner.configure_output()
    if len(sys.argv) == 5 and sys.argv[1] == '--worker':
        folder = Path(sys.argv[4])
        raise SystemExit(consume(Path(sys.argv[2]), sys.argv[3], folder/'download-request.json', folder/'publisher-public.txt', folder))
    raise SystemExit('internal worker; use dev release download')
