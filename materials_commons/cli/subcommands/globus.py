import argparse
import json
import sys
import yaml

from io import StringIO

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.globus as cliglobus
from materials_commons.cli.exceptions import MCCLIException
from materials_commons.cli.list_objects import ListObjects
from materials_commons.cli.user_config import Config


def set_current_globus_upload(project_local_path, upload=None):
    pconfig = clifuncs.read_project_config(project_local_path)
    if upload is None:
        pconfig.globus_upload_id = None
    else:
        pconfig.globus_upload_id = upload.id
    pconfig.save()

def set_current_globus_download(project_local_path, download=None):
    pconfig = clifuncs.read_project_config(project_local_path)
    if download is None:
        pconfig.globus_download_id = None
    else:
        pconfig.globus_download_id = download.id
    pconfig.save()

def make_globus_upload_parser():
    """Make argparse.ArgumentParser for `mc globus download`"""
    return GlobusUploadTaskSubcommand().make_parser()

class GlobusUploadTaskSubcommand(ListObjects):

    desc = """List, create, finish, and delete Globus uploads. By default lists current project uploads. With `--all` lists all Globus uploads."""

    def __init__(self):
        super(GlobusUploadTaskSubcommand, self).__init__(
            ["globus", "upload"], "Upload", "Uploads", desc=self.desc,
            requires_project=False, non_proj_member=True, proj_member=True, expt_member=False,
            remote_help='Remote to upload files to',
            list_columns=['current', 'project_name', 'project_id', 'type', 'name', 'id', 'created', 'status'],
            headers=['', 'project_name', 'project_id', 'type', 'name', 'id', 'created', 'status'],
            deletable=True,
            creatable=True,
            custom_actions=['unset'],
            custom_selection_actions=['finish', 'set', 'goto'],
            request_confirmation_actions={
                'finish': 'Are you sure you want to finish these uploads and transfer files into your project?',
                'goto': 'You want to goto these uploads in a web browser?'
            }
        )

    def __update_upload_object(self, upload, proj):
        upload.project_name = proj.name
        upload.project_id = upload._data["project_id"]
        status_code = upload._data["status"]
        if status_code == 0:
            status_message = "Finishing"  # processing uploaded files into project
        elif status_code == 2:
            status_message = "Ready"    # user may upload
        else:
            status_message = "Error - Unknown status code: " + str(status_code)
        upload.status_code = status_code
        upload.status_message = status_message
        upload.transfer_type = "upload"

    def get_all_from_project(self, proj):
        results = []
        uploads = proj.remote.get_all_globus_upload_requests(proj.id)
        for upload in uploads:
            self.__update_upload_object(upload, proj)
            results.append(upload)
        return results

    def get_all_from_remote(self, remote):
        results = []
        projects = remote.get_all_projects()
        for project in projects:
            project.remote = remote
            results += self.get_all_from_project(project)
        return results

    def list_data(self, obj, args):
        _is_current = ' '
        pconfig = clifuncs.read_project_config()
        if pconfig and obj.id == pconfig.globus_upload_id:
            _is_current = '*'
        return {
            "current": _is_current,
            "project_name": clifuncs.trunc(obj.project_name, 40),
            "project_id": obj.project_id,
            "type": obj.transfer_type,
            "name": clifuncs.trunc(obj.name, 40),
            "id": obj.id,
            "created": clifuncs.format_time(obj.created_at),
            "status": obj.status_message
        }

    def print_details(self, obj, args, out=sys.stdout):
        description = None
        if obj.description:
            description = obj.description

        data = [
            {"project_name": obj.project_name},
            {"project_id": obj.project_id},
            {"type": obj.transfer_type},
            {"name": obj.name},
            {"description": description},
            {"id": obj.id},
            {"uuid": obj.uuid},
            {"owner": obj.owner_id}
        ]
        # TODO: check for quotes around description
        for d in data:
            print(yaml.dump(d, width=70, indent=4), end='')

    def add_custom_options(self, parser):

        # for --create, set globus upload name
        parser.add_argument('--name', type=str, default="", help='New upload name, for use with --create. If not given, a random name will be generated.')

        # --finish: finish upload, adds uploaded files to the project
        parser.add_argument('--finish', action="store_true", default=False, help='Finish an upload. This closes the upload to new files and adds uploaded files to the project.')

        # --goto: go to Globus File Manager in web browser
        parser.add_argument('--goto', action="store_true", default=False, help='Open the Globus File Manager in a web browser.')

        # for --set and --unset, set/unset current globus upload id
        parser.add_argument('--set', action="store_true", default=False, help='Set the Globus upload used by `mc up --globus`.')
        parser.add_argument('--unset', action="store_true", default=False, help='Unset the Globus upload used by `mc up --globus`.')


    def finish(self, objects, args, out=sys.stdout):
        """Finish Globus upload, --finish"""
        proj = clifuncs.make_local_project()
        project_config = clifuncs.read_project_config(proj.local_path)
        globus_upload_id = project_config.globus_upload_id

        for obj in objects:
            try:
                proj.remote.finish_globus_upload_request(proj.id, obj.id)
            except mcapi.MCAPIError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
            if obj.id == globus_upload_id:
                set_current_globus_upload(proj.local_path, None)
                out.write("Finishing current Globus upload\n")

        return

    def goto(self, objects, args, out=sys.stdout):
        """Open Globus File Manager in a web browser"""

        proj = clifuncs.make_local_project()

        origin_id = cliglobus.get_local_endpoint_id()
        origin_path = proj.local_path

        for obj in objects:

            if obj.status_message != "Ready":
                out.write("Globus upload (name=" + obj.name + ", id=" + str(obj.id) + \
                    ") is not 'Ready'. Skipping...\n")
                continue

            destination_id = obj.globus_endpoint_id
            destination_path = obj.globus_path

            url = "https://app.globus.org/file-manager" \
                + "?destination_id=" + destination_id \
                + "&destination_path=" + destination_path \
                + "&origin_id=" + origin_id \
                + "&origin_path=" + origin_path

            try:
                import webbrowser
                webbrowser.open(url)
            except:
                out.write("Could not open a web browser.")
                out.write("URL:", url)

        return

    def create(self, args, out=sys.stdout):
        """Create new globus upload

        Using:
            mc globus upload <upload_name> --create
            mc globus upload --name <upload_name> --create
        """
        proj = clifuncs.make_local_project()

        in_names = []
        if args.expr:
            in_names += args.expr
        if args.name:
            in_names += [args.name]
        if len(in_names) == 0:
            in_names = [clifuncs.random_name()]

        if len(in_names) != 1:
            print('create one Globus upload at a time')
            print('example: mc globus upload <name> --create')
            raise cliexcept.MCCLIException("Invalid globus request")

        resulting_objects = []
        for name in in_names:
            upload = proj.remote.create_globus_upload_request(proj.id, name)
            self.__update_upload_object(upload, proj)
            set_current_globus_upload(clifuncs.project_path(), upload)
            print('Created Globus upload:', upload.id)
            resulting_objects.append(upload)
        self.output(resulting_objects, args, out=out)
        return

    def delete(self, objects, args, dry_run, out=sys.stdout):
        """Delete globus uploads

        Using:
            mc globus upload --id <upload_id> --delete
            mc globus upload <upload_name_search> --delete
        """
        if dry_run:
            out.write('Dry-run is not yet possible when deleting Globus uploads.\n')
            raise cliexcept.MCCLIException("Invalid globus request")

        proj = clifuncs.make_local_project()
        project_config = clifuncs.read_project_config(proj.local_path)
        globus_upload_id = project_config.globus_upload_id

        for obj in objects:
            try:
                proj.remote.delete_globus_upload_request(proj.id, obj.id)
            except mcapi.MCAPIError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
            if obj.id == globus_upload_id:
                set_current_globus_upload(proj.local_path, None)
                out.write("Deleted current Globus upload\n")

        return

    def set(self, objects, args, out=sys.stdout):
        if len(objects) != 1:
            out.write('set one current Globus upload at a time\n')
            out.write('example: mc globus upload --set <name>\n')
            raise cliexcept.MCCLIException("Invalid globus request")

        for upload in objects:
            set_current_globus_upload(clifuncs.project_path(), upload)
            out.write("Set current Globus upload: '" + upload.name + "'\n")
        return

    def unset(self, args, out=sys.stdout):
        set_current_globus_upload(clifuncs.project_path(), None)
        out.write("Unset Globus upload\n")
        return

