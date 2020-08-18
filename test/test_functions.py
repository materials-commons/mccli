import os
import time
import unittest

import materials_commons.cli.functions as clifuncs
from materials_commons.cli.user_config import Config
from .cli_test_project import test_project_directory, rmdir_if, remove_hidden_project_files

class TestFunctions(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        pass

    def test_clone_project(self):
        # create and clone a project
        remote_config = Config().default_remote
        client = remote_config.make_client()
        created_proj = client.create_project("__clitest__make_local_project")
        cloned_proj = clifuncs.clone_project(remote_config, created_proj.id, test_project_directory())

        # check for .mc directory and files
        expected_local_path = os.path.join(test_project_directory(), "__clitest__make_local_project")
        self.assertEqual(cloned_proj.local_path, expected_local_path)
        self.assertEqual(os.path.exists(cloned_proj.local_path), True)
        self.assertEqual(os.path.exists(os.path.join(cloned_proj.local_path, ".mc")), True)
        self.assertEqual(os.path.exists(os.path.join(cloned_proj.local_path, ".mc", "config.json")), True)
        self.assertEqual(os.path.exists(os.path.join(cloned_proj.local_path, ".mc", "project.db")), True)
        self.assertEqual(cloned_proj.id, created_proj.id)

        # clean
        remove_hidden_project_files(cloned_proj.local_path)
        rmdir_if(cloned_proj.local_path)

    def test_make_local_project(self):
        # create and clone a project
        remote_config = Config().default_remote
        client = remote_config.make_client()

        # Set debug on to check API calls
        # client.set_debug_on()

        # print("Step 1")
        created_proj = client.create_project("__clitest__make_local_project")

        # print("Step 2")
        cloned_proj = clifuncs.clone_project(remote_config, created_proj.id, test_project_directory())

        # Construct mcapi.Project from local .mc/config.json.
        # The first time a "project.db" will be saved.
        # print("Step 3")
        local_proj = clifuncs.make_local_project(cloned_proj.local_path)
        self.assertEqual(local_proj.local_path, cloned_proj.local_path)
        self.assertEqual(local_proj.id, cloned_proj.id)
        self.assertEqual(local_proj.remote.base_url, client.base_url)

        # Set fetch lock -- avoid unnecessary API calls
        project_config = clifuncs.read_project_config(cloned_proj.local_path)
        project_config.remote_updatetime = time.time()
        project_config.save()

        # The second time, should make API call again
        # print("Step 4")
        local_proj_2 = clifuncs.make_local_project(cloned_proj.local_path)
        self.assertEqual(local_proj_2.local_path, cloned_proj.local_path)
        self.assertEqual(local_proj_2.id, cloned_proj.id)
        self.assertEqual(local_proj_2.remote.base_url, client.base_url)

        # The third time, should not make API call due to fetch lock
        # print("Step 5")
        local_proj_3 = clifuncs.make_local_project(cloned_proj.local_path)
        self.assertEqual(local_proj_3.local_path, cloned_proj.local_path)
        self.assertEqual(local_proj_3.id, cloned_proj.id)
        self.assertEqual(local_proj_3.remote.base_url, client.base_url)

        # Unset fetch lock -- force API calls to ensure latest data
        project_config = clifuncs.read_project_config(cloned_proj.local_path)
        project_config.remote_updatetime = time.time()
        project_config.save()

        # This time, should make API call again
        # print("Step 6")
        local_proj_4 = clifuncs.make_local_project(cloned_proj.local_path)
        self.assertEqual(local_proj_4.local_path, cloned_proj.local_path)
        self.assertEqual(local_proj_4.id, cloned_proj.id)
        self.assertEqual(local_proj_4.remote.base_url, client.base_url)

        # clean
        remove_hidden_project_files(cloned_proj.local_path)
        rmdir_if(cloned_proj.local_path)
