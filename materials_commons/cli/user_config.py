import getpass
import os
import requests
import warnings
from os.path import join
import json

from materials_commons.api.client import Client


class RemoteConfig(object):
    def __init__(self, mcurl=None, email=None, mcapikey=None):
        self.mcurl = mcurl
        self.email = email
        self.mcapikey = mcapikey

    def __eq__(self, other):
        """Equal if mcurl and email are equal, does not check mcapikey"""
        return self.mcurl == other.mcurl and self.email == other.email

    def get_params(self):
        return {'apikey': self.mcapikey}

    def make_client(self):
        return Client(self.mcapikey, self.mcurl)

class GlobusConfig(object):
    def __init__(self, transfer_rt=None, endpoint_id=None):
        self.transfer_rt = transfer_rt
        self.endpoint_id = endpoint_id

class InterfaceConfig(object):
    def __init__(self, name=None, module=None, subcommand=None, desc=None):
        self.name = name
        self.module = module
        self.subcommand = subcommand
        self.desc = desc

    def __eq__(self, other):
        return vars(self) == vars(other)


class Config(object):
    """Configuration variables

    Order of precedence:
        1. override_config, variables set at runtime
        2. environment variables (both MC_API_URL and MC_API_KEY must be set)
        3. configuration file
        4. default configuration

    Format:
        {
            "default_remote": {
              "mcurl": <url>,
              "email": <email>,
              "apikey": <apikey>
            },
            "remotes": [
                {
                    "mcurl": <url>,
                    "email": <email>,
                    "apikey": <apikey>
                },
                ...
            ],
            "interfaces": [
                {   'name': 'casm',
                    'desc':'Create CASM samples, processes, measurements, etc.',
                    'subcommand':'casm_subcommand',
                    'module':'casm_mcapi'
                },
                ...
            ],
            "globus": {
                "transfer_rt": <token>
            },
            "developer_mode": False,
            "REST_logging": False,
            "mcurl": <url>, # (deprecated) use if no 'default_remote'
            "apikey": <apikey> # (deprecated) use if no 'default_remote'
        }

    Attributes:
        remotes: Dict of RemoteConfig, mapping of remote name to RemoteConfig instance
        default_remote: RemoteConfig, configuration for default Remote
        interfaces: List of InterfaceConfig, settings for software interfaces for the `mc` CLI program
        globus: GlobusConfig, globus configuration settings

    Arguments:
        config_dir_path: str, path to config directory. Defaults to ~/.materialscommons.
        config_file_name: str, name of config file. Defaults to "config.json".
        override_config: dict, config file-like dict, with settings to use instead of those in
            environment variables or the config file. Defaults to {}.
    """

    def __init__(self, config_dir_path=None, config_file_name="config.json", override_config={}):

        # generate config file path
        if not config_dir_path:
            user = getpass.getuser()
            config_dir_path = join(os.path.expanduser('~' + user), '.materialscommons')
        self.config_file = join(config_dir_path, config_file_name)

        # read config file, or use default config
        if os.path.exists(self.config_file):
            with open(self.config_file, 'r') as f:
                config = json.load(f)
        else:
            # default config
            config = {
                'apikey': None,
                'mcurl': None,
                'email': None,
                'remotes': {},
                'interfaces': {},
                'globus': {}
            }

        # check for recognized environment variables
        env_apikey = os.environ.get("MC_API_KEY")
        env_mcurl = os.environ.get("MC_API_URL")
        env_email = os.environ.get("MC_API_EMAIL")

        if env_apikey:
            config['apikey'] = env_apikey
        if env_mcurl:
            config['mcurl'] = env_mcurl
        if env_mcurl:
            config['email'] = env_email

        # override with runtime config
        for key in override_config:
            config[key] = override_config[key]

        # set default configuration
        if config.get('mcurl') and config.get('apikey') and config.get('email'):
            _default_remote = {
                'mcurl': config.get('mcurl'),
                'email': config.get('email'),
                'mcapikey': config.get('apikey'),
            }
            config['default_remote'] = _default_remote
        elif 'default_remote' not in config:
            _default_remote = {
                'mcurl': config.get('mcurl'),
                'email': '__default__',
                'mcapikey': config.get('apikey')
            }
            config['default_remote'] = _default_remote


        self.remotes = [RemoteConfig(**kwargs) for kwargs in config.get('remotes',[])]
        self.default_remote = RemoteConfig(**config.get('default_remote',{}))

        self.interfaces = [InterfaceConfig(**kwargs) for kwargs in config.get('interfaces',[])]

        self.globus = GlobusConfig(**config.get('globus', {}))

        self.developer_mode = config.get('developer_mode', False)
        self.REST_logging = config.get('REST_logging', False)

    def save(self):
        config = {
            'default_remote': vars(self.default_remote),
            'remotes': [vars(value) for value in self.remotes],
            'globus': vars(self.globus),
            'interfaces': [vars(value) for value in self.interfaces],
            'developer_mode': self.developer_mode,
            'REST_logging': self.REST_logging
        }
        if not os.path.exists(self.config_file):
            user = getpass.getuser()
            config_dir_path = join(os.path.expanduser('~' + user), '.materialscommons')
            if not os.path.exists(config_dir_path):
                os.mkdir(config_dir_path)
        with open(self.config_file, 'w') as f:
            f.write(json.dumps(config, indent=2))
        os.chmod(self.config_file, 0o600)

def get_remote_config_and_login_if_necessary(email=None, mcurl=None):
    """Prompt for login if remote is not stored in Config

    Args:
        email (str): User account email.
        mcurl (str): URL for Materials Commons remote instance. Example:
            "https://materialscommons.org/api".

    Returns:
        :class:`user_config.RemoteConfig`: The remote configuration parameters
        for the provided URL and user account.
    """
    config = Config()
    remote_config = RemoteConfig(mcurl=mcurl, email=email)
    if remote_config in config.remotes:
        return config.remotes[config.remotes.index(remote_config)]

    while True:
        try:
            print("Login to:", email, mcurl)
            password = getpass.getpass(prompt='password: ')
            remote_config.mcapikey = Client.get_apikey(email, password, mcurl)
            break
        except requests.exceptions.HTTPError as e:
            print(str(e))
            if not re.search('Bad Request for url', str(e)):
                raise e
            else:
                print("Wrong password for " + email + " at " + mcurl)
        except requests.exceptions.ConnectionError as e:
            print("Could not connect to " + mcurl)
            raise e
    config.remotes.append(remote_config)
    config.save()

    print()
    print("Added APIKey for", email, "at", mcurl, "to", config.config_file)
    print()

    return remote_config

def make_client_and_login_if_necessary(email=None, mcurl=None):
    """Make Client, prompting for login if remote is not stored in Config

    Args:
        email (str): User account email.
        mcurl (str): URL for Materials Commons remote instance. Example:
            "https://materialscommons.org/api".

    Returns:
        :class:`materials_commons.api.Client`: A client for the provided URL
        and user account.
    """
    remote_config = get_remote_config_and_login_if_necessary(mcurl=mcurl,
                                                             email=email)
    return remote_config.make_client()
