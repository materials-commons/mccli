import sys
import json
import yaml
from collections import OrderedDict

import materials_commons.api.models as models
import materials_commons.cli.tmp_functions as tmpfuncs
from materials_commons.cli.exceptions import MCCLIException
from materials_commons.cli.list_objects import ListObjects
from materials_commons.cli.functions import read_project_config, getit, trunc, format_time, \
    remove_hidden_project_files

def make_parser():
    """Make argparse.ArgumentParser for `mc ls`"""
    return ProjSubcommand().make_parser()

class ProjSubcommand(ListObjects):
    def __init__(self):
        super(ProjSubcommand, self).__init__(
            ["proj"], "Project", "Projects",
            requires_project=False, non_proj_member=True, proj_member=False, expt_member=False,
            remote_help='Remote to get projects from',
            list_columns=['current', 'name', 'owner', 'id', 'updated_at'],
            headers=['', 'name', 'owner', 'id', 'updated_at'],
            deletable=True,
            custom_selection_actions=['goto'],
            request_confirmation_actions={
                'goto': 'You want to goto these projects in a web browser?'
            }
        )

    def get_all_from_experiment(self, expt):
        raise MCCLIException("Projects are not members of experiments")

    def get_all_from_project(self, proj):
        raise MCCLIException("Projects are not members of projects")

    def get_all_from_remote(self, remote):
        # # basic call, # TODO: return owner email in project data
        # return remote.get_all_projects()

        # add owner to project
        projects = remote.get_all_projects()
        tmpfuncs.add_owner(remote, projects)
        return projects

    def list_data(self, obj, args):
        _is_current = ' '
        project_config = read_project_config(self.working_dir)
        if project_config and getit(obj, 'id') == project_config.project_id:
            _is_current = '*'

        return {
            'current': _is_current,
            'owner': trunc(obj.owner.email, 40),    # TODO: owner email
            'name': trunc(getit(obj, 'name'), 40),
            'id': getit(obj, 'id'),
            'updated_at': format_time(getit(obj, 'updated_at'))
        }

    def print_details(self, obj, args, out=sys.stdout):
        description = None
        if obj.description:
            description = obj.description
        data = [
            {"name": obj.name},
            {"description": description},
            {"id": obj.id},
            {"uuid": obj.uuid},
            {"owner": obj.owner_id}
        ]
        for d in data:
            out.write(yaml.dump(d, width=70, indent=4))

    def delete(self, objects, args, dry_run, out=sys.stdout):
        # TODO: this needs testing
        if dry_run:
            out.write('Dry-run is not yet possible when deleting projects.\n')
            out.write('Exiting\n')
            return
        remote = self.get_remote(args)
        for obj in objects:
            try:
                # params = {'project_id': clifuncs.getit(obj, 'id')}
                # result = clifuncs.post_v3("deleteProject", params, remote=remote)
                project_id = getit(obj, 'id')
                result = remote.delete_project(project_id)
                # TODO: check return value
                out.write('Deleted project: ' + obj.name + ' ' + str(obj.id) + '\n')
                out.write('Note that this only deletes the project remotely and does not delete any '
                    'local files.\n')
            except:
                out.write('Delete of project failed: ' + obj.name + ' ' + str(obj.id) + '\n')

    def add_custom_options(self, parser):

        # --goto: go to project in web browser
        parser.add_argument('--goto', action="store_true", default=False, help='Open selected projects in a web browser.')

    def goto(self, objects, args, out=sys.stdout):
        """Open selected projects in a web browser"""

        for obj in objects:

            url = "https://materialscommons.org/app/projects/" + str(obj.id)

            try:
                import webbrowser
                webbrowser.open(url)
            except:
                out.write("Could not open a web browser.")
                out.write("URL:", url)

        return
