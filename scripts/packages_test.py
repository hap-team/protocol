import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('packages', Path(__file__).with_name('packages.py'))
packages = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packages)


class ReleaseGatesTest(unittest.TestCase):
    def test_release_manifest_rejects_changed_bytes_and_path_escape(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'schemas.tar.gz').write_bytes(b'fixture')
            manifest = {'schema': 'hap-conformance-release/v1', 'version': packages.CANDIDATE,
                        'sourceCommit': 'a' * 40, 'artifactTests': 'passed', 'binaries': {},
                        'schemaArchive': {'file': 'schemas.tar.gz', 'digest': packages.digest(root / 'schemas.tar.gz')}}
            packages.record(root / 'conformance-manifest.json', manifest)
            self.assertEqual(packages.validated_manifest(root, packages.CANDIDATE), manifest)
            (root / 'schemas.tar.gz').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                packages.validated_manifest(root, packages.CANDIDATE)
            manifest['schemaArchive']['file'] = '../schemas.tar.gz'
            packages.record(root / 'conformance-manifest.json', manifest)
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                packages.validated_manifest(root, packages.CANDIDATE)

    def test_stable_requires_complete_candidate_bound_acceptance(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(packages, 'ROOT', Path(temporary)):
            root = Path(temporary); (root / 'releases').mkdir()
            document = {'schema': 'hap-protocol-release-acceptance/v1', 'candidateCommit': 'a' * 40,
                        'candidateManifestDigest': 'sha256:' + 'b' * 64, 'checks': {}}
            path = root / 'releases/0.2.1-acceptance.json'; packages.record(path, document)
            with self.assertRaisesRegex(ValueError, 'candidate-bound'):
                packages.acceptance('c' * 40, document['candidateManifestDigest'])
            with self.assertRaisesRegex(ValueError, 'all Protocol'):
                packages.acceptance(document['candidateCommit'], document['candidateManifestDigest'])
            names = ['cli', 'sdk-go', 'reference-interfaces', 'marketplace-schemas', 'website-schemas',
                     'research-image', 'writer-image', 'image-image', 'product-image']
            document['checks'] = {name: {'status': 'passed', 'evidenceDigest': 'sha256:' + 'd' * 64} for name in names}
            packages.record(path, document)
            self.assertEqual(packages.acceptance(document['candidateCommit'], document['candidateManifestDigest']), document)
            document['checks']['writer-image']['status'] = 'unknown'; packages.record(path, document)
            with self.assertRaisesRegex(ValueError, 'successful'):
                packages.acceptance(document['candidateCommit'], document['candidateManifestDigest'])


if __name__ == '__main__':
    unittest.main()
