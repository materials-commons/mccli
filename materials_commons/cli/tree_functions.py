import copy
import igittigitt
import json
import os
import pathlib
import requests
import shutil
from sortedcontainers import SortedSet
import time

import materials_commons.api as mcapi
import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.file_functions as filefuncs

def clipaths_to_local_abspaths(proj_local_path, clipaths, working_dir):
    """Convert CLI paths input to local absolute paths

    Args:
        proj_local_path (str): Path to Materials Commons project
        clipaths (List of str): Indicates files and directories, either
            absolute paths or relative to current working directory
        working_dir (str): Directory cli_paths are relative to.

    Returns:
        List local absolute paths to upload, excluding the `.mc` directory.

    Raises:
        MCCLIException, if any path in clipaths is not within the local project
        directory.
    """
    if not os.path.isabs(working_dir):
        raise cliexcept.MCCLIException("Error in clipaths_to_mcpaths: working_dir is not absolute")
    local_abspaths = []
    for p in clipaths:
        if not os.path.isabs(p):
            p = os.path.join(working_dir, p)
        local_abspaths.append(p)
    return local_abspaths

def clipaths_to_mcpaths(proj_local_path, clipaths, working_dir):
    """Convert CLI paths input to Materials Commons standardized paths

    Args:
        proj_local_path (str): Path to Materials Commons project
        clipaths (List of str): Indicates files and directories, either absolute paths or relative to current working directory
        working_dir (str): Directory cli_paths are relative to.

    Returns:
        List Materials Commons paths (does not include project top directory, starts with "/") to
        upload, excluding the `.mc` directory.

    Raises:
        MCCLIException, if any path in clipaths is not within the local project
        directory.
    """
    if not os.path.isabs(working_dir):
        raise cliexcept.MCCLIException("Error in clipaths_to_mcpaths: working_dir is not absolute")
    mcpaths = []
    for p in clipaths:
        if not os.path.isabs(p):
            p = os.path.join(working_dir, p)
        mcpath = filefuncs.make_mcpath(proj_local_path, p)
        mcpaths.append(mcpath)
    return mcpaths

def make_local_abspaths_for_upload(proj_local_path, paths):
    """Clean paths for uploads

    This is written for identifying uploads. If the top directory is included
    it replaces it with all children except `.mc`.

    Args:
        proj_local_path (str): Path to project
        paths (iterable of str): Local absolute paths to filter

    Returns:
        List of str: Materials Commons paths, filtered as described above.
    """
    _paths = []
    for path in paths:
        if os.path.normpath(path) == os.path.normpath(proj_local_path):
            for child in os.listdir(proj_local_path):
                if child == ".mc":
                    continue
                _paths.append(os.path.join(os.path.normpath(proj_local_path), child))
        else:
            _paths.append(path)
    return _paths

def make_mcpaths_for_upload(proj_local_path, paths):
    """Clean paths for uploads

    This is written for identifying uploads. If the top directory is included
    it replaces it with all children except `.mc`.

    Args:
        proj_local_path (str): Path to project
        paths (iterable of str): Paths to filter and convert to absolute paths

    Returns:
        List of str: Materials Commons paths, filtered as described above.
    """
    _paths = []
    for path in paths:
        if path == "/":
            for child in os.listdir(proj_local_path):
                if child == ".mc":
                    continue
                _paths.append(os.path.join("/", child))
        else:
            _paths.append(path)
    return _paths

def upload_file(proj, local_abspath, mcpath, working_dir, parent_id=None, limit=250, remotetree=None, update_remotetree=True):
    """Upload one file

    Notes:
        - Creates parent and intermediate directories as necessary
        - Does not allow filename change, with message:
            `--upload-as file name changed (skipping)`
        - Will not upload files over the size `limit`, with message:
            `file too large (size={1}MB, limit={0}MB) (not uploaded)`

    Args:
        proj (:class:`materials_commons.api.Project`): Project instance with
            proj.local_path indicating local project location
        local_abspath (str): Local absolute path to file to be uploaded
        mcpath (dict): Path where the file will be uploaded. Currently, the basename must be
            the same as local_abspath.
        parent_id (str): ID of parent directory where the file will be uploaded. May be
            None, in which case the directory os.path.dirname(mcpath) will be created.
        working_dir (str): Current working directory, used for making relative
            paths and printing messages.
        limit (int): The limit in MB on the size of the file allowed to be uploaded.
        remotetree (RemoteTree): A RemoteTree object stores remote file and
            directory information to minimize API calls and data transfer.
            Optional, will be used and updated if provided.
        update_remotetree (bool): Set to False to skip updating remotetree for the uploaded
            file. Used when updating via parent directory is preferrable.

    Returns:
        (file_result, error_result):

        file_result: file
            Successfully uploaded files

        error_results: str
            Error messages for unsuccessful file uploads
    """
    file_result = None
    error_result = None

    printpath = os.path.relpath(local_abspath, start=working_dir)

    # for upload_as, if destination basename differs from source, we do a rename
    if os.path.basename(local_abspath) != os.path.basename(mcpath):
        msg = printpath + ": --upload-as file name changed (skipping)"
        print(msg)
        extended_msg = "The --upload-as option may be used to upload a directory with a different "\
            "name, or upload a file to a different directory. To change a file name, first "\
            "upload, then mv."
        print(extended_msg)
        return (file_result, msg)

    # if remote parent does not exist / not known -> mkdir
    #   -> raises exception for failing to mkdir
    if parent_id is None:
        parent_mcpath = os.path.dirname(mcpath)
        parent = mkdir(proj, parent_mcpath, remote_only=True, create_intermediates=True,
                       remotetree=remotetree)
        if parent.path != parent_mcpath:
            msg = "Upload error: "
            msg += " expected parent.path=" + os.path.dirname(parent_mcpath)
            msg += " got parent.path=" + parent.path
            print(msg)
            return (file_result, msg)
        parent_id = parent.id

    # if file size > limit -> error
    file_size_mb = os.path.getsize(local_abspath) >> 20
    if file_size_mb > limit:
        msg = printpath + ": file too large (size={1}MB, limit={0}MB) (not uploaded)".\
            format(limit, file_size_mb)
        print(msg)
        return (file_result, msg)

    # else: -> upload, return results
    file_result = proj.remote.upload_file(proj.id, parent_id, local_abspath)
    if not filefuncs.isfile(file_result):
        msg = printpath + ": unknown error (not uploaded)"
        print(msg)
        return (file_result, msg)

    if remotetree and update_remotetree and file_result:
        print("upload_file remotetree.update")
        remotetree.connect()
        remotetree.update(mcpath, force=True, get_children=False)
        remotetree.close()

    printdestpath = os.path.relpath(
        filefuncs.make_local_abspath(proj.local_path, mcpath),
        start=working_dir)

    if printpath == printdestpath:
        print("uploaded:", printpath)
    else:
        print("uploaded:", printpath, "as", printdestpath)
    return (file_result, error_result)

