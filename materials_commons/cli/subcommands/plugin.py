import argparse
import os
import sys
import pkg_resources
from pathlib import Path

import materials_commons.cli.functions as clifuncs
from materials_commons.cli.exceptions import MCCLIException


def make_parser():
    """Make argparse.ArgumentParser for `mc plugin`"""
    parser = argparse.ArgumentParser(
        description='Create a plugin directory for extending the Materials Commons CLI',
        prog='mc plugin')
    parser.add_argument('plugin_name', help='Name of the plugin')
    parser.add_argument('--python', action='store_true', default=False,
                        help='Specify that the plugin is a Python module with one or more entry points')
    return parser


def create_plugin(plugin_name, is_python_module=False):
    """Create a plugin directory structure at ~/.materialscommons/plugins/<plugin-name>

    Arguments
    ---------
    plugin_name: str, Name of the plugin
    is_python_module: bool, Whether the plugin is a Python module with entry points

    Returns
    -------
    plugin_path: str, Path to the created plugin directory

    Raises
    ------
    MCCLIException: If any of the following occur:
        - plugin directory already exists
        - error creating plugin directory
    """
    # Create the base plugins directory if it doesn't exist
    plugins_base_dir = os.path.expanduser("~/.materialscommons/plugins")
    clifuncs.mkdir_if(os.path.expanduser("~/.materialscommons"))
    clifuncs.mkdir_if(plugins_base_dir)

    # Create the plugin directory
    plugin_path = os.path.join(plugins_base_dir, plugin_name)
    if os.path.exists(plugin_path):
        raise MCCLIException(f"Plugin directory '{plugin_path}' already exists.")

    try:
        os.mkdir(plugin_path)
        print(f"Created plugin directory: {plugin_path}")

        # If it's a Python module, set up the structure for Python modules
        if is_python_module:
            # Create a README file with instructions
            readme_content = f"""# {plugin_name} Plugin

This is a Python module plugin for Materials Commons CLI.

## Installation

To install this plugin, you need to install the Python package:

```
pip install -e /path/to/your/python/package
```

The plugin will automatically find all entry points defined in your package's setup.py.

## Usage

After installation, you can use the plugin commands directly with the `mc` command.
"""
            clifuncs.make_file(os.path.join(plugin_path, "README.md"), readme_content)

            # Create a script to find and link Python entry points
            link_script_content = """#!/usr/bin/env python
import os
import sys
import pkg_resources
from pathlib import Path

# Get the plugin directory
plugin_dir = Path(__file__).parent.absolute()

# Find all entry points in all installed packages
for dist in pkg_resources.working_set:
    if dist.has_metadata('entry_points.txt'):
        entry_map = pkg_resources.get_entry_map(dist.key)
        if 'console_scripts' in entry_map:
            for name, entry_point in entry_map['console_scripts'].items():
                # Create a symlink to the entry point script
                script_path = os.path.join(sys.prefix, 'bin', name)
                if os.path.exists(script_path):
                    link_path = os.path.join(plugin_dir, name)
                    if not os.path.exists(link_path):
                        try:
                            os.symlink(script_path, link_path)
                            print(f"Created link: {link_path} -> {script_path}")
                        except Exception as e:
                            print(f"Error creating link for {name}: {e}")
"""
            clifuncs.make_file(os.path.join(plugin_path, "link_entry_points.py"), link_script_content)
            os.chmod(os.path.join(plugin_path, "link_entry_points.py"), 0o755)

            print(f"Set up Python module plugin structure in {plugin_path}")
            print("To use this plugin:")
            print(f"1. Install your Python package with 'pip install -e /path/to/your/package'")
            print(f"2. Run '{os.path.join(plugin_path, 'link_entry_points.py')}' to create links to your entry points")
        else:
            # Create a README file with instructions for regular plugins
            readme_content = f"""# {plugin_name} Plugin

This is a plugin for Materials Commons CLI.

## Usage

Place your scripts, executables, or other files in this directory.
They will be available as commands when you run `mc {plugin_name} <command>`.
"""
            clifuncs.make_file(os.path.join(plugin_path, "README.md"), readme_content)

            # Create an example script
            example_script_content = """#!/bin/bash
echo "Hello from the plugin!"
echo "This is an example script. Replace it with your own scripts."
"""
            clifuncs.make_file(os.path.join(plugin_path, "example"), example_script_content)
            os.chmod(os.path.join(plugin_path, "example"), 0o755)

            print(f"Set up plugin structure in {plugin_path}")
            print("To use this plugin:")
            print(f"1. Place your scripts or executables in {plugin_path}")
            print(f"2. Make them executable with 'chmod +x {plugin_path}/<script>'")
            print(f"3. Run them with 'mc {plugin_name} <script>'")

    except Exception as e:
        # Clean up if there was an error
        if os.path.exists(plugin_path):
            import shutil
            shutil.rmtree(plugin_path)
        raise MCCLIException(f"Error creating plugin: {str(e)}")

    return plugin_path


def plugin_subcommand(argv, working_dir):
    """
    Create a plugin directory for extending the Materials Commons CLI

    mc plugin <plugin-name> [--python]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    plugin_path = create_plugin(args.plugin_name, args.python)

    print(f"\nPlugin '{args.plugin_name}' created successfully at {plugin_path}")
    return 0
