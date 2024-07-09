import json
import sys
import yaml

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tmp_functions as tmpfuncs
from materials_commons.cli.list_objects import ListObjects
from materials_commons.cli.exceptions import MCCLIException

def make_parser():
    """Make argparse.ArgumentParser for `mc dataset`"""
    return DatasetSubcommand().make_parser()

def _if_attr(obj, attrname, f):
    value = getattr(obj, attrname, None)
    if value is None:
        return None
    else:
        return f(value)

def _print_dataset_data(dataset, project_id=None):
    """Order dataset data for printing"""
    data = [
        {"name": dataset.name},
        {"authors": getattr(dataset, 'authors', None)},
        {"summary": getattr(dataset, 'summary', None)},
        {"license": getattr(dataset, 'license', None)},
        {"owner.name": dataset.owner.name},
        {"owner.email": dataset.owner.email},
        {"owner.id": dataset.owner.id},
        {"description": getattr(dataset, 'description', None)},
        {"tags": getattr(dataset, 'tags', None)},
        {"id": dataset.id},
        {"uuid": dataset.uuid},
        {"doi": getattr(dataset, 'doi', None)}
    ]

    # via --all (is published)

    if project_id is None:
        data += [
            {"published_at": _if_attr(dataset, 'published_at', clifuncs.format_time)},
            {"zipfile_size": _if_attr(dataset, 'zipfile_size', clifuncs.humanize)},
            {"files_count": dataset.files_count},
            {"activities_count": dataset.activities_count},
            {"entities_count": dataset.entities_count},
            {"workflows_count": dataset.workflows_count},
            {"comments_count": dataset.comments_count},
            {"goto": "https://materialscommons.org/public/datasets/" + str(dataset.id) + "/overview"},
            {"goto_globus": "https://materialscommons.org/public/datasets/" + str(dataset.id) + "/globus"}
        ]
    elif dataset.published_at is not None:
        # via project, is published
        data += [
            {"published_at": _if_attr(dataset, 'published_at', clifuncs.format_time)},
            {"zipfile_size": _if_attr(dataset, 'zipfile_size', clifuncs.humanize)},
            {"files_count": dataset.files_count},
            {"activities_count": dataset.activities_count},
            {"entities_count": dataset.entities_count},
            {"workflows_count": dataset.workflows_count},
            {"experiments_count": dataset.experiments_count},
            {"comments_count": dataset.comments_count},
            {"goto": "https://materialscommons.org/public/datasets/" + str(dataset.id) + "/overview"},
            {"goto_globus": "https://materialscommons.org/public/datasets/" + str(dataset.id) + "/globus"}
        ]
    elif dataset.published_at is None:
        # via project, is not published
        data += [
            {"workflows_count": dataset.workflows_count},
            {"experiments_count": dataset.experiments_count}
        ]
    return data

def print_dataset_details(client, project_id, dataset, file_selection=False, out=sys.stdout):

    data = _print_dataset_data(dataset, project_id=project_id)
    if file_selection:
        data.append({
            "file_selection": dataset.file_selection
        })
    for d in data:
        out.write(yaml.dump(d, width=70, indent=4))


def print_published_dataset_details(client, dataset, out=sys.stdout):

    data = _print_dataset_data(dataset)

    for d in data:
        out.write(yaml.dump(d, width=70, indent=4))

