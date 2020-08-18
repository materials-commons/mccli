import os
import sys
from contextlib import contextmanager
from io import StringIO


@contextmanager
def working_dir(wd):
    orig_wd = os.getcwd()
    if wd is None:
        wd = orig_wd
    os.chdir(wd)
    try:
        yield
    finally:
        os.chdir(orig_wd)


@contextmanager
def captured_output(wd=None):
    with working_dir(wd):
        new_out, new_err = StringIO(), StringIO()
        old_out, old_err = sys.stdout, sys.stderr
        try:
            sys.stdout, sys.stderr = new_out, new_err
            yield sys.stdout, sys.stderr
        finally:
            sys.stdout, sys.stderr = old_out, old_err


def print_string_io(strio):
    print("\n----\n", strio.getvalue(), "\n----")
