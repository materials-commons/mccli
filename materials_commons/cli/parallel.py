import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Callable, Any
from queue import Queue
import threading

class ParallelWalker:
    def __init__(
        self, 
        root_dir: str | Path, 
        max_workers: int | None = None,
        file_callback: Callable[[Path], Any] | None = None,
        dir_callback: Callable[[Path], Any] | None = None
    ):
        self.root = Path(root_dir)
        if max_workers is None:
            max_workers = (os.cpu_count() or 1) + 4
        self.max_workers = max_workers
        self.file_callback = file_callback or (lambda x: x)
        self.dir_callback = dir_callback or (lambda x: x)
        self.dirs_queue = Queue()
        self.lock = threading.Lock()

    def _process_directory(self, directory: Path) -> None:
        """Process single directory contents."""
        try:
            # Process all files in directory
            for item in directory.iterdir():
                if item.is_file():
                    if self.file_callback is None:
                        continue
                    self.file_callback(item)
                elif item.is_dir():
                    # Add subdirectories to queue
                    self.dirs_queue.put(item)
                    if self.dir_callback is None:
                        continue
                    self.dir_callback(item)
        except PermissionError:
            # Handle permission errors gracefully
            pass
        except Exception as e:
            print(f"Error processing {directory}: {e}")

    def walk(self) -> list:
        """Perform the parallel directory walk."""
        self.dirs_queue.put(self.root)
        
        with ThreadPoolExecutor(max_workers=self.max_workers) as executor:
            while not self.dirs_queue.empty():
                # Get a directory from the queue
                directory = self.dirs_queue.get()
                # Process the directory
                executor.submit(self._process_directory, directory)


# # Example usage:
# def process_file(file_path: Path) -> dict:
#     """Example callback for processing files."""
#     pass
#
# def process_dir(dir_path: Path) -> dict:
#     """Example callback for processing directories."""
#     pass
#
# # # Use it like this:
# walker = ParallelWalker(
#     root_dir="/path/to/directory",
#     max_workers=4,
#     file_callback=process_file,
#     dir_callback=process_dir
# )
# walker.walk()