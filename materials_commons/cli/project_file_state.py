import os


def remove_ignored_dirs(root, dirs):
    pass


def remove_unknown_dirs(root, dirs):
    pass


# Process unknown dirs and remove them from the list of dirs to traverse
def process_unknown_dirs(root, dirs):
    pass


def file_is_ignored(proj_path):
    return False


def file_is_unknown(proj_path):
    return False


def file_is_in_conflict(proj_path):
    return False


def file_can_be_upload(proj_path, root, file):
    if file_is_added(proj_path):
        return True

    si = os.stat(os.path.join(root, file))

    if file_has_changed(si, proj_path, root, file):
        return True

    return False


def file_is_added(proj_path):
    return False


def file_has_changed(si, proj_path, root, file):
    return True


def upload_file(proj_path, root, file):
    pass


def download_dir(root, dir):
    pass