# Install build and distribution requirements: pip install -r build_requirements.txt
# Create a ~/.pypirc file with [pypi-mc] and/or [testpypi-mc] configured

python3 -m pip install --upgrade pip setuptools wheel twine build
rm -r build dist
python3 -m build
twine upload dist/* -r pypi

# Install from testpypi:
# pip install --index-url https://test.pypi.org/simple/ --extra-index-url https://pypi.org/simple materials-commons-cli
