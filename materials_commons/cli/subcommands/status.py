import argparse
import os

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.project_file_state as pfstate


def status_subcommand(argv, working_dir):
    parser = make_parser()
    # args = parser.parse_args(argv)

    local_proj = clifuncs.make_local_project(working_dir)
    proj_config = clifuncs.read_project_config(local_proj.local_path)

    run_status(local_proj.local_path)


def make_parser():
    parser = argparse.ArgumentParser(description="Get status on local files",
                                     usage="",
                                     prog="mc status")
    return parser


def run_status(project_dir):
    for root, dirs, files in os.walk(project_dir):
        pfstate.remove_ignored_dirs(root, dirs)
        process_unknown_dirs(root, dirs)
        # os.path.abspath(os.path.join("/", "./this/that.txt"))
        for file in files:
            proj_path = os.path.abspath(os.path.join("/", root, file))

            if pfstate.file_is_ignored(proj_path):
                continue

            if pfstate.file_is_unknown(proj_path):
                print(f"file to add: {proj_path}")
                continue

            si = os.stat(os.path.join(root, file))

            if pfstate.file_has_changed(si, proj_path, root, file):
                print(f"file has changed: {proj_path}")


# Process unknown dirs and remove them from the list of dirs to traverse
def process_unknown_dirs(root, dirs):
    pass
