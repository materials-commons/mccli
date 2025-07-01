import argparse
import json
import os
import sys
from datetime import datetime, timedelta
from io import StringIO

import materials_commons.api as mcapi
import pkg_resources
import requests

import materials_commons.cli.functions as clifuncs
from materials_commons.cli.exceptions import MCCLIException, MissingRemoteException, \
	MultipleRemoteException, NoDefaultRemoteException
from materials_commons.cli.subcommands.clone import clone_subcommand
from materials_commons.cli.subcommands.config import config_subcommand
from materials_commons.cli.subcommands.dataset import DatasetSubcommand
from materials_commons.cli.subcommands.down import down_subcommand
from materials_commons.cli.subcommands.expt import ExptSubcommand
from materials_commons.cli.subcommands.fetch import fetch_subcommand
# from materials_commons.cli.subcommands.samp import SampSubcommand
from materials_commons.cli.subcommands.globus import globus_subcommand
from materials_commons.cli.subcommands.init import init_subcommand
from materials_commons.cli.subcommands.ls import ls_subcommand
from materials_commons.cli.subcommands.mkdir import mkdir_subcommand
from materials_commons.cli.subcommands.mv import mv_subcommand
# from materials_commons.cli.subcommands.proc import ProcSubcommand
from materials_commons.cli.subcommands.proj import ProjSubcommand
from materials_commons.cli.subcommands.remote import remote_subcommand
from materials_commons.cli.subcommands.rm import rm_subcommand
from materials_commons.cli.subcommands.up import up_subcommand
from materials_commons.cli.subcommands.versions import versions_subcommand
from materials_commons.cli.user_config import Config

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


CLI_VERSION_CACHE_FILE = os.path.expanduser('~/.materialscommons/.cli-version-cache.json')


def should_check_version():
	try:
		if os.path.exists(CLI_VERSION_CACHE_FILE):
			with open(CLI_VERSION_CACHE_FILE) as f:
				cache = json.load(f)
			last_check = datetime.fromisoformat(cache['last_check'])
			# Only check once per day
			if datetime.now() - last_check < timedelta(days=1):
				return False
	except Exception:
		pass
	return True


def update_version_cache():
	try:
		with open(CLI_VERSION_CACHE_FILE, 'w') as f:
			json.dump({
				'last_check': datetime.now().isoformat()
			}, f)
	except Exception:
		pass


def check_package_version():
	if not should_check_version():
		return

	package_name = 'materials-commons-cli'
	try:
		current_version = pkg_resources.get_distribution(package_name).version
		response = requests.get(f"https://pypi.org/pypi/{package_name}/json")
		latest_version = response.json()["info"]["version"]

		if current_version != latest_version:
			print(
				f"\nWarning: You are using an older version of the Materials Commons CLI ({current_version}).\n"
				f"A newer version is available ({latest_version}).\n"
				f"It is recommended you upgrade to the newest version: pip install --upgrade {package_name}\n",
				file=sys.stderr,
			)

		update_version_cache()

	except Exception as e:
		pass


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
			return 1

		# parse_args defaults to [1:] for args, but you need to
		# exclude the rest of the args too, or validation will fail
		args = parser.parse_args(argv[1:2])

		if args.command in standard_interfaces:
			result = standard_interfaces[args.command]['subcommand'](argv[2:], working_dir)
			check_package_version()
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
		print(
			'** Please edit .mc/config.json to include `"remote":{"mcurl": "Materials Commons https address here","email": "YOUR_EMAIL_HERE"}` **')
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
