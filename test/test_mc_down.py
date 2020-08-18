import os
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.tree_functions as treefuncs
import materials_commons.cli.file_functions as filefuncs
from materials_commons.cli.subcommands.down import standard_download

from .cli_test_project import make_basic_project_1, test_project_directory, remove_if, mkdir_if, \
    upload_project_files, remove_hidden_project_files


class TestStandardDownload(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        """Test the LocalTree"""
        project_name = "__clitest__standard_download"
        project_path = os.path.join(test_project_directory(), project_name)
        self.basic_project_1 = make_basic_project_1(project_path)

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

        # upload files
        upload_project_files(self.proj, self.basic_project_1, self)

        # remove local files
        self.basic_project_1.clean_files()

    def tearDown(self):
        # clean up
        self.basic_project_1.clean_files()
        self.client.delete_project(self.proj.id)

    def test_download_file(self):
        # download file, no intermediate directories need to be created
        path = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
        self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=None, recursive=False,
            no_compare=False, localtree=None, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), True)

    def test_download_file_in_directory(self):
        # download file, with intermediate directory that needs to be created
        path = "/level_1/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, path)
        self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=None, recursive=False,
            no_compare=False, localtree=None, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), True)

    def test_download_directory(self):
        # download directory, recursively
        path = "/level_1"
        expected_paths = [
            "/level_1/file_A.txt",
            "/level_1/file_B.txt",
            "/level_1/level_2/file_A.txt",
            "/level_1/level_2/file_B.txt"]
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=None, recursive=True,
            no_compare=False, localtree=None, remotetree=None)
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), True)

    def test_download_root(self):
        # download root directory, recursively
        path = "/"
        expected_paths = [
            "/file_A.txt",
            "/file_B.txt",
            "/level_1/file_A.txt",
            "/level_1/file_B.txt",
            "/level_1/level_2/file_A.txt",
            "/level_1/level_2/file_B.txt"]
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=None, recursive=True,
            no_compare=False, localtree=None, remotetree=None)
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), True)

    def test_download_compare(self):
        # download root directory, recursively
        path = "/"
        expected_paths = [
            "/file_A.txt",
            "/file_B.txt",
            "/level_1/file_A.txt",
            "/level_1/file_B.txt",
            "/level_1/level_2/file_A.txt",
            "/level_1/level_2/file_B.txt"]
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=None, recursive=True,
            no_compare=False, localtree=None, remotetree=None)
        for expected in expected_paths:
            local_abspath = filefuncs.make_local_abspath(self.proj.local_path, expected)
            self.assertEqual(os.path.exists(local_abspath), True)

        # second time, should not need to re-download
        success = standard_download(self.proj, path, force=False, output=None, recursive=True,
            no_compare=False, localtree=None, remotetree=None)

    def test_download_output(self):
        # download file to alternative location
        path = "/file_A.txt"
        output = "/file_A_new_name.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, output)
        self.assertEqual(os.path.exists(local_abspath), False)
        success = standard_download(self.proj, path, force=False, output=local_abspath, recursive=True,
            no_compare=False, localtree=None, remotetree=None)
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, output)
        self.assertEqual(os.path.exists(local_abspath), True)

        # clean up
        remove_if(local_abspath)
