Plugin Command
=============

The plugin command allows users to create their own extensions to the CLI by placing or linking code under ~/.materialscommons/plugins/<plugin-name>.

Usage
-----

.. code-block:: bash

    mc plugin <plugin-name> [--python]

Arguments
---------

* plugin-name: Name of the plugin
* --python: Specify that the plugin is a Python module with one or more entry points

Description
-----------

The plugin command creates a directory structure at ~/.materialscommons/plugins/<plugin-name> and sets up the necessary files for creating a plugin.

Regular Plugins
~~~~~~~~~~~~~~

By default, the command creates a regular plugin directory where you can place scripts, executables, or other files. These files will be available as commands when you run mc <plugin-name> <command>.

For example, if you create a plugin named "mytools" and place a script named "analyze" in the plugin directory, you can run it with:

.. code-block:: bash

    mc mytools analyze

Python Module Plugins
~~~~~~~~~~~~~~~~~~~

When the --python flag is specified, the command sets up a plugin for a Python module with one or more entry points. It creates a script that uses setuptools to find the Python module and its entry points, and sets up links to these in the plugin directory.

To use a Python module plugin:

1. Install your Python package with pip install -e /path/to/your/package
2. Run the link_entry_points.py script in the plugin directory to create links to your entry points

Examples
--------

Create a regular plugin:

.. code-block:: bash

    mc plugin mytools

Create a Python module plugin:

.. code-block:: bash

    mc plugin mypythontools --python
