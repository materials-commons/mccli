import argparse
import copy
import os
import sys
import time

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
import materials_commons.cli.file_functions as filefuncs
from materials_commons.cli.treedb import LocalTree, RemoteTree

#  Want to print() something like:
#
#  warning! file0 is a local file and remote dir!
#  warning! file1 is a local directory and remote file!
#
#  remote_owner local_mtime local_size remote_mtime remote_size file0
#             - local_mtime local_size            -           - file1
#  remote_owner           -          - remote_mtime remote_size file2
#
#  dir1:
#  remote_owner local_mtime local_size remote_mtime remote_size file0
#             - local_mtime local_size            -           - file1
#  remote_owner           -          - remote_mtime remote_size file2
#
#  dir2:
#  remote_owner local_mtime local_size remote_mtime remote_size file0
#             - local_mtime local_size            -           - file1
#  remote_owner           -          - remote_mtime remote_size file2
#

def _format_path_data(proj, data, columns, refpath=None, checksum=False):
    """Format list of dict for 'mc ls'.

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    data: dict
        Output from `materials_commons.cli.tree_functions.ls_data`

    columns: list of str
        Columns to collect. Expects ['l_mtime', 'l_size', 'l_type', 'r_mtime', 'r_size', 'r_type', 'eq' (optionally), 'name', 'id', 'selected' (optionally)]

    refpath: str or None
        Local absolute path that names are printed relative to. If None, uses os.getcwd().

    checksum: bool (optional, default=False)
        If True, include 'eq' in the output data.

    Returns
    -------
    path_data: list of dict, Sorted by name and containing
        'l_mtime', 'l_size', 'l_type', 'r_mtime', 'r_size', 'r_type', 'eq' (optionally), 'name', 'id'
    """
    path_data = []

    # import json
    # for record in data:
    #     print(json.dumps(record, indent=2))
    #     print("-------------------")

    record_init = {k: '-' for k in columns}
    if refpath is None:
        refpath = os.getcwd()

    def _get_name(mcpath):
        from os.path import abspath, dirname, join, relpath
        local_abspath = filefuncs.make_local_abspath(proj.local_path, mcpath)
        return relpath(local_abspath, refpath)

    for path in sorted(data.keys()):
        record = copy.deepcopy(record_init)
        rec = data[path]

        if not rec['l_type'] and not rec['r_type']:
            continue

        if rec['l_mtime'] is not None:
            record['l_mtime'] = clifuncs.format_time(rec['l_mtime'])
        if rec['l_size'] is not None:
            record['l_size'] = clifuncs.humanize(rec['l_size'])
        if rec['l_type'] is not None:
            record['l_type'] = rec['l_type']
        if rec['r_mtime'] is not None:
            record['r_mtime'] = clifuncs.format_time(rec['r_mtime'])
        if rec['r_size'] is not None:
            record['r_size'] = clifuncs.humanize(rec['r_size'])
        if rec['r_type'] is not None:
            record['r_type'] = rec['r_type']
        if 'eq' in rec and rec['eq'] is not None:
            record['eq'] = rec['eq']
        record['name'] = _get_name(path)
        if 'id' in rec and rec['id'] is not None:
            record['id'] = rec['id']
        if 'selected' in rec and rec['selected'] is not None:
            record['selected'] = rec['selected']
        if 'selected_by' in rec and rec['selected_by'] is not None:
            record['selected_by'] = rec['selected_by']

        path_data.append(record)

    return sorted(path_data, key=lambda k: k['name'])

def _ls_print(proj, data, refpath=None, printjson=False, checksum=False, checkdset=False):
    """Print treecompare output for a set of files, or directory children"""

    # print warnings for type mismatches
    treefuncs.warn_for_type_mismatches(data)

    if checksum:
        columns = ['l_mtime', 'l_size', 'l_type', 'r_mtime', 'r_size', 'r_type', 'eq', 'name', 'id']
        headers = ['l_updated_at', 'l_size', 'l_type', 'r_updated_at', 'r_size', 'r_type', 'eq', 'name', 'id']
    else:
        columns = ['l_mtime', 'l_size', 'l_type', 'r_mtime', 'r_size', 'r_type', 'name', 'id']
        headers = ['l_updated_at', 'l_size', 'l_type', 'r_updated_at', 'r_size', 'r_type', 'name', 'id']

    if checkdset:
        columns += ['selected', 'selected_by']

    if printjson:
        for path, record in data.items():
            if record['r_obj']:
                print(record['r_obj'].input_data)
    else:
        path_data = _format_path_data(proj, data, columns, refpath=refpath, checksum=checksum)
        if len(path_data):
            if refpath:
                print(os.path.relpath(refpath, os.getcwd()) + ":")
            clifuncs.print_table(path_data, columns=columns, headers=headers)
            print("")
        else:
            if refpath:
                print(os.path.relpath(refpath, os.getcwd()) + ": no contents")

