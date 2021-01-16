.. manual/remote.rst

Configuring remotes
===================

The following notation is used to indicate places in example commands which must be replaced with your specific information:

- ``<email>``: Replace with the email address used to log in to your Materials Commons account.
- ``<url>``: Replace with the URL for the API of the instance of Materials Commons you are working with. For the public instance of Materials Commons this is: ``https://materialscommons.org/api``. Note that ``/api`` is included at the end of the URL.

Overview
--------

A remote is a version of Materials Commons hosted on the internet or on a network you can access. The primary public instance is at: ::

    https://materialscommons.org

Organizations may host their own instance of Materials Commons, for example at: ::

    https://materialscommons.other_institution.org

In order to access your projects and data on a particular instance of Materials Commons, you must login with your email and password to receive your apikey. The ``mc`` program will store the apikey in a configuration file located in your local user directory at: ::

    $HOME/.materialscommons/config.json

Keep this file secure. Anyone who knows one of the apikeys found inside it can use them to access, edit, and delete all of the projects and data associated with that account.

You may store credentials for multiple accounts and multiple instances of Materials Commons locally and set one as the default remote. The default remote is the account and instance of Materials Commons which is queried for any calls in which the remote is neither explicitly given (using ``--remote <email> <url>``) nor implicitly specified (because ``mc`` is called from within an existing local project directory).

Getting started
---------------

Assuming you have an account on the public instance of Materials Commons, ``materialscommons.org``, add it as a remote, using the email address used for your account. ::

    mc remote --add <email> https://materialscommons.org/api

This will prompt you for your password in order to get and save your apikey locally. Next, set that account and instance of Materials Commons as the default remote: ::

    mc remote --set-default <email> https://materialscommons.org/api

Once done successfully, you will be able to list projects you have access to at ``materialscommons.org`` using: ::

    mc proj

Common commands
---------------

List known instances of Materials Commons:

::

    mc remote -l

Add a remote, to store your apikey locally. Note that `"/api"` is included at the end of the URL.

::

   	mc remote --add <email> <url>

Remove a remote, deleting your apikey.

::

   	mc remote --remote <email> <url>

List remotes that have been added and show the current default. Optionally, print the apikeys.

::

   	mc remote [--show-apikey]


Set the default remote.

::

   	mc remote --set-default <email> <url>


Reference
---------

For a complete list of options, see:

- `mc remote <../reference/mc/remote.html>`_