def check_and_upload_file(proj, local_abspath, working_dir, limit=250, no_compare=False,
    upload_as=None, localtree=None, remotetree=None, parent_id=None, child_data=None,
    update_remotetree=True):
    """Checks validity and upload one file

    Notes:
        - Checks that target is not a directory, with message:
            `remote is directory (skipping)`
        - Depending on options given, checks if existing remote file is equivalent, with message:
            `local is equivalent to remote (skipping)"
        - Options allow providing the remote parent directory ID, or parent directory `child_data`
          `treecompare` output to reduce the number of API calls

    Args:
        proj (:class:`materials_commons.api.Project`): Project instance with
            proj.local_path indicating local project location
        local_abspath (str): Local absolute path to file to be uploaded
        working_dir (str): Current working directory, used for making relative
            paths and printing messages.
        limit (int): The limit in MB on the size of the file allowed to be uploaded.
        no_compare (bool): By default, this function checks local and remote
            file checksum to avoid downloading files that already exist. If
            no_compare is True, this check is skipped and all specified files
            are downloaded, even if an equivalent file already exists locally.
        upload_as (str): Materials Commons style path specifying where to
            upload. Requires `len(paths) == 1`.
        localtree (LocalTree): A LocalTree object stores local file checksums
            to avoid unnecessary hashing. Optional, will be used and updated if
            provided and checksum == True.
        remotetree (RemoteTree): A RemoteTree object stores remote file and
            directory information to minimize API calls and data transfer.
            Optional, will be used and updated if provided.
        parent_id (str): ID of parent directory where the file will be uploaded. May be
            None, in which case the directory will be created if necessary.
        child_data (dict or None): If available, the `child_data` output from `treecompare`.
            If this file is being uploaded as part of a directory upload, the `child_data`
            comparing the local and remote files might already be available.
        update_remotetree (bool): Set to False to skip updating remotetree for the uploaded
            file. Used when updating via parent directory is preferrable.

    Returns:
        (file_result, error_result):

        file_result: file
            Successfully uploaded files

        error_results: str
            Error messages for unsuccessful file uploads
    """
    file_result = None
    error_result = None

    printpath = os.path.relpath(local_abspath, start=working_dir)

    # get checksum info to compare local and remote files?
    checksum = True
    if no_compare:
        checksum = False

    # Materials Commons style path, where to upload
    mcpath = None
    if upload_as is None:
        mcpath = filefuncs.make_mcpath(proj.local_path, local_abspath)
    else:
        mcpath = upload_as
        checksum = False

    if child_data is not None and mcpath in child_data:

        # if remote exists and is a directory -> error, continue
        if child_data[mcpath]['r_type'] == 'directory':
            msg = printpath + ": remote is directory (skipping)"
            print(msg)
            error_resuts[local_abspath] = msg
            return (file_results, error_results)

        # if local and remote files exists, and checksums known and match -> skip, continue
        if 'eq' in child_data[mcpath] and child_data[mcpath]['eq'] is True:
            msg = printpath + ": local is equivalent to remote (skipping)"
            print(msg)
            error_resuts[child_local_abspath] = msg
            return (file_results, error_results)

        # else, get parent_id if not already known (might be None)
        if parent_id is None:
            parent_id = child_data[mcpath]['parent_id']

    else:

        files_data, dirs_data, child_data, non_existing = treecompare(
            proj, [mcpath], checksum=checksum, localtree=localtree,
            remotetree=remotetree, get_children=False)

        # if remote exists and is a directory -> error, continue
        if mcpath in dirs_data and dirs_data[mcpath]['r_type'] == 'directory':
            msg = printpath + ": remote is directory (skipping)"
            print(msg)
            return (file_result, msg)

        # if remote file exists
        if mcpath in files_data:
            file_data = files_data[mcpath]

            # if checksums known and match -> skip, continue
            if 'eq' in file_data and file_data['eq'] is True:
                msg = printpath + ": local is equivalent to remote (skipping)"
                print(msg)
                return (file_result, msg)

            # else, get parent_id if not already known (still might be None)
            if parent_id is None:
                parent_id = file_data['parent_id']

    return upload_file(proj, local_abspath, mcpath, working_dir, parent_id=parent_id,
                       limit=limit, remotetree=remotetree,
                       update_remotetree=update_remotetree)


def filter_local_abspaths(proj_local_path, local_abspaths, working_dir):
    """Filter local_abspaths, skipping .mc and those specified by .mcignore

    Args:
        proj_local_path (str): Path to project
        local_abspaths (List of str): Local absolute paths to file or directories to be uploaded
        working_dir (str): Current working directory, used for making relative
            paths and printing messages.

    Returns:
        _local_abspaths: List of str
            Filtered local absolute paths

    """
    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj_local_path, filename=".mcignore",
        add_default_patterns=False)
    _local_abspaths = []
    for local_abspath in local_abspaths:
        name = os.path.basename(local_abspath)
        if name == ".mc":
            continue
        if ignore_parser.match(pathlib.Path(local_abspath)):
            continue
        _local_abspaths.append(local_abspath)
    return _local_abspaths


