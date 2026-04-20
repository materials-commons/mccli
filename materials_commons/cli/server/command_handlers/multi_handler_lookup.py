
from typing import Optional

from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc


class MultiHandlerLookup(CommandHandlerLookup):
    def __init__(self, *lookups: CommandHandlerLookup):
        self._lookups = lookups

    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        for lookup in self._lookups:
            handler = lookup.get_handler(cmd)
            if handler is not None:
                return handler
        return None