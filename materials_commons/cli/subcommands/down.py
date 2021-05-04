import argparse
import io
import os
import requests
import sys
import time

import materials_commons.api as mcapi
import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.globus as cliglobus
import materials_commons.cli.tree_functions as treefuncs
import materials_commons.cli.file_functions as filefuncs
from materials_commons.cli.treedb import LocalTree, RemoteTree

def _get_current_globus_download(pconfig, proj, verbose=True):
    all_downloads = {download.id:download for download in proj.remote.get_all_globus_download_requests(proj.id)}

    globus_download_id = None
    if pconfig.globus_download_id:
        globus_download_id = pconfig.globus_download_id
        if globus_download_id not in all_downloads:
            if verbose:
                print("Current globus download (name=?, id=" + str(globus_download_id) + ") no longer exists.")
            globus_download_id = None
    if globus_download_id is None:
        name = clifuncs.random_name()
        download = proj.remote.create_globus_download_request(proj.id, name)
        if verbose:
            print("Created new globus download (name=" + download.name + ", id=" + str(download.id) + ").")
        pconfig.globus_download_id = download.id
        pconfig.save()
    else:
        download = all_downloads[globus_download_id]
        if verbose:
            print("Using current globus download (name=" + download.name + ", id=" + str(download.id) + ").")

    return download

def _check_download_file(proj_id, file_id, local_path, remote, force=False):
    """Prompt user for confirmation before overwriting an existing local file

    Arguments
    ---------
    proj_id: int, Project ID
    file_id: int, ID of file to download
    local_path: str, Location to download file. Intermediate directories are created if necessary.
    remote: mcapi.Client, Materials Commons Client
    force: bool (optional, default=False) If True, force overwrite existing file without confirmation.

    Returns
    -------
    local_path: str or None, Location of downloaded file or None if not downloaded
    """
    if not os.path.exists(local_path) or force:
        dir = os.path.dirname(local_path)
        if not os.path.isdir(dir):
            os.makedirs(dir)
        remote.download_file(proj_id, file_id, local_path)
        return local_path
    else:
        print("Overwrite '" + os.path.relpath(local_path, os.getcwd()) + "'?")
        while True:
            ans = input('y/n: ')
            if ans == 'y':
                dir = os.path.dirname(local_path)
                if not os.path.exists(dir):
                    os.makedirs(dir)
                remote.download_file(proj_id, file_id, local_path)
                return local_path
            elif ans == 'n':
                break
    return None


# def _check_download_directory(proj, dir, dirpath=None, recursive=False, force=False):
#
#     if dirpath is None:
#         dirpath = dir.path
#     results = []
#     children = dir.get_children()
#     for child in children:
#
#         if isinstance(child, mcapi.File):
#             p = os.path.join(dirpath, child.name)
#             result_path = _check_download_file(proj.id, child.id, p, proj.remote, force=force)
#             if result_path is not None:
#                 print("downloaded:", os.path.relpath(result_path, os.getcwd()))
#             results.append(result_path)
#
#         elif isinstance(child, mcapi.Directory) and recursive:
#             _check_download_directory(proj, child, dirpath=os.path.join(dirpath, child.name), recursive=recursive, force=force)
#
#     return results

