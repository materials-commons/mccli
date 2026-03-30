import asyncio

from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.user_config import Config


class DownloadCmdRunner:

    def __init__(self, db_queue: asyncio.Queue, send_queue: asyncio.Queue, project_dbs: ProjectFileDBs, config: Config):
        self.db_queue = db_queue
        self.send_queue = send_queue
        self.project_dbs = project_dbs
        self.config = config
        self.file_download_manager = FileDownloadManager(send_queue=self.send_queue,)