def check_and_upload_directory(proj, local_abspath, working_dir, limit=250,
    no_compare=False, upload_as=None, localtree=None, remotetree=None, parent_id=None):
    """Checks validity and uploads a directory and contents recursively

    Notes:
        - Checks that target is not a file, with message:
            `remote is file (skipping)`
        - Options allow providing the remote parent directory ID to reduce the number of API calls

    Args:
        proj (:class:`materials_commons.api.Project`): Project instance with
            proj.local_path indicating local project location
        local_abspath (str): Local absolute path to directory to be uploaded recursively
        working_dir (str): Current working directory, used for making relative
            paths and printing messages.
        limit (int): The limit in MB on the size of the files allowed to be uploaded.
        no_compare (bool): By default, this function checks local and remote
            file checksum to avoid downloading files that already exist. If
            no_compare is True, this check is skipped and all specified files
            are downloaded, even if an equivalent file already exists locally.
        upload_as (str or None): Materials Commons style path specifying where to
            upload. If None, use the equivalent location within the project.
        localtree (LocalTree): A LocalTree object stores local file checksums
            to avoid unnecessary hashing. Optional, will be used and updated if
            provided and checksum == True.
        remotetree (RemoteTree): A RemoteTree object stores remote file and
            directory information to minimize API calls and data transfer.
            Optional, will be used and updated if provided.
        parent_id (str): ID of parent directory where the file will be uploaded. May be
            None, in which case the directory will be created if necessary.


    Returns:
        (file_results, error_results):

        file_results: dict of path: file
            Successfully uploaded files

        error_results: dict of path: str
            Error messages for unsuccessful or skipped uploads

    """
    file_results = {}
    error_results = {}

    printpath = os.path.relpath(local_abspath, start=working_dir)

    if not os.path.isdir(local_abspath):
        msg = printpath + ": not a directory (skipping)"
        print(msg)
        error_results[local_abspath] = msg
        return (file_results, error_results)

    # get checksum info to compare local and remote files?
    checksum = True
    if no_compare:
        checksum = False

    # Materials Commons style path, where to upload
    mcpath = None
    if upload_as is None:
        mcpath = filefuncs.make_mcpath(proj.local_path, local_abspath)
    else:
        mcpath = upload_as
        checksum = False

    # check remote & children
    files_data, dirs_data, child_data, non_existing = treecompare(
        proj, [mcpath], checksum=checksum, localtree=localtree,
        remotetree=remotetree, get_children=True)

    # if remote exists and is a file -> error, continue
    if mcpath in files_data and files_data[mcpath]['r_type'] == 'file':
        msg = printpath + ": remote is file (skipping)"
        print(msg)
        error_results[local_abspath] = msg
        return (file_results, error_results)

    id = None
    # if remote directory exists, get id
    if mcpath in dirs_data and dirs_data[mcpath]['r_type'] == 'directory':
        id = dirs_data[mcpath]['id']

    # if remote directory does not exist -> create directory
    if id is None:
        dir = mkdir(proj, mcpath, remote_only=True, create_intermediates=True,
                    remotetree=remotetree, parent_id=parent_id)
        if dir is None:
            msg = printpath + ": error creating directory (skipping)"
            print(msg)
            error_results[local_abspath] = msg
            return (file_results, error_results)
        id = dir.id

    # collect children
    child_local_abspaths = []
    for name in os.listdir(local_abspath):
        child_local_abspaths.append(os.path.join(local_abspath, name))

    # filter out .mc and those specified by .mcignore
    child_local_abspaths = filter_local_abspaths(proj.local_path, child_local_abspaths, working_dir)

    # upload children
    for child_local_abspath in child_local_abspaths:
        child_upload_as = None
        if upload_as is not None:
            child_upload_as = os.path.join(mcpath, os.path.basename(child_local_abspath))

        # for each child file: do check_and_upload_file
        if os.path.isfile(child_local_abspath):

            file_result, error_msg = check_and_upload_file(proj, child_local_abspath, working_dir,
                limit=limit, no_compare=no_compare, upload_as=child_upload_as, localtree=localtree,
                remotetree=remotetree, parent_id=id, child_data=child_data, update_remotetree=True)

            if file_result is not None:
                file_results[child_local_abspath] = file_result
            if error_msg is not None:
                error_results[child_local_abspath] = error_msg

        # for each child directory: do recursive check_and_upload_directory
        elif os.path.isdir(child_local_abspath):

            file_results_tmp, error_results_tmp = \
                check_and_upload_directory(proj, child_local_abspath, working_dir, limit=limit,
                    no_compare=no_compare, upload_as=child_upload_as, localtree=localtree,
                    remotetree=remotetree, parent_id=id)

            for tpath in file_results_tmp:
                file_results[tpath] = file_results_tmp[tpath]
            for tpath in error_results_tmp:
                error_results[tpath] = error_results_tmp[tpath]

        # local child does not actually exist, could happen in race conditions
        else:
            pass

    return (file_results, error_results)


def standard_upload_v2(proj, paths, working_dir, recursive=False, limit=250, no_compare=False, upload_as=None, localtree=None, remotetree=None):
    """Upload files and directories to Materials Commons

    Args:
        proj (:class:`materials_commons.api.Project`): Project instance with
            proj.local_path indicating local project location
        paths (List of str):
            List of paths to upload. Expects local absolute paths, or paths
            relative to working_dir.
        working_dir (str): Current working directory, used for finding relative
            paths and printing messages.
        recursive (bool): If True, remove directories recursively. Otherwise,
            will not remove directories.
        limit (int): The limit in MB on the size of the file allowed to be uploaded.
        no_compare (bool): By default, this function checks local and remote
            file checksum to avoid downloading files that already exist. If
            no_compare is True, this check is skipped and all specified files
            are downloaded, even if an equivalent file already exists locally.
        upload_as (str): Materials Commons style path specifying where to
            upload. Requires `len(paths) == 1`.
        localtree (LocalTree): A LocalTree object stores local file checksums
            to avoid unnecessary hashing. Optional, will be used and updated if
            provided and checksum == True.
        remotetree (RemoteTree): A RemoteTree object stores remote file and
            directory information to minimize API calls and data transfer.
            Optional, will be used and updated if provided.

    Returns:
        (file_results, error_results):

        file_results: dict of path: file
            Successfully uploaded files

        error_results: dict of path: str
            Error messages for unsuccessful file uploads

    """
    file_results = {}
    error_results = {}

    # upload_as only allowed with 1 input path
    if upload_as is not None:
        if len(paths) != 1:
            msg = "Upload error: to 'upload as' only 1 input path is allowed"
            raise cliexcept.MCCLIException(msg)

    # check for non-existing paths, paths already uploaded, etc.
    local_abspaths_to_upload = []

    # convert input paths (absolute or relative to working_dir) to local_abspath
    local_abspaths = clipaths_to_local_abspaths(proj.local_path, paths, working_dir)

    # filter, skipping .mc, those specified by .mcignore
    local_abspaths = filter_local_abspaths(proj.local_path, local_abspaths, working_dir)

    for local_abspath in local_abspaths:
        if os.path.isfile(local_abspath):

            file_result, error_msg = check_and_upload_file(proj, local_abspath, working_dir,
                limit=limit, no_compare=no_compare, upload_as=upload_as, localtree=localtree,
                remotetree=remotetree)

            if file_result is not None:
                file_results[local_abspath] = file_result
            if error_msg is not None:
                error_results[local_abspath] = error_msg

        elif os.path.isdir(local_abspath):

            printpath = os.path.relpath(local_abspath, start=working_dir)
            if not recursive:
                msg = printpath + ": is a directory (not uploaded)"
                print(msg)
                error_results[local_abspath] = msg
                continue

            file_results_tmp, error_results_tmp = \
                check_and_upload_directory(proj, local_abspath, working_dir, limit=limit,
                    no_compare=no_compare, upload_as=upload_as, localtree=localtree,
                    remotetree=remotetree)

            for tpath in file_results_tmp:
                file_results[tpath] = file_results_tmp[tpath]
            for tpath in error_results_tmp:
                error_results[tpath] = error_results_tmp[tpath]

        else:
            # should not happen, except maybe in race conditions
            msg = "Upload error: path does not exist"
            msg += " path=" + local_abspath
            raise cliexcept.MCCLIException(msg)

    return (file_results, error_results)