def make_globus_download_parser():
    """Make argparse.ArgumentParser for `mc globus download`"""
    return GlobusDownloadTaskSubcommand().make_parser()

class GlobusDownloadTaskSubcommand(ListObjects):

    desc = """List, create, and delete Globus downloads. By default lists current project downloads. With `--all` lists all Globus downloads."""

    def __init__(self):
        super(GlobusDownloadTaskSubcommand, self).__init__(
            ["globus", "download"], "Download", "Downloads", desc=self.desc,
            requires_project=False, non_proj_member=True, proj_member=True, expt_member=False,
            remote_help='Remote to download files from',
            list_columns=['current', 'project_name', 'project_id', 'type', 'name', 'id',  'created', 'status'],
            headers=['', 'project_name', 'project_id', 'type', 'name', 'id', 'created', 'status'],
            deletable=True,
            creatable=True,
            custom_selection_actions=['goto'],
            request_confirmation_actions={
                'goto': 'You want to goto these downloads in a web browser?'
            }
        )

    def __update_download_object(self, download, proj):
        download.project_name = proj.name
        download.project_id = download._data["project_id"]
        status_code = download._data["status"]
        if status_code == 0:
            status_message = "Ready"    # user may download
        elif status_code == 1:
            status_message = "Working"  # creating links
        elif status_code == 3:
            status_message = "Waiting"  # not yet started
        else:
            status_message = "Unexpected status: " + str(status_code)
        download.status_code = status_code
        download.status_message = status_message
        download.transfer_type = "download"

    def get_all_from_project(self, proj):
        results = []
        downloads = proj.remote.get_all_globus_download_requests(proj.id)
        for download in downloads:
            self.__update_download_object(download, proj)
            results.append(download)
        return results

    def get_all_from_remote(self, remote):
        results = []
        projects = remote.get_all_projects()
        for project in projects:
            project.remote = remote
            results += self.get_all_from_project(project)
        return results

    def list_data(self, obj, args):
        _is_current = ' '
        pconfig = clifuncs.read_project_config()
        if pconfig and obj.id == pconfig.globus_download_id:
            _is_current = '*'
        return {
            "current": _is_current,
            "project_name": clifuncs.trunc(obj.project_name, 40),
            "project_id": obj.project_id,
            "type": obj.transfer_type,
            "name": clifuncs.trunc(obj.name, 40),
            "id": obj.id,
            "created": clifuncs.format_time(obj.created_at),
            "status": obj.status_message
        }

    def print_details(self, obj, args, out=sys.stdout):
        description = None
        if obj.description:
            description = obj.description

        data = [
            {"project_name": obj.project_name},
            {"project_id": obj.project_id},
            {"type": obj.project_id},
            {"name": obj.name},
            {"description": description},
            {"id": obj.id},
            {"uuid": obj.uuid},
            {"owner": obj.owner_id}
        ]
        # TODO: check for quotes around description
        for d in data:
            print(yaml.dump(d, width=70, indent=4), end='')

    def add_custom_options(self, parser):

        # for --create, set globus upload name
        parser.add_argument('--name', type=str, default="", help='New download name, for use with --create. If not given, a random name will be generated.')

        # --goto: go to Globus File Manager in web browser
        parser.add_argument('--goto', action="store_true", default=False, help='Open the Globus File Manager in a web browser.')

        # for --set and --unset, set/unset current globus download id
        parser.add_argument('--set', action="store_true", default=False, help='Set the Globus download used by `mc down --globus`.')
        parser.add_argument('--unset', action="store_true", default=False, help='Unset the Globus download used by `mc down --globus`.')


    def create(self, args, out=sys.stdout):
        """Create new globus download

        Using:
            mc globus download <download_name> --create
            mc globus download --name <download_name> --create
        """
        proj = clifuncs.make_local_project()

        in_names = []
        if args.expr:
            in_names += args.expr
        if args.name:
            in_names += [args.name]
        if len(in_names) == 0:
            in_names = [clifuncs.random_name()]

        if len(in_names) != 1:
            print('create one Globus download at a time')
            print('example: mc globus download <name> --create')
            raise cliexcept.MCCLIException("Invalid globus request")

        resulting_objects = []
        for name in in_names:
            download = proj.remote.create_globus_download_request(proj.id, name)
            self.__update_download_object(download, proj)
            set_current_globus_download(clifuncs.project_path(), download)
            print('Created Globus download:', download.id)
            resulting_objects.append(download)
        self.output(resulting_objects, args, out=out)
        return

    def delete(self, objects, args, dry_run, out=sys.stdout):
        """Delete globus downloads

        Using:
            mc globus download --id <download_id> --delete
            mc globus download <download_name_search> --delete
        """
        if dry_run:
            out.write('Dry-run is not yet possible when deleting Globus downloads.\n')
            raise cliexcept.MCCLIException("Invalid globus request")

        proj = clifuncs.make_local_project()
        project_config = clifuncs.read_project_config(proj.local_path)
        globus_download_id = project_config.globus_download_id

        for obj in objects:
            try:
                proj.remote.delete_globus_download_request(proj.id, obj.id)
            except mcapi.MCAPIError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
            if obj.id == globus_download_id:
                set_current_globus_download(proj.local_path, None)
                out.write("Deleted current Globus download\n")
        return

    def goto(self, objects, args, out=sys.stdout):
        """Open Globus File Manager in a web browser"""

        proj = clifuncs.make_local_project()
        destination_id = cliglobus.get_local_endpoint_id()
        destination_path = proj.local_path

        for obj in objects:

            if obj.status_message != "Ready":
                out.write("Globus download (name=" + obj.name + ", id=" + str(obj.id) + \
                    ") is not 'Ready'. Skipping...\n")
                continue

            origin_id = obj.globus_endpoint_id
            origin_path = obj.globus_path

            url = "https://app.globus.org/file-manager" \
                + "?destination_id=" + destination_id \
                + "&destination_path=" + destination_path \
                + "&origin_id=" + origin_id \
                + "&origin_path=" + origin_path

            try:
                import webbrowser
                webbrowser.open(url)
            except:
                out.write("Could not open a web browser.")
                out.write("URL:", url)

        return

    def set(self, objects, args, out=sys.stdout):
        if len(objects) != 1:
            out.write('set one current Globus download at a time\n')
            out.write('example: mc globus download --set <name>\n')
            raise cliexcept.MCCLIException("Invalid globus request")

        for download in objects:
            set_current_globus_download(clifuncs.project_path(), download)
            out.write("Set current Globus download: '" + download.name + "'\n")
        return

    def unset(self, args, out=sys.stdout):
        set_current_globus_download(clifuncs.project_path(), None)
        out.write("Unset Globus download\n")
        return


