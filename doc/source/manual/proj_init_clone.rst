.. manual/proj_init_clone.rst

Project operations
==================

Overview
--------

All existing Materials Commons projects you have access to can be listed using: ::

    mc proj

Projects can be filtered by name (default), ID (``--id``), universally unique ID (UUID) (``--uuid``), or owner (``--owner``) using regex match (default) or regex search (``--regexsearch``). For example, all projects with name beginning with "Mg" can be listed using: ::

    mc proj Mg

New Materials Commons projects can be created at the default remote. First, set the current working directory to the directory where you want to store project files locally. It should be given the same name that you want for your Materials Commons project. It may be a new, empty directory or an already existing directory filled with files and directories that you want to be included in the project. Then, create the new project with: ::

    mc init

The new project will have the name of the directory from which ``mc init`` is called.

Existing Materials Commons projects at the default remote can be "cloned" (create a local directory for uploading and downloading project files) using: ::

    mc clone <id>

The initialized or cloned project will contain a hidden directory named `.mc` containing configuration and cache data. Subsequent calls of the ``mc`` program from within the local project directory will use that information to interact with the correct project on the correct remote instance of Materials Commons.

Existing Materials Commons projects can be deleted, for example specifying by ID: ::

    mc proj --delete --id <id>

This will prompt for confirmation before deleting, but after confirmation ***the project, and all of its files and data on Materials Commons will be deleted and cannot be recovered***. Local files will not be deleted.

The ``--goto`` option provides a shortcut for launching the Materials Commons project webpage: ::

    mc proj --goto <project_name>

You will be prompted to confirm and to login, if necessary.


``mc proj --help`` documentation
--------------------------------

.. argparse::
    :filename: materials_commons/cli/subcommands/proj.py
    :func: make_parser
    :prog: mc proj

``mc init --help`` documentation
--------------------------------

.. argparse::
    :filename: materials_commons/cli/subcommands/init.py
    :func: make_parser
    :prog: mc init

``mc clone --help`` documentation
---------------------------------

.. argparse::
    :filename: materials_commons/cli/subcommands/clone.py
    :func: make_parser
    :prog: mc clone
