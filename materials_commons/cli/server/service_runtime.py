from materials_commons.cli.server.service_container import ServiceContainer


class ServiceRuntime:
    def __init__(self, container: ServiceContainer):
        self.container = container
        self._started_local_rest_server = False
        self._started_file_upload_manager = False
        self._started_file_download_manager = False
        self._started_file_index_manager = False
        self._started_db_manager = False
        self._started_websocket_listener = False

    async def start(
            self,
            *,
            local_rest_server: bool = False,
            file_upload_manager: bool = False,
            file_download_manager: bool = False,
            file_index_manager: bool = False,
            db_manager: bool = False,
            websocket_listener: bool = False,
    ) -> None:
        if websocket_listener:
            local_rest_server = True
            file_upload_manager = True
            file_download_manager = True
            file_index_manager = True
            db_manager = True

        if file_upload_manager:
            db_manager = True

        if db_manager and not self._started_db_manager:
            await self.container.db_manager.start_workers()
            self._started_db_manager = True

        if file_upload_manager and not self._started_file_upload_manager:
            await self.container.file_upload_manager.start_workers()
            self._started_file_upload_manager = True

        if file_download_manager and not self._started_file_download_manager:
            await self.container.file_download_manager.start_workers()
            self._started_file_download_manager = True

        if file_index_manager and not self._started_file_index_manager:
            await self.container.file_index_manager.start_workers()
            self._started_file_index_manager = True

        if local_rest_server and not self._started_local_rest_server:
            self.container.local_rest_server.start()
            self._started_local_rest_server = True

        if websocket_listener and not self._started_websocket_listener:
            self.container.websocket_listener.start()
            self._started_websocket_listener = True

    async def drain(self) -> None:
        if self._started_file_index_manager:
            await self.container.file_index_queue.join()
        if self._started_db_manager:
            await self.container.db_queue.join()

    async def stop(self, *, drain: bool = False) -> None:
        if self._started_websocket_listener:
            await self.container.websocket_listener.stop()
            self._started_websocket_listener = False

        if self._started_local_rest_server:
            self.container.local_rest_server.stop()
            self._started_local_rest_server = False

        if drain:
            await self.drain()

        if self._started_file_upload_manager:
            await self.container.file_upload_manager.stop_workers()
            self._started_file_upload_manager = False

        if self._started_file_download_manager:
            await self.container.file_download_manager.stop_workers()
            self._started_file_download_manager = False

        if self._started_file_index_manager:
            await self.container.file_index_manager.stop_workers()
            self._started_file_index_manager = False

        if self._started_db_manager:
            await self.container.db_manager.stop_workers()
            self._started_db_manager = False

    async def __aenter__(self) -> "ServiceRuntime":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.stop()
