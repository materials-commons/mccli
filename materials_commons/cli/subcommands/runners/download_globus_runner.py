import time

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.globus as cliglobus
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.treedb import LocalTree, RemoteTree


def run_globus_download(args, working_dir):
    pconfig = clifuncs.read_project_config(working_dir)
    proj = clifuncs.make_local_project(working_dir)
    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths,
                                          working_dir)

    localtree = None
    if not args.no_compare:
        localtree = LocalTree(proj.local_path)

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    download = _get_current_globus_download(pconfig, proj)

    if download.status != 0:  # TODO clean up status code / message

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
        return

    print("Download is ready.")

    label = proj.name + "-" + download.name
    if args.label:
        label = args.label[0]

    globus_ops = cliglobus.GlobusOperations()
    task_id = globus_ops.download_v0(proj, paths, download, working_dir,
                                     recursive=args.recursive, label=label, localtree=localtree,
                                     remotetree=remotetree, no_compare=args.no_compare,
                                     force=args.force)

    if task_id:
        print("Globus transfer task initiated.")
        print("Use `globus task list` to monitor task status.")
        print("Use `mc globus download` to manage Globus downloads.")
        print("Multiple transfer tasks may be initiated.")
        print("When all tasks finish downloading, use `mc globus download --id " + str(download.id) +
              " --delete` " + "to close the download.")


def _get_current_globus_download(pconfig, proj, verbose=True):
    all_downloads = {download.id: download for download in proj.remote.get_all_globus_download_requests(proj.id)}

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
