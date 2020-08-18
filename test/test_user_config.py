import json
import os
import unittest

import materials_commons.api.client as mcclient
import materials_commons.cli.user_config as user_config

from .cli_test_functions import captured_output, print_string_io
from .cli_test_project import test_project_directory, mkdir_if, remove_if, rmdir_if


def config_file_contents():
    return {
        "default_remote": {
            "mcurl": "fake_url_1",
            "email": "fake_email_1",
            "mcapikey": "fake_key_1"
        },
        "remotes": [
            {
              "mcurl": "fake_url_1",
              "email": "fake_email_1",
              "mcapikey": "fake_key_1"
            },
            {
              "mcurl": "fake_url_2",
              "email": "fake_email_2",
              "mcapikey": "fake_key_2"
            }
        ],
        "globus": {
            "transfer_rt": None,
            "endpoint_id": None
        },
        "interfaces": [],
        "developer_mode": False,
        "REST_logging": False
    }

class TestUserConfig(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        pass

    def test_override_config(self):
        override_config={
            "mcurl": "fakeurl_override",
            "apikey": "fakekey_override"
        }
        config = user_config.Config(config_dir_path="fakedir_override", override_config=override_config)
        self.assertEqual(config.config_file, "fakedir_override/config.json")
        default_remote = config.default_remote
        self.assertEqual(default_remote.mcurl, "fakeurl_override")
        self.assertEqual(default_remote.mcapikey, "fakekey_override")

    def test_environment_config(self):
        checkurl = os.environ.get("MC_API_URL", "fakeurl_environ")
        checkkey = os.environ.get("MC_API_KEY", "fakekey_environ")
        os.environ["MC_API_URL"] = checkurl
        os.environ["MC_API_KEY"] = checkkey
        config = user_config.Config()
        default_remote = config.default_remote
        self.assertEqual(default_remote.mcurl, checkurl)
        self.assertEqual(default_remote.mcapikey, checkkey)
        if checkurl == "fakeurl_environ":
            del os.environ["MC_API_URL"]
        if checkkey == "fakekey_environ":
            del os.environ["MC_API_KEY"]

    def test_file_config(self):

        # check, save, delete environ variables
        checkurl = os.environ.get("MC_API_URL", "fakeurl_environ")
        checkkey = os.environ.get("MC_API_KEY", "fakekey_environ")
        if checkurl != "fakeurl_environ":
            del os.environ["MC_API_URL"]
        if checkkey != "fakekey_environ":
            del os.environ["MC_API_KEY"]

        # create tmp config file and dir
        config_dir_path = os.path.join(test_project_directory(), "config")
        config_file_path = os.path.join(config_dir_path, "config.json")
        mkdir_if(config_dir_path)
        with open(config_file_path, "w") as f:
            json.dump(config_file_contents(), f)

        # check reading Config from file
        config = user_config.Config(config_dir_path=config_dir_path)
        default_remote = config.default_remote
        self.assertEqual(default_remote.mcurl, "fake_url_1")
        self.assertEqual(default_remote.mcapikey, "fake_key_1")
        self.assertEqual(default_remote.email, "fake_email_1")
        self.assertEqual(len(config.remotes), 2)
        remote_1 = config.remotes[0]
        self.assertEqual(remote_1.mcurl, "fake_url_1")
        self.assertEqual(remote_1.mcapikey, "fake_key_1")
        self.assertEqual(remote_1.email, "fake_email_1")
        remote_2 = config.remotes[1]
        self.assertEqual(remote_2.mcurl, "fake_url_2")
        self.assertEqual(remote_2.mcapikey, "fake_key_2")
        self.assertEqual(remote_2.email, "fake_email_2")

        # remove tmp config file and dir
        remove_if(config_file_path)
        rmdir_if(config_dir_path)

        # reset environ variables
        if checkurl != "fakeurl_environ":
            os.environ["MC_API_URL"] = checkurl
        if checkkey != "fakekey_environ":
            os.environ["MC_API_KEY"] = checkkey

    def test_get_apikey(self):
        config = user_config.Config()
        remote = config.default_remote
        password = os.environ.get("MC_API_PASSWORD")
        apikey = mcclient.Client.get_apikey(remote.email, password, base_url=remote.mcurl)
        self.assertEqual(apikey, os.environ.get("MC_API_KEY"))

    def test_login(self):
        email = os.environ.get("MC_API_EMAIL")
        password = os.environ.get("MC_API_PASSWORD")
        url = os.environ.get("MC_API_URL")
        client = mcclient.Client.login(email, password, base_url=url)
        self.assertEqual(client.apikey, os.environ.get("MC_API_KEY"))
