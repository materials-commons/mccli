import json
from functools import partial
from http.server import BaseHTTPRequestHandler, HTTPServer
import asyncio
import threading
from pathlib import Path

from materials_commons.cli import desktop


class LocalRestRequestHandler(BaseHTTPRequestHandler):
    """
        Listens for local commands (like 'mc clone') and queues messages
        to be sent over the WebSocket.
        """

    def __init__(self, loop: asyncio.AbstractEventLoop, queue: asyncio.Queue, *args, **kwargs):
        self.loop = loop
        self.queue = queue
        super().__init__(*args, **kwargs)

    def do_GET(self):
        if self.path == "/refresh-projects":
            self._refresh_projects()
        elif self.path == "/ping":
            self._ping()

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
            
    def _ping(self):
        payload = {"status": "ok"}
        self._send_json(payload)

    def _send_json(self, payload, status=200):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

class LocalRestServer:
    """
    Local REST server for handling requests from the desktop client. Writes to the server.json state file so
    the CLI can find the port.
    """

    def __init__(self, loop: asyncio.AbstractEventLoop, queue: asyncio.Queue):
        self.loop = loop
        self.queue = queue
        self._httpd: HTTPServer | None = None
        self._thread: threading.Thread | None = None
        self._state_file: Path | None = None
        self._port: int = 0
        self._host: str = "127.0.0.1"

    def start(self) -> int:
        handler = partial(LocalRestRequestHandler, self.loop, self.queue)
        self._httpd = HTTPServer((self._host, self._port), handler)
        _, bound_port = self._httpd.server_address

        # Write state so we can discover it for CLI commands
        mcdir = Path.home() / ".materialscommons"
        mcdir.mkdir(parents=True, exist_ok=True)
        self._state_file = mcdir / "rest_server.json"
        self._state_file.write_text(json.dumps({"port": bound_port}))

        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
        self._thread.start()
        return bound_port

    def stop(self):
        if self._httpd is not None:
            self._httpd.shutdown()
            self._httpd.server_close()
            self._httpd = None

        if self._thread is not None:
            self._thread.join(timeout=2)
            self._thread = None

        if self._state_file is not None:
            try:
                self._state_file.unlink()
            except FileNotFoundError:
                pass
            self._state_file = None