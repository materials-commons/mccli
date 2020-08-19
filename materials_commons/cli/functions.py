import datetime
import dateutil
import hashlib
import json
import os
import requests
import sys
import time

import materials_commons.api.models as models
from tabulate import tabulate

from materials_commons.cli.exceptions import MCCLIException, MissingRemoteException, \
    MultipleRemoteException, NoDefaultRemoteException
from materials_commons.cli.print_formatter import PrintFormatter, trunc
from materials_commons.cli.sqltable import SqlTable
from materials_commons.cli.user_config import Config, RemoteConfig

# TODO: mcapi.Config, mcapi.Remote, mcapi.RemoteConfig

def mkdir_if(path):
    """Convenience function for making a directory, if it does not exist. """
    if not os.path.exists(path):
        os.mkdir(path)

def remove_if(path):
    """Convenience function for removing a file, if it exists. """
    if os.path.exists(path):
        os.remove(path)

def rmdir_if(path):
    """Convenience function for removing a directory, if it exists. """
    if os.path.exists(path):
        os.rmdir(path)

def make_file(path, text):
    """Convenience function for writing "text" to a file at "path". """
    with open(path, 'w') as f:
        f.write(text)

def remove_hidden_project_files(project_path):
    """Removes a local project's configuration files and directory"""
    remove_if(os.path.join(project_path, ".mc", "config.json"))
    remove_if(os.path.join(project_path, ".mc", "project.db"))
    rmdir_if(os.path.join(project_path, ".mc"))

def getit(obj, name, default=None):
    """Returns the "name" attribute (or default value) whether "obj" is a dict or an object."""
    if isinstance(obj, dict):
        return obj.get(name, default)
    else:
        return getattr(obj, name, default)

def as_is(value):
    """Returns value without any changes. A placeholder for when a function is needed."""
    return value

def epoch_time(time_value):
    """Attempts to convert various time representations into s since the epoch

    Args:
        time_value: A representation of time.

    Returns:
        An integer s since the epoch. Uses the following:

            +-------------------+------------------------------------------------------------+
            | If this type      | Then do this conversion                                    |
            +-------------------+------------------------------------------------------------+
            | str               | time.mktime(dateutil.parser.parse(time_value).timetuple()) |
            +-------------------+------------------------------------------------------------+
            | float, int        | time_value                                                 |
            +-------------------+------------------------------------------------------------+
            | datetime.datetime | time.mktime(time_value.timetuple())                        |
            +-------------------+------------------------------------------------------------+
            | dict              | time_value['epoch_time']                                   |
            +-------------------+------------------------------------------------------------+
            | None              | None                                                       |
            +-------------------+------------------------------------------------------------+
            | Else              | str(type(time_value))                                      |
            +-------------------+------------------------------------------------------------+
    """
    if isinstance(time_value, str): # expect ISO 8601 str
        return time.mktime(dateutil.parser.parse(time_value).timetuple())
    elif isinstance(time_value, (float, int)):
        return time_value
    elif isinstance(time_value, datetime.datetime):
        return time.mktime(time_value.timetuple())
    elif isinstance(time_value, dict) and ('epoch_time' in time_value):
        return time_value['epoch_time']
    elif time_value is None:
        return None
    else:
        return str(type(time_value))

