import stat, os


def is_dir(finfo):
    return stat.S_ISDIR(finfo.st_mode)


def is_file(finfo):
    return stat.S_ISREG(finfo.st_mode)


def do_stat(path):
    try:
        finfo = os.stat(path)
    except (OSError, ValueError):
        return None
    return finfo


def file_exists(finfo):
    return finfo is not None
