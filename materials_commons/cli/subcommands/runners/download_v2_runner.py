from materials_commons.cli.downloader import Downloader
from materials_commons.cli.filedb import FileIndexDB, to_project_db_path

from materials_commons.cli.server import projects
from materials_commons.cli.user_config import Config


async def run_v2_download(args, working_dir):
    proj = await projects.get_local_project(working_dir)
    db = await FileIndexDB.create(to_project_db_path(proj.local_path))
    if proj is None:
        print("Error: Not in a Materials Commons project directory")
        return

    config = Config()

    if args.force and args.recursive:
        print(
            "Warning: --force and --recursive are not compatible. You may only force download individual files, not directories.")
        return

    downloader = Downloader(proj=proj, db=db, config=config, force_download=args.force)
    await downloader.start_workers()

    for path in args.paths:
        is_dir = await projects.is_dir(db, proj, path)
        if is_dir:
            if args.force:
                print(f"Warning: --force is not compatible with directories. Skipping {path}")
                continue
            await downloader.download_dir(path, recursive=args.recursive, ignore_fn=None)
        else:
            await downloader.download_file(path)

    await downloader.stop_workers()
