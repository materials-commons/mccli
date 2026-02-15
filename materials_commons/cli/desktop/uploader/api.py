import asyncio
import logging
from typing import List, Optional
from materials_commons.cli.desktop.websocket_server import WebSocketCommandListener
from materials_commons.cli.desktop.command_handlers import register_handlers
from materials_commons.cli.user_config import Config
import ssl
import websockets
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK, InvalidStatus

logger = logging.getLogger(__name__)


async def ws_upload(
        file_paths: List[str],
        project_paths: List[str],
        project_id: int,
        ws_url: str = "wss://materialscommons.org/ws",
        chunk_size: int = 1024 * 1024,
        max_concurrent: int = 3
) -> bool:
    """
        Upload files using websocket infrastructure, then exit.

        Args:
            file_paths: List of local file paths to upload
            project_paths: List of corresponding project paths (MC server paths)
            project_id: Materials Commons project ID
            ws_url: WebSocket URL for server connection
            chunk_size: Size of chunks for upload (default 1MB)
            max_concurrent: Maximum concurrent uploads (default 3)

        Returns:
            True if all uploads succeeded, False otherwise
    """
    config = Config()
    queue = asyncio.Queue()

    # Create websocket listener
    listener = WebSocketCommandListener(
        ws_url=ws_url,
        token=config.default_remote.mcapikey,
        client_uuid=config.client_uuid,
        handlers=register_handlers(),
        queue=queue,
        max_concurrent=max_concurrent
    )

    # Start upload workers
    upload_workers = await listener.file_transfer_manager.start_workers()

    # Setup SSL context
    ssl_context = ssl.create_default_context()
    ssl_context.check_hostname = False
    ssl_context.verify_mode = ssl.CERT_NONE

    # build headers that are used for authentication
    headers = listener.build_headers()

    try:
        # Connect to websocket server. This is a one-shot connection. We will disconnect
        # once all uploads are complete.
        logger.info("Connecting to websocket server...")

        async with websockets.connect(
                ws_url,
                additional_headers=headers,
                open_timeout=15,
                ssl=ssl_context
        ) as ws:
            # Start sender and receiver tasks
            sender_task = asyncio.create_task(listener._ws_sender_loop(ws))
            receiver_task = asyncio.create_task(listener._ws_receiver_loop(ws))

            # Now that connection is established, and sender/receiver are running, queue uploads
            logger.info("Connection established, queueing uploads...")

            transfer_ids = []
            for file_path, project_path in zip(file_paths, project_paths):
                transfer_id = await listener.file_transfer_manager.upload_file(
                    file_path=file_path,
                    project_id=project_id,
                    project_path=project_path,
                    chunk_size=chunk_size,
                    progress_callback=lambda sent, total, fp=file_path: logger.info(f"\r{fp}: {sent}/{total} bytes")
                )
                transfer_ids.append(transfer_id)

            logger.info(f"Queued {len(transfer_ids)} files for upload")

            # Wait for all uploads to complete
            logger.info("Waiting for uploads to complete...")
            await listener.file_transfer_manager.wait_all()

            # Cancel sender/receiver tasks
            sender_task.cancel()
            receiver_task.cancel()
            try:
                await sender_task
            except asyncio.CancelledError:
                pass
            try:
                await receiver_task
            except asyncio.CancelledError:
                pass

            # Check results for each upload
            for transfer_id in transfer_ids:
                success = listener.file_transfer_manager.results.get(transfer_id, False)
                if not success:
                    # At least one upload failed so return False
                    return False

            # If we are here then all uploads succeeded
            return True

    except (ConnectionClosedOK, ConnectionClosedError, InvalidStatus, OSError, asyncio.TimeoutError) as e:
        logger.error(f"WebSocket connection failed: {e}")
        return False

    finally:
        # Shutdown gracefully
        logger.info("Shutting down websocket infrastructure...")

        # Stop listening on websocket
        await listener.shutdown()

        # Stop upload workers
        for worker in upload_workers:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                # Ignore errors from canceled workers. We are exiting anyway.
                pass


def ws_upload_synchronous(
        file_paths: List[str],
        project_paths: List[str],
        project_id: int,
        ws_url: str = "wss://materialscommons.org/ws",
        chunk_size: int = 1024 * 1024,
        max_concurrent: int = 3) -> bool:
    """
    Uploads files to a specified server synchronously by making use of an asynchronous upload function.

    This function wraps an asynchronous upload operation into a synchronous context by using asyncio's
    event loop. In case of errors such as keyboard interruptions or unexpected issues
    during the upload, the function gracefully handles them by returning a failure state.

    Parameters:
        file_paths (List[str]): A list of local file paths to be uploaded.
        project_paths (List[str]): A list of corresponding project paths for the files being uploaded.
        project_id (int): The unique identifier of the project associated with the upload.
        ws_url (str): The WebSocket URL used for the upload connection. Defaults to "wss://materialscommons.org/ws".
        chunk_size (int): The size of file chunks to be uploaded concurrently. Defaults to 1 MiB.
        max_concurrent (int): The maximum number of concurrent uploads allowed. Defaults to 3.

    Returns:
        bool: True if the upload was successful, False otherwise.
    """
    try:
        return asyncio.run(
            ws_upload(
                file_paths=file_paths,
                project_paths=project_paths,
                project_id=project_id,
                ws_url=ws_url,
                chunk_size=chunk_size,
                max_concurrent=max_concurrent
            )
        )
    except KeyboardInterrupt:
        return False
    except Exception as e:
        logger.error(f"Unexpected error during upload: {e}")
        return False