def standard_upload(proj, paths, working_dir, recursive=False, limit=250, no_compare=False, upload_as=None, localtree=None, remotetree=None):
    """Upload files to Materials Commons

    Args:
        proj (:class:`materials_commons.api.Project`): Project instance with
            proj.local_path indicating local project location
        paths (List of str):
            List of paths to upload. Expects local absolute paths, or paths
            relative to working_dir.
        working_dir (str): Current working directory, used for finding relative
            paths and printing messages.
        recursive (bool): If True, remove directories recursively. Otherwise,
            will not remove directories.
        limit (int): The limit in MB on the size of the file allowed to be uploaded.
        no_compare (bool): By default, this function checks local and remote
            file checksum to avoid downloading files that already exist. If
            no_compare is True, this check is skipped and all specified files
            are downloaded, even if an equivalent file already exists locally.
        upload_as (str): Materials Commons style path specifying where to
            upload. Requires `len(paths) == 1`.
        localtree (LocalTree): A LocalTree object stores local file checksums
            to avoid unnecessary hashing. Optional, will be used and updated if
            provided and checksum == True.
        remotetree (RemoteTree): A RemoteTree object stores remote file and
            directory information to minimize API calls and data transfer.
            Optional, will be used and updated if provided.

    Returns:
        (file_results, error_results):

        file_results: dict of path: file
            Successfully uploaded files

        error_results: dict of path: str
            Error messages for unsuccessful file uploads

    """
    file_results = {}
    error_results = {}

    # check for non-existing paths, paths already uploaded, etc.
    paths_to_upload = []
    if upload_as is not None:

        # upload_as only if 1 input path
        if len(paths) != 1:
            msg = "Upload error: to 'upload as', expected len(paths) == 1"
            raise cliexcept.MCCLIException(msg)

        local_abspaths = clipaths_to_local_abspaths(proj.local_path, paths,
                                                    working_dir)
        local_abspaths = make_local_abspaths_for_upload(proj.local_path,
                                                       local_abspaths)

        local_abspath = local_abspaths[0]
        if not os.path.exists(local_abspath):
            printpath = os.path.relpath(local_abspath, start=working_dir)
            print(printpath + ": does not exist")
        else:
            paths_to_upload.append(local_abspath)

    else:
        # get info to compare local and remote files
        checksum = True
        if no_compare:
            checksum = False

        mcpaths = clipaths_to_mcpaths(proj.local_path, paths, working_dir)
        mcpaths = make_mcpaths_for_upload(proj.local_path, mcpaths)

        files_data, dirs_data, child_data, non_existing = treecompare(
            proj, mcpaths, checksum=checksum, localtree=localtree,
            remotetree=remotetree, get_children=False)

        # check for files that are already uploaded or do not exist
        for mcpath in mcpaths:
            local_abspath = filefuncs.make_local_abspath(proj.local_path, mcpath)
            printpath = os.path.relpath(local_abspath, start=working_dir)

            if os.path.isfile(local_abspath):
                l_checksum = files_data[mcpath]['l_checksum']
                r_checksum = files_data[mcpath]['r_checksum']
                if l_checksum and l_checksum == r_checksum:
                    print(printpath + ": local is equivalent to remote (skipping)")
                    file_results[local_abspath] = files_data[mcpath]['r_obj']
                    continue
            elif not os.path.exists(local_abspath):
                print(printpath + ": does not exist")
                continue

            paths_to_upload.append(local_abspath)

    # do uploads
    for local_abspath in paths_to_upload:

        printpath = os.path.relpath(local_abspath, start=working_dir)

        # Materials Commons style path, where to upload
        dest_path = None
        if upload_as is None:
            dest_path = filefuncs.make_mcpath(proj.local_path, local_abspath)
        else:
            dest_path = upload_as

        printdestpath = os.path.relpath(
            filefuncs.make_local_abspath(proj.local_path, dest_path),
            start=working_dir)

        # note: remote files are versioned, so we skip overwrite checking / force option

        # create missing remote parent directories
        parent_path = os.path.dirname(dest_path)
        parent = mkdir(proj, parent_path, remote_only=True, create_intermediates=True, remotetree=remotetree)
        if parent.path != parent_path:
            msg = "Upload error: "
            msg += " expected parent_path=" + os.path.dirname(parent_path)
            msg += " got parent_path=" + parent.path
            raise cliexcept.MCCLIException(msg)

        try:
            if os.path.isfile(local_abspath):
                if not parent:
                    error_msg = printpath + ": parent=" + parent_path + " is not a directory on remote (not uploaded)"
                    error_results[local_abspath] = error_msg
                    print(error_msg)
                    continue
                file_size_mb = os.path.getsize(local_abspath) >> 20
                if file_size_mb > limit:
                    error_msg = printpath + ": file too large (size={1}MB, limit={0}MB) (not uploaded)".format(limit, file_size_mb)
                    error_results[local_abspath] = error_msg
                    print(error_msg)
                    continue
                result = proj.remote.upload_file(proj.id, parent.id, local_abspath)
                if not filefuncs.isfile(result):
                    error_msg = printpath + ": unknown error (not uploaded)"
                    error_results[local_abspath] = error_msg
                    print(error_msg)
                    continue
                else:
                    if upload_as is None:
                        print("uploaded:", printpath)
                    else:
                        print("uploaded:", printpath, "as", printdestpath)
                file_results[local_abspath] = result

            elif os.path.isdir(local_abspath):
                if recursive:
                    proj.remote.create_directory(proj.id, os.path.basename(dest_path), parent.id)
                    if upload_as is None:
                        child_paths = [os.path.join(local_abspath, name) for name in os.listdir(local_abspath)]
                        file_results_tmp, error_results_tmp = \
                            standard_upload(proj, child_paths, working_dir,
                                recursive=recursive, limit=limit,
                                remotetree=remotetree)
                        for tpath in file_results_tmp:
                            file_results[tpath] = file_results_tmp[tpath]
                        for tpath in error_results_tmp:
                            error_results[tpath] = error_results_tmp[tpath]
                    else:
                        for name in os.listdir(local_abspath):
                            child_path = os.path.join(local_abspath, name)
                            child_upload_as = os.path.join(upload_as, name)
                            file_results_tmp, error_results_tmp = \
                                standard_upload(proj, [child_path], working_dir,
                                    recursive=recursive, limit=limit,
                                    upload_as=child_upload_as,
                                    remotetree=remotetree)
                            for tpath in file_results_tmp:
                                file_results[tpath] = file_results_tmp[tpath]
                            for tpath in error_results_tmp:
                                error_results[tpath] = error_results_tmp[tpath]
                else:
                    error_msg = printpath + ": is a directory (not uploaded)"
                    error_results[local_abspath] = error_msg
                    print(error_msg)
                    continue
            else:
                # should not happen
                msg = "Upload error: path does not exist"
                msg += " path=" + local_abspath
                raise cliexcept.MCCLIException(msg)

            if remotetree:
                remotetree.connect()
                remotetree.update(parent_path, force=True)
                remotetree.close()

        except Exception as e:
            error_msg = printpath + ": " + str(e) + " (not uploaded)"
            error_results[local_abspath] = error_msg
            print(error_msg)
            continue

    return (file_results, error_results)

