#!/usr/bin/env bash

venv/bin/python3 - <<'PY'
import os
import sys

print("executable:     ", sys.executable)
print("prefix:         ", sys.prefix)
print("base_prefix:    ", sys.base_prefix)
print("in venv:        ", sys.prefix != sys.base_prefix)
print("PYTHONHOME:     ", os.environ.get("PYTHONHOME"))
print("VIRTUAL_ENV:    ", os.environ.get("VIRTUAL_ENV"))
PY
