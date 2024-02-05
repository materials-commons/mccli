import globus_sdk
from globus_sdk.scopes import TransferScopes
import os

import materials_commons.api as mcapi
import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.file_functions as filefuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.user_config import Config

CLIENT_ID = '1e4aacbc-8c10-4812-a54a-8434d2030a41'

def check_for_consent_required(client, target):
    try:
        client.operation_ls(target, path="/")
    except globus_sdk.TransferAPIError as err:
        if err.info.consent_required:
            return err.info.consent_required.required_scopes
    return None


def get_transfer_rt_or_login(scopes=TransferScopes.all, reauth=False):
    """Get the Globus transfer refresh token, prompting for login if necessary

    If not yet configured, will prompt user to login and enter a code used to obtain the refresh
    token. Once obtained the token will be saved into the user's config file so future login is
    unnecessary.

    Returns
    -------
    transfer_rt: str, Globus transfer refresh token for authentication
    """

    config = Config()

    # return existing transfer_rt
    if not reauth:
        if config.globus.transfer_rt:
            return config.globus.transfer_rt

    # authenticate

    client = globus_sdk.NativeAppAuthClient(CLIENT_ID)
    client.oauth2_start_flow(refresh_tokens=True, requested_scopes=scopes)

    authorize_url = client.oauth2_get_authorize_url()
    try:
        import webbrowser
        webbrowser.open(authorize_url)
    except:
        pass

    print('Please login. If a webpage does not open automatically, go here:\n\n{0}\n\n'.format(authorize_url))
    auth_code = input('Please enter the code you get after login here: ').strip()
    token_response = client.oauth2_exchange_code_for_tokens(auth_code)

    #globus_auth_data = token_response.by_resource_server['auth.globus.org']
    globus_transfer_data = token_response.by_resource_server['transfer.api.globus.org']
    transfer_rt = globus_transfer_data['refresh_token']

    # save the token
    config.globus.transfer_rt = transfer_rt
    config.save()

    # return the token
    return config.globus.transfer_rt


def make_transfer_client(transfer_rt):
    """Make a Globus TransferClient

    Arguments
    ---------
    transfer_rt: str
        Globus transfer authentication refresh token. Can be obtained from user's globus
        configuration via `Config().globus.transfer_rt`, and will be None if not configured. To
        obtain from configuration or prompt user to login, use `get_transfer_rt_or_login`.

    Returns
    -------
    transfer_client: globus_sdk.TransferClient
    """

    client = globus_sdk.NativeAppAuthClient(CLIENT_ID)
    client.oauth2_start_flow(refresh_tokens=True)

    # authorizer = globus_sdk.RefreshTokenAuthorizer(
    #     transfer_rt, client, access_token=transfer_at, expires_at=expires_at_s)
    authorizer = globus_sdk.RefreshTokenAuthorizer(transfer_rt, client)

    # and try using `tc` to make TransferClient calls. Everything should just
    # work -- for days and days, months and months, even years
    return globus_sdk.TransferClient(authorizer=authorizer)


def get_local_endpoint_id():
    """Get the local endpoint id

    Checks for a local Globus connect personal, if not found checks user config for globus.endpoint_id.

    Returns
    -------
        local_endpoint_id: str or None if not found
    """
    local_endpoint = globus_sdk.LocalGlobusConnectPersonal()
    if local_endpoint.endpoint_id:
        return local_endpoint.endpoint_id
    else:
        config = Config()
        return config.globus.endpoint_id

def get_local_endpoint_id_or_exit():
    """Get the local endpoint id"""
    local_endpoint_id = get_local_endpoint_id()
    if not local_endpoint_id:
        print('No local Globus endpoint id found')
        print('Globus personal endpoint UUIDs can be detected automatically and if installed need not be set.')
        print('Globus public endpoint UUIDs can be found from: https://app.globus.org/endpoints')
        print("Please set your Globus endpoint UUID with `mc config --set-globus-endpoint-id <ID>`")
        raise cliexcept.MCCLIException("Invalid globus request")
    return local_endpoint_id

def check_transfer_types(source_type, target_type, recursive, verbose, printpath):
    """Check if source and target types are valid

    Args:
        source_type: 'file', 'directory', or None
        target_type: 'file', 'directory', or None
        recursive (bool): If True, upload directories recursively
        verbose (bool): If True, print error messages
        printpath (str): If verbose==True, print error messages referring using this path.

    Returns:
        True if transfer may proceed, False if transfer may not proceed
    """
    if source_type == 'directory' and not recursive:
        if verbose:
            print(printpath + ": is a directory (skipping)")
        return False

    if source_type == 'directory' and target_type == 'file':
        if verbose:
            print(printpath + ": not a directory (skipping)")
        return False

    if source_type == 'file' and target_type == 'directory':
        if verbose:
            print(printpath + ": is a directory (skipping)")
        return False
    return True

