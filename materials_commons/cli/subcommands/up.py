import argparse
import asyncio
import logging
import os
import sys
from pathlib import Path

import igittigitt
from websockets import ConnectionClosedOK, ConnectionClosedError, InvalidStatus

import materials_commons.cli.old.exceptions as cliexcept
import materials_commons.cli.old.functions as clifuncs
import materials_commons.cli.old.globus as cliglobus
import materials_commons.cli.old.tree_functions as treefuncs
from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.old.treedb import LocalTree, RemoteTree
from materials_commons.cli.requests import UploadRequest
from materials_commons.cli.server.service_container import ServiceContainer
from materials_commons.cli.server.service_runtime import ServiceRuntime
from materials_commons.cli.subcommands.server import DEFAULT_WS_URL
from materials_commons.cli.walk import local_listdir

logger = logging.getLogger(__name__)


def make_parser():
    """Make argparse.ArgumentParser for `mc up`"""

    mc_up_description = "Upload files to Materials Commons"

    mc_up_usage = """
    mc up [-r] [--no-compare] [--limit] <pathspec> [<pathspec> ...]
    mc up -g [-r] [--no-compare] [--label] <pathspec> [<pathspec> ...]
    mc up --websocket [-r] <pathspec> [<pathspec> ...]"""

    globus_help = """Use globus to upload files. Uses the current active upload or creates a new upload.
     Use `globus task list` to monitor transfer tasks. Use `mc globus upload` to manage uploads."""

    websocket_help = """Use websockets to upload files. Files are uploaded in parallel and progress is reported."""

    parser = argparse.ArgumentParser(
        description=mc_up_description,
        usage=mc_up_usage,
        prog='mc up')
    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action="store_true", default=False,
                        help='Upload directory contents recursively')
    parser.add_argument('--limit', nargs=1, type=float, default=[750],
                        help='File size upload limit (MB). Default=750MB. Max size is also 750MB. Does not apply to Globus uploads.')
    parser.add_argument('-g', '--globus', action="store_true", default=False,
                        help=globus_help)
    parser.add_argument('--websocket', '--ws', action="store_true", default=False, help=websocket_help)
    parser.add_argument('--ws-url', type=str, default=DEFAULT_WS_URL, help='WebSocket URL for commands')
    parser.add_argument('--label', nargs=1, type=str,
                        help='Globus transfer label to make finding tasks simpler. Default is `<project name>-<upload name>.')
    parser.add_argument('--no-compare', action="store_true", default=False,
                        help='Upload without checking if remote is equivalent.')
    parser.add_argument('--upload-as', nargs=1, default=None,
                        help='Upload to a different location than standard upload. Specified as if it were a local path.')
    parser.add_argument('--chunk-size', type=int, default=1024 * 1024,
                        help='Chunk size for websocket uploads (default: 1MB)')
    parser.add_argument('--max-concurrent', type=int, default=3,
                        help='Maximum concurrent uploads for websocket mode (default: 3)')
    return parser


def up_subcommand(argv, working_dir):
    """
    upload files to Materials Commons

    mc up [-r] [--no-compare] [--limit] <pathspec> [<pathspec> ...]
    mc up -g [-r] [--no-compare] [--label] <pathspec> [<pathspec> ...]
    mc up --ws [-r] <pathspec> [<pathspec> ...]
    """
    parser = make_parser()
    args = parser.parse_args(argv)

    showLogs = os.getenv("MC_SHOW_LOGS")
    if showLogs == "true":
        logging.basicConfig(
            level=logging.INFO,
            format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
            handlers=[logging.StreamHandler(sys.stdout)]
        )

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
    if args.upload_as and args.websocket:
        print("--upload-as option is not supported with --websocket")
        raise cliexcept.MCCLIException("Invalid upload request")
    if args.globus and args.websocket:
        print("--globus option is not supported with --websocket")
        raise cliexcept.MCCLIException("Invalid upload request")

    upload_as = None
    if args.upload_as:
        upload_as = treefuncs.clipaths_to_mcpaths(proj.local_path,
                                                  args.upload_as,
                                                  working_dir)[0]

    if args.websocket:
        asyncio.run(ws_upload(args, working_dir))
    elif args.globus:
        globus_upload(args, proj, working_dir, pconfig)
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


def globus_upload(args, proj, working_dir, pconfig):
    # convert input paths (absolute or relative to working_dir) to local_abspath
    local_abspaths = treefuncs.clipaths_to_local_abspaths(
        proj.local_path, args.paths, working_dir)

    # filter, skipping .mc, those specified by .mcignore
    local_abspaths = treefuncs.filter_local_abspaths(
        proj.local_path, local_abspaths, working_dir)

    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)

    all_uploads = {upload.id: upload for upload in proj.remote.get_all_globus_upload_requests(proj.id)}

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

    if upload.status != 2:  # TODO clean up status code / message
        raise cliexcept.MCCLIException(
            "Current Globus upload (id=" + str(globus_upload_id) + ") not ready for uploading.")

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


async def ws_upload(args, working_dir):
    # Load project and set exception handling
    proj = LocalProject.load(working_dir)
    proj.remote.raise_exception = False

    # Initialize database
    db = await proj.get_filedb()

    # Setup ignore parser
    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)

    # Start services
    container = ServiceContainer.create(ws_url=args.ws_url)
    service_runtime = ServiceRuntime(container)
    await service_runtime.start(websocket_listener=True)
    async_reconciler = AsyncReconciler(db=db, proj=proj, reconcile_mode="upload")

    try:
        transfer_ids = []
        for path in args.paths:
            p = Path(path)
            if p.is_dir():
                async for current_path, path_entries in async_reconciler.walk(path=path, listdir_fn=local_listdir,
                                                                              recursive=args.recursive, ignore_fn=None):
                    for entry_name in sorted(path_entries):
                        file_state = path_entries[entry_name]
                        if file_state.exception:
                            logger.error(f"Error encountered while processing {entry_name}: {file_state.exception}")
                            continue
                        if file_state.file_decision.action == "upload":
                            upload_request = UploadRequest(observation=file_state.observation,
                                                           updated_record=file_state.file_decision.updated_record,
                                                           project=proj)
                            transfer_id = await container.file_upload_manager.upload_file(upload_request)
                            transfer_ids.append(transfer_id)
            elif p.is_file():
                file_state = await async_reconciler.reconcile_file(p)
                if file_state.file_decision.action == "upload":
                    upload_request = UploadRequest(observation=file_state.observation,
                                                   updated_record=file_state.file_decision.updated_record,
                                                   project=proj)
                    transfer_id = await container.file_upload_manager.upload_file(upload_request)
                    transfer_ids.append(transfer_id)
    except (ConnectionClosedOK, ConnectionClosedError, InvalidStatus, OSError, asyncio.TimeoutError) as e:
        logger.error(f"WebSocket connection failed: {e}")
        return False

    finally:
        # Shutdown gracefully
        await container.file_upload_manager.wait_all()
        logger.info("Shutting down websocket infrastructure...")
        await service_runtime.stop(drain=True)
        await db.close()

    # if not status:
    #     print("\nSome uploads failed.")
    #     raise cliexcept.MCCLIException(
    #         "Upload failed. Check server logs for details.")
    #
    # print("\nAll uploads completed successfully!")
