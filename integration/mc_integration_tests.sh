#!/usr/bin/env bash

set -u
set -o pipefail

WORKDIR=""
PROJECT_NAME=""
INIT_DIR=""
CLONE_PARENT=""
CLONE_DIR=""

fail() {
  echo ""
  echo "ERROR: $*" >&2
  if [[ -n "${WORKDIR}" && -d "${WORKDIR}" ]]; then
    echo "Leaving integration test directory in place for debugging:"
    echo "  ${WORKDIR}"
  fi
  exit 1
}

run() {
  echo ""
  echo "+ $*"
  "$@"
  local status=$?
  if [[ ${status} -ne 0 ]]; then
    fail "command failed with status ${status}: $*"
  fi
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    fail "required environment variable ${name} is not set"
  fi
}

assert_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    fail "required command '${command_name}' was not found on PATH"
  fi
}

assert_file_exists() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    fail "expected file to exist: ${path}"
  fi
}

assert_dir_exists() {
  local path="$1"
  if [[ ! -d "${path}" ]]; then
    fail "expected directory to exist: ${path}"
  fi
}

assert_ls_contains() {
  local path="$1"
  local expected="$2"
  local output_file

  output_file="$(mktemp "${WORKDIR}/mc-ls.XXXXXX")"

  echo ""
  echo "+ mc ls ${path}"
  if ! mc ls "${path}" >"${output_file}" 2>&1; then
    cat "${output_file}" >&2
    fail "mc ls failed for ${path}"
  fi

  cat "${output_file}"

  if ! grep -Fq "${expected}" "${output_file}"; then
    fail "mc ls ${path} did not contain expected text: ${expected}"
  fi
}

read_project_id() {
  local project_dir="$1"
  local config_file="${project_dir}/.mc/config.json"

  if [[ ! -f "${config_file}" ]]; then
    fail "project config file not found: ${config_file}"
  fi

  local project_id
  project_id="$(jq -r '.project_id // empty' "${config_file}")"

  if [[ -z "${project_id}" || "${project_id}" == "null" ]]; then
    fail "project_id not found in ${config_file}"
  fi

  echo "${project_id}"
}

delete_project() {
  local project_id="$1"
  curl -k -X DELETE "${MC_API_URL}/projects/${project_id}?api_token=${MC_API_KEY}" > /dev/null 2>&1
}

cleanup_test() {
  local project_id="$1"
  delete_project "$project_id"
  if [[ -n "${WORKDIR}" && -d "${WORKDIR}" ]]; then
    rm -rf "${WORKDIR}"
  fi
}

