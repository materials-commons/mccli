import argparse
import os
import sys
import time

from .. import exceptions as cliexcept
from .. import functions as clifuncs
from .. import globus as cliglobus
from .. import tree_functions as treefuncs
from ..treedb import LocalTree, RemoteTree

globus_help = """Use globus to upload files. Uses the current active upload or creates a new upload.
 Use `globus task list` to monitor transfer tasks. Use `mc globus upload` to manage uploads."""

def up_subcommand(argv):
    """
    upload files to Materials Commons

    mc up [-r] [<pathspec> ...]

    """
    parser = argparse.ArgumentParser(
        description='upload files',
        prog='mc up')
    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action="store_true", default=False,
                        help='Upload directory contents recursively')
    parser.add_argument('--limit', nargs=1, type=float, default=[50],
                        help='File size upload limit (MB). Default=50MB. Does not apply to Globus uploads.')
    parser.add_argument('-g', '--globus', action="store_true", default=False,
                        help=globus_help)
    parser.add_argument('--label', nargs=1, type=str,
                        help='Globus transfer label to make finding tasks simpler. Default is `<project name>-<upload name>.')
    parser.add_argument('--no-compare', action="store_true", default=False,
                        help='Upload without checking if remote is equivalent.')

    # ignore 'mc up'
    args = parser.parse_args(argv)

    proj = clifuncs.make_local_project()

    pconfig = clifuncs.read_project_config(proj.local_path)
    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths)

    if args.globus:

        all_uploads = {upload.id:upload for upload in proj.remote.get_all_globus_upload_requests(proj.id)}

        globus_upload_id = None
        if pconfig.globus_upload_id:
            globus_upload_id = pconfig.globus_upload_id
            if globus_upload_id not in all_uploads:
                print("Current globus upload (name=?, id=" + str(globus_upload_id) + ") no longer exists.")
                globus_upload_id = None
        if globus_upload_id is None:
            name = clifuncs.random_name()
            upload = proj.remote.create_globus_upload_request(proj.id, name)
            print("Created new globus upload (name=" + upload.name + ", id=" + str(upload.id) + ").")
            pconfig.globus_upload_id = upload.id
            pconfig.save()
        else:
            upload = all_uploads[globus_upload_id]
            print("Using current globus upload (name=" + upload.name + ", id=" + str(upload.id) + ").")

        if upload.status != 2:    # TODO clean up status code / message
            raise cliexcept.MCCLIException("Current Globus upload (id=" + str(globus_upload_id) + ") not ready for uploading.")

        label = proj.name + "-" + upload.name
        if args.label:
            label = args.label[0]

        globus_ops = cliglobus.GlobusOperations()
        task_id = globus_ops.upload_v0(proj, paths, upload, recursive=args.recursive, label=label)

        if task_id:
            print("Globus transfer task initiated.")
            print("Use `globus task list` to monitor task status.")
            print("Use `mc globus upload` to manage Globus uploads.")
            print("Multiple transfer tasks may be initiated.")
            print("When all tasks finish uploading, use `mc globus upload --id " + str(upload.id) +
                " --finish` " + "to import all uploaded files into the Materials Commons project.")

    else:
        localtree = None
        if not args.no_compare:
            localtree = LocalTree(proj.local_path)

        treefuncs.standard_upload(proj, paths, recursive=args.recursive, limit=args.limit[0],
            no_compare=args.no_compare, localtree=localtree, remotetree=remotetree)

    return