class DatasetSubcommand(ListObjects):

    desc = """List, create, publish, and download datasets. By default lists project datasets. With `--all` lists all public datasets."""

    def __init__(self):
        super(DatasetSubcommand, self).__init__(
            ["dataset"], "Dataset", "Datasets", desc=self.desc,
            requires_project=False, non_proj_member=True, proj_member=True, expt_member=False,
            remote_help='Remote to get datasets from',
            list_columns=['name', 'owner', 'id', 'updated_at', 'zipfile_size', 'published_at'],
            deletable=True,
            creatable=True,
            custom_selection_actions=['down', 'unpublish', 'publish', 'clone_as', 'goto', 'goto_globus'],
            request_confirmation_actions={
                'publish': 'Are you sure you want to publicly publish these datasets?',
                'unpublish': 'Are you sure you want to unpublish these datasets?',
                'clone_as': 'Are you sure you want to clone this dataset?',
                'goto': 'You want to goto these datasets in a web browser?',
                'goto_globus': 'You want to goto the globus manager for these datasets in a web browser?'
            }
        )

    def get_all_from_project(self, proj):
        # # basic call, # TODO: return owner email in dataset data
        # return proj.remote.get_all_datasets(proj.id)

        datasets = proj.remote.get_all_datasets(proj.id)
        tmpfuncs.add_owner(proj.remote, datasets)
        return datasets

    def get_all_from_remote(self, remote):
        datasets = remote.get_all_published_datasets()
        tmpfuncs.add_owner(remote, datasets)
        return datasets

    def list_data(self, obj, args):

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
            'updated_at': clifuncs.format_time(clifuncs.getit(obj, 'updated_at', '-')),
            'zipfile_size': zipfile_size,
            'published_at': published_at
        }

    def print_details(self, obj, args, out=sys.stdout):
        # TODO: fix this
        out.write("** WARNING: `mc dataset --details` is under development, some dataset attributes (for example: tags) may appear as 'null' even if they do exist. **\n")
        if args.all or not clifuncs.project_exists(self.working_dir):
            client = self.get_remote(args)
            if args.file_selection:
                out.write("** NOTE: --file-selection: Not available for public datasets **\n")
            print_published_dataset_details(client, obj, out=out)
        else:
            proj = clifuncs.make_local_project(self.working_dir)
            print_dataset_details(proj.remote, proj.id, obj, \
                file_selection=args.file_selection, out=out)

    def add_custom_options(self, parser):

        # note: add samples via `mc samp`, processes via `mc proc`, files via `mc ls`

        # for --create and --clone-as, set new dataset description
        parser.add_argument('--desc', type=str, default="", help='Dataset description, for use with --create or --clone-as.')

        # for --details, also print dataset file selection
        parser.add_argument('--file-selection',action="store_true", default=False, help='For use with -d,--details: also print dataset file selection.')
        # # for --details, also print dataset files list
        # parser.add_argument('--files', action="store_true", default=False, help='For use with -d,--details: also print dataset files list.')


        # --clone-as
        parser.add_argument('--clone-as', type=str, default="", help='Clone the selected dataset, creating a new dataset, with this name, with the selected dataset\'s file selection, samples, and processes.')

        # --down
        parser.add_argument('--down', action="store_true", default=False, help='Download dataset zipfile')

        # --publish, --unpublish
        parser.add_argument('--unpublish', action="store_true", default=False, help='Unpublish a dataset')
        parser.add_argument('--publish', action="store_true", default=False, help='Publish a public dataset. Makes it available for public download.')

        # --goto: go to datasets in web browser
        parser.add_argument('--goto', action="store_true", default=False, help='Open selected datasets in a web browser.')

        # --goto-globus: go to datasets globus manager in web browser
        parser.add_argument('--goto-globus', action="store_true", default=False, help='Open globus manager for selected datasets in a web browser.')

    def down(self, objects, args, out=sys.stdout):
        """Download dataset zipfile, --down

        .. note:: The downloaded dataset is named dataset.<dataset_uuid>.zip
        """

        if args.all or not clifuncs.project_exists(self.working_dir):
            remote = self.get_remote(args)
        else:
            proj = clifuncs.make_local_project(self.working_dir)
            remote = proj.remote
        for obj in objects:
            self.print_details(obj, args, out=out)
            out.write("Downloading...\n")
            dataset_id = obj.id
            to = "dataset." + obj.uuid + ".zip"
            remote.download_published_dataset_zipfile(dataset_id, to)
            out.write("DONE\n\n")
        return

    def create(self, args, out=sys.stdout):
        """Create new dataset

        Using:
            mc dataset --create [--desc <dataset description>] <dataset_name>
        """
        proj = clifuncs.make_local_project(self.working_dir)

        in_names = []
        if args.expr:
            in_names += args.expr

        if len(in_names) != 1:
            out.write('Creating a dataset requires one name argument\n')
            out.write('example: mc dataset --create --desc "dataset description" <dataset_name>\n')
            return

        resulting_objects = []
        for name in in_names:
            dataset_request = mcapi.CreateDatasetRequest()
            dataset_request.description = args.desc
            dataset = proj.remote.create_dataset(proj.id, name, dataset_request)
            tmpfuncs.add_owner(proj.remote, dataset)
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
            out.write('Exiting\n')
            return
        if args.all:
            out.write('--delete and --all may not be used together: Delete datasets via a project.\n')
            out.write('Exiting\n')
            return

        proj = clifuncs.make_local_project(self.working_dir)
        for obj in objects:
            if obj.published_at is not None:
                out.write('Published dataset (id={0}) may not be deleted. Skipping.\n'.format(obj.id))
                continue
            proj.remote.delete_dataset(proj.id, obj.id)
        return

    def unpublish(self, objects, args, out=sys.stdout):
        """Unpublish dataset

        Using:
            mc dataset --id <dataset_id> --proj --unpublish
            mc dataset <dataset_name_search> --proj --unpublish
        """
        if args.all:
            out.write('--unpublish and --all may not be used together: Unpublish datasets via a project.\n')
            out.write('Exiting\n')
            return

        proj = clifuncs.make_local_project(self.working_dir)

        resulting_objects = []
        for obj in objects:
            if obj.published_at is None:
                out.write('Dataset (id={0}) is not published. Skipping.\n'.format(obj.id))
                continue
            resulting_objects.append(proj.remote.unpublish_dataset(proj.id, obj.id))
        tmpfuncs.add_owner(proj.remote, resulting_objects)
        self.output(resulting_objects, args, out=out)
        return

    def publish(self, objects, args, out=sys.stdout):
        """Publish public dataset

        Using:
            mc dataset --id <dataset_id> --proj --publish
            mc dataset <dataset_name_search> --proj --publish
        """
        if args.all:
            out.write('--publish and --all may not be used together: Publish datasets via a project.\n')
            out.write('Exiting\n')
            return

        proj = clifuncs.make_local_project(self.working_dir)
        resulting_objects = []
        for obj in objects:
            if obj.published_at is not None:
                out.write('Dataset (id={0}) is already published. Skipping.\n'.format(obj.id))
                continue
            resulting_objects.append(proj.remote.publish_dataset(proj.id, obj.id))
        tmpfuncs.add_owner(proj.remote, resulting_objects)
        self.output(resulting_objects, args, out=out)
        return

    def clone_as(self, objects, args, out=sys.stdout):
        """Create a new dataset with the selected dataset's file selection, samples, and processes"""

        if len(objects) != 1:
            print('--clone-as requires that 1 and only 1 project dataset be selected.\n')
            out.write('Exiting\n')
            return
        if args.all:
            print('--clone-as and --all may not be used together: Clone datasets via a project.\n')
            out.write('Exiting\n')
            return

        proj = clifuncs.make_local_project(self.working_dir)

        # get original dataset with as much data as possible
        dataset = proj.remote.get_dataset(proj.id, objects[0].id)

        # copy original metadata    # TODO update this
        dataset_request = mcapi.CreateDatasetRequest()
        dataset_request.description = getattr(dataset, 'description', None)
        dataset_request.summary = getattr(dataset, 'summary', None)
        dataset_request.license = getattr(dataset, 'license', None)
        dataset_request.authors = getattr(dataset, 'authors', None)
        # self.experiments = experiments # TODO
        # self.communities = communities # TODO
        # dataset_request.tags = getattr(dataset, 'tags', None) # TODO

        # update metadata
        dataset_name = args.clone_as
        if args.desc:
            dataset_request.description = args.desc
        new_dataset = proj.remote.create_dataset(proj.id, dataset_name, dataset_request)
        out.write('Created dataset: {0}\n'.format(new_dataset.id))

        # clone file selection
        out.write('Cloning file selection...\n')
        proj.remote.change_dataset_file_selection(proj.id, new_dataset.id, dataset.file_selection)

        # clone samples, processes, workflow
        out.write('** WARNING: Cloning samples, processes, workflows not yet implemented **\n')

        # Complete
        out.write('Cloned dataset: (name={0}, id={1}) -> (name={2}, id={3})\n'.format(dataset.name, dataset.id, new_dataset.name, new_dataset.id))

        return

    def goto(self, objects, args, out=sys.stdout):
        """Open selected datasets in a web browser"""

        proj = None
        if not args.all:
            proj = clifuncs.make_local_project(self.working_dir)


        for obj in objects:

            url = "https://materialscommons.org"
            if args.all:
                url = url + "/public/datasets/" + str(obj.id) + "/overview"
            else:
                url = url + "/app/projects/" + str(proj.id) + "/datasets/" + str(obj.id) + "/overview"

            try:
                import webbrowser
                webbrowser.open(url)
            except:
                out.write("Could not open a web browser.")
                out.write("URL:", url)

        return

    def goto_globus(self, objects, args, out=sys.stdout):
        """Open globus manager for selected datasets in a web browser"""

        if not args.all:
            print('--goto-globus only works for published datasets (--all option is required).\n')
            out.write('Exiting\n')
            return

        for obj in objects:

            url = "https://materialscommons.org/public/datasets/" + str(obj.id) + "/globus"

            try:
                import webbrowser
                webbrowser.open(url)
            except:
                out.write("Could not open a web browser.")
                out.write("URL:", url)

        return