class _TreeCompare(object):
    """Helper for the treecompare function.

    It was slightly easier to write as a class so that variables could be stored as class attributes and assumed present in each subroutine.

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    localtree: LocalTree object (optional, default=None)
        A LocalTree object stores local file checksums to avoid unnecessary hashing. Will be used
        and updated if provided and checksum == True in the call operator.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.

    """
    def __init__(self, proj, localtree=None, remotetree=None):
        self.proj = proj
        self.localtree = localtree
        self.remotetree = remotetree

        columns = ['l_mtime', 'l_size', 'l_type', 'l_checksum', 'r_mtime', 'r_size', 'r_type', 'r_checksum', 'r_obj', 'path', 'id', 'parent_id']
        self.record_init = {k: None for k in columns}

    def _update_local_via_tree(self, path):
        # update self.localtree for path (and if it is a directory, update the children)
        self.localtree.connect()
        self.localtree.update(path, get_children=self.get_children)
        self._update_data_from_tree(path, self.localtree, 'l')
        self.localtree.close()

    def _update_local_record(self, record, local_abspath, checksum=False):
        record['l_mtime'] = clifuncs.epoch_time(os.path.getmtime(local_abspath))
        record['l_size'] = os.path.getsize(local_abspath)
        if os.path.isfile(local_abspath):
            record['l_type'] = 'file'
            if checksum:
                record['l_checksum'] = clifuncs.checksum(local_abspath)
        elif os.path.isdir(local_abspath):
            record['l_type'] = 'directory'

    def _update_local(self, path, checksum=False):
        """Get local file or directory (and children) information"""
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)

        if not os.path.exists(local_abspath):
            return

        if os.path.isfile(local_abspath):
            if path not in self.files_data:
                self.files_data[path] = copy.deepcopy(self.record_init)
            self._update_local_record(self.files_data[path], local_abspath, checksum=checksum)

        elif os.path.isdir(local_abspath):
            if path not in self.dirs_data:
                self.dirs_data[path] = copy.deepcopy(self.record_init)
            self._update_local_record(self.dirs_data[path], local_abspath, checksum=checksum)

            # children
            if not self.get_children:
                return
            if path not in self.child_data:
                self.child_data[path] = {}
            for child in os.listdir(local_abspath):
                childpath = os.path.join(path, child)
                local_childpath = os.path.join(local_abspath, child)
                if childpath not in self.child_data[path]:
                    self.child_data[path][childpath] = copy.deepcopy(self.record_init)
                self._update_local_record(self.child_data[path][childpath], local_childpath, checksum=checksum)

        else:
            raise cliexcept.MCCLIException("TreeCompare error: os.path type error for '" + local_abspath + "'")

        return

    def _update_remote_via_tree(self, path):
        # update self.remotetree for path (and if it is a directory, update the children)
        self.remotetree.connect()
        self.remotetree.update(path, get_children=self.get_children)
        self._update_data_from_tree(path, self.remotetree, 'r')
        self.remotetree.close()

    def _update_remote_record(self, record, obj):
        record['id'] = obj.id
        record['parent_id'] = obj.directory_id
        record['r_size'] = obj.size
        record['r_mtime'] = clifuncs.epoch_time(obj.updated_at)
        record['r_obj'] = obj
        if filefuncs.isfile(obj):
            record['r_type'] = 'file'
            record['r_checksum'] = obj.checksum
        elif filefuncs.isdir(obj):
            record['r_type'] = 'directory'
        return

    def _update_remote(self, path):
        """Get remote file or directory (and children) information"""
        obj = filefuncs.get_by_path_if_exists(self.proj.remote, self.proj.id, path)
        if obj is not None:
            if obj._data.get('deleted_at', False) is not None:
                return
            if filefuncs.isfile(obj):
                if path not in self.files_data:
                    self.files_data[path] = copy.deepcopy(self.record_init)
                self._update_remote_record(self.files_data[path], obj)

            elif filefuncs.isdir(obj):
                if path not in self.dirs_data:
                    self.dirs_data[path] = copy.deepcopy(self.record_init)
                self._update_remote_record(self.dirs_data[path], obj)

                # children
                if not self.get_children:
                    return
                if path not in self.child_data:
                    self.child_data[path] = {}
                for child in self.proj.remote.list_directory(self.proj.id, obj.id):
                    childpath = os.path.join(path, child.name)
                    if childpath not in self.child_data[path]:
                        self.child_data[path][childpath] = copy.deepcopy(self.record_init)
                    self._update_remote_record(self.child_data[path][childpath], child)
            else:
                raise cliexcept.MCCLIException("TreeCompare error: get_by_path type error for '" + path + "'")

        return

    def _update_record_from_tree(self, record, file_or_dir, prefix):
        record[prefix + '_mtime'] = file_or_dir['mtime']
        record[prefix + '_size'] = file_or_dir['size']
        record[prefix + '_type'] = file_or_dir['otype']
        record[prefix + '_checksum'] = file_or_dir['checksum']
        if 'id' in file_or_dir.keys():
            record['id'] = file_or_dir['id']
        if 'parent_id' in file_or_dir.keys():
            record['parent_id'] = file_or_dir['parent_id']

    def _update_data_from_tree(self, path, tree, prefix):
        """Common data conversion from tree to output data"""

        res = tree.select_by_path(path)
        if len(res) > 1:
            raise cliexcept.MCCLIException("_update_data_from_tree error: found > 1 entry for '" + path + "'")
        if not res:
            return
        file_or_dir = res[0]

        if file_or_dir['otype'] == 'file':
            if path not in self.files_data:
                self.files_data[path] = copy.deepcopy(self.record_init)
            self._update_record_from_tree(self.files_data[path], file_or_dir, prefix)

        elif file_or_dir['otype'] == 'directory':
            if path not in self.dirs_data:
                self.dirs_data[path] = copy.deepcopy(self.record_init)
            self._update_record_from_tree(self.dirs_data[path], file_or_dir, prefix)

            # children
            if not self.get_children:
                return
            if path not in self.child_data:
                self.child_data[path] = {}
            results = tree.select_by_parent_path(path)
            for file_or_dir in results:
                childpath = file_or_dir['path']
                if childpath not in self.child_data[path]:
                    self.child_data[path][childpath] = copy.deepcopy(self.record_init)
                self._update_record_from_tree(self.child_data[path][childpath], file_or_dir, prefix)

        elif file_or_dir['otype'] == None:
            # this supports adding records for files that we checked for but do not exist
            pass

        else:
            raise cliexcept.MCCLIException("Localtree error: otype error for '" + path + "'")

        return

    def __call__(self, paths, checksum=False, get_children=True):
        """Compare local and remote tree differences for paths

        paths: List of str
            List of Materials Commons style paths (absolute path, not including project name directory)
            to query.

        checksum: bool (optional, default=False)
            If True, calculate MD5 checksum of local files and compare to remote. If localtree was
            provided to the constructor, the checksums will be saved in the localtree database.

        get_children: bool (optional, default=True)
            If True, compare children of directories.

        Returns
        -------
            (files_data, dirs_data, child_data, not_existing):

            files_data: dict of filepath: file or directory comparison
                Contains file comparisons

            dirs_data: dict of dirpath: file or directory comparison
                Contains directory comparisons

            child_data: dict of dirpath: childpath: file or directory comparison
                Contains directory children comparisons, if
                `get_children==True`, else empty dict.

            not_existing: list of str
                Paths that do not exist locally or remotely

            For each file or directory the comparison data is:
                'l_mtime': float, local file modify time (seconds since epoch)
                'l_size': int, local file size in bytes
                'l_type': str, local file type ('file' or 'directory')
                'l_checksum': str, local file md5 hash
                'r_mtime': float, remote file modify time (seconds since epoch)
                'r_size': int, remote file size in bytes
                'r_type': remote file type ('file' or 'directory')
                'r_checksum': str, remote file md5 hash
                'r_obj': File or Directory, remote object
                'eq': bool, whether the local and remote files are equivalent
                'path': str, path to file or directory (including the project top)
                'id': str, Materials Commons ID, if exists
                'parent_id': str, Materials Commons ID, if exists

            Values are None if the file does not exist in the relevant tree.


        Notes
        -----
            The equivalence check ('eq' in `data`) is only done for files. For directories, it is
            always None.

            When directories are updated in localtree and remotetree their children are also updated
            (not recursively).

            Remote objects, 'r_obj', are only returned if remotetree is None.

        """
        self.files_data = {}
        self.dirs_data = {}
        self.child_data = {}
        self.get_children = get_children

        for path in paths:
            if self.localtree and checksum:
                self._update_local_via_tree(path)
            else:
                self._update_local(path, checksum=checksum)

            if self.remotetree:
                self._update_remote_via_tree(path)
            else:
                self._update_remote(path)

        if checksum:
            for key, value in self.files_data.items():
                if value['l_checksum'] and value['r_checksum']:
                    value['eq'] = (value['l_checksum'] == value['r_checksum'])
            for dir, cdata in self.child_data.items():
                for key, value in cdata.items():
                    if value['l_checksum'] and value['r_checksum']:
                        value['eq'] = (value['l_checksum'] == value['r_checksum'])

        not_existing = []
        for path in paths:
            if path not in self.files_data and path not in self.dirs_data:
                not_existing.append(path)

        return (self.files_data, self.dirs_data, self.child_data, not_existing)