def format_time(time_value, fmt="%Y %b %d %H:%M:%S"):
    """Attempts to put various time representations into specified format for printing

    Args:
        time_value: A representation of time.

        fmt (str): Format to use for the return value.

    Returns:
        A string representation using the specified format. Uses the following:

            +-------------------+-------------------------------------------------+
            | If this type      | Then do this conversion                         |
            +-------------------+-------------------------------------------------+
            | str               | dateutil.parser.parse(time_value).strftime(fmt) |
            +-------------------+-------------------------------------------------+
            | float, int        | time.strftime(fmt, time.localtime(time_value))  |
            +-------------------+-------------------------------------------------+
            | datetime.datetime | time.strftime(fmt, time.localtime(time_value))  |
            +-------------------+-------------------------------------------------+
            | dict              | format_time(time_value['epoch_time'])           |
            +-------------------+-------------------------------------------------+
            | None              | "-"                                             |
            +-------------------+-------------------------------------------------+
            | Else              | str(type(time_value))                           |
            +-------------------+-------------------------------------------------+
    """
    if isinstance(time_value, str): # expect ISO 8601 str
        return dateutil.parser.parse(time_value).strftime(fmt)
    elif isinstance(time_value, (float, int)):
        return time.strftime(fmt, time.localtime(time_value))
    elif isinstance(time_value, datetime.datetime):
        return time_value.strftime(fmt)
    elif isinstance(time_value, dict) and ('epoch_time' in time_value):
        return format_time(time_value['epoch_time'])
    elif time_value is None:
        return '-'
    else:
        return str(type(time_value))

def checksum(path):
    """Generate MD5 checksum for the file at "path" """
    with open(path, 'rb') as f:
        return hashlib.md5(f.read()).hexdigest()

def random_name(n=3, max_letters=6, sep='-'):
    """Generates a random name for "n" words of max "max_letters" length, joined by "sep" """
    import random
    word_file = "/usr/share/dict/words"
    if os.path.exists(word_file):
        WORDS = open(word_file).read().splitlines()
    else:
        import requests
        word_site = "http://svnweb.freebsd.org/csrg/share/dict/words?view=co&content-type=text/plain"
        response = requests.get(word_site)
        WORDS = response.content.decode("utf-8").splitlines()
    results=[]
    count=0
    while count<n:
        word = WORDS[random.randint(0, len(WORDS)-1)]
        if word[0].isupper():
            continue
        if len(word) > max_letters:
            continue
        results.append(word)
        count += 1
    return sep.join(results)


def print_table(data, columns=[], headers=[], out=None):
    """Print table from list of dict

    Args:
        data: list of dict, Data to print
        columns: list of str, Keys of data to print, in order
        headers: list of str, Header strings
        out: stream, Output stream
    """
    tabulate_in = []
    if out is None:
        out = sys.stdout
    for record in data:
        tabulate_in.append([record[col] for col in columns])
    out.write(tabulate(tabulate_in, headers=headers))
    out.write('\n')

def print_projects(projects, current=None):
    """Prints a list of projects, including a '*' indicating the project containing the current working directory

    Args:
        projects: A list of :class:`materials_commons.api.Project`
        current: A `materials_commons.api.Project` containing the current working directory as determined by "os.getcwd()", or None if not in a local project directory.
    """
    data = []
    for p in projects:
        _is_current = ' '
        if current is not None and p.uuid == current.uuid:
            _is_current = '*'

        data.append({
            'current': _is_current,
            'name': trunc(p.name, 40),
            'owner': p.owner_id,    # TODO: owner email
            'id': p.id,
            'uuid': p.uuid,
            'modified_at': format_time(p.updated_at)
        })

    columns=['current', 'name', 'owner', 'id', 'uuid', 'modified_at']
    headers=['', 'name', 'owner', 'id', 'uuid', 'modified_at']
    print_table(data, columns=columns, headers=headers)

def print_remote_help():
    """Print a help message for cases when no remotes are configured"""
    print("Add a remote with:")
    print("    mc remote --add EMAIL URL")
    print("List current remotes with:")
    print("    mc remote")
    print("List other known remote urls with:")
    print("    mc remote -l")


def add_remote_option(parser, help):
    """Add the "--remote <email> <url>" cli option to an ArgumentParser

    - This standardizes the use of the "--remote <email> <url>" cli option which is needed in some contexts, but not others

    Args:
        parser (:class:`argparse.ArgumentParser`): The initial ArgumentParser

    Returns:
        :class:`argparse.ArgumentParser`: The ArgumentParser, with the added option "--remote <email> <url>".
    """
    parser.add_argument('--remote', nargs=2, metavar=('EMAIL', 'URL'), help=help)