globus_interface_usage = [
    {'name': 'download', 'desc': 'Manage Globus downloads', 'subcommand': GlobusDownloadTaskSubcommand()},
    {'name': 'upload', 'desc': 'Manage Globus uploads', 'subcommand': GlobusUploadTaskSubcommand()}
]

def make_globus_parser():
    """Make argparse.ArgumentParser for `mc globus`"""
    usage_help = StringIO()
    usage_help.write("mc globus <transfertype> [<args>]\n\n")
    usage_help.write("The transfer types are:\n")

    for interface in globus_interface_usage:
        usage_help.write("  {:10} {:40}\n".format(interface['name'], interface['desc']))

    parser = argparse.ArgumentParser(
        description='Manage Globus transfers',
        usage=usage_help.getvalue())
    parser.add_argument('transfertype', nargs='?', default=None, help='Type of transfer to manage.')

    parser.add_argument('--set-globus-endpoint-id', type=str, help='Set local globus endpoint ID')
    parser.add_argument('--clear-globus-endpoint-id', action="store_true", default=False, help='Clear local globus endpoint ID')

    return parser

def globus_subcommand(argv):
    """
    Manage Globus uploads, downloads, and configuration.

    mc globus download [... download options ...]
    mc globus upload [... upload options ...]
    mc globus --set-globus-endpoint-id <id>
    mc globus --clear-globus-endpoint-id <id>
    mc globus
    """
    parser = make_globus_parser()
    if len(argv) < 1:
        endpoint_id = cliglobus.get_local_endpoint_id()
        print("Globus endpoint id:", endpoint_id)
        return

    # parse_args defaults to [1:] for args, but you need to
    # exclude the rest of the args too, or validation will fail
    args = parser.parse_args([argv[0]])

    if args.transfertype:

        globus_interfaces = {d['name']: d for d in globus_interface_usage}
        if args.transfertype in globus_interfaces:
            globus_interfaces[args.transfertype]['subcommand'](argv[1:])
        else:
            print('Unrecognized transfertype')
            parser.print_help()
            return

    elif args.set_globus_endpoint_id:
        config = Config()
        config.globus.endpoint_id = args.set_globus_endpoint_id
        config.save()

    elif args.clear_globus_endpoint_id:
        config = Config()
        config.globus.endpoint_id = None
        config.save()

    else:
        parser.print_help()
        return
