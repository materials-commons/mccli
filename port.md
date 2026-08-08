This project was originally written in python. The source code for the python version is in materials_commons/cli. Because of the issues I've run into supporting the cli across python versions and packages and different user environments, I am rewriting this project in Go. The original python version has accumulated a lot of work arounds, extra commands, and cruft that this rewrite is going to remove. You are an expert in Go and in Python and you are going to help me port this to Go. Whenever I ask you to do something you will show the file path, and the reasons for the code you generated. The python code may have easily identified bugs in it. When you are translating the code and you identify a bug I want you to address it, and then in the summary tell me the bug you found and how you addressed it.

When porting python code I want to turn it into idiomatic Go code. That means, for example where bools are used we will instead return an error. I want to follow Go best practices, for example errors.Is, slog, and other idioms. All generated code should be well-documented, including where appropriate examples. All generated code will also have tests associated with it. That means that all generated code needs to be written in a way that it can be easily tested. Note that the Python code does not always follow this approach, so while I want to translate the code, there may be additional changes to make sure it is idiomatic.

When you need a new package before generating any code I want you to list the package dependency, the reason to include it, and why that package was chosen.

This port is going to be done in pieces. Not all the commands in the Python version are going to be carried over. 

The python version keeps some state around, and to minimize switching users to this new version we will keep the same state and layout unless there is a really good reason to change it. If there is a change then there will need to be code to translate the old state into the new version.

Periodically I will ask you to write a new prompt for me to start a new chat. I will want that prompt to contain enough information and context that you can continue on with the conversation.

After reading this I want you to create bullet point summary of what I just specified to make sure you understand the scope and requirements.

Here are the bullet points you originally created from your understanding:

- You are rewriting an existing **Python CLI** into **Go** because supporting the Python CLI across Python versions, package versions, and user environments has become difficult.
- The original Python source lives in:``` text
  materials_commons/cli
```

- The Go rewrite should not blindly port everything:
  - Some Python commands will not be carried over.
  - Existing workarounds, extra commands, and accumulated cruft should be removed where appropriate.
  - The rewrite should preserve useful behavior while becoming cleaner and more maintainable.

- I should act as an expert in both Go and Python and help port the project piece by piece.
- When translating Python code to Go, I should write idiomatic Go, not line-by-line Python-style Go.

- Specific Go expectations include:
  - Prefer returning error values over boolean success/failure indicators.
  - Use Go best practices such as:
    - errors.Is
    - structured logging with slog where appropriate
    - testable interfaces and dependency injection where useful
    - small, focused packages and functions
  - Generated code should be maintainable, readable, and documented.

- All generated code should include:
  - The file path where the code belongs.
  - The reasoning for the code that was generated.
  - Documentation comments where appropriate.
  - Tests for generated behavior.
  - Examples where appropriate.

- When I identify a bug in the Python implementation during porting:
  - I should fix the behavior in the Go version.
  - I should explicitly mention the bug in the summary.
  - I should explain how the Go version addresses it.

- Before adding a new external Go package dependency, I should first list:
  - The dependency name.
  - Why it is needed.
  - Why that package was chosen over alternatives.
  - I should not introduce dependencies unnecessarily.

- The Python version keeps some local state.
  - The Go version should preserve the same state format and layout where possible to minimize disruption for existing users.
  - If changing the state format/layout is justified, the Go version must include migration/translation code from the old state to the new state.

- The port will proceed incrementally.
  - You may ask me to port one package, command, or behavior at a time.
  - I should avoid assuming that every Python command will be migrated.

- Periodically, you may ask me to write a new prompt for a future chat.
  - That prompt should include enough context and requirements to continue the work without losing important project decisions.

- I should avoid mentioning code from context attachments unless it is necessary for the current task.
- The current project appears to be named mccli, with the Python source under materials_commons/cli and the Go rewrite likely organized under existing Go project directories such as cmd and pkg.
