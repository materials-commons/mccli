import asyncio
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from materials_commons.cli.server.uploader.file_uploader import FileUploader
from materials_commons.cli.subcommands.up import ws_upload


@dataclass(frozen=True)
class FakeUpdatedRecord:
    local_checksum: str = "local-checksum"
    local_last_seen_ts: int = 0
    remote_checksum: str = ""
    remote_size: int = 0
    remote_file_id: int = 0
    remote_ctime_ns: int = 0


def make_upload_request(file_path: Path, size: int):
    """
    Build the minimum UploadRequest-like object needed by FileUploader.

    These tests intentionally use lightweight fakes instead of real database
    or request objects so the uploader can be tested in isolation.
    """
    return SimpleNamespace(
        project=SimpleNamespace(id="project-123"),
        observation=SimpleNamespace(
            path=file_path,
            remote_path=Path("/remote/example.txt"),
            local_entry=SimpleNamespace(size=size),
        ),
        updated_record=FakeUpdatedRecord(local_checksum="abc123"),
    )


def make_uploader(
        tmp_path,
        file_bytes=b"hello world",
        chunk_size=5,
        window_size=10,
        progress_callback=None,
):
    """
    Create a FileUploader with mocked queues and a fake UploadRequest.

    The db argument is not used directly by the current implementation, so a
    SimpleNamespace is enough for unit testing.
    """
    file_path = tmp_path / "example.txt"
    file_path.write_bytes(file_bytes)

    ws_send_queue = asyncio.Queue()
    db_write_queue = asyncio.Queue()
    upload_request = make_upload_request(file_path, len(file_bytes))

    uploader = FileUploader(
        ws_send_queue=ws_send_queue,
        db_write_queue=db_write_queue,
        upload_request=upload_request,
        client_id="client-123",
        chunk_size=chunk_size,
        window_size=window_size,
        progress_callback=progress_callback,
    )

    return uploader, ws_send_queue, db_write_queue


def drain_queue(queue):
    """
    Synchronously drain an asyncio.Queue after an asyncio.run() test completes.

    asyncio.Queue#get_nowait() does not need to be awaited, which makes it
    convenient for assertions in these unit tests.
    """
    items = []
    while not queue.empty():
        items.append(queue.get_nowait())
    return items

def test_wait_for_acceptance_already_uploaded_writes_updated_record_to_db_queue(tmp_path):
    """
    Intent:
    Verify TRANSFER_ALREADY_UPLOADED updates the file database queue.

    When the server reports that a file has already been uploaded, the uploader
    should treat acceptance as complete, mark the upload as already uploaded, and
    enqueue the updated remote metadata for persistence.
    """

    async def run_test():
        uploader, _, db_write_queue = make_uploader(tmp_path, file_bytes=b"hello")

        await uploader.handle_response(
            {
                "command": "TRANSFER_ALREADY_UPLOADED",
                "payload": {
                    "file_checksum": "remote-checksum",
                    "file_size": 5,
                    "file_id": 4321,
                    "file_created_at_ns": 987654321,
                },
            }
        )

        result = await uploader._wait_for_acceptance()

        assert result is False
        assert uploader._already_uploaded is True
        assert uploader.waiting_for_response is None

        db_write_request = await db_write_queue.get()
        assert db_write_request.project == uploader.upload_request.project
        assert db_write_request.command == "single"
        assert db_write_request.data.remote_checksum == "remote-checksum"
        assert db_write_request.data.remote_size == 5
        assert db_write_request.data.remote_file_id == 4321
        assert db_write_request.data.remote_ctime_ns == 987654321

    asyncio.run(run_test())


