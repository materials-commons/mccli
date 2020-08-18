import os
import pytest
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.file_functions as filefuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile, isdir
from materials_commons.cli.functions import rmdir_if

from .cli_test_project import make_basic_project_1, test_project_directory

class TestMkdir(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        """Test the LocalTree"""
        project_name = "__clitest__mkdir"
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

    def test_mkdir_without_intermediate_directories(self):
        # make directory, no intermediate directories need to be created
        mcpath = "/example_dir"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), False)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")

        # attempt to make a directory that already exists
        mcpath = "/example_dir"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        existing_dir = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isdir(existing_dir), True)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), False)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")
        self.assertEqual(result.id, existing_dir.id)

        # attempt to make root directory
        mcpath = "/"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), True)
        existing_dir = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isdir(existing_dir), True)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), True)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "/")
        self.assertEqual(result.path, "/")
        self.assertEqual(result.id, existing_dir.id)

        # attempt make directory when intermediate directories need to be created but are not allowed
        mcpath = "/A/B/C/D"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        with pytest.raises(cliexcept.MCCLIException) as e:
            result = treefuncs.mkdir(
                self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        self.assertEqual(os.path.exists(local_abspath), False)

        # attempt make directory, which is an existing file locally & remotely
        mcpath = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath)
        with pytest.raises(cliexcept.MCCLIException) as e:
            result = treefuncs.mkdir(
                self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isfile(result), True)
        self.assertEqual(os.path.isfile(local_abspath), True)


    def test_mkdir_make_remote_and_make_local(self):
        # make directory, no intermediate directories need to be created
        mcpath = "/example_dir"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=False, create_intermediates=False, remotetree=None)
        self.assertEqual(os.path.isdir(local_abspath), True)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")

        # attempt to make a directory that already exists
        mcpath = "/example_dir"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), True)
        existing_dir = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isdir(existing_dir), True)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=False, create_intermediates=False, remotetree=None)
        self.assertEqual(os.path.isdir(local_abspath), True)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")
        self.assertEqual(result.id, existing_dir.id)

        # clean up
        rmdir_if(local_abspath)

        # attempt make directory when intermediate directories need to be created but are not allowed
        mcpath = "/A/B/C/D"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        with pytest.raises(cliexcept.MCCLIException) as e:
            result = treefuncs.mkdir(
                self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        self.assertEqual(os.path.exists(local_abspath), False)

        # attempt make directory, which is an existing file locally & remotely
        mcpath = "/file_A.txt"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.client.upload_file(self.proj.id, self.proj.root_dir.id, local_abspath)
        with pytest.raises(cliexcept.MCCLIException) as e:
            result = treefuncs.mkdir(
                self.proj, mcpath, remote_only=True, create_intermediates=False, remotetree=None)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isfile(result), True)
        self.assertEqual(os.path.isfile(local_abspath), True)


    def test_mkdir_with_intermediate_directories(self):
        # make directory, with intermediate directories that need to be created
        mcpath = "/A/B/C/D"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        result = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(result, None)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=True, create_intermediates=True, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), False)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "D")
        self.assertEqual(result.path, "/A/B/C/D")

        # attempt to make a directory that already exists
        mcpath = "/A/B/C/D"
        local_abspath = filefuncs.make_local_abspath(self.proj.local_path, mcpath)
        self.assertEqual(os.path.exists(local_abspath), False)
        existing_dir = filefuncs.get_by_path_if_exists(self.client, self.proj.id, mcpath)
        self.assertEqual(isdir(existing_dir), True)
        result = treefuncs.mkdir(
            self.proj, mcpath, remote_only=True, create_intermediates=True, remotetree=None)
        self.assertEqual(os.path.exists(local_abspath), False)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "D")
        self.assertEqual(result.path, "/A/B/C/D")
        self.assertEqual(result.id, existing_dir.id)
