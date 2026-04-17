import argparse
import asyncio

from materials_commons.cli.subcommands.runners.download_v2_runner import run_v2_download


def make_parser():
    parser = argparse.ArgumentParser(
        description='Download files from Materials Commons',
        usage='mc down2 [-r] <pathspec> [<pathspec> ...]',
        prog='mc down2')

    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action='store_true', help='Download directories recursively')
    parser.add_argument('--force', action='store_true', help='Force overwrite of existing files')
    return parser


def down2_subcommand(argv, working_dir):
    parser = make_parser()
    args = parser.parse_args(argv)
    asyncio.run(run_v2_download(args, working_dir))
