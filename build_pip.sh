# Install build and distribution requirements: pip install -r build_requirements.txt
# Create a ~/.pypirc file with [pypi-mc] and/or [testpypi-mc] configured

rm -r build dist
python setup.py sdist bdist_wheel --universal
twine upload dist/* -r testpypi-mc

# Install from testpypi:
# pip install --index-url https://test.pypi.org/simple/ --extra-index-url https://pypi.org/simple materials-commons-cli
