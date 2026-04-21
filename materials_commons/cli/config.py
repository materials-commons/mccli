import getpass
import json
import os
import re
from dataclasses import asdict, dataclass, field
from os.path import join
from typing import Any, Dict, List, Optional

import requests
from materials_commons.api.client import Client


def default_config_dir() -> str:
    user = getpass.getuser()
    return join(os.path.expanduser("~" + user), ".materialscommons")


def resolve_config_file(
    config_dir_path: Optional[str],
    config_file_name: str,
) -> str:
    if not config_dir_path:
        config_dir_path = default_config_dir()
    return join(config_dir_path, config_file_name)


def default_data() -> Dict[str, Any]:
    return {
        "apikey": None,
        "mcurl": None,
        "email": None,
        "remotes": [],
        "globus": {},
        "client_uuid": None,
    }


def load_json(path: str) -> Dict[str, Any]:
    if os.path.exists(path):
        with open(path, "r") as f:
            return json.load(f)
    return default_data()


def apply_environment(data: Dict[str, Any]) -> Dict[str, Any]:
    data = dict(data)

    env_apikey = os.environ.get("MC_API_KEY")
    env_mcurl = os.environ.get("MC_API_URL")
    env_email = os.environ.get("MC_API_EMAIL")

    if env_apikey:
        data["apikey"] = env_apikey
    if env_mcurl:
        data["mcurl"] = env_mcurl
    if env_email:
        data["email"] = env_email

    return data


def apply_overrides(
    data: Dict[str, Any],
    override_config: Optional[Dict[str, Any]],
) -> Dict[str, Any]:
    data = dict(data)
    if override_config:
        data.update(override_config)
    return data


def normalize_default_remote(data: Dict[str, Any]) -> Dict[str, Any]:
    data = dict(data)

    if data.get("mcurl") and data.get("apikey") and data.get("email"):
        data["default_remote"] = {
            "mcurl": data.get("mcurl"),
            "email": data.get("email"),
            "mcapikey": data.get("apikey"),
        }
    elif "default_remote" not in data:
        data["default_remote"] = {
            "mcurl": data.get("mcurl"),
            "email": "__default__",
            "mcapikey": data.get("apikey"),
        }

    return data


@dataclass(eq=True)
class RemoteConfig:
    mcurl: Optional[str] = None
    email: Optional[str] = None
    mcapikey: Optional[str] = None

    def get_params(self) -> Dict[str, Optional[str]]:
        return {"apikey": self.mcapikey}

    def make_client(self) -> Client:
        return Client(self.mcapikey, self.mcurl)


@dataclass
class GlobusConfig:
    transfer_rt: Optional[str] = None
    endpoint_id: Optional[str] = None


@dataclass
class Config:
    """Configuration variables."""

    default_remote: RemoteConfig = field(default_factory=RemoteConfig)
    remotes: List[RemoteConfig] = field(default_factory=list)
    globus: GlobusConfig = field(default_factory=GlobusConfig)
    developer_mode: bool = False
    REST_logging: bool = False
    client_uuid: Optional[str] = None
    config_file: Optional[str] = None

    @classmethod
    def load(
        cls,
        config_dir_path: Optional[str] = None,
        config_file_name: str = "config.json",
        override_config: Optional[Dict[str, Any]] = None,
    ) -> "Config":
        config_file = resolve_config_file(config_dir_path, config_file_name)

        data = load_json(config_file)
        data = apply_environment(data)
        data = apply_overrides(data, override_config)
        data = normalize_default_remote(data)

        return cls(
            default_remote=RemoteConfig(**data.get("default_remote", {})),
            remotes=[RemoteConfig(**kwargs) for kwargs in data.get("remotes", [])],
            globus=GlobusConfig(**data.get("globus", {})),
            developer_mode=data.get("developer_mode", False),
            REST_logging=data.get("REST_logging", False),
            client_uuid=data.get("client_uuid"),
            config_file=config_file,
        )

    def to_dict(self) -> Dict[str, Any]:
        return {
            "default_remote": asdict(self.default_remote),
            "remotes": [asdict(value) for value in self.remotes],
            "globus": asdict(self.globus),
            "developer_mode": self.developer_mode,
            "REST_logging": self.REST_logging,
            "client_uuid": self.client_uuid or "",
        }

    def save(self) -> None:
        if not self.config_file:
            raise ValueError("Cannot save Config without config_file")

        config_dir_path = os.path.dirname(self.config_file)
        if config_dir_path and not os.path.exists(config_dir_path):
            os.makedirs(config_dir_path, exist_ok=True)

        with open(self.config_file, "w") as f:
            json.dump(self.to_dict(), f, indent=2)

        os.chmod(self.config_file, 0o600)

    def find_remote(
        self,
        email: Optional[str],
        mcurl: Optional[str],
    ) -> Optional[RemoteConfig]:
        for remote in self.remotes:
            if remote.email == email and remote.mcurl == mcurl:
                return remote
        return None


def get_remote_config_and_login_if_necessary(
    email: Optional[str] = None,
    mcurl: Optional[str] = None,
) -> RemoteConfig:
    """Prompt for login if remote is not stored in Config."""
    config = Config.load()
    existing_remote = config.find_remote(email=email, mcurl=mcurl)

    if existing_remote is not None:
        return existing_remote

    remote_config = RemoteConfig(mcurl=mcurl, email=email)

    while True:
        try:
            print("Login to:", email, mcurl)
            password = getpass.getpass(prompt="password: ")
            remote_config.mcapikey = Client.get_apikey(email, password, mcurl)
            break
        except requests.exceptions.HTTPError as e:
            print(str(e))
            if not re.search("Bad Request for url", str(e)):
                raise
            print("Wrong password for " + email + " at " + mcurl)
        except requests.exceptions.ConnectionError as e:
            print("Could not connect to " + mcurl)
            raise e

    config.remotes.append(remote_config)
    config.save()

    print()
    print("Added APIKey for", email, "at", mcurl, "to", config.config_file)
    print()

    return remote_config


def make_client_and_login_if_necessary(
    email: Optional[str] = None,
    mcurl: Optional[str] = None,
) -> Client:
    """Make Client, prompting for login if remote is not stored in Config."""
    remote_config = get_remote_config_and_login_if_necessary(mcurl=mcurl, email=email)
    return remote_config.make_client()