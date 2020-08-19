.. install.rst

Installation
============


Install using pip
-----------------

::

    pip install materials-commons-cli

or, to install in your user directory:

::

   	pip install --user materials-commons-cli

If installing to a user directory, you may need to set your PATH to find the
installed scripts. This can be done using:

::

   	export PATH=$PATH:`python -m site --user-base`/bin


Install from source
-------------------

1. Clone the repository:

::

    cd /path/to/
    git clone https://github.com/materials-commons/mccli.git
    cd mccli

2. Checkout the branch/tag containing the version you wish to install. Latest is ``master``:

::

    git checkout master

3. From the root directory of the repository:

::

    pip install .

or, to install in your user directory:

::

   		pip install --user .

If installing to a user directory, you may need to set your ``PATH`` to find the
installed scripts. This can be done using:

::

   		export PATH=$PATH:`python -m site --user-base`/bin
