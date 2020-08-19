import os
import sqlite3
import time
import warnings

from tabulate import tabulate

import materials_commons.api as mcapi
import materials_commons.cli.exceptions as cliexcept
import materials_commons.cli.functions as clifuncs
import materials_commons.cli.file_functions as filefuncs
from materials_commons.cli.sqltable import SqlTable, sql_iter


# tree:
# path, name, parent_path, mtime, size, checksum, otype, id

def do_with_queue(f, queue):
    while len(queue):
        f(queue.pop(), queue)

def _print_children_table(all_children, treename='remote'):
    headers = ['path', 'name', treename, 'db']
    print_data = {key:[] for key in headers}

    for path in all_children:
        remote_exists = ('tree' in all_children[path])
        db_exists = ('db' in all_children[path])
        name = all_children
        if remote_exists:
            name = all_children[path]['tree']['name']
        else:
            name = all_children[path]['db']['name']

        print_data['path'].append(path)
        print_data['name'].append(name)
        print_data[treename].append(remote_exists)
        print_data['db'].append(db_exists)

    print('Children:')
    print(tabulate(print_data, headers=headers))
    print()

class TreeTable(SqlTable):
    """Store information on files and directories in a tree

    This is a base class. Derived classes must implement:

        - @staticmethod default_print_fmt(): list of tuple, see an example
        - @staticmethod treename(): str, tree label for printing ('remote', 'local')
        - @staticmethod tablename(): str, tree table name in sqlite database ('remotetree', 'localtree')
        - needs_update(self, existing): bool, check if record needs updating
        - _check(self, path, get_children=True, parent_path=None): (dir, children), see an example

    Values:
        path: str, path including project directory
        name: str, file or directory name
        parent_path: str, path of parent directory (or None for 'top')
        otype: str, 'file' or 'directory'
        mtime: real, file or directory modify time (s since epoch)
        checktime: real, last time the record was updated (s since epoch)
        size: integer, file size
        checksum: str, md5 hash
        id: str, ID string, depends on type of tree. For `RemoteTree` this is the Materials Commons
            id. For `LocalTree` this is None.
        parent_id: str, ID string, depends on type of tree. For `RemoteTree` this is the Materials
            Commons id. For `LocalTree` this is None.

    """

    # column name: ([type, (optional) constraints], fmt)
    @staticmethod
    def tablecolumns():
        return {
            "id": ["text"],
            "parent_id": ["text"],
            "name": ["text"],
            "path": ["text", "UNIQUE"],
            "parent_path": ["text"],
            "mtime": ["real"],
            "size": ["integer"],
            "checksum": ["text"],
            "otype": ["text"],         # "file" or "directory"
            "checktime": ["real"]     # last time the remote data was queried (s since epoch)
        }

    def __init__(self, proj_local_path):
        """

        Arguments:
            proj_local_path: str
                Path to Materials Commons project directory, for storing sqlite db file

        """
        super(TreeTable, self).__init__(proj_local_path)

    def _delete_one_by_path(self, path, verbose=False):
        """Delete one record by path"""
        if verbose:
            print('Deleting ' + path + ' from db... ', end='')
        self.curs.execute("DELETE FROM " + self.tablename() + " WHERE path=?", (path,))
        self.conn.commit()
        if verbose:
            print('DONE')

    def _delete_recurs_by_path(self, path, verbose=False):
        """Delete record by path, and children recursively"""
        def f(path, queue):
            self._delete_one_by_path(path, verbose=verbose)
            for record in self.select_by_parent_path(path):
                queue.add(record["path"])
        queue = set([path])
        do_with_queue(f, queue)

    def delete_by_path(self, path, recurs=False, verbose=False):
        """Delete individual entry by path

        Arguments:
            path: str
                The Materials Commons path of the file_or_dir to delete.
            recurs: bool
                If True, delete children recursively
            verbose: bool
                If True, print status.
        """
        if recurs:
            self._delete_recurs_by_path(path, verbose=verbose)
        else:
            self._delete_one_by_path(path, verbose=verbose)

    def update(self, path, get_children=True, recurs=False, verbose=False, force=False):
        """Update tree table to accurately reflect a particular directory

        Arguments:
            path: str
                Materials Commons path to the directory to update (includes project directory).
            get_children: bool
                If True, also update children files and directories.
            recurs: bool
                If True, updating children is done recursively.
            verbose: bool
                If True, print status.
        """
        if verbose:
            print(path, end='')

        existing = None
        res = self.select_by_path(path)
        if len(res) == 0:
            pass
        elif len(res) == 1:
            existing = res[0]
            if not force and not self.needs_update(existing):
                return
        elif len(res) > 1:
            raise cliexcept.MCCLIException("Error in TableTree.update: >1 record for path")

        checktime = time.time()
        file_or_dir, children  = self._check(path, checktime=checktime, get_children=get_children)

        if verbose:
            if file_or_dir is None:
                print(' -> Does not exist in tree')
            else:
                print(' -> Found')

        # insert / replace / remove
        # if the file or directory does not exist anymore, remove it recursively
        if file_or_dir is None:
            if existing:
                self.delete_by_path(existing['path'], recurs=True, verbose=verbose)

            # NOTE: in some cases this might avoid re-checking paths that have been found not to
            #   exist, but it might not be the best way to do this
            self.insert_non_existent(path, checktime=checktime, verbose=verbose)
            return
        else:
            self.insert_or_replace(file_or_dir, verbose=verbose)

        # check children, optionally
        if get_children:

            # children that only exist in the database must be deleted,
            # those that exist outside the database should be inserted or replaced
            all_children = {}
            def _insert_child(child, category):
                if child['path'] not in all_children:
                    all_children[child['path']] = dict()
                all_children[child['path']][category] = child
            for child in children:
                _insert_child(child, 'tree')
            for child in self.select_by_parent_path(file_or_dir['path']):
                _insert_child(child, 'db')

            # print table of children
            if verbose:
                _print_children_table(all_children, self.treename())

            for path in all_children:

                child = all_children[path].get('tree', None)

                # if child does not exist in tree, then delete it
                if child is None:
                    self.delete_by_path(path, recurs=True)

                # if child is a file that exists in tree
                elif child['otype'] == 'file':
                    self.insert_or_replace(child, verbose=verbose)

                # elif child is a directory that exists remotely
                elif child['otype'] == 'directory':
                    if recurs:
                        # if not recursive, do not update other next children, so children=recurs
                        self.update(child['path'], get_children=recurs, recurs=recurs, verbose=verbose, force=force)
                    else:
                        self.insert_or_replace(child, verbose=verbose)

                else:
                    raise Exception("TreeTable.update error: Unknown error updating children")


        return

    def select_all(self, fetchsize=1000):
        """Select all records

        Yields:
             sqlite3.Row or None, if no records left
        """
        self.curs.execute("SELECT * FROM " + self.tablename())
        for r in sql_iter(self.curs, fetchsize=fetchsize):   #pylint: disable=invalid-name
            yield r

    def select_by_path(self, path):
        """Select records by path

        Returns:
             List of sqlite3.Row
        """
        self.curs.execute("SELECT * FROM " + self.tablename() + " WHERE path=?", (path, ))
        return self.curs.fetchall()

    def select_by_id(self, id):
        """Select record by id

        Returns:
             sqlite3.Row or None
        """
        self.curs.execute("SELECT * FROM " + self.tablename() + " WHERE id=?", (id, ))
        return self.curs.fetchone()

    def select_by_parent_path(self, parent_path):
        """Select records by parent_path

        Returns:
             List of sqlite3.Row
        """
        self.curs.execute("SELECT * FROM " + self.tablename() + " WHERE parent_path=?", (parent_path, ))
        return self.curs.fetchall()

    def select_by_parent_id(self, parent_id):
        """Select records by parent_id

        Returns:
             List of sqlite3.Row
        """
        self.curs.execute("SELECT * FROM " + self.tablename() + " WHERE parent_id=?", (parent_path, ))
        return self.curs.fetchall()

    def _walk_results(self, path):
        results = self.select_by_path(path)

        if not len(results) == 1:
            raise cliexcept.MCCLIException("TreeTable.walk error: multiple records for path '" + path + "'")

        top = results[0]
        if not top['otype'] == 'directory':
            raise cliexcept.MCCLIException("TreeTable.walk error: '" + path + "' is not a directory")

        children = self.select_by_parent_path(top['path'])
        dirs = [x for x in children if x['otype'] == 'directory']
        files = [x for x in children if x['otype'] == 'file']

        return (top, dirs, files)

    def walk(self, path, topdown=True):
        """Yields (parent, dirs, files) records recursively starting from directory at `path`"""

        (root, dirs, files) = self._walk_results(path)

        if topdown:
            yield (root, dirs, files)

        for dir in dirs:
            yield self.walk(dir['path'], topdown=topdown)

        if not topdown:
            yield (root, dirs, files)

