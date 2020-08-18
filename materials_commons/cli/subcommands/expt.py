import json
import sys
import materials_commons.api as mcapi
from .. import functions as clifuncs
from ..list_objects import ListObjects

def set_current_experiment(project_local_path, expt=None):
    pconfig = clifuncs.read_project_config(project_local_path)
    if expt is None:
        pconfig.experiment_id = None
        pconfig.experiment_uuid = None
    else:
        pconfig.experiment_id = expt.id
        pconfig.experiment_uuid = expt.uuid
    pconfig.save()

class ExptSubcommand(ListObjects):
    """
    List, create, delete, and modify experiments

    mc expt
    mc expt [-c] <exptname>

    """
    def __init__(self):
        super(ExptSubcommand, self).__init__(
            ["expt"], "Experiment", "Experiments",
            requires_project=True, proj_member=True, expt_member=False,
            list_columns=['current', 'name', 'description', 'owner', 'id', 'uuid', 'updated_at'],
            headers=['', 'name', 'description', 'owner', 'id', 'uuid', 'updated_at'],
            creatable=True,
            deletable=True,
            custom_actions=['unset'],
            custom_selection_actions=['set']
        )

    def get_all_from_experiment(self, expt):
        raise Exception("Experiments are not members of experiments")

    def get_all_from_project(self, proj):

        users = proj.remote.list_users()
        users_by_id = {u.id:u for u in users}
        all_project_experiments = proj.remote.get_all_experiments(proj.id)
        for expt in all_project_experiments:
            expt.project = proj
            expt.owner = users_by_id[expt.owner_id]
        return all_project_experiments

    def list_data(self, obj):

        _is_current = ' '
        pconfig = clifuncs.read_project_config()
        if pconfig and obj.id == pconfig.experiment_id:
            _is_current = '*'

        return {
            'current': _is_current,
            'name': clifuncs.trunc(obj.name, 40),
            'description': clifuncs.trunc(obj.description, 100),
            'owner': clifuncs.trunc(obj.owner.email, 40),
            'id': obj.id,
            'uuid': obj.uuid,
            'updated_at': clifuncs.format_time(obj.updated_at)
        }

    def print_details(self, obj, out=sys.stdout):
        description = None
        if obj.description:
            description = obj.description
        data = [
            {"name": obj.name},
            {"description": description},
            {"id": obj.id},
            {"uuid": obj.uuid},
            {"owner": obj.owner_id},
            {"updated_at": clifuncs.format_time(obj.updated_at)}
        ]
        for d in data:
            out.write(yaml.dump(d, width=70, indent=4), end='')
        out.write("\n")

    def create(self, args, out=sys.stdout):
        proj = clifuncs.make_local_project()
        expt_list = proj.remote.get_all_experiments(proj.id)
        expt_names = {e.name: e for e in expt_list}

        if len(args.expr) != 1:
            out.write('create one experiment at a time\n')
            out.write('example: mc expt ExptName --create --desc "short description"\n')
            parser.print_help(file=out)
            exit(1)
        for name in args.expr:
            if name not in expt_names:
                expt_request = mcapi.CreateExperimentRequest(description=args.desc)
                expt = proj.remote.create_experiment(proj.id, name, attrs=expt_request)
                out.write('Created experiment: ' + expt.name + "\n")
                set_current_experiment(proj.local_path, expt)
                out.write("Set current experiment: '" + expt.name + "'\n")
            else:
                out.write('experiment: \'' + name + '\' already exists\n')
        return

    def delete(self, objects, args, dry_run, out=sys.stdout):
        if dry_run:
            out.write('Dry-run is not possible when deleting experiments.\n')
            out.write('Aborting\n')
            return

        proj = clifuncs.make_local_project()
        project_config = clifuncs.read_project_config(proj.local_path)
        current_experiment_id = project_config.experiment_id

        for obj in objects:
            if obj.id == current_experiment_id:
                set_current_experiment(proj.local_path, None)
                out.write("Unset current experiment\n")
            proj.remote.delete_experiment(proj.id, obj.id)
            out.write('Deleted experiment: ' + obj.name + ' ' + str(obj.id) + '\n')
            # for key, val in obj.delete_tally.__dict__.items():
            #     out.write(str(key) + ' ' + str(val) + '\n')
            out.write('\n')

    def set(self, objects, args, out=sys.stdout):
        if len(objects) != 1:
            out.write('set one current experiment at a time\n')
            out.write('example: mc expt --set <name>\n')
            exit(1)

        for expt in objects:
            set_current_experiment(expt.project.local_path, expt)
            out.write("Set current experiment: '" + expt.name + "'\n")
        return

    def unset(self, args, out=sys.stdout):
        set_current_experiment(clifuncs.project_path(), None)
        out.write("Unset current experiment\n")
        return

    def add_custom_options(self, parser):

        # for --create, add experiment description
        parser.add_argument('--desc', type=str, default="", help='Experiment description')

        # custom action --set
        parser.add_argument('--set', action="store_true", default=False, help='Set current experiment')

        # custom action --unset
        parser.add_argument('--unset', action="store_true", default=False, help='Unset current experiment')
