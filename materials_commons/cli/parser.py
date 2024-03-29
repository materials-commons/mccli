import argparse
import os
import sys
from io import StringIO

import materials_commons.api as mcapi

from materials_commons.cli.subcommands.clone import clone_subcommand
from materials_commons.cli.subcommands.config import config_subcommand
from materials_commons.cli.subcommands.dataset import DatasetSubcommand
from materials_commons.cli.subcommands.down import down_subcommand
from materials_commons.cli.subcommands.expt import ExptSubcommand
from materials_commons.cli.subcommands.fetch import fetch_subcommand
from materials_commons.cli.subcommands.init import init_subcommand
from materials_commons.cli.subcommands.ls import ls_subcommand
from materials_commons.cli.subcommands.mkdir import mkdir_subcommand
from materials_commons.cli.subcommands.mv import mv_subcommand
# from materials_commons.cli.subcommands.proc import ProcSubcommand
from materials_commons.cli.subcommands.proj import ProjSubcommand
from materials_commons.cli.subcommands.remote import remote_subcommand
from materials_commons.cli.subcommands.rm import rm_subcommand
# from materials_commons.cli.subcommands.samp import SampSubcommand
from materials_commons.cli.subcommands.globus import globus_subcommand
from materials_commons.cli.subcommands.up import up_subcommand
from materials_commons.cli.subcommands.versions import versions_subcommand

import materials_commons.cli.functions as clifuncs
from materials_commons.cli.user_config import Config
from materials_commons.cli.exceptions import MCCLIException, MissingRemoteException, \
    MultipleRemoteException, NoDefaultRemoteException

standard_usage = [
    {'name': 'remote', 'desc': 'List servers', 'subcommand': remote_subcommand},
    {'name': 'proj', 'desc': 'List projects', 'subcommand': ProjSubcommand()},
    {'name': 'dataset', 'desc': 'List datasets', 'subcommand': DatasetSubcommand()},
    {'name': 'expt', 'desc': 'List experiments', 'subcommand': ExptSubcommand()},
    {'name': 'init', 'desc': 'Initialize a new project', 'subcommand': init_subcommand},
    {'name': 'clone', 'desc': 'Clone an existing project', 'subcommand': clone_subcommand},
    {'name': 'ls', 'desc': 'List directory contents', 'subcommand': ls_subcommand},
    {'name': 'mkdir', 'desc': 'Make directories', 'subcommand': mkdir_subcommand},
    {'name': 'rm', 'desc': 'Remove files and directories', 'subcommand': rm_subcommand},
    {'name': 'mv', 'desc': 'Move files', 'subcommand': mv_subcommand},
    {'name': 'fetch', 'desc': 'Remote data fetching and configuration', 'subcommand': fetch_subcommand},
    {'name': 'up', 'desc': 'Upload files', 'subcommand': up_subcommand},
    {'name': 'down', 'desc': 'Download files', 'subcommand': down_subcommand},
    {'name': 'globus', 'desc': 'Manage Globus uploads and downloads', 'subcommand': globus_subcommand},
    {'name': 'versions', 'desc': 'List file versions', 'subcommand': versions_subcommand},
    # {'name': 'proc', 'desc': 'List processes', 'subcommand': ProcSubcommand()},
    # {'name': 'samp', 'desc': 'List samples', 'subcommand': SampSubcommand()},
    {'name': 'config', 'desc': 'Configure `mc`', 'subcommand': config_subcommand}
]
standard_interfaces = {d['name']: d for d in standard_usage}

def make_parser():

    usage_help = StringIO()
    usage_help.write("mc <command> [<args>]\n\n")
    usage_help.write("The standard mc commands are:\n")

    for name, interface in standard_interfaces.items():
        usage_help.write("  {:10} {:40}\n".format(name, interface['desc']))

    parser = argparse.ArgumentParser(
        description='Materials Commons command line interface',
        usage=usage_help.getvalue())
    parser.add_argument('command', help='Subcommand to run')

    return parser


def main(argv=None, working_dir=None):
    if argv is None:
        argv = sys.argv
    if working_dir is None:
        working_dir = os.getcwd()
    try:

        config = Config()
        if config.REST_logging:
            mcapi.Client.set_debug_on()
            pass

        parser = make_parser()

        if len(argv) < 2:
            parser.print_help()
            return

        # parse_args defaults to [1:] for args, but you need to
        # exclude the rest of the args too, or validation will fail
        args = parser.parse_args(argv[1:2])

        if args.command in standard_interfaces:
            result = standard_interfaces[args.command]['subcommand'](argv[2:], working_dir)
            return result

        else:
            print('Unrecognized command')
            parser.print_help()
            return 1

    except MissingRemoteException as e:
        print("Error:", e)
        clifuncs.print_remote_help()
        return 1

    except MultipleRemoteException as e:
        print("Error:", e)
        print('** Please edit .mc/config.json to include `"remote":{"mcurl": "' + data['remote_url']
            + '", "email": "YOUR_EMAIL_HERE"}` **')
        return 1

    except NoDefaultRemoteException as e:
        print("Error:", e)
        print("Set the default remote with:")
        print("    mc remote --set-default EMAIL URL")
        clifuncs.print_remote_help()
        return 1

    except MCCLIException as e:
        print("CLI Error:", e)
        return 1

    except mcapi.MCAPIError as e:
        import json
        print("API Error:", e)
        print("Writing error message to 'mcapi_error.json'")
        with open('mcapi_error.json', 'w') as f:
            json.dump(e.response.json(), f, indent=2)
        return 1
