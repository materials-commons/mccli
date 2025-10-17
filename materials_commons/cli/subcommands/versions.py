import argparse
import difflib
import os
import sys

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.file_functions as filefuncs
import materials_commons.cli.tmp_functions as tmpfuncs
from materials_commons.cli.print_formatter import PrintFormatter

def make_version_record(file, is_current):
    return {
        'current': is_current,
        'owner': file.owner.email,
        'created_at': clifuncs.format_time(file.created_at),
        'size': clifuncs.humanize(file.size),
        'checksum': file.checksum,
        'id': file.id}

def make_versions(proj, path):
    """Make a list of versions records"""
    file = filefuncs.get_by_path_if_exists(proj.remote, proj.id, path)

    if not file:
        print(p + ": No such file or directory on remote")
        return None
    if filefuncs.isdir(file):
        print(p + ": Is a directory on remote")
        return None
    if not filefuncs.isfile(file):
        print(p + ": Not a file on remote")
        return None
    file_versions = proj.remote.get_file_versions(proj.id, file.id)

    version_records = []
    for file_version in file_versions:
        tmpfuncs._add_owner(proj.remote, file_version)
        version_records.append(make_version_record(file_version, ''))
    tmpfuncs._add_owner(proj.remote, file)
    version_records.append(make_version_record(file, '*'))
    return version_records

def list_versions(proj, path):
    versions = make_versions(proj, path)
    if not versions:
        return None

    versions = sorted(versions, key=lambda k: k['created_at'])

    # fmt = [
    #     ('current', '', '<', 2, clifuncs.as_is),
    #     ('created_at', 'created_at', '<', 24, clifuncs.as_is),
    #     ('size', 'size', '<', 8, clifuncs.as_is),
    #     ('id', 'id', '<', 36, clifuncs.as_is),
    #     ('owner', 'owner', '<', 36, clifuncs.as_is),
    #     ('checksum', 'checksum', '<', 36, clifuncs.as_is),
    #     ('version', 'version', '<', 8, clifuncs.as_is)
    # ]

    # pformatter = PrintFormatter(fmt)
    #
    # local_abspath = os.path.join(os.path.dirname(proj.local_path), path)
    # print(os.path.relpath(local_abspath) + ":")
    # pformatter.print_header()
    # for vers in versions:
    #     pformatter.print(vers)

    local_abspath = os.path.join(os.path.dirname(proj.local_path), path)
    print(os.path.relpath(local_abspath) + ":")
    columns=['current', 'owner', 'created_at', 'size', 'checksum', 'id']
    headers=['', 'owner', 'created_at', 'size', 'checksum', 'id']
    clifuncs.print_table(versions, columns=columns, headers=headers)

def version_as_str(proj, path, versions, vers_indicator):
    """Return version as str and standardized version name

    Arguments
    ---------
    path: str, File path
    versions: list of version records, output from `make_versions`
    vers_indicator: int or str, version ID, or 'local', or 'remote' for current remote version

    Returns
    -------
    (s, verspath):
        s: str, File version as a string
        verspath: str, Standardized version path
    """
    if vers_indicator == 'local':
        refpath = os.path.dirname(proj.local_path)
        local_abspath = os.path.join(refpath, path)
        if not os.path.exists(local_abspath):
            print(path + ": does not exist locally")
            raise cliexcept.MCCLIException("Invalid versions request")
        elif not os.path.isfile(local_abspath):
            print(path + ": is not a file locally")
            raise cliexcept.MCCLIException("Invalid versions request")
        versname = path + "-local"
        return (open(local_abspath, 'r').read(), versname)
    else:
        def select_version(versions):
            for version in versions:
                if vers_indicator == 'remote' and version['current'] == '*':
                    return version
                elif str(version['id']) == str(vers_indicator):
                    return version
            return None
        version = select_version(versions)
        if version is None:
            print(vers_indicator + ": version not found")
            raise cliexcept.MCCLIException("Invalid versions request")
        versname = path + "-" + str(version['id'])
        return (filefuncs.download_file_as_string(proj.remote, proj.id, version['id']), versname)

def print_version(proj, path, vers_indicator):
    """
    Arguments
    ---------
    proj: mcapi.Project
    path: str, path in project
    vers_indicator: str or int,
        Version number (positive or negative), or 'local', or 'remote' (=="-1")
    """
    versions = make_versions(proj, path)
    s, verspath = version_as_str(proj, path, versions, vers_indicator)
    refpath = os.path.dirname(proj.local_path)
    local_verspath = os.path.join(refpath, verspath)
    print(os.path.relpath(local_verspath) + ":")
    print(s)