def optional_remote_config(args):
    """Return remote configuration parameters specified by cli option "--remote", or the user's default remote

    Args:
        args (argparse.Namespace): The result of argparse's "parse_args()" method. Checks for "args.remote".

    Returns:
        :class:`materials_commons.api.Client`: The remote configuration parameters for the appropriate client: either the value specified using the "--remote <email> <url>" option, or else the user's configured default.
    """
    config = Config()
    if args.remote:
        email = args.remote[0]
        url = args.remote[1]

        remote_config = RemoteConfig(mcurl=url, email=email)
        if remote_config not in config.remotes:
            raise MissingRemoteException("Could not find remote: {0} {1}".format(remote_config.email, remote_config.mcurl))
        return remote_config
    else:
        if not config.default_remote.mcurl or not config.default_remote.mcapikey:
            raise NoDefaultRemoteException("Default remote not set")
        return config.default_remote

def optional_remote(args, default_client=None):
    """Return remote specified by cli option "--remote", or the user's default remote

    Args:
        args (argparse args): The result of argparse's "parse_args()" method. Checks for "args.remote".
        default_client (:class:`materials_commons.api.Client`): The default client to use. If None, uses the default remote specified in the user's configuration file: ::

            `materials_commons.cli.user_config.Config().default_remote.make_client()`.

    Returns:
        :class:`materials_commons.api.Client`: The appropriate client: the value specified using the "--remote <email> <url>" option, or else the user's configured default.
    """
    config = Config()
    if args.remote:
        email = args.remote[0]
        url = args.remote[1]

        remote_config = RemoteConfig(mcurl=url, email=email)
        if remote_config not in config.remotes:
            raise MissingRemoteException("Could not find remote: {0} {1}".format(remote_config.email, remote_config.mcurl))

        return config.remotes[config.remotes.index(remote_config)].make_client()
    else:

        if default_client is None:
            if not config.default_remote.mcurl or not config.default_remote.mcapikey:
                raise NoDefaultRemoteException("Default remote not set")
            return config.default_remote.make_client()
        else:
            return default_client

def print_remotes(config_remotes, show_apikey=False):
    """Print a table with remote Materials Commons instance configuration parameters (email, url, apikey)

    Args:
        config_remotes: A dict of :class:`materials_commons.cli.user_config.RemoteConfig`, grouped like: "{<url>: {<email>: RemoteConfig, ...}, ...}"
        show_apikey (bool): If True, print apikeys also.
    """
    if not len(config_remotes):
        print("No remotes")
        return
    default_remote = Config().default_remote

    from operator import itemgetter
    #data = sorted([vars(rconfig) for rconfig in config_remotes], key=itemgetter('mcurl', 'email'))

    data = []
    for remote_config in config_remotes:
        values = vars(remote_config)
        if remote_config == default_remote:
            values['is_default'] = '(default)'
        else:
            values['is_default'] = ' '
        data.append(values)
    data = sorted(data, key=itemgetter('mcurl', 'email'))

    columns = ['url', 'email', ' ']
    if show_apikey:
        columns.append('apikey')

    def width(col):
        return max([len(str(record[col])) for record in data])

    fmt = [
        ('is_default', ' ', '<', width('is_default'), as_is),
        ('email', 'email', '<', width('email'), as_is),
        ('mcurl', 'url', '<', width('mcurl'), as_is)
    ]
    if show_apikey:
        fmt.append(('mcapikey', 'apikey', '<', width('mcapikey'), as_is))

    pformatter = PrintFormatter(fmt)

    pformatter.print_header()
    for record in data:
        pformatter.print(record)

def make_local_project_client(path=None):
    """Construct a client to access project data from the local project configuration

    Args:
        path (str): Path to anywhere inside a local project directory

    Returns:
        :class:`materials_commons.api.Client`: A client for the instance of Materials Commons that is storing the project
    """
    project_config = read_project_config(path)
    if not project_config:
        return None

    config = Config()
    remote_config = project_config.remote
    if remote_config == config.default_remote:
        return config.default_remote.make_client()
    elif remote_config not in config.remotes:
        raise MissingRemoteException("Could not make project Client, failed to find remote config: {0} {1}".format(remote_config.email, remote_config.mcurl))
    remote_config_with_apikey = config.remotes[config.remotes.index(remote_config)]
    return remote_config_with_apikey.make_client()

