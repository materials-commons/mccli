import os
import unittest

import materials_commons.api as mcapi

import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.file_functions import isfile, isdir

from .cli_test_project import make_basic_project_1, test_project_directory, remove_if, mkdir_if, \
    upload_project_files, remove_hidden_project_files

class TestTreeCompareNoCache(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        # construct a local project tree
        project_name = "__clitest__treecompare"
        project_path = os.path.join(test_project_directory(), project_name)
        mkdir_if(project_path)

        # initialize a Materials Commons Client
        mcurl = os.environ.get("MC_API_URL")
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        client = mcapi.Client.login(email, password, base_url=mcurl)

        # make sure test project does not already exist
        result = client.get_all_projects()
        for proj in result:
            if proj.name == project_name:
                client.delete_project(proj.id)

        # create a Materials Commons project
        proj = client.create_project(project_name)
        proj.local_path = project_path
        proj.remote = client
        self.assertEqual(proj.root_dir.name, "/")

        self.proj = proj
        self.localtree = None
        self.remotetree = None

    def tearDown(self):
        # clean up
        self.proj.remote.delete_project(self.proj.id)

    def test_empty_local_and_empty_remote(self):
        """Test the treecompare functions, without using LocalTree or RemoteTree"""

        ### empty local project, empty remote project

        # compare root dir (empty local project, empty remote project)
        mcpaths = ["/"]
        check_checksum = False
        files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
            self.proj, mcpaths, checksum=check_checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        self.assertEqual(isinstance(files_data, dict), True)
        self.assertEqual(len(files_data), 0)

        self.assertEqual(isinstance(dirs_data, dict), True)
        self.assertEqual(len(dirs_data), 1)
        self.assertEqual(dirs_data["/"]["l_type"], "directory")
        self.assertEqual(dirs_data["/"]["r_type"], "directory")

        self.assertEqual(isinstance(child_data, dict), True)
        self.assertEqual(len(child_data), 1)
        self.assertEqual(len(child_data["/"]), 0)

        self.assertEqual(isinstance(not_existing, list), True)
        self.assertEqual(len(not_existing), 0)

        # check for non-existing file
        mcpaths = ["/fake"]
        check_checksum = False
        files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
            self.proj, mcpaths, checksum=check_checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        self.assertEqual(isinstance(files_data, dict), True)
        self.assertEqual(len(files_data), 0)

        self.assertEqual(isinstance(dirs_data, dict), True)
        self.assertEqual(len(dirs_data), 0)

        self.assertEqual(isinstance(child_data, dict), True)
        self.assertEqual(len(child_data), 0)

        self.assertEqual(isinstance(not_existing, list), True)
        self.assertEqual(len(not_existing), 1)
        self.assertEqual(not_existing[0], "/fake")


    def test_filled_local_and_empty_remote(self):

        # make local project files
        basic_project_1 = make_basic_project_1(self.proj.local_path)

        # compare root dir
        mcpaths = ["/"]
        check_checksum = False
        files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
            self.proj, mcpaths, checksum=check_checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        self.assertEqual(isinstance(files_data, dict), True)
        self.assertEqual(len(files_data), 0)

        self.assertEqual(isinstance(dirs_data, dict), True)
        self.assertEqual(len(dirs_data), 1)
        self.assertEqual(dirs_data["/"]["l_type"], "directory")
        self.assertEqual(dirs_data["/"]["r_type"], "directory")

        self.assertEqual(isinstance(child_data, dict), True)
        self.assertEqual(len(child_data), 1)
        self.assertEqual(len(child_data["/"]), 4)
        self.assertEqual("/file_A.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_A.txt"]["r_type"], None)
        self.assertEqual(child_data["/"]["/file_A.txt"]["l_type"], "file")
        self.assertEqual("/file_B.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_B.txt"]["r_type"], None)
        self.assertEqual(child_data["/"]["/file_B.txt"]["l_type"], "file")
        self.assertEqual("/level_1" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/level_1"]["r_type"], None)
        self.assertEqual(child_data["/"]["/level_1"]["l_type"], "directory")
        self.assertEqual("/.mc" in child_data["/"], True)   # should this be included?

        self.assertEqual(isinstance(not_existing, list), True)
        self.assertEqual(len(not_existing), 0)

        # clean up
        basic_project_1.clean_files()


    def test_filled_local_and_filled_remote(self):

        # make local project files
        basic_project_1 = make_basic_project_1(self.proj.local_path)

        # create directories and upload files
        upload_project_files(self.proj, basic_project_1, self)

        # compare root dir
        mcpaths = ["/"]
        check_checksum = False
        files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
            self.proj, mcpaths, checksum=check_checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        self.assertEqual(isinstance(files_data, dict), True)
        self.assertEqual(len(files_data), 0)

        self.assertEqual(isinstance(dirs_data, dict), True)
        self.assertEqual(len(dirs_data), 1)
        self.assertEqual(dirs_data["/"]["l_type"], "directory")
        self.assertEqual(dirs_data["/"]["r_type"], "directory")

        self.assertEqual(isinstance(child_data, dict), True)
        self.assertEqual(len(child_data), 1)
        self.assertEqual(len(child_data["/"]), 4)
        self.assertEqual("/file_A.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_A.txt"]["r_type"], "file")
        self.assertEqual(child_data["/"]["/file_A.txt"]["l_type"], "file")
        self.assertEqual("/file_B.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_B.txt"]["r_type"], "file")
        self.assertEqual(child_data["/"]["/file_B.txt"]["l_type"], "file")
        self.assertEqual("/level_1" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/level_1"]["r_type"], "directory")
        self.assertEqual(child_data["/"]["/level_1"]["l_type"], "directory")
        self.assertEqual("/.mc" in child_data["/"], True)   # should this be included?

        self.assertEqual(isinstance(not_existing, list), True)
        self.assertEqual(len(not_existing), 0)

        # clean up
        basic_project_1.clean_files()


    def test_empty_local_and_filled_remote(self):

        # make local project files
        basic_project_1 = make_basic_project_1(self.proj.local_path)

        # create directories and upload files
        upload_project_files(self.proj, basic_project_1, self)

        # remove local project files
        basic_project_1.remove_test_files()

        # compare root dir
        mcpaths = ["/"]
        check_checksum = False
        files_data, dirs_data, child_data, not_existing = treefuncs.treecompare(
            self.proj, mcpaths, checksum=check_checksum,
            localtree=self.localtree, remotetree=self.remotetree)

        self.assertEqual(isinstance(files_data, dict), True)
        self.assertEqual(len(files_data), 0)

        self.assertEqual(isinstance(dirs_data, dict), True)
        self.assertEqual(len(dirs_data), 1)
        self.assertEqual(dirs_data["/"]["l_type"], "directory")
        self.assertEqual(dirs_data["/"]["r_type"], "directory")

        self.assertEqual(isinstance(child_data, dict), True)
        self.assertEqual(len(child_data), 1)
        self.assertEqual(len(child_data["/"]), 4)
        self.assertEqual("/file_A.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_A.txt"]["r_type"], "file")
        self.assertEqual(child_data["/"]["/file_A.txt"]["l_type"], None)
        self.assertEqual("/file_B.txt" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/file_B.txt"]["r_type"], "file")
        self.assertEqual(child_data["/"]["/file_B.txt"]["l_type"], None)
        self.assertEqual("/level_1" in child_data["/"], True)
        self.assertEqual(child_data["/"]["/level_1"]["r_type"], "directory")
        self.assertEqual(child_data["/"]["/level_1"]["l_type"], None)
        self.assertEqual("/.mc" in child_data["/"], True)   # should this be included?

        self.assertEqual(isinstance(not_existing, list), True)
        self.assertEqual(len(not_existing), 0)

        # clean up
        remove_hidden_project_files(basic_project_1.path)
