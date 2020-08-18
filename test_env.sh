# Notes on running tests:
# - Please do not run tests against the production instance of Materials Commons
# - To run a local instance with docker:
#   - docker pull materialscommons/materialscommons-dev
#   - docker run -d -p 8181:8181 materialscommons/materialscommons-dev
# - Before running pytest configure your local environment to use the
#   local instance of Materials Commons

# Materials Commons URL
export MC_API_URL="http://localhost:8181/api"

# A local directory where test data can be written
export MC_TEST_DATA_DIR="${PWD}/test/local_test_projects"

# The container is built with this admin account:
export MC_API_EMAIL="admin@admin.org"
export MC_API_PASSWORD="admin"
export MC_API_KEY="admintoken"

# Then run pytest with:
# - For all tests: pytest -rsap test
# - For a specific test: pytest -rsap test/test_api.py::TestAPI::test_project_api