def treecompare(proj, paths, checksum=False, localtree=None, remotetree=None,
                get_children=True):
    """
    Compare files and directories on the local and remote trees.

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    paths: List of str
        List of Materials Commons style paths (absolute path, not including project name directory)
        to query.

    checksum: bool (optional, default=False)
        If True, calculate MD5 checksum of local files and compared to remote. If False, 'eq' will not be included in the output data.

    localtree: LocalTree object (optional, default=None)
        A LocalTree object stores local file checksums to avoid unnecessary hashing. Will be used
        and updated if provided and checksum == True.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.

    get_children: bool (optional, default=True)
        If True, compare children of directories.


    Returns
    -------
        (files_data, dirs_data, child_data, not_existing):

        files_data: dict of filepath: file or directory comparison
            Contains file comparisons

        dirs_data: dict of dirpath: file or directory comparison
            Contains directory comparisons

        child_data: dict of dirpath: childpath: file or directory comparison
            Contains directory children comparisons, if
            `get_children==True`, else empty dict.

        not_existing: list of str
            Paths that do not exist locally or remotely

        For each file or directory the comparison data is:
            'l_mtime': float, local file modify time (seconds since epoch)
            'l_size': int, local file size in bytes
            'l_type': str, local file type ('file' or 'directory')
            'l_checksum': str, local file md5 hash
            'r_mtime': float, remote file modify time (seconds since epoch)
            'r_size': int, remote file size in bytes
            'r_type': remote file type ('file' or 'directory')
            'r_checksum': str, remote file md5 hash
            'r_obj': File or Directory, remote object
            'eq': bool, whether the local and remote files are equivalent
            'path': str, path to file or directory (including the project top)
            'id': str, Materials Commons ID, if exists
            'parent_id': str, Materials Commons ID, if exists

        Values are None if the file does not exist in the relevant tree.


    Notes
    -----
        The equivalence check ('eq' in `data`) is only done for files. For directories, it is
        always None.

        When directories are updated in localtree and remotetree their children are also updated
        (not recursively).

        Remote objects, 'r_obj', are only returned if remotetree is None.

    """
    _treecomparer = _TreeCompare(proj, localtree=localtree, remotetree=remotetree)
    return _treecomparer(paths, checksum=checksum, get_children=get_children)

def get_types(path, files_data, dirs_data):
    """Use treecompare output to get local and remote types

    Args:
        path (str): Path to check for type
        files_data: The "files_data" output from :func:`treecompare`
        dirs_data: The "dirs_data" output from :func:`treecompare`

    Returns:
        Tuple with (local_type, remote_type) of path.
    """

    l_type = None
    if path in files_data and files_data[path]['l_type']:
        l_type = files_data[path]['l_type']
    if path in dirs_data and dirs_data[path]['l_type']:
        l_type = dirs_data[path]['l_type']

    r_type = None
    if path in files_data and files_data[path]['r_type']:
        r_type = files_data[path]['r_type']
    if path in dirs_data and dirs_data[path]['r_type']:
        r_type = dirs_data[path]['r_type']

    return (l_type, r_type)

def is_type_mismatch(path, files_data, dirs_data):
    """Check treecompare filds_data and dirs_data output to check for type mismatch

    Notes:
        - Not existing is not a type mismatch

    Returns:
        _is_type_mismatch (bool):
            This is True if l_type and r_type are different and not None, otherwise it is False.
    """
    l_type, r_type = get_types(path, files_data, dirs_data)

    if l_type and r_type and l_type != r_type:
        return True
    return False

def is_child_data_mismatch(child_data):
    """Check treecompare child_data file comparison for type mismatch

    Notes:
        - Not existing is not a type mismatch

    Returns:
        _is_type_mismatch (bool):
            This is True if l_type and r_type are different and not None, otherwise it is False.
    """
    if child_data['l_type'] and child_data['r_type'] and child_data['l_type'] != child_data['r_type']:
        return True
    return False