class GlobusOperations(object):
    """Used to perform Globus operations

    Arguments
    ---------
    local_endpoint_id: str or None
        ID of local endpoint. If None provided, will attempt to get from local Globus connect
        personal, or user config file. If not found, will exit with message.
    transfer_client: globus_sdk.TransferClient or None
        Globus TransferClient instance. If None provided, will attempt to construct from user
        config or will prompt user to login to obtain a refresh token which will then be saved into
        the user's config file.
    verbose: bool (optional, default=True)
        Print messages detailing individual steps.

    """
    def __init__(self, local_endpoint_id=None, transfer_client=None, verbose=True):

        if not local_endpoint_id:
            local_endpoint_id = get_local_endpoint_id_or_exit()

        if not transfer_client:
            transfer_rt = get_transfer_rt_or_login()
            transfer_client = make_transfer_client(transfer_rt)

        self.local_endpoint_id = local_endpoint_id
        self.tc = transfer_client
        self.verbose = verbose

    def create_all_directories_on_path(self, path, endpoint_id, endpoint_path):
        """Create all directories on path at upload endpoint

        Arguments:
            path: str, Relative path inside project to a directory (excludes project directory)
            endpoint_id: str, Endpoint ID
            endpoint_path: str, Path to project directory on endpoint. Will create all directories on endpoint for `endpoint_path/path`
        """
        def finddir(contents, name):
            for entry in contents:
                if entry['name'] == name and entry['type'] == 'dir':
                    return True
            return False

        found = True
        curr_relpath = ""
        curr_abspath = os.path.join(endpoint_path, curr_relpath)
        for name in path.split(os.sep):

            if not len(name):
                continue

            if found:
                contents = self.tc.operation_ls(endpoint_id, path=curr_abspath)
                found = finddir(contents, name)

            curr_relpath = os.path.join(curr_relpath, name)
            curr_abspath = os.path.join(endpoint_path, curr_relpath)

            if not found:
                self.tc.operation_mkdir(endpoint_id, curr_abspath)
        return


    def upload_v0(self, proj, paths, upload, working_dir, recursive=False, no_compare=False,
                  label=None, localtree=None, remotetree=None):
        """Upload files and directories to project

        Arguments:
            proj: Project
            paths (List of str): Materials Commons paths (include project directory) to upload
            upload: mcapi.GlobusUpload, Globus upload request
            working_dir (str): Current working directory, used for finding
                relative paths and printing messages.
            recursive: bool, If True, upload directories recursively
            no_compare (bool): If False (default), compare checksum to skip overwriting equivalent
                files. If True, transfer without checking.
            label: str, Globus transfer label to make finding tasks simpler

        Returns:
            None or task_id: str, transfer task id. Returns nothing to transfer.
        """
        if not len(paths):
            return None

        files_data, dirs_data, child_data, non_existing = treefuncs.treecompare(proj, paths, checksum=True, localtree=localtree, remotetree=remotetree)

        # https://globus-sdk-python.readthedocs.io/en/stable/clients/transfer/#globus_sdk.TransferData
        sync_level = "checksum"
        if no_compare is True:
            sync_level = None

        consent_required_scopes = []
        consent_required = check_for_consent_required(self.tc, self.local_endpoint_id)
        if consent_required is not None:
            consent_required_scopes.extend(consent_required)

        consent_required = check_for_consent_required(self.tc, upload.globus_endpoint_id)
        if consent_required is not None:
            consent_required_scopes.extend(consent_required)

        if consent_required_scopes:
            print("One or more of the endpoints requires consent in order to be used.\n"
                  "You must login a second time to grant consents.\n\n")
            tr = get_transfer_rt_or_login(scopes=consent_required_scopes, reauth=True)
            self.tc = make_transfer_client(tr)

        tdata = globus_sdk.TransferData(self.tc, self.local_endpoint_id, upload.globus_endpoint_id, label=label, sync_level=sync_level)

        # add items
        n_items = 0
        for p in paths:

            local_abspath = filefuncs.make_local_abspath(proj.local_path, p)
            relpath = os.path.relpath(local_abspath, proj.local_path)
            printpath = os.path.relpath(local_abspath, working_dir)

            if p in non_existing:
                if self.verbose:
                    print(printpath + ": No such file or directory (skipping)")
                continue

            local_type, remote_type = treefuncs.get_types(p, files_data, dirs_data)

            if not check_transfer_types(local_type, remote_type, recursive, self.verbose, printpath):
                continue

            # note: remote files are versioned, so we skip overwrite checking / force option

            # create missing remote parent directories
            self.create_all_directories_on_path(os.path.dirname(relpath), upload.globus_endpoint_id, upload.globus_path)

            destpath = os.path.join(upload.globus_path, relpath)
            tdata.add_item(local_abspath, destpath, recursive=(recursive and os.path.isdir(local_abspath)))
            n_items += 1

        if not n_items:
            if self.verbose:
                print("Nothing to transfer")
            return None

        # submit transfer request
        transfer_result = self.tc.submit_transfer(tdata)
        task_id = transfer_result["task_id"]

        # print task id
        if self.verbose:
            print("Globus task_id:", task_id)

        return task_id

    def download_v0(self, proj, paths, download, working_dir, recursive=False, no_compare=False,
                    label=None, localtree=None, remotetree=None, force=False):
        """Download files and directories from project

        Arguments:
            proj: Project
            paths: list of str, Materials Commons paths (absolute path, not including project name
                directory)
            download: mcapi.GlobusDownload, Globus download request
            working_dir (str): Current working directory, used for finding
                relative paths and printing messages.
            recursive: bool, If True, download directories recursively
            no_compare (bool): If False (default), compare checksum to skip overwriting equivalent
                files. If True, transfer without checking.
            label: str, Globus transfer label to make finding tasks simpler
            force: bool, If True force download even if local file exists

        Returns:
            None or task_id: str, transfer task id. Returns nothing to transfer.
        """

        if not len(paths):
            return None

        files_data, dirs_data, child_data, non_existing = treefuncs.treecompare(proj, paths, checksum=True, localtree=localtree, remotetree=remotetree)

        # https://globus-sdk-python.readthedocs.io/en/stable/clients/transfer/#globus_sdk.TransferData
        sync_level = "checksum"
        if no_compare is True:
            sync_level = None

        consent_required_scopes = []
        consent_required = check_for_consent_required(self.tc, self.local_endpoint_id)
        if consent_required is not None:
            consent_required_scopes.extend(consent_required)

        consent_required = check_for_consent_required(self.tc, download.globus_endpoint_id)
        if consent_required is not None:
            consent_required_scopes.extend(consent_required)

        if consent_required_scopes:
            print("One or more of the endpoints requires consent in order to be used.\n"
                  "You must login a second time to grant consents.\n\n")
            tr = get_transfer_rt_or_login(scopes=consent_required_scopes, reauth=True)
            self.tc = make_transfer_client(tr)

        tdata = globus_sdk.TransferData(self.tc, download.globus_endpoint_id, self.local_endpoint_id, label=label, sync_level=sync_level)

        # add items
        n_items = 0
        for p in paths:

            local_abspath = filefuncs.make_local_abspath(proj.local_path, p)
            relpath = os.path.relpath(local_abspath, proj.local_path)
            printpath = os.path.relpath(local_abspath, working_dir)

            if p in non_existing:
                if self.verbose:
                    print(printpath + ": No such file or directory (skipping)")
                continue

            local_type, remote_type = treefuncs.get_types(p, files_data, dirs_data)

            if not check_transfer_types(remote_type, local_type, recursive, self.verbose, printpath):
                continue

            # local files may not be backed up, so we check before downloading something that may
            # cause overwriting, unless force==True
            if os.path.exists(local_abspath) and not force:

                what = remote_type
                if remote_type == 'directory':
                    print("Downloading a directory which exists locally may cause remote files to overwrite existing local files")

                print("Overwrite " + what + " '" + os.path.relpath(local_abspath, working_dir) + "'?")
                overwrite = False
                while True:
                    ans = input('y/n: ')
                    if ans == 'y':
                        overwrite = True
                        break
                    elif ans == 'n':
                        overwrite = False
                        break
                if not overwrite:
                    if self.verbose:
                        print(os.path.relpath(local_abspath, working_dir) + \
                            ": Already exists (will not overwrite)")
                    continue

            # create missing local parent directories
            local_dir = os.path.dirname(local_abspath)
            if not os.path.exists(local_dir):
                os.path.makedirs(local_dir)
            if not os.path.isdir(local_dir):
                if self.verbose:
                    print(printpath + ": parent not a directory (skipping)")
                continue

            sourcepath = os.path.join(download.globus_path, relpath)
            tdata.add_item(sourcepath, local_abspath, recursive=(recursive and remote_type=='directory'))
            n_items += 1

        if not n_items:
            if self.verbose:
                print("Nothing to transfer")
            return None

        # submit transfer request
        transfer_result = self.tc.submit_transfer(tdata)
        task_id = transfer_result["task_id"]

        # print task id
        if self.verbose:
            print("task_id =", task_id)

        return task_id
