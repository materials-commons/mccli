import argparse
import os

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.project_file_state as pfstate


def push_subcommand(argv, working_dir):
    parser = make_parser()
    # args = parser.parse_args(argv)

    local_proj = clifuncs.make_local_project(working_dir)
    proj_config = clifuncs.read_project_config(local_proj.local_path)
    
    run_push(proj_config)
    

def make_parser():
    parser = argparse.ArgumentParser(description="Push changed and added files to server",
                                     usage="",
                                     prog="mc push")
    return parser


def run_push(proj_config):
    for root, dirs, files in os.walk(proj_config.project_path):
        pfstate.remove_ignored_dirs(root, dirs)
        pfstate.remove_unknown_dirs(root, dirs)
        for file in files:
            proj_path = os.path.abspath(os.path.join("/", root, file))
            
            if pfstate.file_is_ignored(proj_path):
                continue
                
            if pfstate.file_is_unknown(proj_path):
                continue
                
            if pfstate.file_is_in_conflict(proj_path):
                continue

            if pfstate.file_can_be_upload(proj_path, root, file):
                pfstate.upload_file(proj_path, root, file)