def test_upload_returns_true_and_updates_db_when_server_says_already_uploaded(tmp_path):
    """
    Intent:
    Verify the upload-level TRANSFER_ALREADY_UPLOADED path.

    upload() should return success without sending chunks or TRANSFER_COMPLETE,
    and it should preserve the database update produced during acceptance.
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"hello",
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_ALREADY_UPLOADED",
                "payload": {
                    "file_checksum": "remote-checksum",
                    "file_size": 5,
                    "file_id": 2468,
                    "file_created_at_ns": 13579,
                },
            }
        )

        result = await uploader.upload()

        assert result is True

        sent_messages = drain_queue(ws_send_queue)
        assert len(sent_messages) == 1
        assert sent_messages[0]["command"] == "TRANSFER_INIT"

        db_write_request = await db_write_queue.get()
        assert db_write_request.command == "single"
        assert db_write_request.data.remote_checksum == "remote-checksum"
        assert db_write_request.data.remote_size == 5
        assert db_write_request.data.remote_file_id == 2468
        assert db_write_request.data.remote_ctime_ns == 13579

    asyncio.run(run_test())


def test_ws_upload_single_file_in_mcignore_is_not_uploaded_or_reconciled(tmp_path, monkeypatch):
    """
    Intent:
    Verify websocket single-file upload honors .mcignore.

    If the requested file matches .mcignore, ws_upload() should skip it before
    reconciliation and should not enqueue an upload.
    """

    async def run_test():
        project_root = tmp_path
        ignored_file = project_root / "ignored.txt"
        ignored_file.write_text("do not upload")
        (project_root / ".mcignore").write_text("ignored.txt\n")

        db = SimpleNamespace(close=AsyncMock())
        proj = SimpleNamespace(local_path=project_root, get_filedb=AsyncMock(return_value=db))

        class FakeLocalProject:
            @staticmethod
            def load(_working_dir):
                return proj

        class FakeServiceRuntime:
            def __init__(self, _container):
                pass

            async def start(self, websocket_listener=True):
                return None

            async def stop(self, drain=True):
                return None

        upload_file = AsyncMock()

        fake_container = SimpleNamespace(
            file_upload_manager=SimpleNamespace(
                upload_file=upload_file,
                wait_all=AsyncMock(),
            )
        )

        class FakeServiceContainer:
            @staticmethod
            def create(ws_url):
                return fake_container

        reconcile_file = AsyncMock()

        class FakeAsyncReconciler:
            def __init__(self, db, proj, reconcile_mode):
                pass

            async def reconcile_file(self, path):
                return await reconcile_file(path)

        monkeypatch.setattr("materials_commons.cli.subcommands.up.LocalProject", FakeLocalProject)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.ServiceRuntime", FakeServiceRuntime)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.ServiceContainer", FakeServiceContainer)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.AsyncReconciler", FakeAsyncReconciler)

        args = SimpleNamespace(
            paths=[ignored_file.as_posix()],
            recursive=False,
            ws_url="ws://example.test",
        )

        await ws_upload(args, project_root)

        reconcile_file.assert_not_awaited()
        upload_file.assert_not_awaited()
        fake_container.file_upload_manager.wait_all.assert_awaited_once()
        db.close.assert_awaited_once()

    asyncio.run(run_test())


def test_ws_upload_directory_walk_does_not_upload_files_in_mcignore(tmp_path, monkeypatch):
    """
    Intent:
    Verify websocket directory upload honors .mcignore during walk.

    The directory walk should receive and use the ignore function, causing ignored
    files to be excluded from upload while non-ignored files are still uploaded.
    """

    async def run_test():
        project_root = tmp_path
        allowed_file = project_root / "allowed.txt"
        ignored_file = project_root / "ignored.txt"
        ignored_dir = project_root / "ignored-dir"
        ignored_dir_file = ignored_dir / "nested.txt"

        allowed_file.write_text("upload me")
        ignored_file.write_text("do not upload me")
        ignored_dir.mkdir()
        ignored_dir_file.write_text("do not walk me")
        (project_root / ".mcignore").write_text("ignored.txt\nignored-dir/\n")

        db = SimpleNamespace(close=AsyncMock())
        proj = SimpleNamespace(local_path=project_root, get_filedb=AsyncMock(return_value=db))

        class FakeLocalProject:
            @staticmethod
            def load(_working_dir):
                return proj

        class FakeServiceRuntime:
            def __init__(self, _container):
                pass

            async def start(self, websocket_listener=True):
                return None

            async def stop(self, drain=True):
                return None

        upload_file = AsyncMock(side_effect=["transfer-allowed"])

        fake_container = SimpleNamespace(
            file_upload_manager=SimpleNamespace(
                upload_file=upload_file,
                wait_all=AsyncMock(),
            )
        )

        class FakeServiceContainer:
            @staticmethod
            def create(ws_url):
                return fake_container

        observed_ignore_results = {}

        class FakeAsyncReconciler:
            def __init__(self, db, proj, reconcile_mode):
                pass

            async def walk(self, path, listdir_fn, recursive=False, ignore_fn=None):
                observed_ignore_results["allowed_file"] = ignore_fn(allowed_file, False)
                observed_ignore_results["ignored_file"] = ignore_fn(ignored_file, False)
                observed_ignore_results["ignored_dir"] = ignore_fn(ignored_dir, True)
                observed_ignore_results["ignored_dir_file"] = ignore_fn(ignored_dir_file, False)

                yield Path(path), {
                    "allowed.txt": SimpleNamespace(
                        exception=None,
                        observation=SimpleNamespace(name="allowed.txt"),
                        file_decision=SimpleNamespace(
                            action="upload",
                            updated_record=FakeUpdatedRecord(local_checksum="allowed-checksum"),
                        ),
                    )
                }

        monkeypatch.setattr("materials_commons.cli.subcommands.up.LocalProject", FakeLocalProject)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.ServiceRuntime", FakeServiceRuntime)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.ServiceContainer", FakeServiceContainer)
        monkeypatch.setattr("materials_commons.cli.subcommands.up.AsyncReconciler", FakeAsyncReconciler)

        args = SimpleNamespace(
            paths=[project_root.as_posix()],
            recursive=True,
            ws_url="ws://example.test",
        )

        await ws_upload(args, project_root)

        assert observed_ignore_results == {
            "allowed_file": False,
            "ignored_file": True,
            "ignored_dir": True,
            "ignored_dir_file": True,
        }

        upload_file.assert_awaited_once()
        upload_request = upload_file.await_args.args[0]
        assert upload_request.observation.name == "allowed.txt"
        assert upload_request.updated_record.local_checksum == "allowed-checksum"

        fake_container.file_upload_manager.wait_all.assert_awaited_once()
        db.close.assert_awaited_once()

    asyncio.run(run_test())

def test_send_transfer_init_puts_expected_message_on_websocket_queue(tmp_path):
    """
    Intent:
    Verify the happy path for TRANSFER_INIT.

    The uploader should enqueue a websocket message containing transfer metadata
    and should not directly communicate with the network.
    """

    async def run_test():
        uploader, ws_send_queue, _ = make_uploader(
            tmp_path,
            file_bytes=b"hello",
            chunk_size=2,
        )

        result = await uploader._send_transfer_init()

        assert result is True

        message = await ws_send_queue.get()
        assert message["command"] == "TRANSFER_INIT"
        assert message["client_id"] == "client-123"

        payload = message["payload"]
        assert payload["transfer_id"] == uploader.transfer_id
        assert payload["project_id"] == "project-123"
        assert payload["file_path"] == uploader.file_path.as_posix()
        assert payload["project_path"] == uploader.project_path.as_posix()
        assert payload["file_size"] == 5
        assert payload["chunk_size"] == 2
        assert payload["checksum"] == "abc123"

    asyncio.run(run_test())


def test_wait_for_acceptance_updates_chunk_size_when_server_changes_it(tmp_path):
    """
    Intent:
    Verify that a successful TRANSFER_ACCEPT response returns True and that
    the uploader honors a server-provided chunk size override.
    """

    async def run_test():
        uploader, _, _ = make_uploader(
            tmp_path,
            file_bytes=b"hello",
            chunk_size=5,
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_ACCEPT",
                "payload": {
                    "chunk_size": 2,
                },
            }
        )

        result = await uploader._wait_for_acceptance()

        assert result is True
        assert uploader.chunk_size == 2
        assert uploader.waiting_for_response is None

    asyncio.run(run_test())


def test_wait_for_acceptance_treats_already_uploaded_reject_as_special_case(tmp_path):
    """
    Intent:
    Verify the special rejection case where the remote service says the file
    was already uploaded.

    _wait_for_acceptance() returns False, but marks _already_uploaded so upload()
    can convert that case into an overall success.
    """

    async def run_test():
        uploader, _, _ = make_uploader(tmp_path, file_bytes=b"hello")

        await uploader.handle_response(
            {
                "command": "TRANSFER_REJECT",
                "payload": {
                    "reason": "file already uploaded",
                },
            }
        )

        result = await uploader._wait_for_acceptance()

        assert result is False
        assert uploader._already_uploaded is True
        assert uploader.waiting_for_response is None

    asyncio.run(run_test())


def test_wait_for_acceptance_returns_false_for_regular_reject(tmp_path):
    """
    Intent:
    Verify a normal TRANSFER_REJECT failure.

    The uploader should return False and should not mark the file as already
    uploaded.
    """

    async def run_test():
        uploader, _, _ = make_uploader(tmp_path, file_bytes=b"hello")

        await uploader.handle_response(
            {
                "command": "TRANSFER_REJECT",
                "payload": {
                    "reason": "permission denied",
                },
            }
        )

        result = await uploader._wait_for_acceptance()

        assert result is False
        assert uploader._already_uploaded is False
        assert uploader.waiting_for_response is None

    asyncio.run(run_test())


def test_send_chunks_windowed_sends_binary_frames_with_last_chunk_marker(tmp_path):
    """
    Intent:
    Verify the happy path for windowed chunk sending.

    The file is split into chunks and each chunk is put onto the websocket queue
    as a binary frame. The final chunk should have is_last=True.
    """

    async def run_test():
        uploader, ws_send_queue, _ = make_uploader(
            tmp_path,
            file_bytes=b"abcdef",
            chunk_size=2,
            window_size=10,
        )

        result = await uploader._send_chunks_windowed()

        assert result is True

        frames = drain_queue(ws_send_queue)
        assert len(frames) == 3

        assert frames[0]["_binary_frame"] is True
        assert frames[0]["header"]["sequence"] == 0
        assert frames[0]["header"]["size"] == 2
        assert frames[0]["header"]["is_last"] is False
        assert frames[0]["data"] == b"ab"

        assert frames[1]["header"]["sequence"] == 1
        assert frames[1]["header"]["is_last"] is False
        assert frames[1]["data"] == b"cd"

        assert frames[2]["header"]["sequence"] == 2
        assert frames[2]["header"]["is_last"] is True
        assert frames[2]["data"] == b"ef"

    asyncio.run(run_test())


def test_process_acks_updates_progress_and_window_state(tmp_path):
    """
    Intent:
    Verify ACK handling.

    Each CHUNK_ACK should update bytes_sent, last_acked_chunk, in_flight_chunks,
    and invoke the progress callback.
    """
    progress_events = []

    async def run_test():
        uploader, _, _ = make_uploader(
            tmp_path,
            file_bytes=b"abcdef",
            chunk_size=2,
            progress_callback=lambda sent, total: progress_events.append((sent, total)),
        )

        # Simulate that three chunks have already been sent.
        uploader.next_chunk_to_send = 3
        uploader.in_flight_chunks = 3

        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 0,
                    "bytes_received": 2,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 1,
                    "bytes_received": 4,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 2,
                    "bytes_received": 6,
                },
            }
        )

        await uploader._process_acks()

        assert uploader.last_acked_chunk == 2
        assert uploader.bytes_sent == 6
        assert uploader.in_flight_chunks == 0
        assert progress_events == [(2, 6), (4, 6), (6, 6)]

    asyncio.run(run_test())


def test_process_acks_raises_for_chunk_error(tmp_path):
    """
    Intent:
    Verify failure handling for CHUNK_ERROR.

    A chunk error from the remote service should raise an exception so the upload
    can fail rather than silently continue.
    """

    async def run_test():
        uploader, _, _ = make_uploader(
            tmp_path,
            file_bytes=b"abcdef",
            chunk_size=2,
        )

        await uploader.handle_response(
            {
                "command": "CHUNK_ERROR",
                "payload": {
                    "error": "checksum mismatch",
                },
            }
        )

        with pytest.raises(Exception, match="checksum mismatch"):
            await uploader._process_acks()

    asyncio.run(run_test())


def test_wait_for_finalization_writes_updated_record_to_db_queue(tmp_path):
    """
    Intent:
    Verify the happy path for finalization.

    When TRANSFER_FINALIZE arrives, the uploader should create a DBWriteRequest
    containing remote file metadata and put it on the database write queue.
    """

    async def run_test():
        uploader, _, db_write_queue = make_uploader(tmp_path, file_bytes=b"hello")

        await uploader.handle_response(
            {
                "command": "TRANSFER_FINALIZE",
                "payload": {
                    "file_checksum": "remote-checksum",
                    "file_size": 5,
                    "file_id": 987,
                    "file_created_at_ns": 123456789,
                },
            }
        )

        result = await uploader._wait_for_finalization()

        assert result is True

        db_write_request = await db_write_queue.get()
        assert db_write_request.project == uploader.upload_request.project
        assert db_write_request.command == "single"
        assert db_write_request.data.remote_checksum == "remote-checksum"
        assert db_write_request.data.remote_size == 5
        assert db_write_request.data.remote_file_id == 987
        assert db_write_request.data.remote_ctime_ns == 123456789

    asyncio.run(run_test())


def test_wait_for_finalization_returns_false_for_upload_failed(tmp_path):
    """
    Intent:
    Verify failure handling for finalization.

    UPLOAD_FAILED should return False and should not enqueue a database write.
    """

    async def run_test():
        uploader, _, db_write_queue = make_uploader(tmp_path, file_bytes=b"hello")

        await uploader.handle_response(
            {
                "command": "UPLOAD_FAILED",
                "payload": {
                    "error": "virus scan failed",
                },
            }
        )

        result = await uploader._wait_for_finalization()

        assert result is False
        assert db_write_queue.empty()

    asyncio.run(run_test())


def test_upload_happy_path_sends_init_chunks_complete_and_writes_db(tmp_path):
    """
    Intent:
    Verify the full happy path at the upload() level.

    External services are mocked by preloading the response queue with the exact
    messages the uploader expects: accept, chunk ACKs, then finalize.
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"abcdef",
            chunk_size=2,
            window_size=10,
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_ACCEPT",
                "payload": {},
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 0,
                    "bytes_received": 2,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 1,
                    "bytes_received": 4,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 2,
                    "bytes_received": 6,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "TRANSFER_FINALIZE",
                "payload": {
                    "file_checksum": "remote-checksum",
                    "file_size": 6,
                    "file_id": 123,
                    "file_created_at_ns": 456,
                },
            }
        )

        result = await uploader.upload()

        assert result is True

        sent_messages = drain_queue(ws_send_queue)
        assert sent_messages[0]["command"] == "TRANSFER_INIT"
        assert sent_messages[1]["_binary_frame"] is True
        assert sent_messages[2]["_binary_frame"] is True
        assert sent_messages[3]["_binary_frame"] is True
        assert sent_messages[4]["command"] == "TRANSFER_COMPLETE"
        assert sent_messages[4]["payload"]["total_bytes"] == 6

        db_write_request = await db_write_queue.get()
        assert db_write_request.data.remote_checksum == "remote-checksum"
        assert db_write_request.data.remote_size == 6
        assert db_write_request.data.remote_file_id == 123

    asyncio.run(run_test())


