import asyncio
import sys
from typing import Dict

def format_bytes(value: float) -> str:
    """Format bytes as a human-readable value."""
    units = ("B", "KB", "MB", "GB", "TB")
    size = float(value)

    for unit in units:
        if size < 1024.0 or unit == units[-1]:
            return f"{size:.1f} {unit}"
        size /= 1024.0

    return f"{size:.1f} TB"

class TerminalProgress:
    _lock = asyncio.Lock()
    _rows: Dict[str, int] = {}
    _row_count = 0

    @classmethod
    async def render(cls, progress_id: str, line: str):
        if not sys.stdout.isatty():
            return

        async with cls._lock:
            if progress_id not in cls._rows:
                cls._rows[progress_id] = cls._row_count
                cls._row_count += 1
                print()

            row = cls._rows[progress_id]
            rows_from_cursor = cls._row_count - row

            terminal_width = 120
            if len(line) > terminal_width:
                line = line[:terminal_width - 3] + "..."

            sys.stdout.write("\033[s")
            sys.stdout.write(f"\033[{rows_from_cursor}A")
            sys.stdout.write("\r\033[K")
            sys.stdout.write(line)
            sys.stdout.write("\033[u")
            sys.stdout.flush()