main() {
  assert_command mc
  assert_command python
  assert_command mktemp

  require_env MC_API_KEY
  require_env MC_API_URL
  require_env MC_API_EMAIL

  echo "This integration test will run the Python Materials Commons CLI against:"
  echo "  MC_API_URL=${MC_API_URL}"
  echo "  MC_API_EMAIL=${MC_API_EMAIL}"
  echo ""
  echo "Make sure this is the correct server, that it is running, and that the API key"
  echo "belongs to the account you intend to test with."
  echo ""
  read -r -p "Continue? Type 'yes' to run the integration test: " answer

  if [[ "${answer}" != "yes" ]]; then
    echo "Aborted."
    exit 1
  fi

  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/mc-python-cli-integration.XXXXXX")"
  PROJECT_NAME="python-cli-integration-$(date +%Y%m%d%H%M%S)-$$"
  INIT_DIR="${WORKDIR}/${PROJECT_NAME}"
  CLONE_PARENT="${WORKDIR}/clone"
  CLONE_DIR="${CLONE_PARENT}/${PROJECT_NAME}"
  MC_WSS_URL="wss://$(echo $MC_API_URL | cut -d / -f 3)/ws"

  mkdir -p "${INIT_DIR}"
  mkdir -p "${CLONE_PARENT}"

  echo ""
  echo "Working directory:"
  echo "  ${WORKDIR}"

  echo ""
  echo "Step 1: create a new project with mc init"
  (
    cd "${INIT_DIR}" || exit 1
    run mc init --desc "Python CLI integration test project"
  ) || fail "mc init failed"

  local project_id
  project_id="$(read_project_id "${INIT_DIR}")" || fail "could not read project id"
  echo "Created project id: ${project_id}"

  echo ""
  echo "Step 1: clone the project locally with mc clone"
  (
    cd "${CLONE_PARENT}" || exit 1
    run mc clone "${project_id}"
  ) || fail "mc clone failed"

  assert_dir_exists "${CLONE_DIR}"
  assert_dir_exists "${CLONE_DIR}/.mc"

  echo ""
  echo "Step 2: create a file and upload it using websocket upload"
  (
    cd "${CLONE_DIR}" || exit 1
    printf "hello from python cli integration test\n" > single-file.txt
    run mc up --v2 --ws-url "${MC_WSS_URL}" single-file.txt
  ) || fail "single file upload failed"

  echo ""
  echo "Step 3: verify the single file was uploaded"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_ls_contains "single-file.txt" "single-file.txt"
  ) || fail "single file verification failed"

  echo ""
  echo "Step 4: create nested directories and upload recursively using websocket upload"
  (
    cd "${CLONE_DIR}" || exit 1
    mkdir -p top-dir/nested-dir
    printf "top directory file\n" > top-dir/top-file.txt
    printf "nested directory file\n" > top-dir/nested-dir/nested-file.txt
    run mc up --v2 -r --ws-url "${MC_WSS_URL}" top-dir
  ) || fail "directory upload failed"

  echo ""
  echo "Step 5: verify nested directory files were uploaded"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_ls_contains "top-dir" "top-file.txt"
    assert_ls_contains "top-dir/nested-dir" "nested-file.txt"
  ) || fail "directory upload verification failed"

  echo ""
  echo "Step 6: delete the single file locally"
  (
    cd "${CLONE_DIR}" || exit 1
    rm single-file.txt
  ) || fail "could not remove single-file.txt"

  echo ""
  echo "Step 7: verify mc ls shows the single file is available remotely"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_ls_contains "single-file.txt" "single-file.txt"
  ) || fail "single file remote-only verification failed"

  echo ""
  echo "Step 8: download the single file"
  (
    cd "${CLONE_DIR}" || exit 1
    run mc down --v2 single-file.txt
  ) || fail "single file download failed"

  echo ""
  echo "Step 9: verify the single file downloaded correctly"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_file_exists "single-file.txt"
    grep -Fq "hello from python cli integration test" single-file.txt || fail "single-file.txt contents are incorrect"
    assert_ls_contains "single-file.txt" "single-file.txt"
  ) || fail "single file download verification failed"

  echo ""
  echo "Step 10: delete the uploaded directory locally"
  (
    cd "${CLONE_DIR}" || exit 1
    rm -rf top-dir
  ) || fail "could not remove top-dir"

  echo ""
  echo "Step 10: verify mc ls shows the directory can be downloaded"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_ls_contains "top-dir" "top-dir"
  ) || fail "directory remote-only verification failed"

  echo ""
  echo "Step 10: download the directory recursively"
  (
    cd "${CLONE_DIR}" || exit 1
    run mc down --v2 -r top-dir
  ) || fail "directory download failed"

  echo ""
  echo "Step 10: verify the directory downloaded correctly"
  (
    cd "${CLONE_DIR}" || exit 1
    assert_file_exists "top-dir/top-file.txt"
    assert_file_exists "top-dir/nested-dir/nested-file.txt"
    grep -Fq "top directory file" top-dir/top-file.txt || fail "top-dir/top-file.txt contents are incorrect"
    grep -Fq "nested directory file" top-dir/nested-dir/nested-file.txt || fail "top-dir/nested-dir/nested-file.txt contents are incorrect"
    assert_ls_contains "top-dir" "top-file.txt"
    assert_ls_contains "top-dir/nested-dir" "nested-file.txt"
  ) || fail "directory download verification failed"

  cleanup_test "$project_id"

  echo ""
  echo "Python CLI integration test completed successfully."
}

main "$@"