class ProjectTable(SqlTable):
    """The ProjectTable creates a sqlite "project" table to cache some basic project data"""

    @staticmethod
    def default_print_fmt():
        """Returns project table print formatting parameters"""
        return [
            ("name", "name", "<", 24, as_is),
            ("id", "id", "<", 36, as_is),
            ("uuid", "uuid", "<", 36, as_is),
            ("checktime", "checktime", "<", 24, format_time)
        ]

    @staticmethod
    def tablecolumns():
        """Returns sqlite project table creating parameters

        Returns:
            A dict, with column name as key, and list of column creation args for value.
        """
        return {
            "id": ["integer", "UNIQUE"],
            "uuid": ["text"],
            "name": ["text"],
            "data": ["text"],
            "checktime": ["real"]     # last time the remote data was queried (s since epoch)
        }

    @staticmethod
    def tablename():
        """Returns sqlite project table name "project" """
        return "project"

    def __init__(self, proj_local_path):
        """

        Args:
            proj_local_path (str): Path to local project directory
        """
        super(ProjectTable, self).__init__(proj_local_path)

    def select_all(self):
        """Select record by id

        Returns:
             sqlite3.Row or None
        """
        self.curs.execute("SELECT * FROM " + self.tablename())
        return self.curs.fetchall()

def make_local_project(path=None, data=None):
    """Read local project config file and use to construct materials_commons.api.models.Project

    - Checks if "path" is a path located inside a local project directory (by looking for the ".mc" directory).
    - Use local project configuration to:
        - Construct a "remote" instance (:class:`materials_commons.api.Client`)
        - Call Materials Commons and construct a project instance (:class:`materials_commons.api.Project`)
    - Add attributes to the project:
        - "local_path" (str) providing the absolute path to the local project directory
        - "remote" (:class:`materials_commons.api.Client`) project specific client instance

    Args:
        path (str): Path inside a local project directory
        data (dict): Optional, project data. If stored in cache this avoids an extra API call.

    Notes:
        Caching behavior is currently disabled while updating `materials_commons.cli` for MC2.0. It allows setting a "fetch lock" so that data that is not cached or older than the time the lock was set will be queried from the remote, otherwise the local cache data is used.
    """

    proj_path = project_path(path)
    if not proj_path:
        if path is None:
            path = os.getcwd()
        raise MCCLIException("No Materials Commons project found at " + path)

    project_config = read_project_config(path)

    # check for project data cached in sqlite ".mc/project.db"
    project_table = ProjectTable(proj_path)
    project_table.connect()
    results = project_table.select_all()
    project_table.close()

    if len(results) > 1:
        raise MCCLIException("Project db error: Found >1 project")

    client = make_local_project_client(path)
    if not results or not project_config.remote_updatetime or results[0]['checktime'] < project_config.remote_updatetime:
        checktime = time.time()
        try:
            if data is None:
                proj = client.get_project(project_config.project_id)
            else:
                proj = models.Project(data=data)
        except requests.exceptions.ConnectionError as e:
            raise MCCLIException("Could not connect to " + remote.config.mcurl)
        except requests.exceptions.HTTPError as e:
            raise MCCLIException("HTTPError: " + str(e))

        record = {
            'id': project_config.project_id,
            'uuid': project_config.project_uuid,
            'name': proj.name,
            'data': json.dumps(proj._data),
            'checktime': checktime
        }
        project_table.connect()
        project_table.insert_or_replace(record)
        project_table.close()
    else:
        record = results[0]
        proj = models.Project(data=json.loads(record['data']))

    proj.local_path = proj_path
    proj.remote = client
    return proj

