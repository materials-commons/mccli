import asyncio
import json
import socket
import ssl
from _asyncio import Task
from datetime import datetime, timezone
from typing import Dict, Any, Optional, Callable, Awaitable

import websockets
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK, InvalidStatus

from materials_commons.cli import server
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.server.uploader.file_upload_manager import FileUploadManager
from materials_commons.cli.user_config import Config

# Type alias for handler functions
CommandHandler = Callable[[Any, Dict[str, Any]], Awaitable[None]]


async def cleanup_tasks(receiver_task: Task[None], sender_task: Task[Any], heartbeat_task: Task[None]):
    # Wait for either task to finish/fail. This prevents the issue where
    # one of the tasks aborts and the other keeps going.
    done, pending = await asyncio.wait(
        [sender_task, receiver_task, heartbeat_task],
        return_when=asyncio.FIRST_COMPLETED
    )

    # If we are here, then we need to cancel both done and pending tasks.

    # First we cancel pending tasks
    for task in pending:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass

    # Next, cancel done tasks
    for task in done:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass


class WebSocketCommandListener:
    DEFAULT_RECONNECT_MIN_SEC = 1
    DEFAULT_RECONNECT_MAX_SEC = 30

    def __init__(self, ws_url: str, token: Optional[str], client_uuid: str, handlers: Dict[str, CommandHandler],
                 ws_send_queue: asyncio.Queue, db_write_queue: asyncio.Queue, project_dbs: ProjectFileDBs, max_concurrent: int = 3):
        self.ws_url = ws_url
        self.token = token
        self.client_uuid = client_uuid
        self.ws_send_queue = ws_send_queue
        self.db_write_queue = db_write_queue
        self.project_dbs = project_dbs
        self.backoff = self.DEFAULT_RECONNECT_MIN_SEC
        self.handlers = handlers
        self.user_id: Optional[int] = None
        self.max_concurrent = max_concurrent

        config = Config()

        self.file_upload_manager = FileUploadManager(send_queue=ws_send_queue, db_write_queue=db_write_queue,
                                                     client_id=client_uuid, max_concurrent=max_concurrent)

        self.file_download_manager = FileDownloadManager(send_queue=ws_send_queue, client_id=client_uuid,
                                                         mcurl=config.default_remote.mcurl,
                                                         apitoken=config.default_remote.mcapikey,
                                                         max_concurrent=max_concurrent)
        self.db_manager = DBManager(db_queue=self.db_write_queue, project_dbs=self.project_dbs)

        self._upload_workers: list[Task] = []
        self._download_workers: list[Task] = []
        self._db_workers: list[Task] = []

    async def run(self) -> None:
        """
        Main loop: Connects to the websocket and processes incoming JSON messages.
        Reconnects with exponential backoff on transient failures.
        """
        ssl_context = ssl.create_default_context()
        ssl_context.check_hostname = False
        ssl_context.verify_mode = ssl.CERT_NONE

        self._upload_workers = await self.file_upload_manager.start_workers()
        self._download_workers = await self.file_download_manager.start_workers()
        self._db_workers = await self.db_manager.start_workers()

        while True:
            headers = self.build_headers()

            try:
                async with websockets.connect(
                        self.ws_url,
                        additional_headers=headers,
                        open_timeout=15,
                        ssl=ssl_context
                ) as ws:
                    # Reset backoff after a successful connection
                    self.backoff = self.DEFAULT_RECONNECT_MIN_SEC

                    # Create the sender and receiver tasks for the websocket
                    sender_task = asyncio.create_task(self._ws_sender_loop(ws))
                    receiver_task = asyncio.create_task(self._ws_receiver_loop(ws))
                    heartbeat_task = asyncio.create_task(self._ws_heartbeat_loop(ws))

                    await cleanup_tasks(receiver_task, sender_task, heartbeat_task)

            except (ConnectionClosedOK, ConnectionClosedError, InvalidStatus, OSError, asyncio.TimeoutError) as e:
                print(f"Connection lost: {e}, attemping to reconnect in {self.backoff} seconds")
                await asyncio.sleep(self.backoff)
                self.backoff = min(
                    self.DEFAULT_RECONNECT_MAX_SEC,
                    max(self.DEFAULT_RECONNECT_MIN_SEC, self.backoff * 2)
                )

    async def shutdown(self):
        """Shutdown the file transfer workers"""

        # Stop upload workers
        self.file_upload_manager.stop_workers()
        for worker in self._upload_workers:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass

        # Stop download workers
        self.file_download_manager.stop_workers()
        for worker in self._download_workers:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass

        # Stop db workers
        self.db_manager.stop_workers()
        for worker in self._db_workers:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass

    async def _ws_sender_loop(self, ws):
        """Reads from the queue and sends to the websocket."""
        while True:
            # wait for an item from the queue. This message is now in flight and not in queue.
            msg = await self.ws_send_queue.get()
            try:
                # Check if we are sending a binary frame
                if isinstance(msg, dict) and msg.get("_binary_frame"):
                    # Build binary frame by sending a JSON header + newline + binary data
                    header = msg["header"]
                    data = msg["data"]
                    header_bytes = (json.dumps(header) + "\n").encode("utf-8")
                    frame = header_bytes + data
                    await ws.send(frame)
                else:
                    # Send JSON (text frame) message
                    await ws.send(json.dumps(msg))
            except Exception as e:
                # If we are here, then we took a message out of the queue but failed to send it.
                # That means the message could be lost if we don't re-queue it. For now, we assume
                # there is no message ordering requirement, so we can just re-queue it. This
                # shouldn't cause us any problems. If the server we connected to is dead then
                # we just keep trying to reconnect and send the messages when it comes back.
                await self.ws_send_queue.put(msg)

                # We want to exit the loop if we encounter an error, so raise the exception
                raise e
            finally:
                self.ws_send_queue.task_done()

    async def _ws_receiver_loop(self, ws):
        """Reads from the websocket and puts messages into the queue."""
        while True:
            raw = await ws.recv()
            try:
                data = json.loads(raw)
            except json.JSONDecodeError:
                continue

            # print(f"Received message: {data}")
            if isinstance(data, dict):
                await self._dispatch(data)
            elif isinstance(data, list):
                for item in data:
                    if isinstance(item, dict):
                        await self._dispatch(item)

    async def _ws_heartbeat_loop(self, ws):
        """Periodically sends a heartbeat message to keep the connection alive."""
        while True:
            await asyncio.sleep(20)
            try:
                heartbeat_msg = {
                    "command": "HEARTBEAT",
                    "clientId": self.client_uuid,
                    "timestamp": datetime.now(timezone.utc).isoformat(),
                }
                await self.ws_send_queue.put(heartbeat_msg)
            except Exception as e:
                print(f"heartbeat loop exception: {e}")
                pass
        print("heartbeat loop exiting")

    async def _dispatch(self, cmd: Dict[str, Any]) -> None:
        kind = cmd.get("type") or cmd.get("command")
        if not kind:
            return

        # We special case connected to capture the returned user_id.
        #
        # The flow looks as follows: We have the api_token that is sent to the server
        # on startup. The server then sends back a user_id. We store that in the
        # user_id field and use it for all later requests. The api_token is
        # associated with a specific user_id on the server. We want to save this
        # association so that we can use the api_token to make requests on behalf of
        # the user. Cases where this would be used would be where we want to broadcast
        # to all other connections that are associated with this user_id. For example,
        # if a user has multiple browsers open, we want to broadcast to all of them
        # when a server comes online.
        if kind == "connected":
            user_id = (cmd.get("payload") or {}).get("user_id")
            if user_id:
                self.user_id = user_id
            return

        # Route file transfer messages to the FileTransferManager
        if kind in ["TRANSFER_ACCEPT", "TRANSFER_REJECT", "CHUNK_ACK", "CHUNK_ERROR",
                    "TRANSFER_FINALIZE", "UPLOAD_FAILED", "TRANSFER_RESUME_RESPONSE"]:
            if self.file_upload_manager:
                await self.file_upload_manager.handle_message(cmd)
                return

        # We have handlers for the other file transfer commands. These handlers need access
        # to the file_transfer_manager so that they can pause/resume transfers. So we set
        # the file_transfer_manager on the command object. This is only used for the
        # upload and download handlers.
        if kind in ["UPLOAD_FILE", "UPLOAD_DIRECTORY",
                    "CANCEL_UPLOAD", "PAUSE_UPLOAD", "RESUME_UPLOAD"]:
            cmd["_file_manager"] = self.file_upload_manager

        if kind in ["DOWNLOAD_FILE", "CANCEL_DOWNLOAD", "PAUSE_DOWNLOAD", "RESUME_DOWNLOAD"]:
            cmd["_file_manager"] = self.file_download_manager

        handler = self.handlers.get(kind)
        if handler:
            await handler(self.ws_send_queue, cmd)

    def build_headers(self) -> Dict[str, str]:
        headers = {}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        headers["MC-Client-ID"] = self.client_uuid
        headers["MC-Client-Hostname"] = socket.gethostname()
        headers["MC-Connection-Type"] = "cli"

        try:
            projects = server.list_local_projects()
            headers["MC-Client-Projects"] = ",".join([str(p["project_id"]) for p in projects])
        except Exception:
            # Fallback if listing projects fails
            headers["MC-Client-Projects"] = ""

        return headers