def standard_download(proj, path, force=False, output=None, recursive=False, no_compare=False, localtree=None, remotetree=None):
    """Download files and directories

    Arguments
    ---------
    proj: mcapi.Project, Project to download from

    path: str, Materials Commons style path of file or directory to download

    force: bool (optional, default=False) If True, force overwrite existing files without confirmation.

    output: str (optional, default=None)
        Specify a different download location. By default, files are downloaded to the matching
        location in the local project directory. For example remote file at "/A/B/file.txt" is
        downloaded to "<proj.local_path>/A/B/file.txt" by default.

    recursive: bool (optional, default=False) Download directory contents recursively.

    no_compare: bool (optional, default=False)
        By default, this function checks local and remote file checksum to avoid downloading files
        that already exist. If no_compare is True, this check is skipped and all specified files are
        downloaded, even if an equivalent file already exists locally.

    localtree: LocalTree object (optional, default=None)
        A LocalTree object stores local file checksums to avoid unnecessary hashing. Will be used
        and updated if provided and checksum == True.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.

    Returns
    -------
    success: bool, True if download succeeds, False otherwise
    """
    local_abspath = filefuncs.make_local_abspath(proj.local_path, path)
    printpath = os.path.relpath(local_abspath)

    if output is None:
        output = local_abspath

    checksum = True
    if no_compare:
        checksum = False

    files_data, dirs_data, child_data, non_existing = treefuncs.treecompare(
        proj, [path], checksum=checksum, localtree=localtree, remotetree=remotetree)

    # if remote file:
    if path in files_data and files_data[path]['r_type'] == 'file':

        if files_data[path]['l_type'] == 'directory':
            print(printpath + ": is local directory and remote file")
            return False
        elif 'eq' in files_data[path] and files_data[path]['eq'] and output == local_abspath:
            print(printpath + ": local is equivalent to remote (skipping)")
            return True
        else:
            try:
                result_path =  _check_download_file(proj.id, files_data[path]['id'], output, remote=proj.remote, force=force)
            except Exception as e:
                print(printpath + ": " + str(e) + " (skipping)")
                return False
            if result_path:
                if output != local_abspath:
                    print("downloaded:", os.path.relpath(output))
                else:
                    print("downloaded:", printpath)
                return True
            else:
                return False

    # if directory:
    elif path in dirs_data and dirs_data[path]['r_type'] == 'directory':

        if not recursive:
            print(printpath + ": is a directory")
            return False

        if dirs_data[path]['l_type'] == 'file':
            print(printpath + ": is local file and remote directory")
            return False

        success = True
        for childpath, record in child_data[path].items():
            childoutput = os.path.join(output, os.path.basename(childpath))
            success &= standard_download(proj, childpath, force=force, output=childoutput, recursive=recursive, no_compare=no_compare, localtree=localtree, remotetree=remotetree)
        return success

    else:
        print(printpath + ": does not exist on remote")
        return False

def download_file_as_string(client, project_id, file_id):
    urlpart = "/projects/" + str(project_id) + "/files/" + str(file_id) + "/download"
    url = client.base_url + urlpart
    with requests.get(url, stream=True, verify=False, headers=client.headers) as r:
        client._handle(r)
        f = io.BytesIO()
        for block in r.iter_content(chunk_size=8192):
            f.write(block)
        return f.getvalue().decode('utf-8')

def print_file(proj, path):
    """Print a remote file, without writing it locally

    Arguments
    ---------
    proj: mcapi.Project, Project to get file from

    path: str, Materials Commons style path of file to print

    """
    local_abspath = filefuncs.make_local_abspath(proj.local_path, path)
    printpath = os.path.relpath(local_abspath)
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

def down_subcommand(argv):
    """
    download files from Materials Commons

    mc down [-r] [<pathspec> ...]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    pconfig = clifuncs.read_project_config()
    proj = clifuncs.make_local_project()
    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths)

    localtree = None
    if not args.no_compare:
        localtree = LocalTree(proj.local_path)

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    # validate
    if args.print and len(args.paths) != 1:
        print("--print option acts on 1 file, received", len(args.paths))
        raise cliexcept.MCCLIException("Invalid download request")
    if args.output and len(args.paths) != 1:
        print("--output option acts on 1 file or directory, received", len(args.paths))
        raise cliexcept.MCCLIException("Invalid download request")

    if args.globus:
        download = _get_current_globus_download(pconfig, proj)

        if download.status != 0:    # TODO clean up status code / message

            print("Checking if download is ready.", end='', flush=True)
            count = 0
            while download.status != 0 and count < 5:
                time.sleep(2)
                print(".", end='', flush=True)
                download = _get_current_globus_download(pconfig, proj, verbose=False)
                count += 1
            print("", flush=True)

        if download.status != 0:
            print("Current Globus download (name=" + download.name + ", id=" + str(download.id) + ")"
                + " not ready for downloading. Materials Commons is still preparing the project"
                + " files for download. For large projects this may take some time.")
            print("Use `mc globus download` to check when it is ready and try again.")
            raise cliexcept.MCCLIException("Invalid download request")

        print("Download is ready.")

        label = proj.name + "-" + download.name
        if args.label:
            label = args.label[0]

        globus_ops = cliglobus.GlobusOperations()
        task_id = globus_ops.download_v0(proj, paths, download, recursive=args.recursive, label=label, localtree=localtree, remotetree=remotetree, force=args.force)

        if task_id:
            print("Globus transfer task initiated.")
            print("Use `globus task list` to monitor task status.")
            print("Use `mc globus download` to manage Globus downloads.")
            print("Multiple transfer tasks may be initiated.")
            print("When all tasks finish downloading, use `mc globus download --id " + str(download.id) +
                " --delete` " + "to close the download.")

    elif args.print:
        print_file(proj, paths[0])

    else:

        output=None
        if args.output:
            output = os.path.abspath(args.output[0])

        for path in paths:
            standard_download(proj, path, force=args.force, output=output, recursive=args.recursive, no_compare=args.no_compare, localtree=localtree, remotetree=remotetree)

    return
