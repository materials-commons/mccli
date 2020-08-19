# Generating the documentation

Install documentation dependencies. From repository root:

    pip install -r doc_requirements.txt

From repository root run:

    sphinx-apidoc --separate --implicit-namespaces -f -o doc/source/reference/materials_commons materials_commons
    python setup.py build_sphinx
