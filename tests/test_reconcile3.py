# File: tests/test_reconcile3.py
from unittest.mock import AsyncMock, MagicMock

import pytest
from materials_commons.cli.models import Observation
from materials_commons.cli.reconcile3 import SingleFileReconciler


@pytest.mark.asyncio
async def test_reconcile_file_local_symlink():
    # Create a mock WalkObservation with a local symlink
    observation = MagicMock(spec=Observation)
    observation.local_is_symlink.return_value = True
    observation.has_local.return_value = True
    observation.has_remote.return_value = False
    observation.is_file = False
    observation.is_dir = False

    # Reconciler setup
    reconciler = SingleFileReconciler(mode="test_mode")
    reconciler._classify_observation = MagicMock(return_value="local_only")
    reconciler._record_from_observation = MagicMock()
    reconciler._conflict = MagicMock(return_value="conflict_decision")

    # Call the reconcile_file method
    result = await reconciler.reconcile_file(observation)

    # Assertions
    assert result == "conflict_decision"
    observation.local_is_symlink.assert_called_once()
    reconciler._conflict.assert_called_once()


@pytest.mark.asyncio
async def test_reconcile_file_kind_conflict():
    # Create a mock WalkObservation with a kind conflict
    observation = MagicMock(spec=Observation)
    observation.local_is_symlink.return_value = False
    observation.has_kind_conflict.return_value = True
    observation.is_file = False
    observation.is_dir = False

    # Reconciler setup
    reconciler = SingleFileReconciler(mode="test_mode")
    reconciler._classify_observation = MagicMock(return_value="local_and_remote")
    reconciler._record_from_observation = MagicMock()
    reconciler._conflict = MagicMock(return_value="conflict_decision")

    # Call the reconcile_file method
    result = await reconciler.reconcile_file(observation)

    # Assertions
    assert result == "conflict_decision"
    observation.has_kind_conflict.assert_called_once()
    reconciler._conflict.assert_called_once()


@pytest.mark.asyncio
async def test_reconcile_file_directory():
    # Create a mock WalkObservation for a directory
    observation = MagicMock(spec=Observation)
    observation.local_is_symlink.return_value = False
    observation.has_kind_conflict.return_value = False
    observation.is_dir = True
    observation.is_file = False

    # Reconciler setup
    reconciler = SingleFileReconciler(mode="test_mode")
    reconciler._classify_observation = MagicMock(return_value="local_and_remote")
    reconciler._record_from_observation = MagicMock()
    reconciler._reconcile_directory = AsyncMock(return_value="directory_decision")

    # Call the reconcile_file method
    result = await reconciler.reconcile_file(observation)

    # Assertions
    assert result == "directory_decision"
    reconciler._reconcile_directory.assert_called_once()


@pytest.mark.asyncio
async def test_reconcile_file_regular_file():
    # Create a mock WalkObservation for a regular file
    observation = MagicMock(spec=Observation)
    observation.local_is_symlink.return_value = False
    observation.has_kind_conflict.return_value = False
    observation.is_dir = False
    observation.is_file = True

    # Reconciler setup
    reconciler = SingleFileReconciler(mode="test_mode")
    reconciler._classify_observation = MagicMock(return_value="local_and_remote")
    reconciler._record_from_observation = MagicMock()
    reconciler._reconcile_regular_file = AsyncMock(return_value="file_decision")

    # Call the reconcile_file method
    result = await reconciler.reconcile_file(observation)

    # Assertions
    assert result == "file_decision"
    reconciler._reconcile_regular_file.assert_called_once()


@pytest.mark.asyncio
async def test_reconcile_file_no_entry_observed():
    # Create a mock WalkObservation with no local or remote entry observed
    observation = MagicMock(spec=Observation)
    observation.local_is_symlink.return_value = False
    observation.has_local.return_value = False
    observation.has_remote.return_value = False
    observation.is_file = False
    observation.is_dir = False

    # Reconciler setup
    reconciler = SingleFileReconciler(mode="test_mode")
    reconciler._classify_observation = MagicMock(return_value="neither")
    reconciler._record_from_observation = MagicMock()
    reconciler._skip = MagicMock(return_value="skip_decision")

    # Call the reconcile_file method
    result = await reconciler.reconcile_file(observation)

    # Assertions
    assert result == "skip_decision"
    reconciler._skip.assert_called_once()
