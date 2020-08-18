import os
import pytest
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.file_functions as filefuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile
from materials_commons.cli.functions import make_file

from .cli_test_project import make_basic_project_1, test_project_directory, upload_project_files

class TestRm(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        """Test the LocalTree"""
        project_name = "__clitest__rm"
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

    def tearDown(self):
        # clean up
        self.basic_project_1.clean_files()
        self.client.delete_project(self.proj.id)

    def test_rm_remote_only(self):
        # upload file_A.txt and then rm it (remote only)
        mcpath = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isfile(result), True)

        # rm step (remote only)
        treefuncs.remove(self.proj, [mcpath], recursive=False, no_compare=False, remote_only=True,
            localtree=None, remotetree=None)
        self.assertEqual(os.path.isfile(local_abspath), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result is None, True)

        # clean up not necessary

    def test_rm_remote_and_local(self):
        # upload file_A.txt and then rm it (remote and local)
        mcpath = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isfile(result), True)

        # rm step (remote only)
        treefuncs.remove(self.proj, [mcpath], recursive=False, no_compare=False, remote_only=False,
            localtree=None, remotetree=None)
        self.assertEqual(os.path.isfile(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result is None, True)

        # clean up not necessary

    def test_rm_remote_and_local_compare(self):
        # upload file_A.txt and then rm it (remote and local)
        mcpath = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isfile(result), True)

        # modify local
        make_file(local_abspath, "this text is different")

        # rm step (does not remove because text is different)
        treefuncs.remove(self.proj, [mcpath], recursive=False, no_compare=False, remote_only=False,
            localtree=None, remotetree=None)
        self.assertEqual(os.path.isfile(local_abspath), True)
        self.assertEqual(result is None, False)

        # clean up not necessary

    def test_rm_recursive_remote_only(self):
        # upload basic_project_1 files (and assert they have been uploaded)
        upload_project_files(self.proj, self.basic_project_1, self)

        # rm step (remote only)
        treefuncs.remove(self.proj, ["/level_1"], recursive=True, no_compare=False, remote_only=True,
            localtree=None, remotetree=None)

        # check for local files (should all exist)
        for local_abspath, text in self.basic_project_1.files:
            self.assertEqual(os.path.isfile(local_abspath), True)

        # check for remote files (some should exist)
        should_exist = ["/file_A.txt", "/file_B.txt"]
        for local_abspath, text in self.basic_project_1.files:
            mcpath = filefuncs.make_mcpath(self.proj.local_path, local_abspath)
            result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
            self.assertEqual(result is None,  mcpath not in should_exist)

        # clean up not necessary

    def test_rm_recursive_remote_and_local(self):
        # upload basic_project_1 files (and assert they have been uploaded)
        upload_project_files(self.proj, self.basic_project_1, self)

        # rm step (remote only)
        treefuncs.remove(self.proj, ["/level_1"], recursive=True, no_compare=False, remote_only=False,
            localtree=None, remotetree=None)

        # check for files (some should exist locally and remotely)
        should_exist = ["/file_A.txt", "/file_B.txt"]
        for local_abspath, text in self.basic_project_1.files:
            mcpath = filefuncs.make_mcpath(self.proj.local_path, local_abspath)
            result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
            self.assertEqual(os.path.isfile(local_abspath), mcpath in should_exist)
            self.assertEqual(result is None,  mcpath not in should_exist)

        # clean up not necessary
