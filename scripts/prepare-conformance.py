#!/usr/bin/env python3
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

root=Path(__file__).resolve().parents[1]
output=Path(os.environ['RUNNER_OUTPUT_DIR'])/'release'
# The packaged checker must build from published, pinned modules.
module=json.loads(subprocess.check_output(['go','mod','edit','-json'],cwd=root,text=True))
if module.get('Replace') or any(m['Version']=='v0.0.0' for m in module.get('Require',[])):
    sys.exit('publish and pin receipt dependencies before packaging conformance')
output.mkdir()
for name in ('go.mod','go.sum','Dockerfile.conformance','cmd','compatibility','definition','internal','schemas','conformance','scripts/git-askpass.sh'):
    source=root/name;destination=output/name
    destination.parent.mkdir(parents=True,exist_ok=True)
    if source.is_dir():shutil.copytree(source,destination,ignore=shutil.ignore_patterns('*_test.go'))
    else:shutil.copyfile(source,destination)
print('Conformance verifier source context prepared; publication not performed')
