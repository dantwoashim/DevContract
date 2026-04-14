# Tooling Notes

EnvSync can generate optional instruction files and MCP config for editor or assistant tooling, but this repository does not commit those outputs.

If you experiment with those features locally:

- generate them in your own clone
- keep them ignored
- review them with `envsync guard scan`
- avoid committing tokens, local file paths, or machine-specific endpoints

The public repository stays source-only so contributors do not inherit one maintainer's editor state.