def download_version(proj, path, vers_indicator):
    """
    Arguments
    ---------
    proj: mcapi.Project
    path: str, path in project
    vers_indicator: str or int
        Version number (positive or negative), or 'local', or 'remote' (=="-1")
    """
    versions = make_versions(proj, path)
    s, verspath = version_as_str(proj, path, versions, vers_indicator)

    refpath = os.path.dirname(proj.local_path)
    local_verspath = os.path.join(refpath, verspath)
    if os.path.exists(local_verspath):
        while True:
            print("Overwrite '" + os.path.relpath(local_verspath) + "'?")
            ans = input('y/n: ')
            if ans == 'y':
                break
            elif ans == 'n':
                return
    with open(local_verspath, 'w') as f:
        f.write(s)
    print("wrote:", os.path.relpath(local_verspath))


def diff_versions(proj, path, vers_indicator_a, vers_indicator_b, method):
    """
    Arguments
    ---------
    proj: mcapi.Project
    path: str, path in project
    vers_indicator_a: str or int,
        Version number (positive or negative), or 'local', or 'remote' (=="-1") of 'from' file.
    vers_indicator_b: str or int,
        Version number (positive or negative), or 'local', or 'remote' (=="-1") of 'to' file.
    method: function,
        libdiff method to use to compare files
    """
    versions = make_versions(proj, path)
    s_a, verspath_a = version_as_str(proj, path, versions, vers_indicator_a)
    s_b, verspath_b = version_as_str(proj, path, versions, vers_indicator_b)

    refpath = os.path.dirname(proj.local_path)
    local_verspath_a = os.path.join(refpath, verspath_a)
    local_verspath_b = os.path.join(refpath, verspath_b)

    result = method(s_a.splitlines(keepends=True), s_b.splitlines(keepends=True), fromfile=os.path.relpath(local_verspath_a), tofile=os.path.relpath(local_verspath_b))
    sys.stdout.writelines(result)

def make_parser():
    """Make argparse.ArgumentParser for `mc versions`"""

    mc_versions_description = "List, print, download, and compare file versions"

    mc_versions_usage = """
    mc versions <pathspec>
    mc versions <pathspec> --print --version <version_indicator>
    mc versions <pathspec> --down --version <version_indicator>
    mc versions <pathspec> --diff --version <version_indicator> <version_indicator> [--context]"""

    parser = argparse.ArgumentParser(
        description=mc_versions_description,
        usage=mc_versions_usage,
        prog='mc versions')
    parser.add_argument('path', nargs=1, help='File to list versions of')
    parser.add_argument('-v', '--version', nargs="*", default=None, help='Select versions. Use a version id, or \'local\' to compare with local version, or \'remote\' to indicate current remote version.')
    parser.add_argument('-p', '--print', action="store_true", default=False, help='Print selected version')
    parser.add_argument('--down', action="store_true", default=False, help='Download selected version')
    parser.add_argument('--diff', action="store_true", default=False, help='Compare selected versions. By default, \'--versions remote local\' is used')
    parser.add_argument('--context', action="store_true", default=False, help='Print diff using \'context diff\' method')
    return parser

def versions_subcommand(argv, working_dir):
    """
    List, print, download, and compare file versions

    mc versions <pathspec>

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    proj = clifuncs.make_local_project(working_dir)
    pconfig = clifuncs.read_project_config(proj.local_path)

    # convert cli input to materials commons path convention: <projectname>/path/to/file_or_dir
    refpath = os.path.dirname(proj.local_path)

    for p in args.path:
        local_abspath = os.path.abspath(p)
        path = os.path.relpath(local_abspath, refpath)

        if args.print:
            if len(args.version) != 1:
                print("--print requires 1 version id provided to -v,--version")
                raise cliexcept.MCCLIException("Invalid versions request")
            print_version(proj, path, args.version[0])
        elif args.down:
            if len(args.version) != 1:
                print("--down requires 1 version id provided to -v,--version")
                raise cliexcept.MCCLIException("Invalid versions request")
            download_version(proj, path, args.version[0])
        elif args.diff:
            if not args.version or len(args.version) == 0:
                args.version = ['remote', 'local']
            elif len(args.version) != 2:
                print("--diff requires 2 version ids (or strings 'local' or 'remote') if -v,--version is given")
                raise cliexcept.MCCLIException("Invalid versions request")
            if args.context:
                method = difflib.context_diff
            else:
                method = difflib.unified_diff

            diff_versions(proj, path, args.version[0], args.version[1], method)
        else:
            list_versions(proj, path)


    return
