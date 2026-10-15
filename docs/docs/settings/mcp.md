---
sidebar_position: 7
title: Connecting an AI assistant
description: Letting an AI application read b4's state over MCP and, when allowed, change settings.
---

An AI application such as Claude Desktop, LM Studio, Cursor or Jan can read b4's state over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP). It then answers questions about it: which set matches a domain, whether traffic reached b4, what the log says. The model runs inside that application. b4 contacts no AI provider and needs no API key.

## Requirements

- An AI application that connects to MCP servers over HTTP.
- Access from that application to the b4 web interface. The MCP endpoint uses the web server's address, port and TLS certificate, and it is unreachable while the web server is off.

## Setup

1. Open **Settings > Integrations** and turn on **Enable MCP server** on the **MCP server** card.
2. Click **Generate** next to **Access token**.
3. Click **Save Changes**. The token is stored only on save.
4. Click **Copy** under **Client configuration**. The entry holds the endpoint, which is the address the web interface is open at followed by `/api/mcp`, and an `Authorization` header with the token.
5. Paste the entry into the MCP configuration of the AI application.

![MCP server card](/img/mcp/20261004000301.png)

VS Code reads `.vscode/mcp.json` in the workspace, or the user-level `mcp.json`. Its `inputs` block makes the editor ask for the token instead of keeping it in the file:

```json
{
  "inputs": [
    {
      "id": "b4-token",
      "type": "promptString",
      "description": "b4 MCP token",
      "password": true
    }
  ],
  "servers": {
    "b4-asuswrt": {
      "type": "http",
      "url": "https://192.168.1.1:7000/api/mcp",
      "headers": {
        "Authorization": "Bearer ${input:b4-token}"
      }
    }
  }
}
```

LM Studio uses the same kind of file with `mcpServers` as the top-level key and no `type`. Several entries can sit side by side, for example a router and a local instance:

```json
{
  "mcpServers": {
    "b4-asuswrt": {
      "url": "https://192.168.1.1:7000/api/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    },
    "b4-local": {
      "url": "http://localhost:7000/api/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

An application without an equivalent of `inputs` keeps the token in the file as plain text, so such a file does not belong in a repository.

### The token {#token}

While a token is set, it is the only credential the MCP endpoint accepts, and no other part of b4 accepts it. It keeps working when b4 restarts. **Regenerate** replaces it, and the application then needs the new one.

Without a token, the endpoint accepts the login token of the web interface instead. That token expires after a day without use and is discarded on every restart, so an application set up with it soon loses access.

:::warning The token on an untrusted network
The token travels in a header of every request. Over plain HTTP anyone on the network path can read it and reuse it, so without [HTTPS](./security.md#https) the port belongs on a trusted network only. With no MCP token and no username and password under **Settings > System > Web Server**, anyone who reaches the port can read b4's status, configuration and diagnostics.
:::

## What the assistant can do {#tools}

By default the assistant only reads. It can report b4's status and counters, show the configuration and the sets, and check which set covers a domain. It can also look up GeoSite and GeoIP categories, read the recent connections, the error log and the update log, and run diagnostics. [API and MCP tools](../advanced/api.md#mcp-tools) lists every tool and the permission it needs.

## Allowing changes and probes {#changing-settings}

Two switches on the card widen what the assistant may do. Both are off by default, and each works without the other.

| Switch | What the assistant may do |
| --- | --- |
| **Allow configuration changes** | Change the settings inside sets, the MTProto and SOCKS5 settings and the logging settings. Create, move, enable and delete sets, edit their targets, and undo its own changes |
| **Allow active probes** | Make the router fetch a domain to see whether it loads, and run [Discovery](../discovery.md) to find a working strategy. Private and local addresses are refused |

A change goes through the same checks as a save in the web interface and applies at once. It appears under **Recent changes** on the [dashboard](../dashboard.md#recent-changes) as **AI agent changed a setting**.

Some settings stay out of the assistant's reach whatever the switches say, because a wrong value could cut off access to the router or open it to the internet:

- credentials, the web server settings and the MCP settings;
- the capture engine and the firewall backend;
- the **Expose to internet** switches, and any change that would open a port while one of them is on;
- packet marks, routing tables and DSCP values;
- a set's id, its Discovery addresses and its watchdog switch, which only the watchdog actions change, under their own permissions;
- the log directory and the locations of the geodata files.

[API and MCP tools](../advanced/api.md#writable-settings) gives the details.

## Undoing a change {#undoing-a-change}

Asking the assistant to undo a change, such as "that made it worse, put it back", restores the configuration from before its last change and applies it at once. Asking again goes one more change back. The history covers only changes made over MCP since b4 started, and edits in the web interface are not part of it.

An undo is refused when the configuration changed after the change being undone: in the web interface, by the watchdog or by another tool. It is also refused when it would turn a watchdog on, or bring back addresses a watchdog checks, while **Allow active probes** is off.

## What the assistant sees {#what-is-stripped}

Tool output may be passed on to a third-party model, so b4 removes passwords, tokens and secrets from it. Host names and URLs are passed as configured, and the diagnostics include the router's hostname, its interface addresses and its firewall rules. [API and MCP tools](../advanced/api.md#what-is-stripped) lists what is removed.

## Checking the result

A question such as "Is b4 running, and which capture engine is active?" gets an answer with the version and the capture engine. Each tool call adds a line to the log on the [Logs](../logs.md) page, with the tool and the address of the caller.

## When it does not work

- **The application cannot connect.** The MCP server is off, the token in the application differs from the saved one, or the web server's port cannot be reached from that computer. The **Logs** page shows each refused request with the reason. The token itself never appears in the log.
- **A test request gets 405.** The endpoint answers `POST` only, so a `GET` from a browser or a script gets 405. This is normal.
- **A web page cannot reach the endpoint.** A request from a browser page is accepted only when the page's origin is b4's own address written as an IP address or `localhost`. AI applications send no origin and are not affected. A host name is allowed by adding it to the allowed origins in the [configuration file](../advanced/config.md#settings-without-a-field).
- **The assistant says it cannot change a setting.** **Allow configuration changes** is off, or the setting is one that MCP never changes.
