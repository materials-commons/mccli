import os
import pathlib
import tempfile
from contextlib import contextmanager

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
from materials_commons.cli.user_config import Config, \
    get_remote_config_and_login_if_necessary
from materials_commons.cli.subcommands.down import down_subcommand
from materials_commons.cli.subcommands.up import up_subcommand

# TODO: only call os.getcwd() from parser::main

class ClonedProject(object):
    """A cloned Materials Commons project instance

    Attributes:
        local_path (pathlib.Path): Location of the cloned Materials Commons
            project
        proj (materials_commons.api.models.Project): Materials Commons project
            object
        tmpdir (tempfile.TemporaryDirectory or None): Temporary directory
            instance. If not None, the temporary directory is the parent of the
            cloned Materials Commons project directory.

    """

    def __init__(self, email=None, mcurl=None, proj_id=None, path=None,
                 parent_path=None, name=None):
        """Construct a cloned Materials Commons project instance

        Examples:

            Open a project that has already been cloned:

            .. code-block:: python

                path = "/path/to/materials_commons_projects/ProjectName"
                mc_proj = ClonedProject(path=path)

            Clone project to a particular directory or open if already cloned:

            .. code-block:: python

                email = "username@domain.com"
                mcurl = "https://materialscommons.org/api"
                proj_id = 25
                parent_path = "/path/to/materials_commons_projects"
                name = None  # default uses remote project name
                mc_proj = ClonedProject(email=email,
                                        mcurl=mcurl,
                                        proj_id=proj_id,
                                        parent_path=parent_path,
                                        name=name)

            Clone project to a temporary directory:

            .. code-block:: python

                email = "username@domain.com"
                mcurl = "https://materialscommons.org/api"
                proj_id = 25
                mc_proj = ClonedProject(email=email, mcurl=mcurl, proj_id=proj_id)

        Args:
            email (str): User account email
            mcurl (str): URL for Materials Commons remote instance containing
                the project. Example: "https://materialscommons.org/api".
            proj_id (int): ID of project to clone.
            path (str): Path where the project exists, if already cloned.
            parent_path (str): Path to parent directory where the project should
                be cloned if path is None. If neither path nor parent_path are
                given, uses a tempfile.TemporaryDirectory for parent_path.
            name (str): Name of created project directory. Default is remote
                project name.
        """
        self.local_path = None
        self.proj = None
        self.tmpdir = None

        if path is not None:
            if clifuncs.project_exists(path):
                self.proj = clifuncs.make_local_project(path)
            else:
                raise cliexcept.MCCLIException("No project found at " + path)
        else:
            if proj_id is None:
                raise cliexcept.MCCLIException("`proj_id` is required if `path` is not provided")

            if email is None or mcurl is None:
                config = Config()
                if not config.default_remote.mcurl or not config.default_remote.mcapikey:
                    raise cliexcept.NoDefaultRemoteException("Default remote not set")
                remote_config = config.default_remote
            else:
                remote_config = get_remote_config_and_login_if_necessary(
                    mcurl=mcurl, email=email)

            if parent_path is None:
                self.tmpdir = tempfile.TemporaryDirectory()
                self.proj = clifuncs.clone_project(remote_config, proj_id, self.tmpdir.name)

            else:
                self.tmpdir = None
                parent_path = str(parent_path)

                # check if project already exists, then construct or clone
                client = remote_config.make_client()
                proj = client.get_project(proj_id)
                path = os.path.join(parent_path, proj.name)
                if clifuncs.project_exists(path):
                    self.proj = clifuncs.make_local_project(path, proj._data)
                else:
                    self.proj = clifuncs.clone_project(remote_config, proj_id, parent_path)

        self.local_path = pathlib.Path(self.proj.local_path)

    def glob(self, pattern):
        """Helper to construct paths for upload or download

        Args:
            pattern (str): Pattern, relative to local project directory root,
                passed as argument to `self.local_path.glob(pattern)`.

        Returns:
            List of str, File paths found from use of `glob`, made relative to
            self.local_path and converted to str.

        """
        return [str(file.relative_to(self.local_path)) for file in self.local_path.glob(pattern)]

    def download(self, *paths, recursive=False, only_print=False, force=False,
                 output=None, globus=False, label=None, no_compare=False):
        """Download requested files from the Materials Commons project

        Args:
            recursive (bool): Download directory contents recursively
            force (bool): Force overwrite of existing files
            only_print (bool): Print file, do not write
            output (str): Download file name. Only allowed if `len(paths) == 1`.
            globus (bool): Use globus to download files
            label (str): Globus transfer label to make finding tasks simpler
            no_compare (bool): Download remote without checking if local is
                equivalent
            *paths (str): Files or directories to download, specified either
                using absolute paths or paths relative to the project root
                directory (`self.local_path`).
        """
        # TODO: convert to direct function calls rather than arg parsing

        working_dir = self.local_path

        argv = []
        if recursive is True:
            argv.append("-r")
        if only_print is True:
            argv.append("-p")
        if force is True:
            argv.append("-f")
        if output is not None:
            argv.append("-o")
            argv.append(str(output))
        if globus is True:
            argv.append("-g")
        if label is not None:
            argv.append("--label")
            argv.append(str(label))
        if no_compare is True:
            argv.append("--no-compare")
        if len(paths):
            # using relpaths is more robust within the working_dir context
            # argv += [str(os.path.relpath(os.path.abspath(path), self.local_path)) for path in paths]
            argv += [os.path.normpath(os.path.join(working_dir, path)) for path in paths]
        try:
            down_subcommand(argv, working_dir)
        except SystemExit as e:
            print("Invalid download request")

    def upload(self, *paths, recursive=False, limit=None, globus=False,
               label=None, no_compare=False, upload_as=None):
        """Upload requested files to Materials Commons

        Args:
            recursive (bool): Download directory contents recursively
            limit (str): File size upload limit (MB). Default="50" (50MB). Does
                not apply to Globus uploads.
            globus (bool): Use globus to download files
            label (str): Globus transfer label to make finding tasks simpler
            no_compare (bool): Download remote without checking if local is
                equivalent
            upload_as (str): Upload a file or directory to a particular location in the project. Raises if `len(paths) != 1`.
            *paths (str): Files or directories to upload, specified either
                using absolute paths or paths relative to the project root
                directory (`self.local_path`).
        """
        # TODO: convert to direct function calls rather than arg parsing

        working_dir = self.local_path

        argv = []
        if recursive is True:
            argv.append("-r")
        if limit is not None:
            argv.append("--limit")
            argv.append(str(limit))
        if globus is True:
            argv.append("-g")
        if label is not None:
            argv.append("--label")
            argv.append(str(label))
        if no_compare is True:
            argv.append("--no-compare")
        if upload_as is not None:
            argv.append("--upload-as")
            argv.append(str(upload_as))
        if len(paths):
            # using relpaths is more robust within the working_dir context
            # argv += [str(os.path.relpath(os.path.abspath(path), self.local_path)) for path in paths]
            argv += [os.path.normpath(os.path.join(working_dir, path)) for path in paths]
        try:
            up_subcommand(argv, working_dir)
        except SystemExit as e:
            print("Invalid upload request")
