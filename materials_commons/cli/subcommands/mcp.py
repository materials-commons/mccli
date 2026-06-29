import argparse
import functools
import inspect

from mcp.server.fastmcp import FastMCP
from materials_commons.cli.config import Config
from materials_commons.api.client import Client


mcp = FastMCP(
    name="mcmcp",
    instructions=""
)


def make_parser():
    parser = argparse.ArgumentParser(
        description="MCP Server for Materials Commons",
        usage="mc mcp",
    )
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--mc-url", type=str, default="https://materialscommons.org/api")
    return parser

def mcp_subcommand(argv, working_dir=None):
    parser = make_parser()
    args = parser.parse_args(argv)
    config = Config.load()
    client = config.default_remote.make_client()
    register_mcapi_tools(client)
    mcp.run()
    return args

def register_mcapi_tools(client: Client):
    for method_name, method in inspect.getmembers(client, predicate=callable):
        if method_name.startswith("_"):
            continue
        if not is_get_or_list_method(method_name):
            continue

        doc = inspect.getdoc(method)
        if doc is None:
            continue

        signature = inspect.signature(method)

        @functools.wraps(method)
        def tool_wrapper(*args, __method=method, **kwargs):
            result = __method(*args, **kwargs)
            return to_jsonable(result)

        tool_wrapper.__name__ = method_name
        tool_wrapper.__doc__ = doc
        tool_wrapper.__signature__ = signature

        mcp.tool(name=method_name)(tool_wrapper)

def is_get_or_list_method(method_name: str):
    return method_name.startswith("get_") or method_name.startswith("list_")

def to_jsonable(value):
    if value is None:
        return None

    if isinstance(value, (str, int, float, bool)):
        return value

    if isinstance(value, list):
        return [to_jsonable(item) for item in value]

    if isinstance(value, tuple):
        return [to_jsonable(item) for item in value]

    if isinstance(value, dict):
        return {
            str(key): to_jsonable(item)
            for key, item in value.items()
        }

    if hasattr(value, "to_dict"):
        return to_jsonable(value.to_dict())

    if hasattr(value, "__dict__"):
        return to_jsonable(value.__dict__)

    return str(value)