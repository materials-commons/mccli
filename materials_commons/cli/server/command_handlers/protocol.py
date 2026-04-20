import asyncio
from typing import Protocol, Callable, Dict, Awaitable, Any, Optional

HandlerFunc = Callable[[asyncio.Queue, Dict[str, Any]], Awaitable[None]]

class CommandHandlerLookup(Protocol):
    def get_handler(self, cmd: str) -> Optional[HandlerFunc]: ...