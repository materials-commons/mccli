.. manual/ls_mkdir_rm_mv.rst

File operations
===============

Overview
--------

Typical filesystem operations can be performed with ``mc ls``, ``mc mkdir``, ``mc rm``, and ``mc mv``. By default, these act on both local and remote files and directories. If you want them to act on only remote files or directories use the ``--remote-only`` option.

The ``mc ls`` command displays a table showing information on both local (prepended with "l\_") and remote files and directories (prepended with "r\_"). Including the ``--checksum`` option will calculate the MD5 checksum of the local file for comparison with the remote file. The result of the comparison is shown in the "eq" column. For example, checking contents of both the current (".") and the "level_1" directory:

::

    $ mc ls . level_1 --checksum
    .:
    l_updated_at          l_size    l_type     r_updated_at          r_size    r_type     eq    name        id
    --------------------  --------  ---------  --------------------  --------  ---------  ----  ----------  -------
    2020 Aug 17 23:25:48  23B       file       2020 Aug 18 03:59:05  23B       file       True  file_A.txt  2659322
    2020 Aug 17 23:25:48  23B       file       -                     -         -          -     file_B.txt  -
    2020 Aug 17 23:25:48  -         directory  2020 Aug 18 03:31:46  0B        directory  -     level_1     2659316

    level_1:
    l_updated_at          l_size    l_type     r_updated_at          r_size    r_type     eq    name             id
    --------------------  --------  ---------  --------------------  --------  ---------  ----  ----------  -------
    2020 Aug 17 23:25:48  31B       file       2020 Aug 18 03:31:47  31B       file       True  file_A.txt  2659317
    2020 Aug 17 23:25:48  31B       file       2020 Aug 18 03:31:49  31B       file       True  file_B.txt  2659318
    2020 Aug 17 23:25:48  -         directory  2020 Aug 18 03:31:49  0B        directory  -     level_2     2659319


Reference
---------

For a complete list of options, see:

- `mc ls <../reference/mc/ls.html>`_
- `mc mkdir <../reference/mc/mkdir.html>`_
- `mc mv <../reference/mc/mv.html>`_
- `mc rm <../reference/mc/rm.html>`_