class _Mover(object):
    """Helper for the move function"""
    def __init__(self, proj, remote_only=False, localtree=None, remotetree=None):
        self.proj = proj
        self.remote_only = remote_only
        self.localtree = localtree
        self.remotetree = remotetree

    def _move_remote_file(self, path, to_directory_path, to_directory_id, name=None):
        file_id = self.files_data[path]['id']
        if os.path.dirname(path) != to_directory_path:
            self.proj.remote.move_file(self.proj.id, file_id, to_directory_id)
        if name:
            self.proj.remote.rename_file(self.proj.id, file_id, name)

    def _move_remote_directory(self, path, to_directory_path, to_directory_id, name=None):
        directory_id = self.dirs_data[path]['id']
        if os.path.dirname(path) != to_directory_path:
            self.proj.remote.move_directory(self.proj.id, directory_id, to_directory_id)
        if name:
            self.proj.remote.rename_directory(self.proj.id, directory_id, name)

    def _move_remote(self, path, to_directory_path, to_directory_id, name=None):
        """ Move file or directory on remote

        Arguments
        ---------
            path: str, Source file or directory
            to_directory_path: str, Destination directory
            name: str or None, If name is not None, rename file or directory after moving
        """
        if path in self.files_data:
            self._move_remote_file(path, to_directory_path, to_directory_id, name=name)
        else:
            self._move_remote_directory(path, to_directory_path, to_directory_id, name=name)

    def _move_local(self, path, to_directory_path, name=None):
        if name is None:
            name = os.path.basename(path)
        src = filefuncs.make_local_abspath(self.proj.local_path, path)
        if not os.path.exists(src):
            # printpath = os.path.relpath(src)
            # print(printpath + ": does not exist (skipping)")
            return
        dest = filefuncs.make_local_abspath(self.proj.local_path, os.path.join(to_directory_path, name))
        shutil.move(src, dest)

    def _validate_destination(self, paths):
        dest_path = paths[-1]
        dest_local_abspath = filefuncs.make_local_abspath(self.proj.local_path, dest_path)
        dest_printpath = os.path.relpath(dest_local_abspath)

        # get type of remote destination
        self.dest_remote_type = None
        if dest_path in self.files_data:
            self.dest_remote_type = self.files_data[dest_path]['r_type']
        elif dest_path in self.dirs_data:
            self.dest_remote_type = self.dirs_data[dest_path]['r_type']

        # get type of local destination
        self.dest_local_type = None
        if dest_path in self.files_data:
            self.dest_local_type = self.files_data[dest_path]['l_type']
        elif dest_path in self.dirs_data:
            self.dest_local_type = self.dirs_data[dest_path]['l_type']

        valid_usage = True

        # check remote dest type
        if self.dest_remote_type == 'file':
            print(dest_printpath + ": is an existing file on remote (will not overwrite)")
            valid_usage = False
        elif self.dest_remote_type is None:
            # dest is non-existant on remote
            if len(paths) != 2:
                print(dest_printpath + ": does not exist on remote (may not rename multiple src)")
                valid_usage = False
        elif self.dest_remote_type != 'directory':
            raise cliexcept.MCCLIException("Error in mv: dest_path='" + dest_path + "', dest_remote_type='" + str(dest_remote_type) + "'")

        # check local dest type
        if not self.remote_only:
            if self.dest_local_type == 'file':
                print(dest_printpath + ": is an existing file locally (will not overwrite)")
                valid_usage = False
            elif self.dest_local_type is None:
                # dest is non-existant on remote
                if len(paths) != 2:
                    print(dest_printpath + ": does not exist locally (may not rename multiple src)")
                    valid_usage = False
            elif self.dest_local_type != 'directory':
                raise cliexcept.MCCLIException("Error in mv: dest_path='" + dest_path + "', dest_remote_type='" + str(dest_local_type) + "'")

            if self.dest_remote_type != self.dest_local_type:
                print(dest_printpath + ": local and remote types do not match")
                valid_usage = False

        return valid_usage

    def _validate_source(self, path, to_directory_path, name=None):
        if name is None:
            name = os.path.basename(path)

        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
        printpath = os.path.relpath(local_abspath)

        dest_path = os.path.join(to_directory_path, name)
        dest_local_abspath = filefuncs.make_local_abspath(self.proj.local_path, dest_path)
        dest_printpath = os.path.relpath(dest_local_abspath)

        # check source exists
        if path in self.not_existing:
            print(printpath + ": no such file or directory")
            return False

        # check source exists remotely
        if path in self.files_data and not self.files_data[path]['r_type']:
            print(printpath + ": does not exist on remote")
            return False
        if path in self.dirs_data and not self.dirs_data[path]['r_type']:
            print(printpath + ": does not exist on remote")
            return False

        # if not remote_only, check that local and remote types match
        # - Note, not existing is not a type mismatch
        if not self.remote_only:
            if is_type_mismatch(path, self.files_data, self.dirs_data):
                print(printpath + ": local and remote types do not match")
                return False

        return True

    def __call__(self, paths):
        dest_path = paths[-1]

        if not paths or len(paths) < 2:
            print("Expects 2 or more paths: `mc mv <src> <target>` or `mc mv <src> ... <directory>`")
            return

        self.files_data, self.dirs_data, self.child_data, self.not_existing = treecompare(
            self.proj, paths, localtree=self.localtree, remotetree=self.remotetree)

        if not self._validate_destination(paths):
            return

        if self.dest_remote_type == 'directory':
            to_directory_path = dest_path
            to_directory_id = self.dirs_data[dest_path]['id']
            name = None
        else:
            to_directory_path = os.path.dirname(dest_path)
            local_to_directory_abspath = filefuncs.make_local_abspath(self.proj.local_path, to_directory_path)
            local_to_directory_printpath = os.path.relpath(local_to_directory_abspath)

            # if destination name is different, must move then rename to `name`
            name = None
            if os.path.basename(paths[0]) != os.path.basename(dest_path):
                name = os.path.basename(dest_path)

            to_directory = filefuncs.get_by_path_if_exists(self.proj.remote, self.proj.id, to_directory_path)
            if not filefuncs.isdir(to_directory):
                print(to_directory_path + ": not a directory on remote")
                return
            if not self.remote_only and not os.path.isdir(local_to_directory_abspath):
                print(local_to_directory_printpath + ": not a directory locally")
                return
            to_directory_id = to_directory.id

        # move, and rename if necessary
        for p in paths[0:-1]:

            if not self._validate_source(p, to_directory_path, name=name):
                continue

            self._move_remote(p, to_directory_path, to_directory_id, name=name)

            if self.remotetree:
                self.remotetree.connect()
                self.remotetree.update(p, force=True)
                self.remotetree.update(to_directory_path, force=True)
                self.remotetree.close()

            if not self.remote_only:
                self._move_local(p, to_directory_path, name=name)
                if self.localtree:
                    self.localtree.connect()
                    self.localtree.update(p)
                    self.localtree.update(to_directory_path)
                    self.localtree.close()

def move(proj, paths, remote_only=False, localtree=None, remotetree=None):
    """Move files and directories

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    paths: List of str
        List of Materials Commons style paths (absolute path, not including project name directory)
        to move.

    remote_only: bool (optional, default=False)
        If True, only move files and directories on remote. If False, move on local and remote.

    localtree: LocalTree object (optional, default=None)
        A LocalTree object stores local file checksums to avoid unnecessary hashing. Will be used
        and updated if provided and checksum == True.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.
    """
    _mover = _Mover(proj, remote_only=remote_only, localtree=localtree, remotetree=remotetree)
    _mover(paths)


