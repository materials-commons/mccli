import os
import unittest

import materials_commons.api as mcapi

from materials_commons.cli.file_functions import isfile, isdir
from materials_commons.cli.treedb import LocalTree, RemoteTree

from .cli_test_project import make_basic_project_1, test_project_directory, remove_if

class TestTreeTable(unittest.TestCase):

    @classmethod
    def tearDownClass(cls):
        pass

    def test_localtree(self):
        """Test the LocalTree"""
        project_name = "__clitest__localtree"
        project_path = os.path.join(test_project_directory(), project_name)
        basic_project_1 = make_basic_project_1(project_path)

        # initialize the LocalTree
        localtree = LocalTree(project_path)
        localtree.connect()
        records = {record['path']:record for record in localtree.select_all()}
        self.assertEqual(len(records), 0)
        localtree.close()

        # update root dir record (no children, not recursively), then delete record
        localtree.connect()
        localtree.update("/", get_children=False, recurs=False)
        records = localtree.select_by_path("/")
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0]['name'], '/')
        self.assertEqual(records[0]['path'], '/')
        self.assertEqual(records[0]['parent_path'], None)
        self.assertEqual(records[0]['otype'], 'directory')

        localtree.delete_by_path("/")
        records = {record['path']:record for record in localtree.select_all()}
        self.assertEqual(len(records), 0)
        localtree.close()

        # update root dir record (w/ children, not recursively), then delete records
        localtree.connect()
        localtree.update("/", get_children=True, recurs=False)
        records = {record['path']:record for record in localtree.select_all()}

        self.assertEqual(len(records), 4)
        self.assertEqual(records["/"]["name"], "/")
        self.assertEqual(records["/"]["path"], "/")
        self.assertEqual(records["/"]['parent_path'], None)
        self.assertEqual(records["/"]['otype'], 'directory')
        self.assertEqual(records["/file_A.txt"]["name"], "file_A.txt")
        self.assertEqual(records["/file_A.txt"]["path"], "/file_A.txt")
        self.assertEqual(records["/file_A.txt"]['parent_path'], "/")
        self.assertEqual(records["/file_A.txt"]['otype'], 'file')
        self.assertEqual(records["/file_B.txt"]["name"], "file_B.txt")
        self.assertEqual(records["/file_B.txt"]["path"], "/file_B.txt")
        self.assertEqual(records["/file_B.txt"]['parent_path'], "/")
        self.assertEqual(records["/file_B.txt"]['otype'], 'file')
        self.assertEqual(records["/level_1"]["name"], "level_1")
        self.assertEqual(records["/level_1"]["path"], "/level_1")
        self.assertEqual(records["/level_1"]['parent_path'], "/")
        self.assertEqual(records["/level_1"]['otype'], 'directory')

        localtree.delete_by_path("/", recurs=False)
        records = [record for record in localtree.select_all()]
        self.assertEqual(len(records), 3)
        localtree.delete_by_path("/file_A.txt", recurs=False)
        localtree.delete_by_path("/file_B.txt", recurs=False)
        localtree.delete_by_path("/level_1", recurs=False)
        records = [record for record in localtree.select_all()]
        self.assertEqual(len(records), 0)
        localtree.close()

        # update root dir record (w/ children, recursively), then delete records
        localtree.connect()
        localtree.update("/", get_children=True, recurs=True)
        records = {record['path']:record for record in localtree.select_all()}
        self.assertEqual(len(records), 9)

        localtree.delete_by_path("/", recurs=True)
        records = [record for record in localtree.select_all()]
        self.assertEqual(len(records), 0)
        localtree.close()

        # clean up
        basic_project_1.clean_files()

    def test_remotetree(self):
        """Test the RemoteTree"""

        # create local test files and directories
        project_name = "__clitest__remotetree"
        project_path = os.path.join(test_project_directory(), project_name)
        basic_project_1 = make_basic_project_1(project_path)

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

        # initialize the RemoteTree
        remotetree = RemoteTree(proj, None)
        remotetree.connect()
        records = {record['path']:record for record in remotetree.select_all()}
        self.assertEqual(len(records), 0)
        remotetree.close()

        # create directories and upload files
        root_file_A = client.upload_file(proj.id, proj.root_dir.id, os.path.join(project_path, "file_A.txt"))
        self.assertEqual(isfile(root_file_A), True)
        root_file_B = client.upload_file(proj.id, proj.root_dir.id, os.path.join(project_path, "file_B.txt"))
        self.assertEqual(isfile(root_file_B), True)
        level_1_dir = client.create_directory(proj.id, "level_1", proj.root_dir.id)
        self.assertEqual(isdir(level_1_dir), True)

        level_1_file_A = client.upload_file(proj.id, level_1_dir.id, os.path.join(project_path, "level_1", "file_A.txt"))
        self.assertEqual(isfile(level_1_file_A), True)
        level_1_file_B = client.upload_file(proj.id, level_1_dir.id, os.path.join(project_path, "level_1", "file_B.txt"))
        self.assertEqual(isfile(level_1_file_B), True)
        level_2_dir = client.create_directory(proj.id, "level_2", level_1_dir.id)
        self.assertEqual(isdir(level_2_dir), True)

        level_2_file_A = client.upload_file(proj.id, level_2_dir.id, os.path.join(project_path, "level_1", "level_2", "file_A.txt"))
        self.assertEqual(isfile(level_2_file_A), True)
        level_2_file_B = client.upload_file(proj.id, level_2_dir.id, os.path.join(project_path, "level_1", "level_2", "file_B.txt"))
        self.assertEqual(isfile(level_2_file_B), True)

        # update root dir record (no children, not recursively), then delete record
        remotetree.connect()
        remotetree.update("/", get_children=False, recurs=False)
        records = remotetree.select_by_path("/")
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0]['name'], '/')
        self.assertEqual(records[0]['path'], '/')
        self.assertEqual(records[0]['parent_path'], None)
        self.assertEqual(records[0]['otype'], 'directory')

        remotetree.delete_by_path("/")
        records = {record['path']:record for record in remotetree.select_all()}
        self.assertEqual(len(records), 0)
        remotetree.close()

        # update root dir record (w/ children, not recursively), then delete records
        remotetree.connect()
        remotetree.update("/", get_children=True, recurs=False)
        records = {record['path']:record for record in remotetree.select_all()}

        self.assertEqual(len(records), 4)
        self.assertEqual(records["/"]["name"], "/")
        self.assertEqual(records["/"]["path"], "/")
        self.assertEqual(records["/"]['parent_path'], None)
        self.assertEqual(records["/"]['otype'], 'directory')
        self.assertEqual(records["/file_A.txt"]["name"], "file_A.txt")
        self.assertEqual(records["/file_A.txt"]["path"], "/file_A.txt")
        self.assertEqual(records["/file_A.txt"]['parent_path'], "/")
        self.assertEqual(records["/file_A.txt"]['otype'], 'file')
        self.assertEqual(records["/file_B.txt"]["name"], "file_B.txt")
        self.assertEqual(records["/file_B.txt"]["path"], "/file_B.txt")
        self.assertEqual(records["/file_B.txt"]['parent_path'], "/")
        self.assertEqual(records["/file_B.txt"]['otype'], 'file')
        self.assertEqual(records["/level_1"]["name"], "level_1")
        self.assertEqual(records["/level_1"]["path"], "/level_1")
        self.assertEqual(records["/level_1"]['parent_path'], "/")
        self.assertEqual(records["/level_1"]['otype'], 'directory')

        remotetree.delete_by_path("/", recurs=False)
        records = [record for record in remotetree.select_all()]
        self.assertEqual(len(records), 3)
        remotetree.delete_by_path("/file_A.txt", recurs=False)
        remotetree.delete_by_path("/file_B.txt", recurs=False)
        remotetree.delete_by_path("/level_1", recurs=False)
        records = [record for record in remotetree.select_all()]
        self.assertEqual(len(records), 0)
        remotetree.close()

        # update root dir record (w/ children, recursively), then delete records
        remotetree.connect()
        remotetree.update("/", get_children=True, recurs=True)
        records = {record['path']:record for record in remotetree.select_all()}
        self.assertEqual(len(records), 9)

        remotetree.delete_by_path("/", recurs=True)
        records = [record for record in remotetree.select_all()]
        self.assertEqual(len(records), 0)
        remotetree.close()

        # clean up
        basic_project_1.clean_files()
        client.delete_project(proj.id)
