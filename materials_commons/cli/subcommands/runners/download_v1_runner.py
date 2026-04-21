import os

import materials_commons.cli.old.file_functions as filefuncs
import materials_commons.cli.old.functions as clifuncs
import materials_commons.cli.old.tree_functions as treefuncs
from materials_commons.cli.old.treedb import LocalTree, RemoteTree


def run_v1_download(args, working_dir):
    pconfig = clifuncs.read_project_config(working_dir)
    proj = clifuncs.make_local_project(working_dir)
    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths,
                                          working_dir)

    output = None
    if args.output:
        output = os.path.abspath(args.output[0])

    localtree = None
    if not args.no_compare:
        localtree = LocalTree(proj.local_path)

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    for path in paths:
        standard_download(proj, path, working_dir, force=args.force,
                          output=output, recursive=args.recursive,
                          no_compare=args.no_compare, localtree=localtree,
                          remotetree=remotetree)


def standard_download(proj, path, working_dir, force=False, output=None, recursive=False, no_compare=False,
                      localtree=None, remotetree=None):
    """Download files and directories

    Arguments
    ---------
    proj: mcapi.Project, Project to download from

    path: str, Materials Commons style path of file or directory to download

    working_dir (str): Current working directory, used for finding relative
        paths and printing messages.

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
    printpath = os.path.relpath(local_abspath, start=working_dir)

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
                result_path = _check_download_file(proj.id,
                                                   files_data[path]['id'],
                                                   output, proj.remote,
                                                   working_dir, force=force)
            except Exception as e:
                print(printpath + ": " + str(e) + " (skipping)")
                return False
            if result_path:
                if output != local_abspath:
                    print("downloaded:", printpath, "as",
                          os.path.relpath(output, start=working_dir))
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
            success &= standard_download(proj, childpath, working_dir,
                                         force=force, output=childoutput,
                                         recursive=recursive,
                                         no_compare=no_compare,
                                         localtree=localtree,
                                         remotetree=remotetree)
        return success

    else:
        print(printpath + ": does not exist on remote")
        return False


def _check_download_file(proj_id, file_id, local_path, remote, working_dir, force=False):
    """Prompt user for confirmation before overwriting an existing local file

    Arguments
    ---------
    proj_id: int, Project ID
    file_id: int, ID of file to download
    local_path: str, Location to download file. Intermediate directories are created if necessary.
    remote: mcapi.Client, Materials Commons Client
    working_dir (str): Current working directory, used for finding relative
        paths and printing messages.
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
        print("Overwrite '" + os.path.relpath(local_path, working_dir) + "'?")
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
