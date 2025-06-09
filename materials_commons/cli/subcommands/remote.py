import argparse
import getpass
import re
import requests
import sys

import materials_commons.api as mcapi
from materials_commons.cli.functions import print_remotes
from materials_commons.cli.user_config import Config, RemoteConfig

def print_known_remotes():
    print("Known remotes:")
    print("    https://materialscommons.org/api")
    # print("    https://lift.materialscommons.org/api") TODO: update with lift

def make_parser():
    """Make argparse.ArgumentParser for `mc remote`"""
    parser = argparse.ArgumentParser(
        description='Server settings',
        prog='mc remote')

    parser.add_argument('-l', '--list', action="store_true", default=False,  help='List known remote urls.')
    parser.add_argument('--show-apikey', action="store_true", default=False,  help='Show apikey.')
    parser.add_argument('--add', nargs=1, metavar=('EMAIL'), help='Add a new remote. Defaults to URL https://materialscommons.org/api. Use --url to specify a different URL.')
    parser.add_argument('--remove', nargs=1, metavar=('EMAIL'), help='Remove a remote from the list. Defaults to URL https://materialscommons.org/api. Use --url to specify a different URL.')
    parser.add_argument('--url', default='https://materialscommons.org/api', help='Remote server API URL (default: https://materialscommons.org/api)')
    parser.add_argument('--set-default', nargs=2, metavar=('EMAIL', 'URL'), help='Set default remote to be used when not in a project.')
    return parser

def remote_subcommand(argv, working_dir):
    """
    Show / modify list of known Materials Commons accounts.

    Actions:
        mc remote                              # list known remotes
        mc remote --add <email> <url>          # add a remote
        mc remote --remove <email> <url>       # remove a remote
        mc remote --set-default <email> <url>  # set the default remote
        mc remote --set-project <email> <url>  # change the remote used for the current project

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    if args.list:
        print_known_remotes()

    elif args.add:
        email = args.add[0]
        url = args.url

        config = Config()
        remote_config = RemoteConfig(mcurl=url, email=email)
        if remote_config in config.remotes:
            print(email + " at " + url + " already known")
            return 0

        while True:
            try:
                password = getpass.getpass(prompt='password: ')
                remote_config.mcapikey = mcapi.Client.get_apikey(email, password, url)
                break
            except requests.exceptions.HTTPError as e:
                print(str(e))
                if not re.search('Bad Request for url', str(e)):
                    raise e
                else:
                    print("Wrong password for " + email + " at " + url)
            except requests.exceptions.ConnectionError as e:
                print("Could not connect to " + url)
                return 1

        config.remotes.append(remote_config)
        config.save()
        if url == 'https://materialscommons.org/api':
            set_default_remote(email, url)
        print("Added " + email + " at " + url)


    elif args.remove:
        email = args.remove[0]
        url = args.url

        config = Config()
        remote_config = RemoteConfig(mcurl=url, email=email)
        if remote_config not in config.remotes:
            print("Failed: " + email + " at " + url + " not found.")
            print_remotes(config.remotes)
            return 1
        config.remotes.remove(remote_config)
        config.save()
        print("Removed " + email + " at " + url)

    elif args.set_default:
        email = args.set_default[0]
        url = args.set_default[1]
        rv = set_default_remote(email, url)
        if rv != 0:
            return rv
        print("Set default: " + email + " at " + url)

    else:
        config = Config()
        print_remotes(config.remotes, args.show_apikey)

        if not len(config.remotes):
            print()
            print("List known remote urls with:")
            print("    mc remote -l")
            print("Add a remote with:")
            print("    mc remote --add EMAIL URL")
            return 1

    return

def set_default_remote(email, url):
    config = Config()
    remote_config = RemoteConfig(mcurl=url, email=email)

    if remote_config in config.remotes:
        config.default_remote = config.remotes[config.remotes.index(remote_config)]
    else:
        print("Failed: " + email + " at " + url + " not found.")
        print_remotes(config.remotes)
        return 1
    config.save()
    return 0
