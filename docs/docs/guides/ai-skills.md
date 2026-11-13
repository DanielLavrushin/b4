---
title: Skills for AI assistants
description: Install the b4 skills, which guide an AI assistant through checking and adjusting b4 over its MCP server.
---

The b4 skills are instructions that an AI assistant follows while it uses the tools of b4's [MCP server](../settings/mcp.md). A skill uses the open [Agent Skills](https://agentskills.io/) format. The skills are published in the [b4-agent-skills](https://github.com/DanielLavrushin/b4-agent-skills) repository.

## The skills

| Skill | What it does |
| --- | --- |
| **b4-connect** | Connects the AI app to b4's MCP server and fixes the connection when b4's tools are missing: the app's entry, the token, certificate errors, a reconnect after a switch. Works without b4's tools. |
| **b4-troubleshoot** | Finds out why a site, an app or b4 itself does not work: a site that does not open, Discovery that finds nothing, a problem that began after an update, a clash with firmware features or another proxy. |
| **b4-unblock-site** | Confirms the block with b4's own test of the site, runs Discovery after the user agrees and applies an approved result as a set. Then tests the site again and says how to undo the change. Stops when the router already loads the site. |
| **b4-proxy-routing** | Sets up or fixes sets that send sites or devices through a VPN interface or a SOCKS5 upstream, and works out which set takes a connection. |
| **b4-find-app-hosts** | Finds which hosts and protocols an app on one device uses, such as a TV's YouTube app, Discord voice or a game. Says whether b4 sees that traffic, which set takes it and what to add to a set. Reports only that device. |
| **b4-report-issue** | Writes a short bug report for b4's GitHub issues, without the router's host name, addresses and other private data. Checks first whether a newer release fixes the problem. Without b4's tools, it works from text pasted into the chat. |

Every skill has the assistant:

- read b4's state and b4's own description of a setting before it answers;
- test a site, run Discovery or change a setting only on request, and only while the matching switch is on;
- say that a site opens only after b4's own test of that site succeeded;
- leave other devices' traffic and the router's private data out of answers and reports.

## Requirements

- b4's MCP server is on and connected to the AI app, as described in [Connecting an AI assistant](../settings/mcp.md). Reading b4's state needs nothing more.
- Testing a site and running Discovery need **Allow active probes**. Changes to settings and sets need **Allow configuration changes**. Both switches are on the **MCP server** card under **Settings > Integrations**, see [Allowing changes and probes](../settings/mcp.md#changing-settings).
- An AI app from the table below that loads skills and runs on a computer that reaches the router.

## AI apps

| App | Works with the skills | Note |
| --- | --- | --- |
| Claude Code | yes | Installs with the skills CLI or as a plugin. |
| Claude Desktop | yes | The **Code** tab works like Claude Code. In Chat, the plugin installs under **Customize > Plugins**, and b4's MCP server goes into the app's configuration file through a local bridge such as [mcp-remote](https://github.com/geelen/mcp-remote). Connectors added in its settings connect from Anthropic's cloud and cannot reach the router. |
| ChatGPT desktop app | yes | Installs as a plugin. b4's MCP server goes into the MCP configuration that the app shares with the Codex CLI. |
| Codex CLI, Gemini CLI, Cursor, VS Code with GitHub Copilot | yes | Installs with the skills CLI. |
| Antigravity CLI | yes | The b4 plugin loads with its skills. |
| claude.ai, ChatGPT on the web | no | They connect to MCP servers from the vendor's cloud and cannot reach a router on a home network. |
| LM Studio and Jan desktop apps | no | They use b4's MCP tools without the skills. |
| Ollama | no | It does not connect to MCP servers on its own. |
| b4's **AI Assistant** card | no | It explains settings in the web interface and does not use the skills. |

## Installing

### With the skills CLI

The skills CLI needs [Node.js](https://nodejs.org/).

1. Open a terminal in the folder that the AI app works in.
2. Run:

   ```sh
   npx skills add DanielLavrushin/b4-agent-skills
   ```

3. Choose the skills and the AI apps when the command asks.

With `-g`, the skills are installed for the user account instead of one folder. Options answer the questions in advance, for example every skill for Claude Code:

```sh
npx skills add DanielLavrushin/b4-agent-skills --skill '*' -a claude-code -y
```

The other app names are `codex`, `gemini-cli`, `cursor` and `github-copilot`.

### As a Claude Code plugin

In Claude Code:

```text
/plugin marketplace add DanielLavrushin/b4-agent-skills
/plugin install b4@b4-agent-skills
```

From a terminal:

```sh
claude plugin marketplace add DanielLavrushin/b4-agent-skills
claude plugin install b4@b4-agent-skills
```

### In the ChatGPT desktop app

The marketplace is added with the Codex CLI.

1. Run in a terminal:

   ```sh
   codex plugin marketplace add DanielLavrushin/b4-agent-skills
   ```

2. Restart the ChatGPT desktop app.
3. Open **Plugins**, choose the `b4-agent-skills` source and install `b4`.

## Using the skills

A request in plain words is enough, and the app loads the matching skill by itself. Examples:

- "discord.com stopped opening after the b4 update"
- "make meduza.io open through b4"
- "route youtube.com through the wg0 interface"
- "which servers does Discord on my PC use, and does b4 handle them?"
- "help me write a bug report for b4"
- "the AI app does not show b4's tools"

In Claude Code, a skill can also be called by name, for example `/b4-troubleshoot`. With the plugin, the name is `/b4:b4-troubleshoot`.

## Checking the result

A question such as "is b4 running?" gets an answer with b4's version once the MCP connection works. In Claude Code, calling a skill by name, such as `/b4-troubleshoot`, shows whether the skills are installed.

## When it does not work

- **The app does not load the skills.** The app does not support skills, see the table, or the skills CLI installed them into a folder the app does not open. With `-g`, they are installed for every folder.
- **The app shows no b4 tools.** A request to connect to b4 loads b4-connect, which works without b4's tools. [When it does not work](../settings/mcp.md#when-it-does-not-work) on the MCP page lists the usual causes.
- **The assistant says it cannot test a site or change a setting.** The matching switch is off, or the app connected before the switch was turned on. Reconnecting the app to the MCP server fetches the current list of tools.
- **The problem is still there.** The assistant lists what to attach to a report, and b4-report-issue writes it. [Reporting a problem](../troubleshooting/reporting.md) covers where to send it.
