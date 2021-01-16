.. manual/proj_init_clone.rst

Project operations
==================


Listing projects
----------------

All existing Materials Commons projects you have access to can be listed using: ::

    mc proj

Projects can be filtered by name (default), ID (``--id``), or owner (``--owner``) using regex match (default) or regex search (``--regexsearch``). For example, all projects with name beginning with "Mg" can be listed using: ::

    mc proj Mg


Creating projects
-----------------

New Materials Commons projects can be created at the default remote. First, set the current working directory to the directory where you want to store project files locally. It should be given the same name that you want for your Materials Commons project. It may be a new, empty directory or an already existing directory filled with files and directories that you want to be included in the project. Then, create the new project with: ::

    mc init

The new project will have the name of the directory from which ``mc init`` is called.


Cloning projects
----------------

Existing Materials Commons projects at the default remote can be "cloned". This creates a local directory for uploading and downloading project files. To clone a project, obtain the project ID from the "id" column printed by ``mc proj``. Then use: ::

    mc clone <project_id>

The cloned project directory will initially look empty. Even if your remote project contains files, no files will be downloaded until you initiate downloads explicitly. The cloned project directory does contain a hidden directory named `.mc` containing configuration and cache data. Subsequent calls of the ``mc`` program from within the local project directory will use that information to interact with the correct project on the correct remote instance of Materials Commons.


Other project actions
---------------------

Existing Materials Commons projects can be deleted, for example specifying by project ID: ::

    mc proj --delete --id <project_id>

This will prompt for confirmation before deleting, but after confirmation ***the project, and all of its files and data on Materials Commons will be deleted and cannot be recovered***. Local files will not be deleted.

The ``--goto`` option provides a shortcut for launching the Materials Commons project webpage: ::

    mc proj --goto <project_name>

You will be prompted to confirm and to login, if necessary.


Reference
---------

For a complete list of options, see:

- `mc clone <../reference/mc/clone.html>`_
- `mc init <../reference/mc/init.html>`_
- `mc proj <../reference/mc/proj.html>`_
