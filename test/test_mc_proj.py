import argparse
import pytest
import unittest
import os

import materials_commons.api as mcapi

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.user_config as user_config
from materials_commons.cli.exceptions import MCCLIException
from materials_commons.cli.subcommands.proj import ProjSubcommand

from .cli_test_functions import captured_output, print_string_io
from .cli_test_project import test_project_directory, rmdir_if, remove_hidden_project_files


class TestMCProj(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.setup_failure = None
        cls.proj_subcommand = ProjSubcommand()
        cls.proj_subcommand.working_dir = os.getcwd()

        # ensure there are at least three projects
        client = user_config.Config().default_remote.make_client()
        client.create_project("__clitest__proj_1")
        client.create_project("__clitest__proj_2")
        client.create_project("__clitest__proj_3")
        result = client.get_all_projects()
        if len(result) < 3:
            cls.setup_failure = "project creation failure"

    def setUp(self):
        if self.setup_failure:
            self.fail(self.setup_failure)

    @classmethod
    def tearDownClass(cls):
        client = user_config.Config().default_remote.make_client()
        result = client.get_all_projects()
        for proj in result:
            try:
                client.delete_project(proj.id)
            except:
                pass

    def test_parse_args(self):
        # exclude 'mc', 'proj'
        testargs = []
        args = self.proj_subcommand.parse_args(testargs)
        self.assertEqual(isinstance(args, argparse.Namespace), True)
        self.assertEqual(len(args.expr), 0)

    def test_get_all_from_experiment(self):
        print(os.getcwd())
        self.assertEqual(1, 1)

        # TODO:
        # proj = clifuncs.make_local_project(working_dir)
        # expt = clifuncs.make_local_expt(proj)
        # with pytest.raises(MCCLIException):
        #     result = self.proj_subcommand.get_all_from_experiment(expt)

    def test_get_all_from_project(self):
        # create and clone a project, try to get_all_from_project with that proj, raise error
        remote_config = user_config.Config().default_remote
        client = remote_config.make_client()
        project_id = client.create_project("__clitest__get_all_from_project").id
        proj = clifuncs.clone_project(remote_config, project_id, test_project_directory())
        with pytest.raises(MCCLIException):
            result = self.proj_subcommand.get_all_from_project(proj)

        # clean
        remove_hidden_project_files(proj.local_path)
        rmdir_if(proj.local_path)

    def test_get_all_from_remote(self):
        """Get all from remote should succeed and not require an existing local project"""

        # setUpClass ensures at least 3 projects should exist
        # exclude 'mc', 'proj'
        testargs = []
        args = self.proj_subcommand.parse_args(testargs)
        client = self.proj_subcommand.get_remote(args)
        result = self.proj_subcommand.get_all_from_remote(client)
        self.assertEqual(isinstance(result, list), True)
        for obj in result:
            self.assertEqual(isinstance(obj, mcapi.Project), True)

    def test_mc_proj_output(self):
        testargs = []
        working_dir = os.getcwd()
        with captured_output() as (sout, serr):
            self.proj_subcommand(testargs, working_dir)
        print_string_io(sout)
        out = sout.getvalue().splitlines()
        err = serr.getvalue().splitlines()

        headers = out[0].split()
        self.assertEqual(headers[0], "name")
        self.assertEqual(headers[1], "owner")
        self.assertEqual(headers[2], "id")
        self.assertEqual(headers[3], "updated_at")
        self.assertEqual(len(headers), 4)