def make_parser():
    """Make argparse.ArgumentParser for `mc ls`"""
    parser = argparse.ArgumentParser(
        description='List local & remote directory contents',
        prog='mc ls')
    parser.add_argument('paths', nargs='*', default=[os.getcwd()], help='Files or directories')
    parser.add_argument('--checksum', action="store_true", default=False, help='Calculate MD5 checksum for local files')
    parser.add_argument('--json', action="store_true", default=False, help='Print JSON exactly')

    # TODO: re-implement w/datasets
    # # --include-files file_or_dir
    # # --exclude-files file_or_dir
    # # --clear-files file_or_dir
    parser.add_argument('--dataset', type=str, default="", metavar='DATASET_ID', help='Specify a dataset to act on.')
    parser.add_argument('--include', action="store_true", default=False, help='Include files and directories in the specified dataset. Including a directory includes all files and sub-directories recursively, unless prevented by --exclude-files.')
    parser.add_argument('--exclude', action="store_true", default=False, help='Exclude file and directories from the specified dataset. Excluding prevents inclusion of files and directories that would otherwise be included due to a higher-level directory being included.')
    parser.add_argument('--clear', action="store_true", default=False, help='Clear files and directories from the include/exclude selection lists of the specified dataset. A file or directory may still be included in or excluded from the dataset afterwards if a higher level directory is included or excluded')
    return parser

def ls_subcommand(argv):
    """
    'ls' a project directory to see local and remote files and directories.

    mc ls [<pathspec> ...]

    """
    parser = make_parser()
    args = parser.parse_args(argv)
    updatetime = time.time()

    proj = clifuncs.make_local_project()
    pconfig = clifuncs.read_project_config()

    # convert cli input to materials commons path convention: /path/to/file_or_dir
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths)

    if args.checksum:
        localtree = LocalTree(proj.local_path)
    else:
        localtree = None

    if args.json:
        remotetree = None
    else:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    # compare local and remote tree
    files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
        proj, mcpaths, checksum=args.checksum,
        localtree=localtree, remotetree=remotetree)

    for p in mcpaths:
        if treefuncs.is_type_mismatch(p, files_data, dirs_data):
            print("** WARNING: ", p, "local and remote types do not match! **")

    if pconfig.remote_updatetime:
        print("** Fetch lock ON at:", clifuncs.format_time(pconfig.remote_updatetime), "**")

    if not_existing:
        for path in not_existing:
            local_abspath = filefuncs.make_local_abspath(proj.local_path, path)
            print(os.path.relpath(local_abspath) + ": No such file or directory")
        print("")

    if args.dataset:
        raise MCCLIException("dataset actions are not yet implemented") #TODO datasets
        dataset = mcapi.get_dataset(proj.id, args.dataset, remote=proj.remote)
        file_selection = dataset.input_data['file_selection']
        # print(json.dumps(file_selection, indent=2))

        if args.include or args.exclude or args.clear:
            for p in mcpaths:

                if treefuncs.is_type_mismatch(p, files_data, dirs_data) and not args.clear:
                    print(p + ": Local and remote types do not match, skipping")
                    continue

                if args.include:
                    raise MCCLIException("--include is not yet implemented") #TODO w/datasets
                    if p in files_data:
                        file_selection['include_files'].append(p)
                        if p in file_selection['exclude_files']:
                            file_selection['exclude_files'].remove(p)
                    if p in dirs_data:
                        file_selection['include_dirs'].append(p)
                        if p in file_selection['exclude_dirs']:
                            file_selection['exclude_dirs'].remove(p)
                elif args.exclude:
                    raise MCCLIException("--exclude is not yet implemented") #TODO w/datasets
                    if p in files_data:
                        file_selection['exclude_files'].append(p)
                        if p in file_selection['include_files']:
                            file_selection['include_files'].remove(p)
                    elif p in dirs_data:
                        file_selection['exclude_dirs'].append(p)
                        if p in file_selection['include_dirs']:
                            file_selection['include_dirs'].remove(p)
                elif args.clear:
                    raise MCCLIException("--clear is not yet implemented") #TODO w/datasets
                    for name in ['include_files', 'exclude_files', 'include_dirs', 'exclude_dirs']:
                        if p in file_selection[name]:
                            file_selection[name].remove(p)

            # remove duplicates
            for name in ['include_files', 'exclude_files', 'include_dirs', 'exclude_dirs']:
                file_selection[name] = list(set(file_selection[name]))

            dataset = mcapi.update_dataset_file_selection(proj.id, dataset.id, file_selection)
            file_selection = dataset.input_data['file_selection']
            # print("Updated dataset file selection:")
            # print(json.dumps(file_selection, indent=2))

        for f in files_data:
            selected, selected_by = mcapi.check_file_selection(f, file_selection)
            files_data[f]['selected'] = selected
            if selected_by:
                files_data[f]['selected_by'] = selected_by

        for key, child_files_data in child_data.items():
            for f in child_files_data:
                selected, selected_by = mcapi.check_file_selection(f, file_selection)
                child_files_data[f]['selected'] = selected
                if selected_by:
                    child_files_data[f]['selected_by'] = selected_by

    # print files
    _ls_print(proj, files_data, refpath=None, printjson=args.json, checksum=args.checksum, checkdset=args.dataset)

    # print directory children
    for d in child_data:
        local_dirpath = filefuncs.make_local_abspath(proj.local_path, d)
        _ls_print(proj, child_data[d], refpath=local_dirpath, printjson=args.json, checksum=args.checksum, checkdset=args.dataset)


    return
