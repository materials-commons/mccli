import argparse
import os

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.project_file_state as pfstate


def pull_subcommand(argv, working_dir):
    parser = make_parser()
    # args = parser.parse_args(argv)

    local_proj = clifuncs.make_local_project(working_dir)
    proj_config = clifuncs.read_project_config(local_proj.local_path)

    run_pull(proj_config.project_path)


def run_pull(project_dir):
    for root, dirs, files in os.walk(project_dir):
        pfstate.remove_ignored_dirs(root, dirs)
        pfstate.remove_unknown_dirs(root, dirs)
        for dir in dirs:
            pfstate.download_dir(root, dir)


def make_parser():
    parser = argparse.ArgumentParser(description="Push changed and added files to server",
                                     usage="",
                                     prog="mc push")
    return parser
