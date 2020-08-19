import json
import sys
import yaml

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
from materials_commons.cli.list_objects import ListObjects
from materials_commons.cli.exceptions import MCCLIException

def make_parser():
    """Make argparse.ArgumentParser for `mc dataset`"""
    return DatasetSubcommand().make_parser()

class DatasetSubcommand(ListObjects):

    desc = """List, create, publish, and download datasets. By default lists all public datasets at remote. With `--proj` lists private and public datasets for the current project."""

    def __init__(self):
        super(DatasetSubcommand, self).__init__(
            ["dataset"], "Dataset", "Datasets", desc=self.desc,
            requires_project=False, non_proj_member=True, proj_member=True, expt_member=False,
            remote_help='Remote to get datasets from',
            list_columns=['name', 'owner', 'id', 'uuid', 'updated_at', 'zipfile_size', 'published_at'],
            deletable=True,
            creatable=True,
            custom_selection_actions=['down', 'unpublish', 'publish', 'publish_private', 'clone'],
            request_confirmation_actions={
                'publish': 'Are you sure you want to publicly publish these datasets?',
                'unpublish': 'Are you sure you want to unpublish these datasets?',
                'publish_private': 'Are you sure you want to privately publish these datasets?',
                'clone': 'Are you sure you want to clone this dataset?'
            }
        )

    # TODO: double check this is no longer valid
    # def get_all_from_experiment(self, expt):
    #     # return mcapi.get_all_datasets_from_experiment(expt.project.id, expt.id, expt.project.remote)
    #     return []

    def get_all_from_project(self, proj):
        # # basic call, # TODO: return owner email in dataset data
        # return proj.remote.get_all_datasets(proj.id)

        # add owner to project
        users = proj.remote.list_users()
        users_by_id = {u.id:u for u in users}
        datasets = proj.remote.get_all_datasets(proj.id)
        for d in datasets:
            d.owner = users_by_id[d.owner_id]
        return datasets

    def get_all_from_remote(self, remote):
        users = remote.list_users()
        users_by_id = {u.id:u for u in users}
        datasets = remote.get_all_published_datasets()
        for d in datasets:
            d.owner = users_by_id[d.owner_id]
        return datasets

    def list_data(self, obj):

        zipfile_size = '-'
        if obj.zipfile_size:
            zipfile_size = clifuncs.humanize(obj.zipfile_size)

        published_at = '-'
        if obj.published_at:
            published_at = clifuncs.format_time(obj.published_at)

        return {
            'name': clifuncs.trunc(clifuncs.getit(obj, 'name', '-'), 40),
            'owner': clifuncs.trunc(obj.owner.email, 40),
            'id': clifuncs.trunc(clifuncs.getit(obj, 'id', '-'), 40),
            'uuid': clifuncs.trunc(clifuncs.getit(obj, 'uuid', '-'), 40),
            'updated_at': clifuncs.format_time(clifuncs.getit(obj, 'updated_at', '-')),
            'zipfile_size': zipfile_size,
            'published_at': published_at
        }

    def print_details(self, obj, out=sys.stdout):
        description = None
        if obj.description:
            description = obj.description

        zipfile_size = None
        if obj.zipfile_size:
            zipfile_size = clifuncs.humanize(obj.zipfile_size)

        published_at = None
        if obj.published_at:
            published_at = clifuncs.format_time(obj.published_at)

        data = [
            {"name": obj.name},
            # TODO: authors, summary
            {"owner.name": obj.owner.name},
            {"owner.email": obj.owner.email},
            {"published_at": published_at},
            {"description": description},
            {"id": obj.id},
            {"uuid": obj.uuid},
            {"zipfile_size": zipfile_size}
        ]
        # TODO: why quotes around description here, but not in `mc proj`?
        for d in data:
            print(yaml.dump(d, width=70, indent=4), end='')

    def add_custom_options(self, parser):

        # note: add samples via `mc samp`, processes via `mc proc`, files via `mc ls`

        # for --create and --clone, set dataset name, description
        parser.add_argument('--desc', type=str, default="", help='Dataset description, for use with --create or --clone.')
        parser.add_argument('--name', type=str, default="", help='New dataset name / title, for use with --create or --clone.')

        # --clone
        parser.add_argument('--clone', action="store_true", default=False, help='Clone the selected dataset. Only allowed with --proj and only for a single dataset.')
        parser.add_argument('--refresh-processes', action="store_true", default=False, help='For use with --clone. If provided, the new dataset will be constructed such that the samples in the original dataset determine which processes are included in the new dataset.')

        # --down
        parser.add_argument('--down', action="store_true", default=False, help='Download dataset zipfile')

        # --publish, --publish-private, --unpublish
        parser.add_argument('--unpublish', action="store_true", default=False, help='Unpublish a dataset')
        parser.add_argument('--publish', action="store_true", default=False, help='Publish a public dataset. Makes it available for public download.')
        parser.add_argument('--publish-private', action="store_true", default=False, help='Publish a private dataset. Makes it available for globus download by project collaborators.')



    def down(self, objects, args, out=sys.stdout):
        """Download dataset zipfile, --down

        .. note:: The downloaded dataset is named <dataset_id>.zip
        """
        proj = clifuncs.make_local_project()
        for obj in objects:
            print(type(obj), obj)
            # out.write("Name: " + obj.name + "\n")
            # out.write("ID: " + obj.id + "\n")
            self.print_details(obj, out=out)
            out.write("Downloading...\n")
            dataset_id = obj.id
            to = obj.uuid + ".zip"
            proj.remote.download_dataset_zipfile(dataset_id, to)
            out.write("DONE\n\n")
        return

    def create(self, args, out=sys.stdout):
        """Create new dataset

        Using:
            mc dataset <dataset_name> [--desc <dataset description>] --create
            mc dataset --name <dataset_name> [--desc <dataset description>] --create
        """
        proj = clifuncs.make_local_project()

        in_names = []
        if args.expr:
            in_names += args.expr
        if args.name:
            in_names += [args.name]

        if len(in_names) != 1:
            print('create one dataset at a time')
            print('example: mc dataset DatasetName --create --desc "dataset description"')
            parser.print_help()
            exit(1)

        resulting_objects = []
        for name in in_names:
            dataset = mcapi.create_dataset(proj.id, name, description=args.desc, remote=proj.remote)
            print('Created dataset:', dataset.id)
            resulting_objects.append(dataset)
        self.output(resulting_objects, args, out=out)
        return

    def delete(self, objects, args, dry_run, out=sys.stdout):
        """Delete datasets

        Using:
            mc dataset --id <dataset_id> --proj --delete
            mc dataset <dataset_name_search> --proj --delete
        """
        if dry_run:
            out.write('Dry-run is not yet possible when deleting datasets.\n')
            out.write('Aborting\n')
            return

        proj = clifuncs.make_local_project()

        for obj in objects:
            try:
                mcapi.delete_dataset(proj.id, obj.id, remote=proj.remote)
            except requests.exceptions.HTTPError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
        return

    def unpublish(self, objects, args, out=sys.stdout):
        """Unpublish dataset

        Using:
            mc dataset --id <dataset_id> --proj --unpublish
            mc dataset <dataset_name_search> --proj --unpublish
        """
        proj = clifuncs.make_local_project()

        resulting_objects = []
        for obj in objects:
            try:
                # current situation is private datasets get returned after unpublishing
                # but public datasets return nothing
                res = mcapi.unpublish_dataset(proj.id, obj.id, proj.remote)
                if not res:
                    res = mcapi.get_dataset(proj.id, obj.id, proj.remote)
                resulting_objects.append(res)
            except requests.exceptions.HTTPError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
        self.output(resulting_objects, args, out=out)
        return

    def publish(self, objects, args, out=sys.stdout):
        """Publish public dataset

        Using:
            mc dataset --id <dataset_id> --proj --publish
            mc dataset <dataset_name_search> --proj --publish
        """
        proj = clifuncs.make_local_project()

        resulting_objects = []
        for obj in objects:
            try:
                resulting_objects.append(mcapi.publish_dataset(proj.id, obj.id, proj.remote))
            except requests.exceptions.HTTPError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
        self.output(resulting_objects, args, out=out)
        return

    def publish_private(self, objects, args, out=sys.stdout):
        """Publish private dataset

        Using:
            mc dataset --id <dataset_id> --proj --publish-private
            mc dataset <dataset_name_search> --proj --publish-private
        """
        proj = clifuncs.make_local_project()

        resulting_objects = []
        for obj in objects:
            try:
                resulting_objects.append(mcapi.publish_private_dataset(proj.id, obj.id, proj.remote))
            except requests.exceptions.HTTPError as e:
                try:
                    print(e.response.json()["error"])
                except:
                    print("  FAILED, for unknown reason")
                return False
        self.output(resulting_objects, args, out=out)
        return

    def clone(self, objects, args, out=sys.stdout):
        """Create a dataset with the selected samples and file selection

        If the --refresh-processes option is included, then the new dataset will be constructed such that the samples in the original dataset determine which processes are included in the new dataset.

        If the --refresh-processes option is not included, then the new dataset will be constructed identically to the original dataset. The current backend does not currently support this option.

        """

        if len(objects) != 1 or not args.proj:
            print("--clone requires the --proj option and 1 and only 1 project dataset to be selected. Exiting...")
            exit(1)

        if not args.refresh_processes:
            print("--clone is currently only available with the --refresh-processes option.")
            exit(1)

        proj = clifuncs.make_local_project()

        orig = objects[0]

        dataset_name = orig.name
        if args.name:
            dataset_name = args.name

        dataset_desc = orig.description
        if args.desc:
            dataset_desc = args.desc

        if 'samples' not in orig.input_data:
            print("Error reading samples from original dataset data.")
            exit(1)
        sample_ids = [samp['id'] for samp in orig.input_data['samples']]

        # remove duplicates:
        sample_ids = list(set(sample_ids))

        if 'file_selection' not in orig.input_data:
            print("Error reading file_selection from original dataset data.")
            exit(1)
        file_selection = orig.input_data['file_selection']

        dataset = mcapi.create_dataset(proj.id, dataset_name, dataset_desc, sample_ids=sample_ids, file_selection=file_selection, remote=proj.remote)

        print('Created dataset:', dataset.id)
        self.output([dataset], args, out=out)

        return