def test_upload_returns_true_when_server_says_file_already_uploaded(tmp_path):
    """
    Intent:
    Verify the upload-level special case for duplicate uploads.

    TRANSFER_REJECT with reason "file already uploaded" should be treated as
    success by upload().
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"hello",
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_REJECT",
                "payload": {
                    "reason": "file already uploaded",
                },
            }
        )

        result = await uploader.upload()

        assert result is True

        sent_messages = drain_queue(ws_send_queue)
        assert len(sent_messages) == 1
        assert sent_messages[0]["command"] == "TRANSFER_INIT"
        assert db_write_queue.empty()

    asyncio.run(run_test())


def test_upload_returns_false_when_acceptance_is_rejected(tmp_path):
    """
    Intent:
    Verify upload-level failure when the remote service rejects the transfer.

    The uploader should stop after TRANSFER_REJECT and should not send chunks,
    completion, or database writes.
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"hello",
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_REJECT",
                "payload": {
                    "reason": "permission denied",
                },
            }
        )

        result = await uploader.upload()

        assert result is False

        sent_messages = drain_queue(ws_send_queue)
        assert len(sent_messages) == 1
        assert sent_messages[0]["command"] == "TRANSFER_INIT"
        assert db_write_queue.empty()

    asyncio.run(run_test())


