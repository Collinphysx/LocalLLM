# Task Delegation Rules
- Use your own reasoning for architecture, multi-file planning, and complex debugging.
- Delegate unit test generation, boilerplate code, diff reviews, and file summarization to the `local-llm` MCP tools to conserve cloud tokens.
  - `generate_unit_tests`, `generate_boilerplate`, `review_diff`, `summarize_file`
  - Server: [tools/local_llm_mcp.py](tools/local_llm_mcp.py), configured in [.mcp.json](.mcp.json); proxies to LM Studio at http://localhost:1234/v1.
  - Review the local model's output before writing it to disk.
