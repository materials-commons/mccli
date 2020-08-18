import argparse
import imp
import sys
from io import StringIO

# import materials_commons.api as mcapi
# from .actions import ActionsSubcommand
from .subcommands.clone import clone_subcommand
from .subcommands.config import config_subcommand
from .subcommands.dataset import DatasetSubcommand
# from .diff import diff_subcommand
from .subcommands.down import down_subcommand
from .subcommands.expt import ExptSubcommand
# from .fetch import fetch_subcommand
from .subcommands.init import init_subcommand
from .subcommands.ls import ls_subcommand
from .subcommands.mkdir import mkdir_subcommand
from .subcommands.mv import mv_subcommand
# from .proc import ProcSubcommand
from .subcommands.proj import ProjSubcommand
from .subcommands.remote import remote_subcommand
from .subcommands.rm import rm_subcommand
# from .samp import SampSubcommand
from .subcommands.globus import globus_subcommand
# from .templates import TemplatesSubcommand
from .subcommands.up import up_subcommand
# from .versions import versions_subcommand

from . import functions as clifuncs
from .user_config import Config
from .exceptions import MCCLIException, MissingRemoteException, MultipleRemoteException, \
    NoDefaultRemoteException

class CommonsCLIParser(object):

    standard_usage = [
        {'name': 'remote', 'desc': 'List servers', 'subcommand': remote_subcommand},
        {'name': 'proj', 'desc': 'List projects', 'subcommand': ProjSubcommand()},
        {'name': 'dataset', 'desc': 'List datasets', 'subcommand': DatasetSubcommand()},
        {'name': 'expt', 'desc': 'List, create, delete, and modify experiments', 'subcommand': ExptSubcommand()},
        {'name': 'init', 'desc': 'Initialize a new project', 'subcommand': init_subcommand},
        {'name': 'clone', 'desc': 'Clone an existing project', 'subcommand': clone_subcommand},
        {'name': 'ls', 'desc': 'List local and remote directory contents', 'subcommand': ls_subcommand},
        {'name': 'mkdir', 'desc': 'Make remote directories', 'subcommand': mkdir_subcommand},
        {'name': 'rm', 'desc': 'Remove files and directories', 'subcommand': rm_subcommand},
        {'name': 'mv', 'desc': 'Move files', 'subcommand': mv_subcommand},
        # {'name': 'diff', 'desc': 'Compare local and remote files', 'subcommand': diff_subcommand},
        # {'name': 'fetch', 'desc': 'Remote data fetching and configuration', 'subcommand': fetch_subcommand},
        {'name': 'up', 'desc': 'Upload files', 'subcommand': up_subcommand},
        {'name': 'down', 'desc': 'Download files', 'subcommand': down_subcommand},
        {'name': 'globus', 'desc': 'Manage Globus uploads and downloads', 'subcommand': globus_subcommand},
        # {'name': 'versions', 'desc': 'List file versions', 'subcommand': versions_subcommand},
        # {'name': 'templates', 'desc': 'List process templates', 'subcommand': TemplatesSubcommand()},
        # {'name': 'proc', 'desc': 'List processes', 'subcommand': ProcSubcommand()},
        # {'name': 'samp', 'desc': 'List samples', 'subcommand': SampSubcommand()},
        {'name': 'config', 'desc': 'Configure `mc`', 'subcommand': config_subcommand}
    ]

    developer_usage = [
        # {'name': 'actions', 'desc': 'List REST API actions', 'subcommand': ActionsSubcommand()}
    ]

    def __init__(self, argv):

        config = Config()
        if config.REST_logging:
            import materials_commons.api as mcapi
            mcapi.Client.set_debug_on()
            pass

        usage_help = StringIO()
        usage_help.write("mc <command> [<args>]\n\n")
        usage_help.write("The standard mc commands are:\n")

        for interface in CommonsCLIParser.standard_usage:
            usage_help.write("  {:10} {:40}\n".format(interface['name'], interface['desc']))
        standard_interfaces = {d['name']: d for d in CommonsCLIParser.standard_usage}

        # read custom interfaces from config file
        custom_interfaces = {d['name']: d for d in config.interfaces}
        if len(config.interfaces):
            usage_help.write("\nSpecialized commands are:\n")
        for interface in config.interfaces:
            usage_help.write("  {:10} {:40}\n".format(interface['name'], interface['desc']))

        # hide from most users
        if config.developer_mode:
            usage_help.write("\nDeveloper commands are:\n")
            for interface in CommonsCLIParser.developer_usage:
                usage_help.write("  {:10} {:40}\n".format(interface['name'], interface['desc']))
            developer_interfaces = {d['name']: d for d in CommonsCLIParser.developer_usage}

        parser = argparse.ArgumentParser(
            description='Materials Commons command line interface',
            usage=usage_help.getvalue())
        parser.add_argument('command', help='Subcommand to run')

        if len(argv) < 2:
            parser.print_help()
            return

        # parse_args defaults to [1:] for args, but you need to
        # exclude the rest of the args too, or validation will fail
        args = parser.parse_args(argv[1:2])

        if hasattr(self, args.command):
            getattr(self, args.command)(argv[2:])
        elif args.command in standard_interfaces:
            standard_interfaces[args.command]['subcommand'](argv[2:])
        elif args.command in custom_interfaces:
            # load module and run command
            modulename = custom_interfaces[args.command]['module']
            subcommandname = custom_interfaces[args.command]['subcommand']
            f, filename, description = imp.find_module(modulename)
            try:
                module = imp.load_module(modulename, f, filename, description)
                getattr(module, subcommandname)(argv[2:])
            finally:
                if f:
                    f.close()
        elif config.developer_mode and args.command in developer_interfaces:
            developer_interfaces[args.command]['subcommand'](argv[2:])

        else:
            print('Unrecognized command')
            parser.print_help()
            exit(1)

def main(argv=None):
    if argv is None:
        argv = sys.argv
    try:
        cli = CommonsCLIParser(argv)
    except MissingRemoteException as e:
        print("Error:", e)
        clifuncs.print_remote_help()
        exit(1)
    except MultipleRemoteException as e:
        print("Error:", e)
        print('** Please edit .mc/config.json to include `"remote":{"mcurl": "' + data['remote_url']
            + '", "email": "YOUR_EMAIL_HERE"}` **')
        exit(1)
    except NoDefaultRemoteException as e:
        print("Error:", e)
        print("Set the default remote with:")
        print("    mc remote --set-default EMAIL URL")
        clifuncs.print_remote_help()
        exit(1)
    except MCCLIException as e:
        print("Error:", e)
        exit(1)
