"""Minimal MCP server (stdio) that proxies task delegation to a local LM Studio model.

No third-party dependencies. Configure with environment variables:
  LOCAL_LLM_BASE_URL  default http://localhost:1234/v1
  LOCAL_LLM_MODEL     default google/gemma-4-12b-qat
  LOCAL_LLM_TIMEOUT   seconds, default 600
"""

import json
import os
import sys
import urllib.error
import urllib.request

BASE_URL = os.environ.get("LOCAL_LLM_BASE_URL", "http://localhost:1234/v1").rstrip("/")
MODEL = os.environ.get("LOCAL_LLM_MODEL", "google/gemma-4-12b-qat")
TIMEOUT = float(os.environ.get("LOCAL_LLM_TIMEOUT", "600"))

TOOLS = {
    "generate_unit_tests": {
        "description": "Generate unit tests for the given source code using the local LLM. Returns only the test file contents.",
        "system": "You are an expert software engineer. Write thorough, idiomatic unit tests for the code provided. "
                  "Use the language's standard testing framework unless told otherwise. "
                  "Output only the complete test file contents, with no explanation and no markdown fences.",
        "properties": {
            "code": {"type": "string", "description": "Source code to test."},
            "language": {"type": "string", "description": "Programming language, e.g. go."},
            "instructions": {"type": "string", "description": "Optional extra requirements (file name, package, cases to cover)."},
        },
        "required": ["code"],
    },
    "generate_boilerplate": {
        "description": "Generate boilerplate code from a description using the local LLM.",
        "system": "You are an expert software engineer. Produce clean, idiomatic boilerplate code for the request. "
                  "Output only code, with no explanation and no markdown fences unless multiple files are needed, "
                  "in which case prefix each file with a line '// FILE: <path>'.",
        "properties": {
            "description": {"type": "string", "description": "What to generate."},
            "language": {"type": "string", "description": "Programming language."},
            "context": {"type": "string", "description": "Optional existing code or conventions to follow."},
        },
        "required": ["description"],
    },
    "review_diff": {
        "description": "Review a diff for bugs, risky changes and style issues using the local LLM.",
        "system": "You are a careful code reviewer. Review the diff for correctness bugs, edge cases, security issues "
                  "and readability. Reply with a concise bullet list of findings, each with file/line if known. "
                  "Say 'No issues found.' if there are none.",
        "properties": {
            "diff": {"type": "string", "description": "Unified diff to review."},
            "focus": {"type": "string", "description": "Optional area to focus on."},
        },
        "required": ["diff"],
    },
    "summarize_file": {
        "description": "Summarize a file's purpose and contents using the local LLM. Pass either a path or the content.",
        "system": "Summarize the file concisely: its purpose, key types/functions, and notable dependencies or side effects.",
        "properties": {
            "path": {"type": "string", "description": "Path to a file to read and summarize."},
            "content": {"type": "string", "description": "File contents to summarize (used if path is not given)."},
        },
        "required": [],
    },
}


def build_prompt(name, args):
    if name == "summarize_file":
        content = args.get("content")
        path = args.get("path")
        if not content and path:
            with open(path, "r", encoding="utf-8", errors="replace") as f:
                content = f.read()
        if not content:
            raise ValueError("Provide 'path' or 'content'.")
        return f"File: {path or '(inline)'}\n\n{content}"
    parts = []
    for key, value in args.items():
        if value:
            parts.append(f"## {key}\n{value}")
    return "\n\n".join(parts)


def call_llm(system, user):
    body = json.dumps({
        "model": MODEL,
        "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}],
        "temperature": 0.2,
        "stream": False,
    }).encode("utf-8")
    req = urllib.request.Request(
        f"{BASE_URL}/chat/completions", data=body, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        data = json.loads(resp.read().decode("utf-8"))
    choice = data["choices"][0]
    content = (choice["message"].get("content") or "").strip()
    if not content or choice.get("finish_reason") == "length":
        usage = data.get("usage") or {}
        raise RuntimeError(
            f"Model returned {'no' if not content else 'truncated'} output "
            f"(finish_reason={choice.get('finish_reason')}, prompt_tokens={usage.get('prompt_tokens')}, "
            f"completion_tokens={usage.get('completion_tokens')}, total_tokens={usage.get('total_tokens')}). "
            "The prompt likely filled the model's context window: increase the context length "
            "in LM Studio or send a smaller request."
        )
    return content


def handle(msg):
    method = msg.get("method")
    params = msg.get("params") or {}
    if method == "initialize":
        return {
            "protocolVersion": params.get("protocolVersion", "2025-06-18"),
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "local-llm", "version": "0.1.0"},
        }
    if method == "ping":
        return {}
    if method == "tools/list":
        return {"tools": [
            {
                "name": name,
                "description": f"{spec['description']} (model: {MODEL})",
                "inputSchema": {"type": "object", "properties": spec["properties"], "required": spec["required"]},
            }
            for name, spec in TOOLS.items()
        ]}
    if method == "tools/call":
        name = params.get("name")
        spec = TOOLS.get(name)
        if not spec:
            return {"content": [{"type": "text", "text": f"Unknown tool: {name}"}], "isError": True}
        try:
            text = call_llm(spec["system"], build_prompt(name, params.get("arguments") or {}))
            return {"content": [{"type": "text", "text": text}]}
        except urllib.error.URLError as e:
            err = f"Could not reach local LLM at {BASE_URL}: {e}"
        except Exception as e:  # noqa: BLE001
            err = f"{type(e).__name__}: {e}"
        return {"content": [{"type": "text", "text": err}], "isError": True}
    raise LookupError(method)


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        if "id" not in msg:  # notification
            continue
        try:
            reply = {"jsonrpc": "2.0", "id": msg["id"], "result": handle(msg)}
        except LookupError as e:
            reply = {"jsonrpc": "2.0", "id": msg["id"], "error": {"code": -32601, "message": f"Method not found: {e}"}}
        except Exception as e:  # noqa: BLE001
            reply = {"jsonrpc": "2.0", "id": msg["id"], "error": {"code": -32603, "message": str(e)}}
        sys.stdout.write(json.dumps(reply) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
