import json
import os
import pytest
import time
import unittest


import materials_commons.api as mcapi

# The purpose of this test to check expected server and api behavior independently of cli code
# Restrict use of CLI functions to configuration and only very basic functions
from materials_commons.cli.file_functions import isfile, isdir
from materials_commons.cli.functions import checksum

from .cli_test_project import make_basic_project_1, test_project_directory, remove_if

class TestAPI(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass
        # from materials_commons.cli.user_config import Config
        # client = Config().default_remote.make_client()
        # result = client.get_all_projects()
        # for proj in result:
        #     try:
        #         client.delete_project(proj.id)
        #     except:
        #         pass

    def test_project_api(self):
        """Test the basic API project commands independently of the CLI"""
        # test "get_all_projects"
        mcurl = os.environ.get("MC_API_URL")
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        client = mcapi.Client.login(email, password, base_url=mcurl)

        print("email:", email)
        print("client.base_url:", client.base_url)
        print("client.token:", client.apikey)

        project_names = [
            "__clitest__test_project_api_1",
            "__clitest__test_project_api_2",
            "__clitest__test_project_api_3"]

        # make sure test projects do not already exist
        result = client.get_all_projects()
        for proj in result:
            print("proj.name:", proj.name)
            if proj.name in project_names:
                client.delete_project(proj.id)

        # get currently existing projects
        result = client.get_all_projects()
        init_project_ids = [proj.id for proj in result]
        n_projects_init = len(init_project_ids)
        self.assertEqual(len(result) - n_projects_init, 0)

        # test "create_project"
        for name in project_names:
            client.create_project(name)
        result = client.get_all_projects()
        self.assertEqual(len(result) - n_projects_init, 3)

        # test "get_project"
        for proj in result:
            tmp_proj = client.get_project(proj.id)
            self.assertEqual(isinstance(tmp_proj, mcapi.Project), True)
            self.assertEqual(tmp_proj._data, proj._data)

        # test "update_project"
        for proj in result:
            if proj.name in project_names:
                update_request = mcapi.UpdateProjectRequest(
                    proj.name,
                    description="<new description>")
                tmp_proj = client.update_project(proj.id, update_request)
                self.assertEqual(tmp_proj.description, "<new description>")
                break

        # test "delete_project"
        for proj in result:
            if proj.name in project_names:
                client.delete_project(proj.id)

        result = client.get_all_projects()
        self.assertEqual(len(result) - n_projects_init, 0)

    def test_experiment_api(self):
        """Test the basic API experiment commands independently of the CLI"""
        # test "get_all_projects"
        mcurl = os.environ.get("MC_API_URL")
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        client = mcapi.Client.login(email, password, base_url=mcurl)

        # make sure test project does not already exist
        result = client.get_all_projects()
        for proj in result:
            if proj.name == "__clitest__experiment_api":
                client.delete_project(proj.id)

        proj = client.create_project("__clitest__experiment_api")
        experiment_request = mcapi.CreateExperimentRequest(description="<experiment description>")
        experiment_1 = client.create_experiment(proj.id, "<experiment name 1>", experiment_request)
        experiment_2 = client.create_experiment(proj.id, "<experiment name 2>", experiment_request)
        experiment_3 = client.create_experiment(proj.id, "<experiment name 3>", experiment_request)
        self.assertEqual(experiment_1.name, "<experiment name 1>")
        self.assertEqual(experiment_1.description, "<experiment description>")

        all_experiments = client.get_all_experiments(proj.id)
        self.assertEqual(len(all_experiments), 3)
        for experiment in all_experiments:
            self.assertEqual(isinstance(experiment, mcapi.Experiment), True)
            expt = client.get_experiment(experiment.id)
            self.assertEqual(expt.id, experiment.id)
            experiment_request = mcapi.CreateExperimentRequest(description="<new description>")
            updated_expt = client.update_experiment(expt.id, experiment_request)
            self.assertEqual(updated_expt.description, "<new description>")
            client.delete_experiment(proj.id, expt.id)

        all_experiments = client.get_all_experiments(proj.id)
        self.assertEqual(len(all_experiments), 0)
        client.delete_project(proj.id)


    def test_file_api(self):
        """Test the API files and directories commands independently of the CLI"""

        mcurl = os.environ.get("MC_API_URL")
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        client = mcapi.Client.login(email, password, base_url=mcurl)

        # make sure test project does not already exist
        all_projects = client.get_all_projects()
        for proj in all_projects:
            if proj.name == "__clitest__file_api":
                client.delete_project(proj.id)

        proj = client.create_project("__clitest__file_api")

        ### test empty directory api commands ###

        # get root directory by path (get_file_by_path gets files and directories)
        result = client.get_file_by_path(proj.id, "/")
        self.assertEqual(isdir(result), True)
        root_directory_id = result.id

        # get root directory by id
        result = client.get_directory(proj.id, root_directory_id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.id, root_directory_id)

        # get root directory contents by id (should be empty)
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(result, list())

        # get root directory contents by path (should be empty)
        result = client.list_directory_by_path(proj.id, "/")
        self.assertEqual(result, list())

        # create directory
        result = client.create_directory(proj.id, "example_dir", root_directory_id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")
        example_dir_id = result.id

        # create same directory
        result = client.create_directory(proj.id, "example_dir", root_directory_id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.path, "/example_dir")
        self.assertEqual(result.id, example_dir_id)

        # check list_directory w/ existing directory
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(isdir(result[0]), True)
        self.assertEqual(result[0].name, "example_dir")
        self.assertEqual(result[0].id, example_dir_id)
        self.assertEqual(result[0].path, "/example_dir")
        self.assertEqual(isinstance(result[0].size, int), True)
        self.assertEqual(result[0].size, 0)
        self.assertEqual(isinstance(result[0].checksum, str), True)
        self.assertEqual(result[0].checksum, '')

        # get root directory contents by id (should have example_dir)
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(len(result), 1)
        self.assertEqual(isdir(result[0]), True)
        self.assertEqual(result[0].name, "example_dir")
        self.assertEqual(result[0].id, example_dir_id)
        self.assertEqual(result[0].path, "/example_dir")

        # get example_dir by id
        result = client.get_directory(proj.id, example_dir_id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.id, example_dir_id)
        self.assertEqual(result.path, "/example_dir")

        # get example_dir by path (get_file_by_path gets files and directories)
        result = client.get_file_by_path(proj.id, "/example_dir")
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir")
        self.assertEqual(result.id, example_dir_id)
        self.assertEqual(result.path, "/example_dir")

        # rename example_dir -> example_dir_new_name
        result = client.rename_directory(proj.id, example_dir_id, "example_dir_new_name")
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir_new_name")
        self.assertEqual(result.id, example_dir_id)
        self.assertEqual(result.path, "/example_dir_new_name")

        # move /example_dir_new_name -> /other_dir/example_dir_new_name
        result = client.create_directory(proj.id, "other_dir", root_directory_id)
        self.assertEqual(isdir(result), True)
        other_dir_id = result.id
        result = client.move_directory(proj.id, example_dir_id, other_dir_id)
        self.assertEqual(isdir(result), True)
        self.assertEqual(result.name, "example_dir_new_name")
        self.assertEqual(result.id, example_dir_id)
        self.assertEqual(result.path, "/other_dir/example_dir_new_name")

        # delete directories
        client.delete_directory(proj.id, example_dir_id)
        client.delete_directory(proj.id, other_dir_id)

        # check that root directory is now empty
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(len(result), 0)

        ### create local project and test upload / download of files ###

        # write local project files and directories locally
        proj_path = os.path.join(test_project_directory(), proj.name)
        basic_project_1 = make_basic_project_1(proj_path)
        self.assertEqual(os.path.isdir(proj_path), True)

        # upload file
        filepath = os.path.join(proj_path, "file_A.txt")
        local_checksum = checksum(filepath)
        local_size = os.path.getsize(filepath)
        client.upload_file(proj.id, root_directory_id, filepath)
        result = client.get_file_by_path(proj.id, "/file_A.txt") # TODO: return File from upload_file?
        self.assertEqual(isfile(result), True)
        self.assertEqual(result.name, "file_A.txt")
        self.assertEqual(int(result.directory_id), root_directory_id) # TODO: no cast
        self.assertEqual(result.path, None)
        self.assertEqual(result.checksum, local_checksum)
        self.assertEqual(int(result.size), local_size) # TODO: no cast
        file_id = result.id

        # check list_directory w/ existing file
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(isfile(result[0]), True)
        self.assertEqual(result[0].name, "file_A.txt")
        self.assertEqual(result[0].id, file_id)
        self.assertEqual(result[0].path, None)
        self.assertEqual(isinstance(result[0].size, int), True)
        self.assertEqual(isinstance(result[0].checksum, str), True)

        # get file by id
        result = client.get_file(proj.id, file_id)
        self.assertEqual(isfile(result), True)
        self.assertEqual(result.name, "file_A.txt")
        self.assertEqual(result.id, file_id)
        self.assertEqual(result.path, "/file_A.txt")

        # get file by path
        result = client.get_file_by_path(proj.id, "/file_A.txt")
        self.assertEqual(isfile(result), True)
        self.assertEqual(result.name, "file_A.txt")
        self.assertEqual(result.id, file_id)
        self.assertEqual(result.path, None)

        # download file by id
        filepath = os.path.join(proj_path, "file_A.txt")
        orig_checksum = checksum(filepath)
        orig_size = os.path.getsize(filepath)
        remove_if(filepath)
        result = client.get_file(proj.id, file_id)

        self.assertEqual(os.path.exists(filepath), False)
        try:
            client.download_file(proj.id, file_id, filepath)
        except mcapi.MCAPIError as e:
            if e.response.status_code == 500:
                msg = "download_file error: " + str(e) + "\nAborting: Please restart materialscommmons"
                pytest.exit(msg)
            raise e
        self.assertEqual(os.path.exists(filepath), True)
        self.assertEqual(os.path.isfile(filepath), True)

        new_checksum = checksum(filepath)
        new_size = os.path.getsize(filepath)
        self.assertEqual(result.checksum, orig_checksum)
        self.assertEqual(result.checksum, new_checksum)
        self.assertEqual(int(result.size), orig_size) # TODO: no cast
        self.assertEqual(int(result.size), new_size) # TODO: no cast

        # rename file
        result = client.rename_file(proj.id, file_id, "file_A_new_name.txt")
        self.assertEqual(isfile(result), True)
        self.assertEqual(result.name, "file_A_new_name.txt")
        self.assertEqual(result.id, file_id)
        self.assertEqual(result.path, "/file_A_new_name.txt")

        # download file by id (after renaming)
        orig_filepath = os.path.join(proj_path, "file_A.txt")
        new_filepath = os.path.join(proj_path, "file_A_new_name.txt")
        result = client.get_file(proj.id, file_id)

        self.assertEqual(os.path.exists(new_filepath), False)
        client.download_file(proj.id, file_id, new_filepath)
        self.assertEqual(os.path.exists(new_filepath), True)
        self.assertEqual(os.path.isfile(new_filepath), True)

        orig_checksum = checksum(orig_filepath)
        orig_size = os.path.getsize(orig_filepath)
        new_checksum = checksum(new_filepath)
        new_size = os.path.getsize(new_filepath)
        self.assertEqual(result.checksum, orig_checksum)
        self.assertEqual(result.checksum, new_checksum)
        self.assertEqual(int(result.size), orig_size) # TODO: no cast
        self.assertEqual(int(result.size), new_size) # TODO: no cast

        remove_if(new_filepath)

        # move file
        result = client.create_directory(proj.id, "example_dir", root_directory_id)
        self.assertEqual(isdir(result), True)
        example_dir_id = result.id
        result = client.move_file(proj.id, file_id, example_dir_id)
        self.assertEqual(isfile(result), True)
        self.assertEqual(result.name, "file_A_new_name.txt")
        self.assertEqual(result.id, file_id)
        self.assertEqual(result.path, "/file_A_new_name.txt")
        self.assertEqual(int(result.directory_id), example_dir_id) # TODO: no cast
        result = client.list_directory(proj.id, example_dir_id)
        self.assertEqual(len(result), 1)

        # delete directory containing a file (should fail and raise)
        with pytest.raises(Exception) as e:
            client.delete_directory(proj.id, example_dir_id)
        assert "Bad Request" in str(e)
        result = client.get_directory(proj.id, example_dir_id)
        self.assertEqual(result.id, example_dir_id)

        # delete file
        client.delete_file(proj.id, file_id)
        result = client.list_directory(proj.id, example_dir_id)
        self.assertEqual(len(result), 0)

        # delete directory
        client.delete_directory(proj.id, example_dir_id)
        result = client.list_directory(proj.id, root_directory_id)
        self.assertEqual(len(result), 0)

        # clean up
        basic_project_1.clean_files()
        client.delete_project(proj.id)