def test_upload_returns_false_when_finalization_fails(tmp_path):
    """
    Intent:
    Verify upload-level failure after chunks are successfully sent and ACKed.

    If the server returns UPLOAD_FAILED during finalization, upload() should
    return False and should not write the updated record to the database queue.
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"abcd",
            chunk_size=2,
            window_size=10,
        )

        await uploader.handle_response(
            {
                "command": "TRANSFER_ACCEPT",
                "payload": {},
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 0,
                    "bytes_received": 2,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "chunk_sequence": 1,
                    "bytes_received": 4,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "UPLOAD_FAILED",
                "payload": {
                    "error": "remote write failed",
                },
            }
        )

        result = await uploader.upload()

        assert result is False

        sent_messages = drain_queue(ws_send_queue)
        assert sent_messages[0]["command"] == "TRANSFER_INIT"
        assert sent_messages[-1]["command"] == "TRANSFER_COMPLETE"
        assert db_write_queue.empty()

    asyncio.run(run_test())


@pytest.mark.xfail(
    reason=(
            "Current bug: empty files skip TRANSFER_INIT but upload() still waits "
            "for TRANSFER_ACCEPT. Apply the empty-file upload() fix to make this pass."
    )
)
def test_upload_empty_file_returns_true_without_waiting_for_acceptance(tmp_path):
    """
    Intent:
    Capture the current empty-file bug.

    Expected behavior:
    Empty files should be skipped successfully without waiting for remote
    acceptance, because no TRANSFER_INIT message is sent.

    Current behavior:
    upload() calls _wait_for_acceptance() anyway.
    """

    async def run_test():
        uploader, ws_send_queue, db_write_queue = make_uploader(
            tmp_path,
            file_bytes=b"",
        )

        uploader._wait_for_acceptance = AsyncMock(return_value=False)

        result = await uploader.upload()

        assert result is True
        uploader._wait_for_acceptance.assert_not_called()
        assert ws_send_queue.empty()
        assert db_write_queue.empty()

    asyncio.run(run_test())


@pytest.mark.xfail(
    reason=(
            "Current bug: _send_chunks() always sends is_last=False. "
            "Apply the is_last calculation fix to make this pass."
    )
)
def test_send_chunks_marks_final_chunk_as_last(tmp_path):
    """
    Intent:
    Capture the current legacy _send_chunks() bug.

    resume() uses _send_chunks(), so the final chunk should be marked with
    is_last=True. Currently every chunk is sent with is_last=False.
    """

    async def run_test():
        uploader, ws_send_queue, _ = make_uploader(
            tmp_path,
            file_bytes=b"abcd",
            chunk_size=2,
        )

        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "bytes_received": 2,
                },
            }
        )
        await uploader.handle_response(
            {
                "command": "CHUNK_ACK",
                "payload": {
                    "bytes_received": 4,
                },
            }
        )

        result = await uploader._send_chunks()

        assert result is True

        frames = drain_queue(ws_send_queue)
        assert len(frames) == 2
        assert frames[0]["header"]["is_last"] is False
        assert frames[1]["header"]["is_last"] is True

    asyncio.run(run_test())
