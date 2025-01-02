#!/usr/bin/env python3
import json
import hashlib
import os

from pathlib import Path

f = open('cf.json', 'r')
data = f.read()
json.loads(data)
cf = json.loads(data)

for v in cf['Results']:
    url = v['file']['url']
    sha = hashlib.md5(url.encode()).hexdigest()
    p = Path('/Users/me/Code/cf/files/').expanduser()/sha[0]/f"{sha}.cf"
    os.makedirs(p.parent, exist_ok=True)

    p.write_text(v['file']['content'])