.. manual/up_down_globus.rst

Jupyter notebook examples
=========================

In addition to using the `mc` program directly from the command line, it is possible to make use of its functionality from within Jupyter notebooks. This can be done either via shell commands (using ``! mc ...`` as `documented <https://ipython.readthedocs.io/en/stable/interactive/reference.html#system-shell-access>`_ for the IPython kernel) or using a fully Python interface provided by the ``ClonedProject`` class.

The ``ClonedProject`` class provides:

- easy login and project cloning, either permanently or in temporary directories
- a Python interface for the ``mc up`` and ``mc down`` functionality
- access to a ``materials_commons.api.Client`` instance for complete project access and control

Example Jupyter notebooks demonstrating these interfaces are available here:

- ``ClonedProject`` usage: `[View online] <../examples/MaterialsCommons-Project-Example.html>`_ `[Download] <../examples/MaterialsCommons-Project-Example.ipynb>`_
- Shell command usage: `[View online] <../examples/MaterialsCommons-Project-Shell-Example.html>`_ `[Download] <../examples/MaterialsCommons-Project-Shell-Example.ipynb>`_

With these interfaces, Jupyter notebooks that run and document workflows can be uploaded to a Materials Commons project containing the data it acts on to enable reproducible and customizeable workflows.

Several different types of workflow are possible with Materials Commons:

- Work outside of Materials Commons, then create a project, upload files, and construct datasets when you are ready to share or publish.
- Store project data only on Materials Commons, temporarily cloning a Materials Commons project to download data for calculations and analysis, uploading the results immediately, and then removing the temporary downloaded data.
- A mixed workflow, keeping some data stored locally or at a computational center, but immediately uploading analysis and results for access by all collaborators.
