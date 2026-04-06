import asyncio
from dataclasses import dataclass
from typing import AsyncIterator, Literal

StreamName = Literal["stdout", "stderr"]


@dataclass(frozen=True)
class CommandOutputLine:
    which_stream: StreamName
    line: str


@dataclass(frozen=True)
class CommandFinished:
    exit_code: int


CommandEvent = CommandOutputLine | CommandFinished


async def run_command_stream(*args: str) -> AsyncIterator[CommandEvent]:
    process = await asyncio.create_subprocess_exec(
        *args,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )

    assert process.stdout is not None
    assert process.stderr is not None


    async def read_stream(stream: asyncio.StreamReader, stream_name: StreamName,
                          queue: asyncio.Queue[CommandEvent]) -> None:
        try:
            async for line in stream:
                await queue.put(CommandOutputLine(which_stream=stream_name, line=line.decode().rstrip("\n")))
        except asyncio.CancelledError:
            raise

    queue: asyncio.Queue[CommandEvent] = asyncio.Queue()
    stdout_task = asyncio.create_task(read_stream(process.stdout, "stdout", queue))
    stderr_task = asyncio.create_task(read_stream(process.stderr, "stderr", queue))

    async def wait_for_process() -> None:
        try:
            exit_code = await process.wait()
            await queue.put(CommandFinished(exit_code=exit_code))
        except asyncio.CancelledError:
            process.kill()
            raise

    wait_task = asyncio.create_task(wait_for_process())

    try:
        while True:
            event = await queue.get()
            yield event
            if isinstance(event, CommandFinished):
                break
    finally:
        for task in (stdout_task, stderr_task, wait_task):
            if not task.done():
                task.cancel()
        await asyncio.gather(stdout_task, stderr_task, wait_task, return_exceptions=True)
