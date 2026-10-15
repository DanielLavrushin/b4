---
sidebar_position: 50
title: Integrations
description: The IPinfo token, the AI assistant, the MCP server and the Community Hub connection.
---

The **Integrations** tab connects b4 to outside services. **AI Assistant** lets b4 ask an AI provider about its settings, while **MCP server** lets an AI application ask b4. Changes are saved with **Save Changes** in the page header.

## IPinfo {#ipinfo}

| Field | What it does | Default |
| --- | --- | --- |
| **Access token** | The token for ipinfo.io. With it, the **Add to a set** dialog of the [Traffic](../connections.md#adding-addresses-and-networks-to-sets) page and the dashboard offers **Enrich with IPInfo**, which shows the organization, host name and location of an address. b4 requests these details from the router. **Get a token** opens ipinfo.io, where the free tier is enough. | Empty |

## AI Assistant {#ai-assistant}

The assistant explains settings inside the web interface. b4 sends the question to the chosen AI provider. While the assistant is on, an AI icon shows next to many fields, most of them in the set editor, and **Ask AI to explain this setting** opens the answer.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable AI assistance** | The switch in the card header. | Off |
| **OpenAI**, **Anthropic**, **Ollama**, **OpenAI-compatible** | The provider. With OpenAI or Anthropic, the questions and the parts of the configuration they touch leave the router for that provider. With Ollama they stay local while **Endpoint** points to the router or a computer in the local network. | None |
| **Model** | The model, picked from the provider's list or typed in. | Empty |
| **API key** | Required for OpenAI and Anthropic, optional for OpenAI-compatible. **Set key** stores it on the router in a file of its own, not in the configuration. | Empty |

The **Advanced** section holds the connection details.

| Field | What it does | Default |
| --- | --- | --- |
| **Endpoint** | The address of the provider's API, filled in for each provider. Another value points to another server, such as a self-hosted one. | Per provider |
| **Max tokens** | The longest answer, in tokens. | `1024` |
| **Temperature** | From 0 to 2. Lower values give more predictable answers. | `0.2` |
| **Timeout (s)** | How long one request may take. | `120` |

While the provider cannot be used, for example without a key, the card shows the reason.

## MCP server {#mcp-server}

The MCP server lets an AI application, such as Claude Desktop, Cursor or LM Studio, read the state of b4 and, when allowed, change its settings. The model runs in that application, so b4 needs no API key for it. [Connecting an AI assistant](./mcp.md) shows the setup.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable MCP server** | The switch in the card header. The server uses the port of the web interface. | Off |
| **Access token** | The token the AI application presents. **Generate** creates one, **Regenerate** replaces it. Without a token, the application needs a [web login](./security.md#authentication) while one is set, and that login ends when b4 restarts. Without either, anyone who reaches the port can use the server, including changes and probes when they are allowed. | Empty |
| **Allow configuration changes** | Lets the AI change the sets and the MTProto, SOCKS5 and logging settings, not only read them. Credentials and all other settings stay read-only. | Off |
| **Allow active probes** | Lets the AI make b4 fetch a domain and run Discovery, which sends real traffic from the router. | Off |
| **Client configuration** | The entry to paste into the AI application, with the address and the token. | - |

## Community Hub {#community-hub}

The connection to the Community Hub, where b4 users publish sets and report whether they work. [Community Hub](../community/index.md#switching-it-on) describes the card in full.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Community Hub** | Adds the **Community** page, the hourly sync and the **Share with other b4 users** section of the set editor. | Off |
| **Hub mirrors** | Addresses of the same hub, one per line, tried in order. Empty means `https://hub.b4core.app`. | Empty |
| **Hub public key** | Only for a self-hosted hub with its own key. The key of `hub.b4core.app` is built in. | Empty |

**Hub status** shows the catalogue, the last sync with its error and the hub addresses, and **Sync now** syncs at once. **Author identity** shows the key that signs the sets and reports sent from this router. **Show recovery code** gives the code that moves this key to another router, where **Restore from code** takes it in.
