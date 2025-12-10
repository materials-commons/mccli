from http.server import BaseHTTPRequestHandler
import asyncio

from materials_commons.cli import desktop


class LocalRestServer(BaseHTTPRequestHandler):
    """
        Listens for local commands (like 'mc clone') and queues messages
        to be sent over the WebSocket.
        """

    def __init__(self, loop: asyncio.AbstractEventLoop, queue: asyncio.Queue, *args, **kwargs):
        self.loop = loop
        self.queue = queue
        super().__init__(*args, **kwargs)

    def do_POST(self):
        if self.path == "/refresh-projects":
            self._refresh_projects()

    def _refresh_projects(self):
        try:
            projects = desktop.list_local_projects()
            project_ids = [str(p["project_id"]) for p in projects]
            payload = {"type": "refresh-projects", "project_ids": project_ids}

            self.loop.call_soon_threadsafe(self.queue.put_nowait, payload)

            self.send_response(200)
            self.end_headers()

        except Exception as e:
            self.send_error(500, str(e))
            self.end_headers()
