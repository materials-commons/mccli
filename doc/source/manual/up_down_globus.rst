.. manual/up_down_globus.rst

Upload / Download
=================

Standard upload and download
----------------------------

When a local project directory exists (created using ``mc init`` or ``mc clone``), files can be transferred between the local project and the remote project.

File uploading and downloading is performed with: ::

    mc up [paths [paths ...]]

and: ::

    mc down [paths [paths ...]]

Use the ``-r`` option to upload and download directory contents recursively. For ``mc up``, any files given as "path" arguments existing in the local project directory will be uploaded to the corresponding directory in the remote project. Similarly, for ``mc down`` any files given as "path" arguments existing in the remote project directory will be downloaded to the corrsponding directory in the local project. Any intermediate directories that do not exist will be created automatically.

By default, ``mc up``/``mc down`` will check MD5 checksums and not transfer files that already exist. This can be skipped with the ``--no-compare`` option.

There is a limit to the size of files that can be uploaded using the standard file upload process. that depends on the configuration of the particular instance of Materials Commons. The ``mc`` program will skip (with a warning message) uploading any file larger than the size given by the ``--limit`` option (given in MB). The default limit is 50MB.

When uploading a file results in "overwriting" an existing file at the same location, Materials Commons saves the previously existing file as a "version". Access to previous file versions will be enabled in a subsequent release, along with details about how ``mc rm`` and ``mc mv`` effect versions.


Globus file transfer
--------------------

The Globus_ transfer service can be used when transfering larger or more files.


Installation and configuration
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The Globus client should already be installed as part of the ``materials-commons-cli`` installation process. But, if it is not installed, install with: ::

    pip install globus-sdk
    pip install globus-cli

The following assumes you have a valid Globus_ account.

If you are using Globus Personal Connect, for instance on your personal computer:

- Login to Globus: ::

    globus login


If you are using a Globus endpoint managed by someone else, for instance on a shared cluster:

- Find the Globus endpoint id for the endpoint you will use. Endpoint UUIDs can be found on the `Globus endpoints web interface`_.
- Configure ``mc`` to use the Globus endpoint id: ::

    mc globus --set-globus-endpoint-id <id>


Globus file transfer overview
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The process for uploading files to a Materials Commons project via Globus is:

1. Request that Materials Commons creates a new Globus upload directory for a project on the Materials Commons Globus endpoint. At this point Materials Commons creates a new upload directory and sets access control to allow only you to privately access it via Globus.
2. Initiate one or more Globus transfers from any endpoints you can access to the upload directory on the Materials Commons endpoint.
3. Finish or delete upload:

  a. Tell Materials Commons that transfers are complete and files at the upload directory should be processed into the Materials Commons project. At this point Materials Commons removes access control so no more files can be uploaded while they are being processed. To upload more files via Globus repeat from (1).
  b. Or, tell Materials Commons to discard uploaded files and do not process them into the project.

The process for downloading files from a Materials Commons project via Globus is:

1. Request that Materials Commons creates a new Globus download directory for a project on the Materials Commons Globus endpoint. At this point Materials Commons creates hardlinks in the download directory to the current version of all project files. For large projects this may take some time. Then it sets access control to allow only you to privately access the download directory via Globus. Any file changes in the project that occur after a download directory is created are *not* reflected in that download directory.
2. Initiate one or more Globus transfers from the download directory on the Materials Commons endpoint to any endpoints you can access.
3. Tell Materials Commons that transfers are complete and the download directory can be deleted. The download directory may be left as long as desired, but it will *not* reflect any file or directory changes to project.


Initiating transfers using Globus
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

To initiate an upload using Globus, add the ``-g``/``--globus`` option: ::

    mc up -g [paths [paths ...]]

Since a user can have multiple existing Globus upload directories, the ``mc`` program stores, for each local project, the id of a "current" Globus upload indicating which upload directory to transfer files to. If no "current" upload exists when ``mc up`` is called, then ``mc`` will request a new upload directory with a random name and initiate a Globus transfer to it. The newly created upload directory becomes the "current" upload directory and is used when ``mc up -g`` is called subsequently.

Globus downloads can be initiated similarly, with: ::

    mc down -g [paths [paths ...]]


Managing Globus transfers
^^^^^^^^^^^^^^^^^^^^^^^^^

The command ``globus task list`` can be used to check the status of all initiated transfers.

Globus upload and download directories on Materials Commons can be managed with ``mc globus upload``/``mc globus download``. This provides options to list, create, delete, and go to (in the Globus web UI) upload and download directories, finish an upload, and set or unset the "current" upload/download directory for the local project.

When Globus upload transfers complete, use the ID of the upload directory to be finished, and begin processing of files into the Materials Commons project with: ::

    ``mc globus upload --finish --id <id>``

Depending on the number of files, it may take some time to process uploaded files before they appear in the project.


``--help`` documentation:
-------------------------

.. argparse::
    :filename: materials_commons/cli/subcommands/up.py
    :func: make_parser
    :prog: mc up

.. argparse::
    :filename: materials_commons/cli/subcommands/down.py
    :func: make_parser
    :prog: mc down

.. argparse::
    :filename: materials_commons/cli/subcommands/globus.py
    :func: make_globus_parser
    :prog: mc globus

.. argparse::
    :filename: materials_commons/cli/subcommands/globus.py
    :func: make_globus_upload_parser
    :prog: mc globus upload

.. argparse::
    :filename: materials_commons/cli/subcommands/globus.py
    :func: make_globus_download_parser
    :prog: mc globus download

.. _Globus: https://www.globus.org/
.. _`Globus endpoints web interface`: https://app.globus.org/endpoints
