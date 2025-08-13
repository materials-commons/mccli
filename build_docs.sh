sphinx-apidoc --separate --implicit-namespaces -f -o doc/source/reference/materials_commons materials_commons
sphinx-build -b html doc/source doc/build/html
