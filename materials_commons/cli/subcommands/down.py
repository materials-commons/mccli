import argparse
import asyncio
import io
import os

import requests

import materials_commons.cli.old.exceptions as cliexcept
import materials_commons.cli.old.file_functions as filefuncs
import materials_commons.cli.old.functions as clifuncs
import materials_commons.cli.old.tree_functions as treefuncs
from materials_commons.cli.subcommands.runners.download_globus_runner import run_globus_download
from materials_commons.cli.subcommands.runners.download_v1_runner import run_v1_download
from materials_commons.cli.subcommands.runners.download_v2_runner import run_v2_download


def make_parser():
    """Make argparse.ArgumentParser for `mc down`"""

    mc_down_description = "Download files from Materials Commons"

    mc_down_usage = """
    mc down [-r] [-p] [-o] [-f] [--no-compare] <pathspec> [<pathspec> ...]
    mc down -p <pathspec>
    mc down -g [-r] [--no-compare] [--label] <pathspec> [<pathspec> ...]"""

    parser = argparse.ArgumentParser(
        description=mc_down_description,
        usage=mc_down_usage,
        prog='mc down')
    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action="store_true", default=False,
                        help='Download directory contents recursively')
    parser.add_argument('--v2', action="store_true", default=False,
                        help='Download files using download_async_v2')
    parser.add_argument('-f', '--force', action="store_true", default=False,
                        help='Force overwrite of existing files')
    parser.add_argument('-p', '--print', action="store_true", default=False,
                        help='Print file, do not write')
    parser.add_argument('-o', '--output', nargs=1, default=None, help='Download file name')
    parser.add_argument('-g', '--globus', action="store_true", default=False,
                        help='Use globus to download files.')
    parser.add_argument('--label', nargs=1, type=str,
                        help='Globus transfer label to make finding tasks simpler.')
    parser.add_argument('--no-compare', action="store_true", default=False,
                        help='Download remote without checking if local is equivalent.')
    return parser


def down_subcommand(argv, working_dir):
    """
    download files from Materials Commons

    mc down [-r] [<pathspec> ...]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    # validate
    if args.print and len(args.paths) != 1:
        print("--print option acts on 1 file, received", len(args.paths))
        raise cliexcept.MCCLIException("Invalid download request")
    if args.output and len(args.paths) != 1:
        print("--output option acts on 1 file or directory, received", len(args.paths))
        raise cliexcept.MCCLIException("Invalid download request")
    if args.output and args.globus:
        print("--output option is not supported with --globus")
        raise cliexcept.MCCLIException("Invalid upload request")
    if args.output and args.v2:
        print("--output option is not supported with --v2")

    if args.globus:
        run_globus_download(args, working_dir)
    elif args.print:
        print_file(args, working_dir)
    elif args.v2:
        asyncio.run(run_v2_download(args, working_dir))
    else:
        run_v1_download(args, working_dir)
    return


def print_file(args, working_dir):
    """Print a remote file without writing it locally

    Arguments
    ---------
    args: argparse.Namespace, Command line arguments

    working_dir (str): Current working directory, used for finding relative
        paths and printing messages.

    """

    proj = clifuncs.make_local_project(working_dir)
    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths,
                                          working_dir)
    path = paths[0]

    local_abspath = filefuncs.make_local_abspath(proj.local_path, path)
    printpath = os.path.relpath(local_abspath, start=working_dir)
    file = filefuncs.get_by_path_if_exists(proj.remote, proj.id, path)
    if not file:
        print(printpath + ": No such file or directory on remote")
        return
    if filefuncs.isdir(file):
        print(printpath + ": Is a directory on remote")
        return

    s = download_file_as_string(proj.remote, proj.id, file.id)
    print(printpath + ":")
    print(s, end='')


def download_file_as_string(client, project_id, file_id):
    urlpart = "/projects/" + str(project_id) + "/files/" + str(file_id) + "/download"
    url = client.base_url + urlpart
    with requests.get(url, stream=True, verify=False, headers=client.headers) as r:
        client._handle(r)
        f = io.BytesIO()
        for block in r.iter_content(chunk_size=8192):
            f.write(block)
        return f.getvalue().decode('utf-8')
