import asyncio
from typing import Optional, Dict, Any

from materials_commons.cli.run import run_command_stream, CommandOutputLine
from materials_commons.cli.server import projects
from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc


class SearchFindHandlerLookup(CommandHandlerLookup):
    def __init__(self):
        self._handlers: Dict[str, HandlerFunc] = {
            # Search commands
            "SEARCH_FILES": _handle_search_files,
            "SEARCH_FILES_AT_PATH": _handle_search_files_at_path,

            # Find commands
            "FIND_FILES": _handle_find_files,
            "FIND_FILES_AT_PATH": _handle_find_files_at_path,
        }

    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        return self._handlers.get(cmd)


async def _handle_search_files(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] search_files -> {cmd}")
    payload = cmd.get("payload") or {}
    await _handle_run_query_command(queue=queue, cmd="rg", args="-i", resp_cmd="SEARCH_FILES", payload=payload)


async def _handle_search_files_at_path(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] search_files_at_path -> {cmd}")
    payload = cmd.get("payload") or {}
    await _handle_run_query_command(queue=queue, cmd="rg", args="-i", resp_cmd="SEARCH_FILES_AT_PATH",
                                    payload=payload)


async def _handle_find_files(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] find_files -> {cmd}")
    payload = cmd.get("payload") or {}
    await _handle_run_query_command(queue=queue, cmd="fd", args="-i", resp_cmd="FIND_FILES", payload=payload)


async def _handle_find_files_at_path(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] find_files_at_path -> {cmd}")
    payload = cmd.get("payload") or {}
    await _handle_run_query_command(queue=queue, cmd="fd", args="-i", resp_cmd="FIND_FILES_AT_PATH",
                                    payload=payload)


async def _handle_run_query_command(queue: asyncio.Queue, cmd: str, args: str, resp_cmd: str,
                                    payload: Dict[Any, Any]) -> None:
    request_id = payload.get("request_id")
    project_id = payload.get("project_id")
    query = payload.get("query")
    response_payload = {"matches": [], "request_id": request_id}
    path = _get_path_for_cmd(project_id, payload)

    if not path:
        await queue.put({"command": resp_cmd, "payload": response_payload})
        return

    matches = []
    async for event in run_command_stream(cmd, args, query, path):
        if isinstance(event, CommandOutputLine):
            matches.append(event.line)
    response_payload["matches"] = matches
    print(f"[handler] {resp_cmd} response: {response_payload}")
    await queue.put({"command": resp_cmd, "payload": response_payload})


def _get_path_for_cmd(project_id: Optional[str], payload: Dict[str, Any]) -> Optional[str]:
    if project_id:
        proj = projects.get_local_project_by_id(project_id)
        if proj:
            return proj["project_dir_path"]
    return payload.get("path", None)