class _Remover(object):
    """Helper for the remove function"""
    def __init__(self, proj, recursive=False, no_compare=False, remote_only=False, localtree=None, remotetree=None):
        self.proj = proj
        self.recursive = recursive
        self.no_compare = no_compare
        self.remote_only = remote_only
        self.dry_run = False   # needs work to support this
        self.localtree = localtree
        self.remotetree = remotetree

    def _update_remote(self, path):
        if not self.remotetree:
            return

        # sets updatetime temporarily, does not save
        self.remotetree.connect()
        self.remotetree.update(path, force=True)
        self.remotetree.close()

    def _update_local(self, path):
        if not self.localtree:
            return

        self.localtree.connect()
        self.localtree.update(path)
        self.localtree.close()

    def _remove_remote_file(self, path, record):
        if self.dry_run:
            print("(dry run) rm remote:", path)
            return True
        else:
            print("rm remote:", path)
            try:
                self.proj.remote.delete_file(self.proj.id, record['id'])
                return True
            except requests.exceptions.HTTPError as e:
                try:
                    print(e.response.json()['error'])
                except:
                    print(e)
                    print("  FAILED, for unknown reason")
                return False


    def _remove_local_file(self, path):
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
        if self.dry_run:
            print("(dry run) rm local:", local_abspath)
        else:
            print("rm local:", local_abspath)
            os.remove(local_abspath)

    def _remove_file(self, path, record, updatetree=False):
        """Remove a file

        - Will remove local and remote as specified by constructor options.
        - Will update local and remote tree after deletion if updatree==True
        """
        parent_path = os.path.dirname(path)

        if not record['r_type']:
            print(path + ": does not exist on remote")
            return

        elif not record['l_type']:
            self._remove_remote_file(path, record)
            if updatetree:
                self._update_remote(parent_path)
            return

        elif self.remote_only:
            self._remove_remote_file(path, record)
            if updatetree:
                self._update_remote(parent_path)
            return

        elif self.no_compare:
            res = self._remove_remote_file(path, record)
            if res:
                self._remove_local_file(path)
                if updatetree:
                    self._update_remote(parent_path)
                    self._update_local(parent_path)
            return

        elif not record['eq']:
            print(path + ": local and remote are not equal")
            return

        else:
            res = self._remove_remote_file(path, record)
            if res:
                self._remove_local_file(path)
                if updatetree:
                    self._update_remote(parent_path)
                    self._update_local(parent_path)
            return

    def _remove_remote_directory(self, path, record):
        if self.dry_run:
            print("(dry run) rm remote:", path)
            return True
        else:
            try:
                print("rm remote:", path)
                self.proj.remote.delete_directory(self.proj.id, record['id'])
                return True
            except requests.exceptions.HTTPError as e:
                try:
                    print(e)
                    print(json.dumps(e.response.json(), indent=2))
                except:
                    print("  FAILED, for unknown reason")
                return False


    def _remove_local_directory(self, path):
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
        if not os.path.exists(local_abspath):
            return
        if self.dry_run:
            print("(dry run) rm local:", local_abspath)
        else:
            print("rm local:", local_abspath)
            shutil.rmtree(local_abspath)

    def _remove_directory(self, path, record, updatetree=False):
        """Remove a directory

        - Will remove local and remote as specified by constructor options.
        - Will always update local and remote tree once before deletion to check for children, and if updatetree==True will update again after deletion
        """
        if self.remote_only:
            self._remove_remote_directory(path, record)
            if updatetree:
                self._update_remote(path)
            return

        res = self._remove_remote_directory(path, record)
        if res:
            self._remove_local_directory(path)
            if updatetree:
                self._update_remote(path)
                self._update_local(path)

    def __call__(self, path):

        checksum=True
        if self.no_compare:
            checksum=False

        # if remotetree provided, set updatetime to now
        if self.remotetree:
            orig_remote_updatetime = self.remotetree.updatetime
            self.remotetree.updatetime = time.time()

        files_data, dirs_data, child_data, not_existing = treecompare(
            self.proj, [path], checksum=checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        # reset remotree updatetime
        if self.remotetree:
            self.remotetree.updatetime = orig_remote_updatetime

        # act on treecompare results

        # if path does not exist, do nothing
        if not_existing:
            for path in not_existing:
                local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
                print(os.path.relpath(local_abspath) + ": No such file or directory")

        # if path is a file, attempt to remove it
        elif path in files_data:
            self._remove_file(path, files_data[path], updatetree=True)

        # if path is a directory, attempt to remove children and then it
        elif path in dirs_data:

            if not self.recursive:
                print(path + ": is a directory")
                return

            if not dirs_data[path]['r_type']:
                print(path + ": does not exist on remote")
                return

            self._remove_directory(path, dirs_data[path])
            self._update_remote(path)
            self._update_local(path)
        else:
            raise cliexcept.MCCLIException("Error in rm_file: unknown error")

def remove(proj, paths, recursive=False, no_compare=False, remote_only=False, localtree=None, remotetree=None):
    """Remove files and directories

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    paths: List of str
        List of Materials Commons style paths (absolute path, not including project name directory)
        to remove.

    recursive: bool (optional, default=False)
        If True, remove directories recursively. Otherwise, will not remove directories.

    no_compare: bool (optional, default=False)
        If True, remove files and directories without checking for equality between local and
        remote.

    remote_only: bool (optional, default=False)
        If True, only remove files and directories on remote. If False, remove on local and remote.

    localtree: LocalTree object (optional, default=None)
        A LocalTree object stores local file checksums to avoid unnecessary hashing. Will be used
        and updated if provided and checksum == True.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.
    """

    _remover = _Remover(proj, recursive=recursive, no_compare=no_compare, remote_only=remote_only, localtree=localtree, remotetree=remotetree)
    for p in paths:
        _remover(p)


def mkdir(proj, path, remote_only=False, create_intermediates=False, remotetree=None,
          parent_id=None):
    """Make directories

    Arguments
    ---------
    proj: mcapi.Project
        Project instance with proj.local_path indicating local project location

    path: str
        Materials Commons style path (absolute path, not including project name directory) of
        directory to make.

    create_intermediates: bool (optional, default=False)
        If True, make intermediate directories as necessary when they do not exist.

    remote_only: bool (optional, default=False)
        If True, only make directories on remote. If False, make on local and remote.

    remotetree: RemoteTree object (optional, default=None)
        A RemoteTree object stores remote file and directory information to minimize API calls and
        data transfer. Will be used and updated if provided.

    parent_id (str): ID of parent directory where the directory should be created, if already
        known. May be None, in which case the parent directory will be found using `path`.

    Returns
    -------
    result: mcapi.File or None
        mcapi.File object representing the created directory, if successful.

    Raises
    ------
    Raises MCCLIException if unsuccessful with one of following messages:

            - path + ": is a local file":
                If attempting to create "/A/B/C" and any of "/A", "/A/B", or "/A/B/C" is an existing
                file locally and remote_only==False.
            - path + ": is a remote file":
                If attempting to create "/A/B/C" and any of "/A", "/A/B", or "/A/B/C" is an existing
                file on Materials Commons.
            - parent_path + ": parent directory does not exist":
                If attempting to create "/A/B/C" and the parent directory, "/A/B" does not exist
                on Materials Commons and create_intermediates==False.

    """
    local_abspath = filefuncs.make_local_abspath(proj.local_path, path)
    if not remote_only:
        if os.path.isfile(local_abspath):
            raise cliexcept.MCCLIException(path + ": is a local file")

    if parent_id is not None:
        result = proj.remote.create_directory(proj.id, os.path.basename(path), parent_id)
        if remotetree:
            remotetree.connect()
            remotetree.update(os.path.dirname(path), force=True)
            remotetree.close()
        if not remote_only:
            clifuncs.mkdir_if(local_abspath)
        return result

    result = filefuncs.get_by_path_if_exists(proj.remote, proj.id, path)
    if filefuncs.isdir(result):
        if not remote_only:
            clifuncs.mkdir_if(local_abspath)
        return result
    elif filefuncs.isfile(result):
        raise cliexcept.MCCLIException(path + ": is a remote file")
    elif result is None:
        parent_path = os.path.dirname(path)
        if create_intermediates:
            parent = mkdir(proj, parent_path, remote_only=remote_only,
                create_intermediates=create_intermediates, remotetree=remotetree)
            result = proj.remote.create_directory(proj.id, os.path.basename(path), parent.id)
            if remotetree:
                remotetree.connect()
                remotetree.update(parent_path, force=True)
                remotetree.close()
            if not remote_only:
                clifuncs.mkdir_if(local_abspath)
            return result
        else:
            parent = filefuncs.get_by_path_if_exists(proj.remote, proj.id, parent_path)
            if filefuncs.isfile(parent):
                raise cliexcept.MCCLIException(parent_path + ": is a remote file")
            if parent is None:
                raise cliexcept.MCCLIException(parent_path + ": parent directory does not exist")
            result = proj.remote.create_directory(proj.id, os.path.basename(path), parent.id)
            if remotetree:
                remotetree.connect()
                remotetree.update(os.path.dirname(path), force=True)
                remotetree.close()
            if not remote_only:
                clifuncs.mkdir_if(local_abspath)
            return result