def make_local_expt(proj):
    """Read local project configuration to construct "current experiment" object

    - The "current experiment" is a default experiment for putting newly created samples or processes

    Args:
        proj (:class:`materials_commons.api.models.Project`): A Project instance, including the `materials_commons.cli` added attribute "local_path".

    Returns:
        Returns the local project's "current experiment" (:class:`materials_commons.api.models.Experiment`), or None if not set.
    """
    project_config = read_project_config(proj.local_path)

    if project_config:
        expt = proj.remote.get_experiment(project_config.experiment_id)
        expt.project = proj
        return expt

    return None

def humanize(file_size_bytes):
    """Get a nice string representation of file size

    Args:
        file_size_bytes (int): File size in bytes

    Returns:
        str: File size as human readable string (ex: "10B", "8K", "5M", "2G", etc.)
    """
    abbrev = [("B", 0), ("K", 10), ("M", 20), ("G", 30), ("T", 40)]
    for key, val in abbrev:
        _size = (file_size_bytes >> val)
        if _size < 1000 or key == "T":
            return str(_size) + key

def request_confirmation(msg, force=False):
    """Request user confirmation

    Args:
        msg (str): For example, the value "Are you sure you want to permanently delete these?", will prompt user with: ::

            "Are you sure you want to permanently delete these? ('Yes'/'No'): "

        force (bool): Proceed without user confirmation

    Returns:
        bool: True if confirmed or forced, False if not confirmed.
    """
    if not force:
        msg = msg + " ('Yes'/'No'): "
        while True:
            input_str = input(msg)
            if input_str == 'No':
                return False
            elif input_str == 'Yes':
                return True
            print("Invalid input")
    else:
        return True

def _proj_path(path=None):
    """Returns the path to a local project directory if it contains "path", else None"""
    if path is None:
        path = os.getcwd()
    # if not os.path.isdir(path):
    #   raise Exception("Error, no directory named: " + path)
    curr = path
    cont = True
    while cont is True:
        test_path = os.path.join(curr, '.mc')
        if os.path.isdir(test_path):
            return curr
        elif curr == os.path.dirname(curr):
            return None
        else:
            curr = os.path.dirname(curr)
    return None

def project_path(path=None):
    """Returns the path to a local project directory if it contains "path", else None"""
    return _proj_path(path)

def project_exists(path=None):
    """Returns True if a local project directory exists containing "path" """
    if _proj_path(path):
        return True
    return False

def _mcdir(path=None):
    """Find project .mc directory path if it already exists, else return None"""
    dirpath = _proj_path(path)
    if dirpath is None:
        return None
    return os.path.join(dirpath, '.mc')


def _proj_config(path=None):
    """Find project config path if .mc directory already exists, else return None"""
    dirpath = _proj_path(path)
    if dirpath is None:
        return None
    return os.path.join(dirpath, '.mc', 'config.json')

