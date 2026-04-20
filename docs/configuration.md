# Configuration

See `config.example.json` for the full reference.

## Sections

- **runtime** -- logging level, shutdown timeout, timezone
- **gateway** -- HTTP listen address and public URL
- **agent** -- max iterations, turn timeout, worker concurrency, token budget
- **transport** -- message bus plugin
- **store** -- persistence plugin (SQLite)
- **sandbox** -- isolated execution environment (Docker)
- **llm** -- LLM drivers, endpoints, and routing (primary + fallback)
- **channels** -- chat platform adapters (Telegram, Teams)
- **tools** -- tool plugins
- **middleware** -- message processing pipeline

## Environment Variables

Environment variables in config strings are resolved automatically:

- `env:VARIABLE_NAME` -- resolves to the value of the environment variable
- `env_path:VARIABLE_NAME` -- resolves to the value, treated as a file path

Example:

```json
{
  "api_key": "env:OPENAI_API_KEY"
}
```
