import os
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile, isdir, \
    make_local_abspath

from .cli_test_project import make_basic_project_1, test_project_directory, \
    make_file, remove_if

class TestStandardUpload(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        """Test the LocalTree"""
        project_name = "__clitest__standard_upload"
        project_path = os.path.join(test_project_directory(), project_name)
        self.basic_project_1 = make_basic_project_1(project_path)
        self.working_dir = os.getcwd()

        # initialize a Materials Commons Client
        mcurl = os.environ.get("MC_API_URL")
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        self.client = mcapi.Client.login(email, password, base_url=mcurl)

        # make sure test project does not already exist
        result = self.client.get_all_projects()
        for proj in result:
            if proj.name == project_name:
                self.client.delete_project(proj.id)

        # create a Materials Commons project
        self.proj = self.client.create_project(project_name)
        self.proj.local_path = project_path
        self.proj.remote = self.client
        self.assertEqual(self.proj.root_dir.name, "/")

    def tearDown(self):
        # clean up
        self.basic_project_1.clean_files()
        self.client.delete_project(self.proj.id)

    def test_upload_without_intermediate_directories(self):
        # upload files, no intermediate directories need to be created
        mcpaths = ["/file_A.txt", "/file_B.txt"]
        paths = [make_local_abspath(self.proj.local_path, path) for path in mcpaths]
        print("self.proj.local_path:", self.proj.local_path)
        print("paths:", paths)
        files_data, errors_data = treefuncs.standard_upload(
            self.proj, paths, self.working_dir, recursive=False, limit=50,
            remotetree=None)
        self.assertEqual(len(files_data), len(paths))
        for path in paths:
            self.assertEqual(path in files_data, True)
            self.assertEqual(isfile(files_data[path]), True)
            self.assertEqual(files_data[path].name, os.path.basename(path))
            self.assertEqual(files_data[path].directory_id, self.proj.root_dir.id)
        self.assertEqual(len(errors_data), 0)

        for path in paths:
            self.assertEqual(path in files_data, True)
            self.assertEqual(isfile(files_data[path]), True)

        result = self.client.list_directory(self.proj.id, self.proj.root_dir.id)
        list_directory_data = {os.path.join(self.proj.root_dir.path, file.name):file for file in result}
        self.assertEqual(len(list_directory_data), len(paths))
        for path in mcpaths:
            self.assertEqual(path in list_directory_data, True)
            self.assertEqual(isfile(list_directory_data[path]), True)

    def test_upload_with_intermediate_directories(self):
        # upload files, creating intermediate directories
        mcpaths = ["/level_1/file_A.txt", "/level_1/level_2/file_B.txt"]
        paths = [make_local_abspath(self.proj.local_path, path) for path in mcpaths]
        files_data, errors_data = treefuncs.standard_upload(
            self.proj, paths, self.working_dir, recursive=False, limit=50,
            remotetree=None)
        self.assertEqual(len(files_data), len(paths))
        for path in paths:
            self.assertEqual(path in files_data, True)
            self.assertEqual(isfile(files_data[path]), True)
        self.assertEqual(len(errors_data), 0)

    def test_upload_recursively(self):
        # upload files, by uploading recursively from directory "/level_1"
        expected_mcpaths = [
            "/level_1/file_A.txt",
            "/level_1/file_B.txt",
            "/level_1/level_2/file_A.txt",
            "/level_1/level_2/file_B.txt"]
        mcpaths = ["/level_1"]
        expected_paths = [make_local_abspath(self.proj.local_path, path) for path in expected_mcpaths]
        paths = [make_local_abspath(self.proj.local_path, path) for path in mcpaths]
        files_data, errors_data = treefuncs.standard_upload(
            self.proj, paths, self.working_dir, recursive=True, limit=50,
            remotetree=None)
        self.assertEqual(len(files_data), len(expected_paths))
        for path in expected_paths:
            self.assertEqual(path in files_data, True)
            self.assertEqual(isfile(files_data[path]), True)
            self.assertEqual(files_data[path].name, os.path.basename(path))
        self.assertEqual(len(errors_data), 0)

    def test_upload_root_dir_recursively(self):
        # upload files, by uploading recursively from root directory "/"
        # uploading root dir, we expect that the ".mc" directory exists, but is skipped
        expected_mcpaths = [
            "/file_A.txt",
            "/file_B.txt",
            "/level_1/file_A.txt",
            "/level_1/file_B.txt",
            "/level_1/level_2/file_A.txt",
            "/level_1/level_2/file_B.txt"]
        mcpaths = ["/"]
        expected_paths = [make_local_abspath(self.proj.local_path, path) for path in expected_mcpaths]
        paths = [make_local_abspath(self.proj.local_path, path) for path in mcpaths]
        mc_dir_local_path = os.path.join(self.proj.local_path, ".mc")
        tmp_file_local_path = os.path.join(mc_dir_local_path, "tmp.txt")
        make_file(tmp_file_local_path, "this is tmp.txt")
        files_data, errors_data = treefuncs.standard_upload(
            self.proj, paths, self.working_dir, recursive=True, limit=50,
            remotetree=None)

        print("expected_paths:", expected_paths)
        print("files_data:", files_data)

        # check that .mc directory and contents are not uploaded
        self.assertEqual(os.path.exists(mc_dir_local_path), True)
        self.assertEqual(os.path.exists(tmp_file_local_path), True)
        result = self.client.list_directory(self.proj.id, self.proj.root_dir.id)
        for child in result:
            self.assertEqual(child.name != ".mc", True)

        # check expected uploads
        self.assertEqual(len(files_data), len(expected_paths))
        for path in expected_paths:
            self.assertEqual(path in files_data, True)
            self.assertEqual(isfile(files_data[path]), True)
            self.assertEqual(files_data[path].name, os.path.basename(path))
        self.assertEqual(len(errors_data), 0)

        # clean up
        remove_if(tmp_file_local_path)
