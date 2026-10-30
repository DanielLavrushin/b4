---
name: b4-mcp
description: Add or change a tool, description, schema, topic text or write guard in b4's own MCP server (src/http/handler/mcp.go and mcp_write.go, the corpus in src/ai/topics.json) - within the tools/list byte budget, the limits real AI clients enforce (LM Studio, OpenAI and Azure, Gemini CLI, VS Code), the write guards, and the switch texts. Use before touching any MCP tool, its input or output struct, its annotations, an MCP-facing message, or the writable paths.
---

# b4's MCP server

b4 serves MCP at `/api/mcp`, off by default behind `system.web_server.mcp.enabled` and its own
token. Read tools are in `mcp.go`. Writes sit behind a second switch, `allow_writes`, in
`mcp_write.go`, and the tools that fetch a site or run a Discovery search sit behind "Allow
active probes". Each switch changes the tool list a client sees.

## The byte budget

`TestToolsListStaysWithinContextBudget` caps the whole `tools/list` answer, output schemas
included: 17,000 bytes with the default switches, 30,000 with writes and probes on. Small local
models get it in every request. On 2026-10-30 the default surface was at 97% and the full one
32 bytes short of its cap, so new tool text needs a trim somewhere else. The test logs
the current size; run it before and after a change:

```sh
go -C src test ./http/handler -run 'TestToolsListStaysWithinContextBudget' -count=1 -v
```

## What clients enforce

| Client | Limit | Guard |
| --- | --- | --- |
| LM Studio | Every input schema has `properties`, even an empty one; one tool without it fails the whole chat request. | `TestToolInputSchemasAlwaysListProperties` |
| OpenAI, Azure | A tool description over 1,024 characters is rejected with a 400. | `TestToolDescriptionsFitOpenAILimit` |
| Gemini CLI | Adds a boolean `wait_for_previous` to every tool and sends it; b4's closed schemas would reject it, so a middleware strips only that key. Keep it. | `mcpWaitForPrevious` |
| VS Code | Prefixes tool names with the server title cut to 13 characters. | none |
| go-sdk | Validates every tool's output against its inferred output schema and fails the call on a mismatch. | the tool tests |

Schemas are objects, never booleans (`TestToolSchemasAreObjectsNotBooleans`). Changing an output
field breaks TypeScript-SDK clients that cached the old `tools/list` across a b4 upgrade, so
prefer adding fields to renaming them.

## Writing tool text

- AI-facing messages that point at a switch name its place through `mcpSwitchWhere`: "in
  b4's own web interface (Settings -> Integrations -> MCP server), not in the AI app or the
  IDE". A model otherwise tells the user to look in the wrong program.
- b4 runs go-sdk in stateless mode, which never sends `tools/list_changed`. A tool added by a
  switch appears only when the client lists tools again, usually on reconnect; a withdrawn one
  answers with go-sdk's bare `unknown tool`. Messages about a switch say that.
- `src/ai/topics.json` answers `b4_get_topic` and the UI's "Ask AI to explain". Models treat it
  as authoritative, so every sentence is checked against the code, and a false one is a bug
  to fix like any other.
- Annotations tell the truth: a tool marked `readOnlyHint` changes nothing, not even
  temporarily (`TestToolAnnotationsMatchWhatRepeatedCallsDo`).
- Input fields carry `jsonschema:"..."` descriptions that name an example and the tool that
  lists accepted values, the way `b4_set_config_value`'s `path` points at
  `b4_list_writable_paths`.

## Write guards

- Only the subtrees in `mcpWritableRoots` can be written: sets, `system.mtproto`,
  `system.socks5`, `system.logging`. Anything else is refused with a reason from
  `mcpDeniedPathHint`.
- A field tagged `mcp:"deny"` is refused even inside those roots: credentials, `expose`
  switches, anything that opens a port or that an AI must not change. The MCP server's own
  settings are never writable, so an AI cannot widen its own permissions.
- A write that would open a port to the internet, or run the SOCKS5 server without a password
  or an allowed source list, is refused over MCP; the reply says it can be made in the web
  interface.
- A string with a fixed set of values that `config.Validate` does not enforce goes into
  `mcpEnums`, so the tool can list it and refuse anything else.
- Every write reports the value read back after the save. `changed: false` means b4 did not
  accept it.

## Domain checks

`b4_check_domain` must not count relation `covers` (a set lists only a subdomain) as coverage:
an ad-block set listing `ads.youtube.com` would make `youtube.com` read as covered. Matching
costs entries times name length on big sets, so names over 253 bytes are ignored and each set
entry is parsed once.

## Verify

`go -C src test ./http/handler -count=1` covers the guards above. Then connect a real
client to a test box (a raw JSON-RPC client over HTTP works against a local degraded b4) and
call the changed tool. `/b4-check` for the rest.
