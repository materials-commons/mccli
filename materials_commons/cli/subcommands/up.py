import argparse
import threading
from pathlib import Path

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.globus as cliglobus
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.treedb import LocalTree, RemoteTree
from materials_commons.cli.file_functions import make_mcpath


def make_parser():
    """Make argparse.ArgumentParser for `mc up`"""

    mc_up_description = "Upload files to Materials Commons"

    mc_up_usage = """
    mc up [-r] [--no-compare] [--limit] <pathspec> [<pathspec> ...]
    mc up -g [-r] [--no-compare] [--label] <pathspec> [<pathspec> ...]"""

    globus_help = """Use globus to upload files. Uses the current active upload or creates a new upload.
     Use `globus task list` to monitor transfer tasks. Use `mc globus upload` to manage uploads."""

    parser = argparse.ArgumentParser(
        description=mc_up_description,
        usage=mc_up_usage,
        prog='mc up')
    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action="store_true", default=False,
                        help='Upload directory contents recursively')
    parser.add_argument('--limit', nargs=1, type=float, default=[250],
                        help='File size upload limit (MB). Default=250MB. Does not apply to Globus uploads.')
    parser.add_argument('-g', '--globus', action="store_true", default=False,
                        help=globus_help)
    parser.add_argument('--label', nargs=1, type=str,
                        help='Globus transfer label to make finding tasks simpler. Default is `<project name>-<upload name>.')
    parser.add_argument('--no-compare', action="store_true", default=False,
                        help='Upload without checking if remote is equivalent.')
    parser.add_argument('--upload-as', nargs=1, default=None, help='Upload to a different location than standard upload. Specified as if it were a local path.')
    return parser

def up_subcommand(argv, working_dir):
    """
    upload files to Materials Commons

    mc up [-r] [--no-compare] [--limit] <pathspec> [<pathspec> ...]
    mc up -g [-r] [--no-compare] [--label] <pathspec> [<pathspec> ...]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    proj = clifuncs.make_local_project(working_dir)

    pconfig = clifuncs.read_project_config(proj.local_path)
    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    # validate
    if args.upload_as and len(args.paths) != 1:
        print("--upload-as option acts on 1 file or directory, received", len(args.paths))
        raise cliexcept.MCCLIException("Invalid upload request")
    if args.upload_as and args.globus:
        print("--upload-as option is not supported with --globus")
        raise cliexcept.MCCLIException("Invalid upload request")

    upload_as = None
    if args.upload_as:
        upload_as = treefuncs.clipaths_to_mcpaths(proj.local_path,
                                                  args.upload_as,
                                                  working_dir)[0]

    if args.globus:

        # convert input paths (absolute or relative to working_dir) to local_abspath
        local_abspaths = treefuncs.clipaths_to_local_abspaths(
            proj.local_path, args.paths, working_dir)

        # filter, skipping .mc, those specified by .mcignore
        local_abspaths = treefuncs.filter_local_abspaths(
            proj.local_path, local_abspaths, working_dir)

        mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)

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
        task_id = globus_ops.upload_v0(proj, mcpaths, upload, working_dir,
                                       recursive=args.recursive, no_compare=args.no_compare,
                                       label=label)

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

        treefuncs.standard_upload_v2(proj, args.paths, working_dir,
                                  recursive=args.recursive, limit=args.limit[0],
                                  no_compare=args.no_compare,
                                  upload_as=upload_as, localtree=localtree,
                                  remotetree=remotetree)

    return

MB = 1024 * 1024

class UploadCallbacks:
    def __init__(self, proj, max_upload_size):
        self.proj = proj
        self.max_upload_size = max_upload_size * MB

    def file_upload_callback(self, p: Path):

        # We can only upload files up to self.max_upload_size. To eliminate calls to the
        # server for files that are too big we first check the file size. If it's larger
        # than self.max_upload_size, then we skip processing this file.
        sinfo = p.stat()
        if sinfo.st_size > self.max_upload_size:
            # The file is too big to upload
            return

        # If we are here, then this file is a candidate for upload. First, let's do
        # some conversion, so we have the file represented as the project path on
        # the Materials Commons server.
        file_path_on_mc = make_mcpath(self.proj.local_path, p.as_posix())

        # Next, let's retrieve the remote file and do some sanity checking.
        remote_file = self.proj.client.get_file_by_path(self.proj.id, file_path_on_mc)
        if remote_file.mime_type == "directory":
            # Locally we have a file, but on the server that file is a directory. We can't upload
            # a file with the same name so we skip it.
            return

        if remote_file.size != sinfo.st_size:
            # This is a shortcut check. If the files are different size then they can't be the
            # same. Thus, it is safe to upload the file.
            self.proj.client.upload_file_by_path(self.proj.id, p.as_posix(), file_path_on_mc)
        else:
            # Sizes are the same. Compare checksums.
            checksum = clifuncs.checksum(p.as_posix())
            if checksum != remote_file.checksum:
                self.proj.client.upload_file_by_path(self.proj.id, p.as_posix(), file_path_on_mc)
