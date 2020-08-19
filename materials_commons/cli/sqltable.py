import os
import re
import sqlite3
import warnings

from materials_commons.cli.print_formatter import PrintFormatter

def dbpath(proj_local_path):
    """Location of a sqlite database to cache project data locally"""
    return os.path.join(proj_local_path, ".mc", "project.db")

def sql_iter(curs, fetchsize=1000):
    """ Iterate over the results of a SELECT statement """
    while True:
        records = curs.fetchmany(fetchsize)
        if not records:
            break
        else:
            for r in records:
                yield r

class SqlTable(object):
    """Generic code for interacting with one table in a project database

    This is a base class. Derived classes must implement:

        - @staticmethod default_print_fmt(): list of tuple, see an example
        - @staticmethod tablecolumns(): dict, column name as key, list of table creation args for value
        - @staticmethod tablename(): str, table name in sqlite database

    """

    # Example 'tablecolumns':
    #
    # @staticmethod
    # def tablecolumns():
    #     return {
    #         "id": ["text"],
    #         "name": ["text"],
    #         "path": ["text", "UNIQUE"],
    #         "parent_id": ["text"],
    #         "modified_at": ["real"],
    #         "size": ["integer"],
    #         "checksum": ["text"],
    #         "otype": ["text"],
    #         "checktime": ["real"]
    #     }

    # Example 'default_print_fmt':
    #
    # @staticmethod
    # def default_print_fmt():
    #     from materials_commons.cli.functions import as_is, format_time, humanize
    #     # (key, header, fmt, size, function)
    #     return [
    #         ("path", "path","<", 80, as_is),
    #         ("name", "name", "<", 24, as_is),
    #         ("otype", "otype", "<", 16, as_is),
    #         ("modified_at", "modified_at", "<", 24, format_time),
    #         ("checktime", "checktime", "<", 24, format_time),
    #         ("size", "size", "<", 8, humanize),
    #         ("checksum", "checksum", "<", 36, as_is),
    #         ("id", "id", "<", 80, as_is),
    #         ("parent_id", "parent_id", "<", 80, as_is)
    #     ]

    @staticmethod
    def _sql_create_table_str(columns):
        """Returns a string for CREATE TABLE"""
        s = "("
        for key, value in columns.items():
            s += " ".join([key] + value) + ", "
        return s[:-2] + ")"

    @staticmethod
    def _sql_insert_or_replace_str(record):
        """Returns a string for INSERT OR REPLACE INTO

        Arguments:
            record: dict
                The record to insert or replace into the database.

        Returns:
            (colstr, questionstr, valtuple)
                These values are appropriate to be used as:

                    (colstr, questionstr, valtuple) = sql_insert_str(file_or_dir_data)
                    insertstr = "INSERT OR REPLACE INTO <table> {0} VALUES {1}".format(colstr, questionstr)
                    self.curs.execute(insertstr, valtuple)

        """
        # if "id" not in record:
        #     raise Exception("Error constructing INSERT OR REPLACE INTO statement: no 'id'")

        colstr = "("
        questionstr = "("
        val = []
        for key, value in record.items():
            colstr = colstr + key + ", "
            questionstr = questionstr + "?, "
            val.append(value)
        colstr = colstr[:-2] + ")"
        questionstr = questionstr[:-2] + ")"
        return colstr, questionstr, tuple(val)

    @staticmethod
    def _regexp(pattern, string):
        """ Regexp to bool wrapper"""
        return re.match(pattern, string) is not None

    def __init__(self, proj_local_path):
        """

        Arguments:
            proj_local_path: str, local project path

        """
        self.dbpath = dbpath(proj_local_path)
        self.connect()
        self._create_table()
        self.close()

    def connect(self):
        """Connect to sqlite database, creating database and table if necessary"""

        # print("Connect to:", self.dbpath)
        self.conn = sqlite3.connect(self.dbpath)
        self.conn.row_factory = sqlite3.Row
        self.conn.create_function("REGEXP", 2, self._regexp)
        self.curs = self.conn.cursor()

    def close(self):
        self.conn.close()
        self.curs = None

    def _create_table(self):

        # get list of tables
        self.curs.execute("SELECT name FROM sqlite_master WHERE type='table';")
        tables = self.curs.fetchall()

        for table in tables:
            if table['name'] == self.tablename():
                # check columns
                self.curs.execute("SELECT * FROM " + self.tablename())
                cols = [desc[0] for desc in self.curs.description]

                for key in self.tablecolumns():
                    if key not in cols:
                        warnings.warn("Column '" + key + "' not in " + self.tablename() + " table.")
                return

        # if table not found, create it
        # print("Creating table:", "CREATE TABLE " + self.tablename() + " " + self._sql_create_table_str(self.tablecolumns()))
        self.curs.execute("CREATE TABLE " + self.tablename() + " " + self._sql_create_table_str(self.tablecolumns()))
        self.conn.commit()
        self.close()
        return

    def insert_or_replace(self, record, verbose=False):
        """Insert or replace individual entries in the table

        Arguments:
            record: dict
                Record to insert or replace in the database.
            verbose: bool
                If True, print status
        """
        if verbose:
            print("Insert or replace '", record['name'], "' ...", end='')

        (colstr, questionstr, valtuple) = self._sql_insert_or_replace_str(record)
        insertstr = "INSERT OR REPLACE INTO {0} {1} VALUES {2}".format(self.tablename(), colstr, questionstr)
        self.curs.execute(insertstr, valtuple)
        self.conn.commit()

        if verbose:
            print('DONE')

    def size(self):
        """Return table size"""
        self.curs.execute("SELECT count(*) FROM " + self.tablename())
        self.conn.commit()
        return self.curs.fetchone()[0]

    def print_selection(self, iterable, fmt=None):
        if fmt is None:
            fmt = self.default_print_fmt()

        def f(key, record):
            return (key in record.keys())
        pformatter = PrintFormatter(fmt)
        pformatter.if_key_in_record = f

        pformatter.print_header()
        for record in iterable:
            if record is None:
                break
            pformatter.print(record)

    def print_selection_detail(self, iterable, fmt=None):
        if fmt is None:
            fmt = self.default_print_fmt()

        def f(key, record):
            return (key in record.keys())
        pformatter = PrintFormatter(fmt)
        pformatter.if_key_in_record = f

        for record in iterable:
            if record is None:
                break
            pformatter.print_detail("id", record)
