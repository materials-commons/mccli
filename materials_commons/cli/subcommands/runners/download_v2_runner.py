import logging
from pathlib import Path
from pprint import pprint

from igittigitt import igittigitt

from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.requests import DownloadRequest
from materials_commons.cli.server.service_container import ServiceContainer
from materials_commons.cli.server.service_runtime import ServiceRuntime
from materials_commons.cli.walk import make_merged_listdir_func

logger = logging.getLogger(__name__)


async def run_v2_download(args, working_dir):
    proj = LocalProject.load(working_dir)

    # Initialize database
    db = await proj.get_filedb()

    # Setup ignore parser
    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)

    # Start services
    container = ServiceContainer.create()
    service_runtime = ServiceRuntime(container)
    await service_runtime.start(db_manager=True, file_download_manager=True)
    async_reconciler = AsyncReconciler(db=db, proj=proj, reconcile_mode="download")
    listdir_fn = make_merged_listdir_func(proj)
    try:
        transfer_ids = []
        for path in args.paths:
            p = Path(path)
            if p.is_dir():
                async for current_path, path_entries in async_reconciler.walk(path=path, listdir_fn=listdir_fn,
                                                                              recursive=args.recursive, ignore_fn=None):
                    for entry_name in sorted(path_entries):
                        file_state = path_entries[entry_name]
                        if file_state.exception:
                            logger.error(f"Error encountered while processing {entry_name}: {file_state.exception}")
                            print(f"Error encountered while processing {p}: {file_state.exception}")
                            continue
                        if file_state.file_decision.action == "download":
                            download_request = DownloadRequest(observation=file_state.observation,
                                                               updated_record=file_state.file_decision.updated_record,
                                                               project=proj)
                            await container.file_download_manager.download_file(download_request)
            elif p.is_file():
                file_state = await async_reconciler.reconcile_file(p)
                if file_state.exception:
                    logger.error(f"Error encountered while processing {p}: {file_state.exception}")
                    print(f"Error encountered while processing {p}: {file_state.exception}")
                    continue
                if file_state.file_decision.action == "download":
                    download_request = DownloadRequest(observation=file_state.observation,
                                                       updated_record=file_state.file_decision.updated_record,
                                                       project=proj)
                    transfer_id = await container.file_download_manager.download_file(download_request)
                    transfer_ids.append(transfer_id)
            else:
                remote_path = proj.to_remote_path(p)
                try:
                    f = proj.remote.get_file_by_path(proj.id, remote_path.as_posix())
                except Exception as e:
                    # logger.error(f"Error getting file by path {remote_path}: {e}")
                    print(f"No such remote file {p}")
                    continue
                if f is None:
                    continue
                if f.mime_type == "directory":
                    pass
                else:
                    file_state = await async_reconciler.reconcile_file(p)
                    if file_state.exception:
                        logger.error(f"Error encountered while processing {p}: {file_state.exception}")
                        print(f"Error encountered while processing {p}: {file_state.exception}")
                        continue
                    if file_state.file_decision.action == "download":
                        print(f"Downloading file {p} from remote path {remote_path}")
                        download_request = DownloadRequest(observation=file_state.observation,
                                                           updated_record=file_state.file_decision.updated_record,
                                                           project=proj)
                        transfer_id = await container.file_download_manager.download_file(download_request)
                        transfer_ids.append(transfer_id)
                    else:
                        print(f"{file_state.file_decision.action} for {p}")
    except Exception as e:
        logger.error(f"Unexpected error during download: {e}")
        return False

    finally:
        await container.file_download_manager.wait_all()
        await service_runtime.stop()
        await db.close()

