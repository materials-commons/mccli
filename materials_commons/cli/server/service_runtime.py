from materials_commons.cli.server.service_container import ServiceContainer


class ServiceRuntime:
    def __init__(self, container: ServiceContainer):
        self.container = container
        self._started_local_rest_server = False
        self._started_file_upload_manager = False
        self._started_file_download_manager = False
        self._started_db_manager = False

    async def start(
        self,
        *,
        local_rest_server: bool = False,
        file_upload_manager: bool = False,
        file_download_manager: bool = False,
        db_manager: bool = False,
    ) -> None:
        if db_manager:
            await self.container.db_manager.start_workers()
            self._started_db_manager = True

        if file_upload_manager:
            if not self._started_db_manager:
                await self.container.db_manager.start_workers()
                self._started_db_manager = True
            await self.container.file_upload_manager.start_workers()
            self._started_file_upload_manager = True

        if file_download_manager:
            await self.container.file_download_manager.start_workers()
            self._started_file_download_manager = True

        if local_rest_server:
            self.container.local_rest_server.start()
            self._started_local_rest_server = True

    async def stop(self) -> None:
        if self._started_local_rest_server:
            self.container.local_rest_server.stop()
            self._started_local_rest_server = False

        if self._started_file_upload_manager:
            await self.container.file_upload_manager.stop_workers()
            self._started_file_upload_manager = False

        if self._started_file_download_manager:
            await self.container.file_download_manager.stop_workers()
            self._started_file_download_manager = False

        if self._started_db_manager:
            await self.container.db_manager.stop_workers()
            self._started_db_manager = False

    async def __aenter__(self) -> "ServiceRuntime":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.stop()