class RemoteTree(TreeTable):
    """Store information on files and directories in the Materials Commons project

    Values:
        id: str, Materials Commons id string
        parent_id: str, Materials Commons id string
        path: str, path including project directory
        name: str, file or directory name
        otype: str, 'file' or 'directory'
        mtime: real, file or directory modify time (s since epoch)
        checktime: real, last time the record was updated (s since epoch)
        size: integer, file size
        checksum: str, md5 hash
        parent_path: str, path of parent directory (or none for 'top')
    """

    @staticmethod
    def default_print_fmt():
        from materials_commons.cli.functions import as_is, format_time, humanize
        # (key, header, fmt, size, function)
        return [
            ("path", "path","<", 80, as_is),
            ("name", "name", "<", 24, as_is),
            ("otype", "otype", "<", 16, as_is),
            ("mtime", "mtime", "<", 24, format_time),
            ("checktime", "checktime", "<", 24, format_time),
            ("size", "size", "<", 8, humanize),
            ("checksum", "checksum", "<", 36, as_is),
            ("id", "id", "<", 36, as_is),
            ("parent_id", "parent_id", "<", 36, as_is),
            ("parent_path", "parent_path", "<", 80, as_is)
        ]

    @staticmethod
    def treename():
        return "remote"

    @staticmethod
    def tablename():
        return "remotetree"

    def __init__(self, proj, updatetime):
        super(RemoteTree, self).__init__(proj.local_path)
        self.updatetime = updatetime
        self.proj = proj

    def needs_update(self, existing):
        if not existing['checktime']:
            return True
        if self.updatetime and existing['checktime'] > self.updatetime:
            return False
        return True

    def insert_non_existent(self, path, checktime=None, verbose=False):
        """Do insert non-existent"""

        if path == self.proj.name:
            parent_path = None
        else:
            parent_path = os.path.dirname(path)

        if checktime is None:
            checktime = time.time()
        checktime = checktime

        record = {
            "path":path,
            "name": os.path.basename(path),
            "parent_path": parent_path,
            "checktime": checktime
        }
        self.insert_or_replace(record, verbose=verbose)
        return

    def _make_record(self, file_or_dir, checktime):
        """Make a record dict from a mcapi.File instance

        Arguments:
            file_or_dir: mcapi.File
                The object to be inserted.
            checktime: float
                Time the API call to check the object was made (s since the epoch).

        Returns:
            record: dict, suitable for database insertion
        """
        record = {}
        for key in self.tablecolumns():
            if key == "id":
                if file_or_dir.id:
                    record["id"] = str(file_or_dir.id)
            elif key == "parent_id":
                if file_or_dir.directory_id:
                    record["parent_id"] = str(file_or_dir.directory_id)
            elif key == "checktime":
                record["checktime"] = checktime
            elif key == "mtime":
                record["mtime"] = clifuncs.epoch_time(file_or_dir.updated_at)
            elif key == "path":
                record["path"] = file_or_dir.path
            elif key == "parent_path":
                parent_path = os.path.dirname(file_or_dir.path)
                if parent_path != file_or_dir.path:
                    record["parent_path"] = parent_path
            elif key == "otype":
                if filefuncs.isfile(file_or_dir):
                    record["otype"] = "file"
                elif filefuncs.isdir(file_or_dir):
                    record["otype"] = "directory"
                else:
                    raise cliexcept.MCCLIException("Invalid file_or_dir type: " + str(type(file_or_dir)))
            elif key not in file_or_dir._data:
                continue
            elif file_or_dir._data[key] is None:
                continue
            elif isinstance(file_or_dir._data[key], str) and not file_or_dir._data[key]:
                continue
            else:
                record[key] = file_or_dir._data[key]
        return record

    def _check(self, path, checktime=None, get_children=True):
        """Get current status of file or directory at path

        Arguments:
            path: str
                Materials Commons path to update.
            get_children: boolean
                If True, also get children.

        Returns:
            (file_or_dir, children):

                file_or_dir: dict, or None
                    None if File or Directory not found on server. Else, record representing the file or directory.
                children: List of dict, or None
                    None if children not requested or is file, else records representing each child.

        """
        file_or_dir_obj = None
        file_or_dir = None
        children = None

        if checktime is None:
            checktime = time.time()
        checktime = checktime

        if isinstance(path, str):
            try:

                file_or_dir_obj = filefuncs.get_by_path_if_exists(self.proj.remote, self.proj.id, path)
                if file_or_dir_obj is None:
                    return (file_or_dir, children)
                if file_or_dir_obj.path is None:
                    file_or_dir_obj.path = path
                file_or_dir = self._make_record(file_or_dir_obj, checktime)
            except cliexcept.MCCLIException:
                pass
        else:
            raise cliexcept.MCCLIException("Error getting Directory: 'path' is of type '" + str(type(path)) + "'")

        if get_children:
            children = []
            if file_or_dir_obj is not None and filefuncs.isdir(file_or_dir_obj):
                for child in self.proj.remote.list_directory(self.proj.id, file_or_dir_obj.id):
                    # TODO: child does not have 'size' or 'checksum'
                    _checktime = checktime
                    if filefuncs.isdir(child):
                        child = self.proj.remote.get_directory(self.proj.id, child.id) # TODO: remove this
                        _checktime = None
                    if filefuncs.isfile(child):
                        child = self.proj.remote.get_file(self.proj.id, child.id) # TODO: remove this
                        child.path = os.path.join(path, child.name)
                    children.append(self._make_record(child, _checktime))

        return (file_or_dir, children)