class ProjectConfig(object):
    """Facilitates reading and writing a JSON file storing local project configuration values

    Project ``.mc/config.json`` file format: ::

        {
            "remote": {
                "mcurl": <url>,
                "email": <email>
            },
            "project_id": <id>,
            "project_uuid": <uuid>,
            "experiment_id": <id>,
            "experiment_uuid": <uuid>,
            "remote_updatetime": <number>,
            "globus_upload_id": <id>,
            "globus_download_id": <id>
        }

    Attributes:
        project_path (str): Absolute path to local project directory.
        config_dir (str): Absolute path to local project configuration directory (".mc").
        config_path (str): Absolute path to local project configuration file (".mc/config.json").
        project_id (int or None): Project ID
        project_uuid (str or None): Project UUID
        experiment_id (int or None): Current experiment ID
        experiment_uuid (str or None): Current experiment UUID
        remote (user_config.RemoteConfig): Holds configuration variables (email, url, apikey) for the remote instance of Materials Commons where the project is stored.
        remote_updatetime (number or None): For use with optional caching, holds the last time local cache data was updated from the remote.
        globus_upload_id (int or None): ID specifying which Globus upload directory should be used for Globus uploads.
        globus_download_id (int or None): ID specifying which Globus download directory should be used for Globus downloads.

    """
    def __init__(self, project_path):
        """Construct by reading local project configuration file if it exists

        Args:
            project_path (str): Absolute path to local project directory. Will read `<project_path>/.mc/config.json`. All attributes will be None if the configuration file does not exist.
        """
        self.project_path = project_path
        self.config_dir = os.path.join(self.project_path, ".mc")
        self.config_path = os.path.join(self.config_dir, "config.json")

        data = {}
        if os.path.exists(self.config_path):
            with open(self.config_path) as f:
                data = json.load(f)

        # handle deprecated 'remote_url'
        if 'remote_url' in data and 'remote' not in data:
            config = Config()
            matching = [remote_config for remote_config in config.remotes if remote_config.mcurl == data['remote_url']]
            if len(matching) == 0:
                raise MissingRemoteException("Could not get project data. Failed to find remote with url: {0}".format(data['remote_url']))
            elif len(matching) > 1:
                raise MultipleRemoteException("Could not get project data. Found multiple remote accounts for url: {0}".format(data['remote_url']))
            else:
                data['remote'] = dict()
                data['remote']['mcurl'] = matching[0].mcurl
                data['remote']['email'] = matching[0].email

        self.remote = RemoteConfig(**data.get('remote', {}))
        self.project_id = data.get('project_id', None)
        self.project_uuid = data.get('project_uuid', None)
        self.experiment_id = data.get('experiment_id', None)
        self.experiment_uuid = data.get('experiment_uuid', None)
        self.remote_updatetime = data.get('remote_updatetime', None)
        self.globus_upload_id = data.get('globus_upload_id', None)
        self.globus_download_id = data.get('globus_download_id', None)

    def to_dict(self):
        """Returns the project configuration as a dict"""
        return {
            'remote': {
                'mcurl': self.remote.mcurl,
                'email': self.remote.email
            },
            'project_id': self.project_id,
            'project_uuid': self.project_uuid,
            'experiment_id': self.experiment_id,
            'experiment_uuid': self.experiment_uuid,
            'remote_updatetime': self.remote_updatetime,
            'globus_upload_id': self.globus_upload_id,
            'globus_download_id': self.globus_download_id,
        }

    def save(self):
        """Save project configuration as a JSON file"""
        if not os.path.exists(self.config_dir):
            os.mkdir(self.config_dir)
        with open(self.config_path, 'w') as f:
            json.dump(self.to_dict(), f)
        return

def read_project_config(path=None):
    """Read local project configuration

    Returns:
         If the project configuration file ("<project>/.mc/config.json") exists, returns a :class:`ProjectConfig` instance. Else, returns None.
    """
    # get config
    proj_config_path = _proj_config(path)
    if proj_config_path:
        return ProjectConfig(project_path(path))
    else:
        return None

def clone_project(remote_config, project_id, parent_dest):
    """Clone a remote project to a local directory

    - Will create the local project directory "<parent_dest>/<project_name>"
    - Will save local project configuration "<parent_dest>/.mc/config.json" with project info

    Args:
        remote_config (:class:`user_config.RemoteConfig`): Holds configuration variables (email, url, apikey) for the remote instance of Materials Commons where the project to be cloned is stored.
        project_id (int): ID of the project to clone
        parent_dest (str): Absolute path to a local directory which will become the parent of the cloned project directory.

    Returns:
        :class:`materials_commons.api.models.Project`: Object representing the cloned project
    """
    # get Project
    client = remote_config.make_client()
    proj = client.get_project(project_id)

    # check if project already exists
    dest = os.path.join(parent_dest, proj.name)
    if project_path(dest):
        raise MCCLIException("Project already exists at:", project_path(dest))

    # create project directory
    if not os.path.exists(dest):
        os.mkdir(dest)

    project_config = ProjectConfig(dest)
    project_config.remote = remote_config
    project_config.project_id = proj.id
    project_config.project_uuid = proj.uuid
    project_config.save()

    return make_local_project(dest, proj._data)
