.. manual/remote.rst

Configuring remotes
===================

Overview
--------

A remote is a version of Materials Commons hosted on the internet or on a network you can access. The primary public instance is at: ::

    https://materialscommons.org

Organizations may host their own instance of Materials, for example at: ::

    https:://materialscommons.other_institution.org

In order to access your projects and data on a particular instance of Materials Commons, you must login with your email and password to receive your "apikey". The ``mc`` program will store the apikey in your local user directory in a configuration file: ::

    ~/.materialscommons/config.json``

Keep this file secure because anyone who has the apikeys inside can use them to access and edit the projects and data you have access to at the instance of Materials Commons you logged in to.

You may store credentials for multiple accounts and multiple instances of Materials Commons locally and set one as the "default remote". The default remote is the account and instance of Materials Commons which is queried for any calls in which the remote is neither explicitly given (using ``--remote <email> <url>``) nor implicitly specified because ``mc`` is called from inside an existing local project directory.

Getting started
---------------

Assuming you have an account on "materialscommons.org", login to get and save your apikey: ::

    mc remote --login <email> https:://materialscommons/api

Set that account and instance of Materials Commons as the "default remote": ::

    mc remote --set-default <email> https:://materialscommons/api

List projects you have access to at "materialscommons.org": ::

    mc proj

Common commands
---------------

List known instances of Materials Commons:

::

    mc remote -l

Add a remote, to store your apikey locally. Note that "/api" is included at the end of the URL.

::

   	mc remote --add <email> <url>

Remove a remote, deleting your apikey.

::

   	mc remote --remote <email> <url>

List remotes that have been added and show the current default. Optionally print the apikeys.

::

   	mc remote [--show-apikey]


Set the default remote.

::

   	mc remote --set-default <email> <url>


`mc remote --help` documentation
--------------------------------

.. argparse::
    :filename: materials_commons/cli/subcommands/remote.py
    :func: make_parser
    :prog: mc remote
