import argparse
import json
import os
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile, isdir
from materials_commons.cli.subcommands.dataset import DatasetSubcommand
from materials_commons.cli.subcommands.init import init_project
from materials_commons.cli.subcommands.ls import ls_subcommand
import materials_commons.cli.user_config as user_config

from .cli_test_functions import working_dir, captured_output, print_string_io
from .cli_test_project import make_basic_project_1, test_project_directory, make_file, remove_if, \
    upload_project_files

def is_equal(A, B):
    if not type(A) is type(B):
        return False
    return A.__dict__ == B.__dict__

class TestMCDataset(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        project_name = "__clitest__dataset"
        project_path = os.path.join(test_project_directory(), project_name)
        self.basic_project_1 = make_basic_project_1(project_path)

        # initialize a Materials Commons Client
        remote_config = user_config.Config().default_remote
        self.client = remote_config.make_client()

        # make sure test project does not already exist
        result = self.client.get_all_projects()
        for proj in result:
            if proj.name == project_name:
                self.client.delete_project(proj.id)

        # create a Materials Commons project
        self.proj = init_project(project_name, description="", prefix=test_project_directory(), remote_config=remote_config)
        self.assertEqual(self.proj.root_dir.name, "/")

    def tearDown(self):
        # clean up
        self.basic_project_1.clean_files()
        self.client.delete_project(self.proj.id)

    def test_parse_args(self):
        # exclude 'mc', 'dataset'
        testargs = []
        dataset_subcommand = DatasetSubcommand()
        args = dataset_subcommand.parse_args(testargs)
        self.assertEqual(isinstance(args, argparse.Namespace), True)
        self.assertEqual(len(args.expr), 0)

    def create_dataset(self, dataset_name):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = ['--create', dataset_name]
            dataset_subcommand = DatasetSubcommand()
            dataset_subcommand(testargs, self.proj.local_path)

    def delete_dataset(self, dataset_name):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = ['--delete', dataset_name, '--force']
            dataset_subcommand = DatasetSubcommand()
            dataset_subcommand(testargs, self.proj.local_path)

    def include_file_or_dir(self, file_or_dir, dataset_id):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = [file_or_dir, '--dataset', str(dataset_id), '--include']
            ls_subcommand(testargs, self.proj.local_path)

    def exclude_file_or_dir(self, file_or_dir, dataset_id):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = [file_or_dir, '--dataset', str(dataset_id), '--exclude']
            ls_subcommand(testargs, self.proj.local_path)

    def clear_file_or_dir(self, file_or_dir, dataset_id):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = [file_or_dir, '--dataset', str(dataset_id), '--clear']
            ls_subcommand(testargs, self.proj.local_path)

    def clone_dataset(self, dataset_name, new_dataset_name):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = ['--force', dataset_name, '--clone-as', new_dataset_name]
            dataset_subcommand = DatasetSubcommand()
            dataset_subcommand(testargs, self.proj.local_path)

    def print_dataset_details(self, dataset_name):
        with captured_output(wd=self.proj.local_path) as (sout, serr):
            # exclude 'mc', 'dataset'
            testargs = ['--details', dataset_name, '--file-selection']
            dataset_subcommand = DatasetSubcommand()
            dataset_subcommand(testargs, self.proj.local_path)
        print_string_io(sout)
        print_string_io(serr)

    def ls(self, file_or_dir, dataset_id):
        with working_dir(self.proj.local_path):
            # exclude 'mc', 'dataset'
            testargs = [file_or_dir, '--dataset', str(dataset_id)]
            ls_subcommand(testargs, self.proj.local_path)

    def test_create_delete_dataset(self):

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 0)

        dataset_name = "clitest_dataset"
        self.create_dataset(dataset_name)

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 1)

        self.delete_dataset(dataset_name)

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 0)

    def test_file_selection(self):

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 0)

        upload_project_files(self.proj, self.basic_project_1, self)

        # create dataset
        dataset_name = "clitest_dataset"
        self.create_dataset(dataset_name)

        # get dataset id
        all_datasets_by_name = {dataset.name: dataset for dataset in self.client.get_all_datasets(self.proj.id)}
        dataset_id = all_datasets_by_name[dataset_name].id

        # include file
        self.include_file_or_dir("file_A.txt", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print(file_selection)
        self.assertEqual(file_selection['include_files'], ['/file_A.txt'])
        self.assertEqual(file_selection['include_dirs'], [])
        self.assertEqual(file_selection['exclude_files'], [])
        self.assertEqual(file_selection['exclude_dirs'], [])

        # clear file
        self.clear_file_or_dir("file_A.txt", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print(file_selection)
        self.assertEqual(file_selection['include_files'], [])
        self.assertEqual(file_selection['include_dirs'], [])
        self.assertEqual(file_selection['exclude_files'], [])
        self.assertEqual(file_selection['exclude_dirs'], [])

        # include directory
        self.include_file_or_dir("level_1", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print(file_selection)
        self.assertEqual(file_selection['include_files'], [])
        self.assertEqual(file_selection['include_dirs'], ["/level_1"])
        self.assertEqual(file_selection['exclude_files'], [])
        self.assertEqual(file_selection['exclude_dirs'], [])

        # exclude file
        self.exclude_file_or_dir("level_1/file_B.txt", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print(file_selection)
        self.assertEqual(file_selection['include_files'], [])
        self.assertEqual(file_selection['include_dirs'], ["/level_1"])
        self.assertEqual(file_selection['exclude_files'], ["/level_1/file_B.txt"])
        self.assertEqual(file_selection['exclude_dirs'], [])

        # exclude directory
        self.exclude_file_or_dir("level_1/level_2", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print(file_selection)
        self.assertEqual(file_selection['include_files'], [])
        self.assertEqual(file_selection['include_dirs'], ["/level_1"])
        self.assertEqual(file_selection['exclude_files'], ["/level_1/file_B.txt"])
        self.assertEqual(file_selection['exclude_dirs'], ["/level_1/level_2"])

        # # print dataset details
        # self.print_dataset_details(dataset_name)
        # self.ls(".", dataset_id)
        # self.ls("level_1", dataset_id)
        # self.ls("level_1/level_2", dataset_id)

    def test_clone_dataset(self):

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 0)

        upload_project_files(self.proj, self.basic_project_1, self)

        # create dataset
        dataset_name = "clitest_clone_dataset_1"
        self.create_dataset(dataset_name)

        # get dataset id
        all_datasets_by_name = {dataset.name: dataset for dataset in self.client.get_all_datasets(self.proj.id)}
        dataset_id = all_datasets_by_name[dataset_name].id

        # include file
        self.include_file_or_dir("file_A.txt", dataset_id)
        file_selection = self.client.get_dataset(self.proj.id, dataset_id).file_selection
        print("original file selection:", file_selection)
        self.assertEqual(file_selection['include_files'], ['/file_A.txt'])
        self.assertEqual(file_selection['include_dirs'], [])
        self.assertEqual(file_selection['exclude_files'], [])
        self.assertEqual(file_selection['exclude_dirs'], [])

        # create cloned dataset
        cloned_dataset_name = "clitest_clone_dataset_2"
        self.clone_dataset(dataset_name, cloned_dataset_name)

        # get dataset id
        all_datasets_by_name = {dataset.name: dataset for dataset in self.client.get_all_datasets(self.proj.id)}
        cloned_dataset_id = all_datasets_by_name[cloned_dataset_name].id

        # check file selection
        cloned_file_selection = self.client.get_dataset(self.proj.id, cloned_dataset_id).file_selection
        print("cloned file selection:", file_selection)
        self.assertEqual(cloned_file_selection['include_files'], ['/file_A.txt'])
        self.assertEqual(cloned_file_selection['include_dirs'], [])
        self.assertEqual(cloned_file_selection['exclude_files'], [])
        self.assertEqual(cloned_file_selection['exclude_dirs'], [])


    def test_publish_dataset(self):

        all_datasets = self.client.get_all_datasets(self.proj.id)
        self.assertEqual(len(all_datasets), 0)

        upload_project_files(self.proj, self.basic_project_1, self)

        # create dataset
        dataset_name = "clitest_dataset"
        self.create_dataset(dataset_name)

        # get dataset id
        all_datasets_by_name = {dataset.name: dataset for dataset in self.client.get_all_datasets(self.proj.id)}
        dataset_id = all_datasets_by_name[dataset_name].id

        # include all file
        self.include_file_or_dir("file_A.txt", dataset_id)
