import argparse
import os
import sys

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.treedb import RemoteTree

def make_parser():
    """Make argparse.ArgumentParser for `mc mv`"""
    desc = "Move files. Use `mc move <src> <target>` to move and/or rename a file or directory. Use `mc move <src> ... <directory>` to move a list of files or directories into an existing directory."

    parser = argparse.ArgumentParser(
        description=desc,
        prog='mc mv')
    parser.add_argument('paths', nargs="*", help='Sources and target or directory destination')
    parser.add_argument('--remote-only', action="store_true", default=False,
                        help='Move remote files only. Does not compare to local files.')
    return parser

def mv_subcommand(argv):
    """
    Move files

    mc move <src> <target>
    mc move <src> ... <directory>

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    if not args.paths or len(args.paths) < 2:
        print("Expects 2 or more paths: `mc mv <src> <target>` or `mc mv <src> ... <directory>`")
        exit(1)

    proj = clifuncs.make_local_project()
    pconfig = clifuncs.read_project_config()

    localtree = None

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    # convert cli input to materials commons path convention: /path/to/file_or_dir
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths)

    treefuncs.move(proj, mcpaths, remote_only=args.remote_only, localtree=localtree, remotetree=remotetree)

    return
