import os
import pytest
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.file_functions as filefuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile, isdir
from materials_commons.cli.functions import remove_if, rmdir_if, mkdir_if

from .cli_test_project import make_basic_project_1, test_project_directory

class TestMv(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        """Test the LocalTree"""
        project_name = "__clitest__mv"
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

    def test_mv_rename_remote_only(self):
        # upload file_A.txt and then mv-rename it (remote only)
        mcpath_before = "/file_A.txt"
        mcpath_after = "/file_A_new_name.txt"
        local_abspath_before = filefuncs.make_local_abspath(self.proj.local_path, mcpath_before)
        local_abspath_after = filefuncs.make_local_abspath(self.proj.local_path, mcpath_after)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath_before)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)

        # move step
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=True, localtree=None, remotetree=None)
        self.assertEqual(os.path.isfile(local_abspath_before), True)
        self.assertEqual(os.path.isfile(local_abspath_after), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(result is None, True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(isfile(result), True)

        # clean up not necessary

    def test_mv_move_remote_only(self):
        # upload file_A.txt and then mv-rename it (remote only)
        mcpath_before = "/file_A.txt"
        mcpath_after = "/example_dir/file_A.txt"
        local_abspath_before = filefuncs.make_local_abspath(self.proj.local_path, mcpath_before)
        local_abspath_after = filefuncs.make_local_abspath(self.proj.local_path, mcpath_after)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath_before)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)

        # move step (fail, due to missing remote destination /example_dir)
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=True, localtree=None, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)
        self.assertEqual(os.path.isfile(local_abspath_before), True)
        self.assertEqual(os.path.exists(local_abspath_after), False)

        # create destination directory remotely
        result = self.client.create_directory(self.proj.id, "example_dir", self.proj.root_dir.id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.path, "/example_dir")

        # move step (succeed, despite missing local destination /example_dir because remote_only=True)
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=True, localtree=None, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(result is None, True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(isfile(result), True)

        # but locally, no changes occur
        self.assertEqual(os.path.isfile(local_abspath_before), True)
        self.assertEqual(os.path.exists(local_abspath_after), False)


        # clean up not necessary

    def test_mv_rename(self):
        # upload file_A.txt and then mv-rename it (remote and local)
        mcpath_before = "/file_A.txt"
        mcpath_after = "/file_A_new_name.txt"
        local_abspath_before = filefuncs.make_local_abspath(self.proj.local_path, mcpath_before)
        local_abspath_after = filefuncs.make_local_abspath(self.proj.local_path, mcpath_after)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath_before)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)

        # move step
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=False, localtree=None, remotetree=None)
        self.assertEqual(os.path.isfile(local_abspath_before), False)
        self.assertEqual(os.path.isfile(local_abspath_after), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(result is None, True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(isfile(result), True)

        # clean up
        remove_if(local_abspath_after)

    def test_mv_move(self):
        # upload file_A.txt and then move it (remote and local)
        mcpath_before = "/file_A.txt"
        mcpath_after = "/example_dir/file_A.txt"
        local_abspath_before = filefuncs.make_local_abspath(self.proj.local_path, mcpath_before)
        local_abspath_after = filefuncs.make_local_abspath(self.proj.local_path, mcpath_after)

        # upload step
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath_before)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)

        # move step (fail, due to missing local and remote destination /example_dir)
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=False, localtree=None, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)
        self.assertEqual(os.path.isfile(local_abspath_before), True)
        self.assertEqual(os.path.exists(local_abspath_after), False)

        # create destination directory remotely
        result = self.client.create_directory(self.proj.id, "example_dir", self.proj.root_dir.id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.path, "/example_dir")

        # move step (fail, due to missing local destination /example_dir)
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=False, localtree=None, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(isfile(result), True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(result is None, True)
        self.assertEqual(os.path.isfile(local_abspath_before), True)
        self.assertEqual(os.path.exists(local_abspath_after), False)

        # create destination directory locally
        mkdir_if(os.path.dirname(local_abspath_after))
        self.assertEqual(os.path.isdir(os.path.dirname(local_abspath_after)), True)

        # move step (success)
        treefuncs.move(self.proj, [mcpath_before, mcpath_after],
            remote_only=False, localtree=None, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_before)
        self.assertEqual(result is None, True)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath_after)
        self.assertEqual(isfile(result), True)
        self.assertEqual(os.path.exists(local_abspath_before), False)
        self.assertEqual(os.path.isfile(local_abspath_after), True)

        # clean up
        remove_if(local_abspath_after)
        rmdir_if(os.path.dirname(local_abspath_after))
