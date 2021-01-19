.. manual/proj_init_clone.rst

Project operations
==================


Listing projects
----------------

All existing Materials Commons projects you have access to can be listed using ``mc proj``: ::

    $ mc proj
        name                                      owner                           id  updated_at
    --  ----------------------------------------  ----------------------------  ----  --------------------
        Mg-Nd CASM project                        adam@email.com                  33  2020 Oct 16 17:40:12
        Mg-Y CASM project                         adam@email.com                  39  2020 Oct 16 17:40:12
        MyProject                                 adam@email.com                  89  2021 Jan 13 15:57:50
        PF Simulation Mg-Nd Precipitates          bob@email.com                    3  2020 Oct 16 17:40:09
        PF Simulation Mg-Y Precipitates           bob@email.com                   88  2020 Oct 16 17:40:09
        Magnesium Rare Eath Alloy Study           clare@email.com                149  2020 Oct 16 17:40:16
        dislocation_dynamics                      bob@email.com                  340  2020 Oct 16 17:41:21
        test project                              adam@email.com                 588  2020 Oct 27 18:57:15


Projects can be filtered by name (default), ID (``--id``), or owner (``--owner``) using regular expression match (default) or regular expression search (``--regxsearch``).

For example, all projects with a name beginning with "Mg" can be listed using: ::

    $ mc proj Mg
        name                                      owner                           id  updated_at
    --  ----------------------------------------  ----------------------------  ----  --------------------
        Mg-Nd CASM project                        adam@email.com                  33  2020 Oct 16 17:40:12
        Mg-Y CASM project                         adam@email.com                  39  2020 Oct 16 17:40:12

All projects containing "Mg" can be listed using ``--regxsearch``: ::

    $ mc proj --regxsearch Mg
        name                                      owner                           id  updated_at
    --  ----------------------------------------  ----------------------------  ----  --------------------
        Mg-Nd CASM project                        adam@email.com                  33  2020 Oct 16 17:40:12
        Mg-Y CASM project                         adam@email.com                  39  2020 Oct 16 17:40:12
        PF Simulation Mg-Nd Precipitates          bob@email.com                    3  2020 Oct 16 17:40:09
        PF Simulation Mg-Y Precipitates           bob@email.com                   88  2020 Oct 16 17:40:09

All projects owned by `clare@email.com` can be listed using ``--owner``: ::

    $ mc proj --owner clare@email.com
        name                                      owner                           id  updated_at
    --  ----------------------------------------  ----------------------------  ----  --------------------
        Magnesium Rare Eath Alloy Study           clare@email.com                149  2020 Oct 16 17:40:16

Other options for listing projects with ``mc proj`` include ``--sort-by`` to sort by specified columns, ``--json`` to print raw JSON output, and ``-d``/``--details`` to print a more detailed view of project data.

Creating projects
-----------------

New Materials Commons projects can be created at the default remote. First, set the current working directory to the directory where you want to store project files locally. It should be given the same name that you want for your Materials Commons project. It may be a new, empty directory or an already existing directory filled with files and directories that you want to be included in the project. For instance, if your directory is called "MyProject", ``cd`` to that directory and then create the new project with ``mc init``: ::

    $ cd /path/to/MyProject
    $ mc init
    Created new project at: https://materialscommons.org/api
        name         owner      id  modified_at
    --  -----------  -------  ----  --------------------
    *   MyProject    <email>   601  2021 Jan 19 18:04:47

The new project will have the name of the directory from which ``mc init`` is called.


Cloning projects
----------------

Existing Materials Commons projects at the default remote can be "cloned". This creates a local directory for uploading and downloading project files. To clone a project, obtain the project ID from the "id" column printed by ``mc proj``. Then use: ::

    $ mc clone <project_id>
    Cloned project from https://materialscommons.org/api to /path/to/MyProject
        name           owner            id  modified_at
    --  -----------  -------  ------------  --------------------
        MyProject    <email>  <project_id>  2021 Jan 19 18:04:47

The cloned project directory will initially look empty. Even if your remote project contains files, no files will be downloaded until you initiate downloads explicitly.


The local project directory
---------------------------

The local project directory is where ``mc init`` has been used to create a new project, or which has been created by ``mc clone``. It will contain a hidden directory named ".mc" containing configuration and cache data. Subsequent calls of the ``mc`` program from within the local project directory, or any of its subdirectories, may use that information to interact with the correct project on the correct remote instance of Materials Commons.

To check if you are currently in a local project directory, use ``mc proj``. If the current working directory is the local project directory, or any of its subdirectories, that project is given an asterisk (`*`) in the project list. For example: ::

    $ pwd
    /path/to/MyProject/subdirectory
    $ mc proj
        name                                      owner                           id  updated_at
    --  ----------------------------------------  ----------------------------  ----  --------------------
        Mg-Nd CASM project                        adam@email.com                  33  2020 Oct 16 17:40:12
        Mg-Y CASM project                         adam@email.com                  33  2020 Oct 16 17:40:12
    *   MyProject                                 adam@email.com                  89  2021 Jan 13 15:57:50
        PF Simulation Mg-Nd Precipitates          bob@email.com                    3  2020 Oct 16 17:40:09
        PF Simulation Mg-Y Precipitates           bob@email.com                    3  2020 Oct 16 17:40:09
        Magnesium Rare Eath Alloy Study           clare@email.com                149  2020 Oct 16 17:40:16
        dislocation_dynamics                      bob@email.com                  340  2020 Oct 16 17:41:21
        test project                              adam@email.com                 588  2020 Oct 27 18:57:15



Other actions
-------------

As a design pattern, the ``mc`` program lets you select objects, such as projects with ``mc proj``, and then do something with the selected objects. The default is to print a list of the selected objects. Other actions, depending on the type of object, include creation, deletion, adding or removing to datasets, and other actions.

Existing Materials Commons projects can be deleted. For example, when specifying the project to be deleted by project ID: ::

    $ mc proj --delete --id 588
        name          owner             id  updated_at
    --  ------------  --------------  ----  --------------------
        test project  adam@email.com   588  2020 Oct 27 18:57:15

    Are you sure you want to permanently delete these? ('Yes'/'No'): Yes
    Deleted project: test project 588
    Note that this only deletes the project remotely and does not delete any local files.

This will prompt for confirmation before deleting, unless the ``--force`` option is given, but after confirmation ***the project, and all of its files and data on Materials Commons will be deleted and cannot be recovered***. Local files will not be deleted.

The ``--goto`` option provides a shortcut for launching the Materials Commons project webpage: ::

    $ mc proj --goto "test project"
        name          owner             id  updated_at
    --  ------------  --------------  ----  --------------------
        test project  adam@email.com   588  2020 Oct 27 18:57:15

    You want to goto these projects in a web browser? ('Yes'/'No'): Yes

After confirming, your default web browser will open and take you to the project page. You will be required to log in if you are not already.


Reference
---------

For a complete list of options, see:

- `mc clone <../reference/mc/clone.html>`_
- `mc init <../reference/mc/init.html>`_
- `mc proj <../reference/mc/proj.html>`_
