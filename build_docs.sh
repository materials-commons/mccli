sphinx-apidoc --separate --implicit-namespaces -f -o doc/source/reference/materials_commons materials_commons
python3 setup.py build_sphinx
