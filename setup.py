# build with: python setup.py install --user

from setuptools import setup, find_namespace_packages
from materials_commons.cli import __version__

setup(
    name='materials_commons_cli',
    version=__version__,
    description='Materials Commons CLI',
    long_description="""This package contains the materials_commons.cli module. This module is an interface
    to the Materials Commons project. We assume you have used (or are otherwise familiar with) the Materials
    Commons web site, https://materialscommons.org/, or a similar site based on the
    Materials Commons code (https://github.com/materials-commons/materialscommons), and intend to use these
    tools in that context.""",
    url='https://materials-commons.github.io/materials-commons-cli/html/index.html',
    author='Materials Commons development team',
    author_email='materials-commons-help@umich.edu',
    license='MIT',
    package_data={
        # If any package contains *.txt or *.rst files, include them:
        "": ["*.txt"]
    },
    packages=find_namespace_packages(
        include=['materials_commons.cli', 'materials_commons.cli.subcommands']
    ),
    entry_points={
        'console_scripts': ['mc=materials_commons.cli.parser:main']
    },
    zip_safe=False,
    classifiers=[
        # How mature is this project? Common values are
        #   3 - Alpha
        #   4 - Beta
        #   5 - Production/Stable
        'Development Status :: 4 - Beta',

        # Indicate who your project is intended for
        'Intended Audience :: Developers',
        'Intended Audience :: Science/Research',
        'Topic :: Scientific/Engineering :: Information Analysis',

        # Pick your license as you wish (should match "license" above)
        'License :: OSI Approved :: MIT License',

        # Specify the Python versions you support here. In particular, ensure
        # that you indicate whether you support Python 2, Python 3 or both.
        'Programming Language :: Python :: 3',
        'Programming Language :: Python :: 3.8'
    ],
    keywords='materials science mc materials-commons prisms',
    install_requires=[
        "globus-cli",
        "globus-sdk",
        "igittigitt==2.1.0",
        "materials-commons-api>=2.1.0",
        "python-dateutil",
        "pyyaml",
        "requests",
        "setuptools",
        "sortedcontainers",
        "tabulate"
    ]
)
