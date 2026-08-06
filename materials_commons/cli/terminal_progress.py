import asyncio
import shutil
import sys
from collections import OrderedDict
from typing import Iterable, Optional

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
    """Renders in-progress transfers as a block of lines pinned to the bottom of
    the terminal.

    The block is repainted in its entirety on every update, using only cursor
    movements relative to the line just below the block. Absolute row addressing
    is deliberately avoided: it breaks as soon as the terminal scrolls, the
    window is resized, or more transfers are tracked than the screen has rows.
    """

    _lock: Optional[asyncio.Lock] = None
    _active: "OrderedDict[str, str]" = OrderedDict()

    # Number of terminal rows the live block currently occupies. The cursor is
    # always left at column 0 of the row immediately below the block.
    _painted = 0

    @classmethod
    def _get_lock(cls) -> asyncio.Lock:
        # Created lazily so the lock binds to the running loop, not to whatever
        # loop happened to exist at import time.
        if cls._lock is None:
            cls._lock = asyncio.Lock()
        return cls._lock

    @staticmethod
    def _fit(line: str, width: int) -> str:
        # Stay one column short of the edge; writing into the last column
        # triggers deferred wrapping on most terminals, which would make the
        # line occupy two rows and throw off the repaint arithmetic.
        limit = max(width - 1, 8)
        if len(line) <= limit:
            return line
        return line[:limit - 3] + "..."

    @classmethod
    def _repaint(cls, permanent_lines: Iterable[str] = ()) -> None:
        size = shutil.get_terminal_size(fallback=(80, 24))
        max_rows = max(size.lines - 1, 1)

        out = []
        if cls._painted:
            out.append(f"\033[{cls._painted}A")
        out.append("\r")

        # Lines that are done scroll away into the terminal's scrollback.
        for text in permanent_lines:
            out.append("\033[2K" + cls._fit(text, size.columns) + "\n")

        # Only as many rows as fit on screen; anything above the top would be
        # clamped by the terminal and repaint over itself.
        visible = list(cls._active.values())[-max_rows:]
        for text in visible:
            out.append("\033[2K" + cls._fit(text, size.columns) + "\n")

        # Clear whatever the previous, taller block left behind.
        out.append("\033[J")

        cls._painted = len(visible)
        sys.stdout.write("".join(out))
        sys.stdout.flush()

    @classmethod
    async def render(cls, progress_id: str, line: str, done: bool = False):
        """Update the row for ``progress_id``.

        When ``done`` is set the row is retired: it is written out as permanent
        output above the live block and no longer takes up a row.
        """
        if not sys.stdout.isatty():
            return

        async with cls._get_lock():
            if done:
                cls._active.pop(progress_id, None)
                cls._repaint([line])
            else:
                cls._active[progress_id] = line
                cls._repaint()

    @classmethod
    async def write_line(cls, line: str):
        """Emit a normal output line without corrupting the progress block."""
        if not sys.stdout.isatty():
            print(line)
            return

        async with cls._get_lock():
            cls._repaint([line])