class LocalTree(TreeTable):
    """Store information on files and directories in the working tree

    Values:
        id: None, (always None)             # TODO: clean up 'id', 'parent_id', 'parent_path'
        parent_id: None, (always None)
        path: str, relative path including project directory (matches Materials Commons 'path')
        name: str, file or directory name
        otype: str, 'file' or 'directory'
        mtime: real, file or directory modify time (s since epoch)
        checktime: real, last time the record was updated (s since epoch)
        size: integer, file size
        checksum: str, md5 hash
        parent_path: str, path of parent directory (or none for 'top')

    """

    @staticmethod
    def default_print_fmt():
        from materials_commons.cli.functions import as_is, format_time, humanize
        # (key, header, fmt, size, function)
        return [
            ("path", "path","<", 80, as_is),
            ("name", "name", "<", 24, as_is),
            ("otype", "otype", "<", 16, as_is),
            ("mtime", "mtime", "<", 24, format_time),
            ("checktime", "checktime", "<", 24, format_time),
            ("size", "size", "<", 8, humanize),
            ("checksum", "checksum", "<", 36, as_is),
            ("id", "id", "<", 80, as_is),
            ("parent_path", "parent_path", "<", 80, as_is)
        ]

    @staticmethod
    def treename():
        return "local"

    @staticmethod
    def tablename():
        return "localtree"

    def __init__(self, proj_local_path):
        super(LocalTree, self).__init__(proj_local_path)
        self.proj_local_path = proj_local_path

    def needs_update(self, existing):
        path = existing['path']
        if not os.path.exists(path):
            return True
        if os.path.getmtime(path) != existing['mtime']:
            return True
        return False

    def insert_non_existent(self, path, checktime=None, verbose=False):
        """Do not insert non-existent"""
        return

    def _make_record(self, local_abspath, checktime):
        """Make a record dict for a local path

        Arguments:
            local_abspath: str
                Absolute path to file or directory
            checktime: float
                Time the API call to check the object was made (s since the epoch).

        Returns:
            record: dict, suitable for database insertion
        """
        record = {}
        if os.path.isdir(local_abspath):
            record['otype'] = 'directory'
        elif os.path.isfile(local_abspath):
            record['otype'] = 'file'
            record['checksum'] = clifuncs.checksum(local_abspath)
            record['size'] = os.path.getsize(local_abspath)
        else:
            raise cliexcept.MCCLIException("LocalTree._make_record error: 'local_abspath'='" + local_abspath + "' is not a file or directory.")

        record['path'] = filefuncs.make_mcpath(self.proj_local_path, local_abspath)
        if local_abspath == self.proj_local_path:
            record['parent_path'] = None
            record['name'] = "/"
        else:
            record['parent_path'] = os.path.dirname(record['path'])
            record['name'] = os.path.basename(record['path'])

        record['mtime'] = os.path.getmtime(local_abspath)
        record['checktime'] = checktime

        return record

    def _check(self, path, checktime=None, get_children=True):
        """Get current status of a directory

        Arguments:
            path: str
                Materials Commons path to update.
            checktime: float
                When the status is being checked
            get_children: boolean
                If True, also get children.

        Returns:
            (dir, children):

                dir: dict, or None
                    None if Directory not found on server. Else, record representing the directory.
                children: List of dict, or None
                    None if children not requested, else records representing each child.

        """
        file_or_dir = None
        children = None
        local_abspath = filefuncs.make_local_abspath(self.proj_local_path, path)

        if checktime is None:
            checktime = time.time()
        checktime = checktime

        if isinstance(local_abspath, str):
            try:
                if not os.path.exists(local_abspath):
                    return (file_or_dir, children)

                file_or_dir = self._make_record(local_abspath, checktime)
            except cliexcept.MCCLIException:
                pass
        else:
            raise cliexcept.MCCLIException("LocalTree._check error: 'path' is of type '" + str(type(path)) + "'")

        if get_children:
            children = []
            if os.path.isdir(local_abspath):
                for child in os.listdir(local_abspath):
                    if child == ".mc":
                        continue
                    children.append(self._make_record(os.path.join(local_abspath, child), checktime))

        return (file_or_dir, children)
