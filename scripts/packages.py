#!/usr/bin/env python3
"""Immutable Protocol modules, schemas and checker binaries, operated by Runner."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'hap-team/protocol'
CANDIDATE = 'v0.2.1-rc.1'
STABLE = 'v0.2.1'


def command(args, cwd=ROOT, **kwargs):
    result = subprocess.run(args, cwd=cwd, capture_output=True, text=True, **kwargs)
    if result.returncode:
        raise ValueError('command failed: ' + ' '.join(args[:2]))
    return result.stdout.strip()


def digest(path):
    return 'sha256:' + hashlib.sha256(Path(path).read_bytes()).hexdigest()


def record(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + '\n')


def remote_commit(version):
    lines = command(['git', 'ls-remote', 'origin', 'refs/tags/' + version, 'refs/tags/' + version + '^{}']).splitlines()
    refs = dict(line.split()[::-1] for line in lines)
    return refs.get('refs/tags/' + version + '^{}') or refs.get('refs/tags/' + version)


def validated_manifest(directory, version):
    directory = Path(directory)
    manifest = json.loads((directory / 'conformance-manifest.json').read_text())
    if manifest['schema'] != 'hap-conformance-release/v1' or manifest['version'] != version or manifest['artifactTests'] != 'passed':
        raise ValueError('tested release identity required')
    if not re.fullmatch(r'[0-9a-f]{40}', manifest['sourceCommit']):
        raise ValueError('immutable source required')
    for asset in [manifest['schemaArchive'], *manifest['binaries'].values()]:
        name = asset['file']
        if not re.fullmatch(r'[A-Za-z0-9_.-]+', name) or digest(directory / name) != asset['digest']:
            raise ValueError('release asset digest mismatch')
    return manifest


def download(version, destination):
    command(['gh', 'release', 'download', version, '--repo', REPOSITORY, '--dir', str(destination)])
    manifest = validated_manifest(destination, version)
    if remote_commit(version) != manifest['sourceCommit']:
        raise ValueError('published tag source mismatch')
    return manifest


def acceptance(candidate, manifest_digest):
    accepted = json.loads((ROOT / 'releases/0.2.1-acceptance.json').read_text())
    if accepted['schema'] != 'hap-protocol-release-acceptance/v1' or accepted['candidateCommit'] != candidate or accepted['candidateManifestDigest'] != manifest_digest:
        raise ValueError('candidate-bound acceptance required')
    required = {'cli', 'sdk-go', 'reference-interfaces', 'marketplace-schemas', 'website-schemas',
                'research-image', 'writer-image', 'image-image', 'product-image'}
    checks = accepted['checks']
    if set(checks) != required:
        raise ValueError('all Protocol release-policy checks required')
    for check in checks.values():
        if check['status'] != 'passed' or not re.fullmatch(r'sha256:[0-9a-f]{64}', check['evidenceDigest']):
            raise ValueError('successful bounded acceptance evidence required')
    return accepted


def prepare(version):
    if command(['git', 'status', '--porcelain']):
        raise ValueError('commit release preparation before execution')
    commit = command(['git', 'rev-parse', 'HEAD'])
    destination = Path(os.environ['RUNNER_OUTPUT_DIR']) / 'package'
    destination.mkdir()
    with tempfile.TemporaryDirectory(prefix='hap-protocol-release-') as temporary:
        base = Path(temporary)
        accepted = None
        if version == STABLE:
            published = base / 'candidate'; published.mkdir()
            candidate = download(CANDIDATE, published)
            commit = candidate['sourceCommit']
            accepted = acceptance(commit, digest(published / 'conformance-manifest.json'))
            command(['git', 'diff', '--exit-code', commit, 'HEAD', '--', '.', ':!releases'])
        source = base / 'source'
        command(['git', 'clone', '--no-local', '--quiet', str(ROOT), str(source)])
        command(['git', 'checkout', '--detach', commit], cwd=source)
        if command(['git', 'status', '--porcelain'], cwd=source):
            raise ValueError('clean release clone required')
        module = json.loads(command(['go', 'mod', 'edit', '-json'], cwd=source))
        if module.get('Replace') or any(item['Version'] == 'v0.0.0' for item in module.get('Require', [])):
            raise ValueError('released Go dependencies required')
        environment = {**os.environ, 'GOWORK': 'off', 'GOPRIVATE': 'github.com/hap-team/*',
                       'GOMODCACHE': str(base / 'modcache'), 'GIT_TERMINAL_PROMPT': '0'}
        command(['go', 'mod', 'download'], cwd=source, env=environment)
        command(['python3', '-B', 'scripts/packages_test.py'], cwd=source, env=environment)
        command(['scripts/verify-release.sh', version[1:]], cwd=source, env=environment)
        command(['go', 'test', '-race', './...'], cwd=source, env=environment)
        command(['go', 'vet', './...'], cwd=source, env=environment)
        archive = source / 'dist' / ('hap-protocol-' + version[1:] + '.tar.gz')
        shutil.copyfile(archive, destination / archive.name)
        extracted = base / 'schema-package'; extracted.mkdir()
        with tarfile.open(archive) as stream:
            stream.extractall(extracted, filter='data')
        package = extracted / ('hap-protocol-' + version[1:])
        command(['shasum', '-a', '256', '-c', 'SHA256SUMS'], cwd=package)
        binaries = {}
        for platform in ('darwin-arm64', 'linux-amd64', 'linux-arm64'):
            goos, goarch = platform.split('-')
            binary = destination / ('hap-conformance_' + version + '_' + platform.replace('-', '_'))
            command(['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags=-buildid=', '-o', str(binary), './cmd/hap-conformance'],
                    cwd=source, env={**environment, 'GOOS': goos, 'GOARCH': goarch, 'CGO_ENABLED': '0'})
            binaries[platform] = {'file': binary.name, 'digest': digest(binary)}
        host = destination / binaries['darwin-arm64']['file']
        command([str(host), '--schema', str(package / 'schemas/0.2/hap-agent.schema.json'),
                 '--file', str(package / 'conformance/descriptors/valid/minimal.yaml')], cwd=base)
        manifest = {'schema': 'hap-conformance-release/v1', 'version': version, 'sourceCommit': commit,
                    'artifactTests': 'passed', 'schemaArchive': {'file': archive.name, 'digest': digest(archive)}, 'binaries': binaries}
        if accepted is not None:
            manifest['acceptanceDigest'] = digest(ROOT / 'releases/0.2.1-acceptance.json')
        record(destination / 'conformance-manifest.json', manifest)
        assets = sorted(path for path in destination.iterdir() if path.is_file())
        (destination / 'SHA256SUMS').write_text(''.join(digest(path)[7:] + '  ' + path.name + '\n' for path in assets))
    print('Protocol clean-clone, reference interfaces, packaged schemas and checker checks passed: ' + version)


def publish(version):
    item = json.loads(Path(os.environ['RUNNER_INPUTS_FILE']).read_text())['artifact']
    if item['kind'] != 'artifact' or not re.fullmatch(r'sha256:[0-9a-f]{64}', item['digest']):
        raise ValueError('typed package input required')
    directory = Path(item['path'])
    manifest = validated_manifest(directory, version)
    commit = manifest['sourceCommit']
    existing = remote_commit(version)
    if existing and existing != commit:
        raise ValueError('immutable Protocol tag conflict')
    result = subprocess.run(['gh', 'release', 'view', version, '--repo', REPOSITORY, '--json', 'tagName'], capture_output=True)
    if result.returncode == 0:
        raise ValueError('release exists; reconcile instead of repeating publication')
    if version == STABLE:
        with tempfile.TemporaryDirectory(prefix='hap-candidate-gate-') as temporary:
            candidate = download(CANDIDATE, Path(temporary))
            accepted = acceptance(candidate['sourceCommit'], digest(Path(temporary) / 'conformance-manifest.json'))
            if candidate['sourceCommit'] != commit or manifest.get('acceptanceDigest') != digest(ROOT / 'releases/0.2.1-acceptance.json'):
                raise ValueError('stable acceptance changed')
    if not existing:
        command(['git', 'push', 'origin', commit + ':refs/tags/' + version])
    notes = directory / 'release-notes.md'
    notes.write_text('Protocol ' + version + ' binds packaged conformance to immutable agent images and publishes checksum-pinned checker binaries.\n\nSchemas, reference interface checks, clean-clone tests and Go dependencies were verified before publication.\n')
    args = ['gh', 'release', 'create', version, '--repo', REPOSITORY, '--verify-tag', '--title', 'HAP Protocol ' + version, '--notes-file', str(notes)]
    if version != STABLE:
        args.append('--prerelease')
    args.extend(str(directory / name) for name in [manifest['schemaArchive']['file'], 'conformance-manifest.json', 'SHA256SUMS', *(asset['file'] for asset in manifest['binaries'].values())])
    command(args)
    print('Immutable Protocol release published: ' + version)


def reconcile(version):
    destination = Path(os.environ['RUNNER_OUTPUT_DIR']) / 'publication'; destination.mkdir()
    with tempfile.TemporaryDirectory(prefix='hap-protocol-reconcile-') as temporary:
        base = Path(temporary)
        manifest = download(version, base)
        environment = {**os.environ, 'GOPRIVATE': 'github.com/hap-team/*', 'GOWORK': 'off', 'GOMODCACHE': str(base / 'modcache')}
        module = json.loads(command(['go', 'mod', 'download', '-json', 'github.com/hap-team/protocol@' + version], cwd=base, env=environment))
        if module.get('Origin', {}).get('Hash') != manifest['sourceCommit']:
            raise ValueError('published Go module source mismatch')
        record(destination / 'publication.json', {'schema': 'hap-protocol-publication/v1', 'status': 'published',
               'release': manifest, 'manifestDigest': digest(base / 'conformance-manifest.json'),
               'go': {'module': 'github.com/hap-team/protocol', 'version': version, 'sum': module['Sum'], 'goModSum': module['GoModSum']}})
    print('Published Protocol package bytes and Go module identity verified: ' + version)


if __name__ == '__main__':
    try:
        if len(sys.argv) != 3 or sys.argv[2] not in (CANDIDATE, STABLE):
            raise ValueError('declared release version required')
        {'prepare': prepare, 'publish': publish, 'reconcile': reconcile}[sys.argv[1]](sys.argv[2])
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        label = str(error) if type(error) is ValueError else 'release input unavailable'
        sys.exit('Protocol package action failed: ' + label + '; reconcile before retrying publication')
