#!/usr/bin/env node
import crypto from "node:crypto";
import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import path from "node:path";
import { fileURLToPath } from "node:url";

const protocolVersion = "2025-11-25";
const historyLimit = 20;

const usage = `usage: node toolkit/b4-mcp.mjs [options] <command> ...

commands:
  servers                        the servers in the config file and their addresses
  tools <server>                 the tools a server lists, with the size of each in bytes
  call <server> <tool> [json]    call a tool with JSON arguments and print its result as JSON
  snap <server> <file>           save a fingerprint of b4's configuration to file
  restore <server> <file>        undo b4's MCP changes one at a time until the configuration
                                 matches the fingerprint in file, listing each change undone

<server> is a name from the config file, or the http(s) address of a b4's /api/mcp; an
address takes its token from B4_MCP_TOKEN. Results are compact JSON: pipe them to jq.

options:
  --config <file>  MCP client configuration with an mcpServers map, as Claude Code writes it;
                   default B4_MCP_CONFIG, else .mcp.json at the repository root. \${VAR} and
                   \${VAR:-default} in its addresses and headers are read from the environment
  --insecure       accept a certificate that does not verify, such as b4's self-signed one;
                   NODE_EXTRA_CA_CERTS=<ca.pem> verifies it instead
  --timeout <s>    seconds to wait for one answer (default 120)
  --max <n>        restore: undo at most n changes (default ${historyLimit}, the length of b4's undo list)
`;

class UsageError extends Error {}

function parseArgs(argv) {
  const opt = { config: "", insecure: false, timeout: 120, max: historyLimit };
  const args = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) throw new UsageError(`${a} needs a value`);
      return argv[++i];
    };
    const count = (name, raw) => {
      const n = Number(raw);
      if (!Number.isInteger(n) || n < 1) throw new UsageError(`${name} takes a whole number above 0, not ${raw}`);
      return n;
    };
    switch (a) {
      case "--config":
        opt.config = value();
        break;
      case "--insecure":
        opt.insecure = true;
        break;
      case "--timeout":
        opt.timeout = count(a, value());
        break;
      case "--max":
        opt.max = count(a, value());
        break;
      case "-h":
      case "--help":
        args.length = 0;
        args.push("help");
        return { opt, args };
      default:
        if (a.startsWith("--")) throw new UsageError(`unknown option ${a}`);
        args.push(a);
    }
  }
  return { opt, args };
}

function configFile(opt) {
  if (opt.config) return opt.config;
  if (process.env.B4_MCP_CONFIG) return process.env.B4_MCP_CONFIG;
  return path.join(path.dirname(fileURLToPath(import.meta.url)), "..", ".mcp.json");
}

function readServers(file) {
  let raw;
  try {
    raw = fs.readFileSync(file, "utf8");
  } catch (e) {
    throw new UsageError(`cannot read ${file} (${e.code || e.message}); give --config <file> or a server address`);
  }
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (e) {
    throw new UsageError(`${file} is not valid JSON: ${e.message}`);
  }
  const servers = parsed && typeof parsed === "object" ? parsed.mcpServers : undefined;
  if (!servers || typeof servers !== "object") throw new UsageError(`${file} has no mcpServers map`);
  return servers;
}

function expand(value, where) {
  return String(value).replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}/g, (_, name, fallback) => {
    if (process.env[name] !== undefined) return process.env[name];
    if (fallback !== undefined) return fallback;
    throw new UsageError(`${name} is not set; ${where} uses it`);
  });
}

function shownAddress(raw) {
  try {
    const u = new URL(raw);
    return `${u.protocol}//${u.host}${u.pathname}`;
  } catch {
    return "(not a valid address)";
  }
}

function resolveServer(name, opt) {
  if (/^https?:\/\//i.test(name)) {
    const token = process.env.B4_MCP_TOKEN;
    return { name: shownAddress(name), url: name, headers: token ? { Authorization: `Bearer ${token}` } : {} };
  }
  const file = configFile(opt);
  const servers = readServers(file);
  const entry = servers[name];
  if (!entry) throw new UsageError(`no server ${name} in ${file}; it has: ${Object.keys(servers).join(", ") || "none"}`);
  if (!entry.url) throw new UsageError(`server ${name} in ${file} has no url: only HTTP servers can be called`);
  const where = `server ${name} in ${file}`;
  const headers = {};
  for (const [k, v] of Object.entries(entry.headers || {})) headers[k] = expand(v, where);
  return { name, url: expand(entry.url, where), headers };
}

function post(server, opt, body, protocol) {
  let url;
  try {
    url = new URL(server.url);
  } catch {
    throw new UsageError(`${server.name}: the address is not a valid URL`);
  }
  const lib = url.protocol === "https:" ? https : http;
  const data = JSON.stringify(body);
  const headers = {
    "Content-Type": "application/json",
    Accept: "application/json, text/event-stream",
    "Content-Length": Buffer.byteLength(data),
    ...server.headers,
  };
  if (protocol) headers["MCP-Protocol-Version"] = protocol;
  return new Promise((resolve, reject) => {
    let settled = false;
    let deadline;
    const settle = (done, value) => {
      if (settled) return;
      settled = true;
      clearTimeout(deadline);
      done(value);
    };
    const req = lib.request(url, { method: "POST", headers, rejectUnauthorized: !opt.insecure }, (res) => {
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("error", (e) => settle(reject, networkError(server, e)));
      res.on("end", () =>
        settle(resolve, { status: res.statusCode, type: res.headers["content-type"] || "", text: Buffer.concat(chunks).toString("utf8") }),
      );
    });
    deadline = setTimeout(() => {
      settle(reject, new Error(`${server.name}: no answer within ${opt.timeout} s`));
      req.destroy();
    }, opt.timeout * 1000);
    req.on("error", (e) => settle(reject, networkError(server, e)));
    req.end(data);
  });
}

function networkError(server, e) {
  const tls = ["DEPTH_ZERO_SELF_SIGNED_CERT", "SELF_SIGNED_CERT_IN_CHAIN", "UNABLE_TO_VERIFY_LEAF_SIGNATURE", "ERR_TLS_CERT_ALTNAME_INVALID", "CERT_HAS_EXPIRED"];
  const hint = tls.includes(e.code) ? ": the certificate does not verify; give --insecure, or NODE_EXTRA_CA_CERTS=<ca.pem>" : "";
  return new Error(`${server.name}: ${e.message}${hint}`);
}

function messages(res) {
  if (!res.type.includes("text/event-stream")) {
    const text = res.text.trim();
    return text ? [JSON.parse(text)] : [];
  }
  const out = [];
  for (const event of res.text.split(/\r?\n\r?\n/)) {
    const data = event
      .split(/\r?\n/)
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).replace(/^ /, ""))
      .join("\n");
    if (data) out.push(JSON.parse(data));
  }
  return out;
}

let nextId = 1;

async function request(session, method, params) {
  const id = nextId++;
  const res = await post(session.server, session.opt, { jsonrpc: "2.0", id, method, params }, session.protocol);
  const name = session.server.name;
  if (res.status === 401) throw new Error(`${name}: the MCP token was refused (HTTP 401)`);
  if (res.status < 200 || res.status > 299) throw new Error(`${name}: HTTP ${res.status}: ${res.text.trim().slice(0, 300)}`);
  let msg;
  try {
    msg = messages(res).find((m) => m && m.id === id);
  } catch (e) {
    throw new Error(`${name}: ${method}: the answer is not JSON: ${e.message}`);
  }
  if (!msg) throw new Error(`${name}: ${method}: no answer in the reply`);
  if (msg.error) throw new Error(`${name}: ${method}: ${msg.error.message}`);
  return msg.result;
}

async function connect(serverName, opt) {
  const session = { server: resolveServer(serverName, opt), opt, protocol: "" };
  const init = await request(session, "initialize", {
    protocolVersion,
    capabilities: {},
    clientInfo: { name: "b4-toolkit-mcp", version: "1" },
  });
  session.protocol = init.protocolVersion || protocolVersion;
  session.info = init.serverInfo || {};
  return session;
}

class ToolError extends Error {}

async function callTool(session, name, args) {
  const r = await request(session, "tools/call", { name, arguments: args });
  const text = (r.content || [])
    .filter((c) => c.type === "text")
    .map((c) => c.text)
    .join("\n");
  if (r.isError) throw new ToolError(`${name}: ${text || "the tool reported an error"}`);
  if (r.structuredContent !== undefined) return r.structuredContent;
  try {
    return JSON.parse(text);
  } catch {
    return { text };
  }
}

async function listTools(session) {
  const tools = [];
  let cursor;
  do {
    const r = await request(session, "tools/list", cursor ? { cursor } : {});
    tools.push(...(r.tools || []));
    cursor = r.nextCursor;
  } while (cursor);
  return tools;
}

function stable(value) {
  if (Array.isArray(value)) return `[${value.map(stable).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${stable(value[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

async function fingerprint(session) {
  const got = await callTool(session, "b4_get_config", {});
  const config = got && got.config;
  if (!config || typeof config !== "object") throw new Error("b4_get_config returned no configuration");
  if (config.system && config.system.web_server) delete config.system.web_server.mcp;
  const listed = await callTool(session, "b4_list_sets", {});
  return {
    address: shownAddress(session.server.url),
    hash: crypto.createHash("sha256").update(stable(config)).digest("hex").slice(0, 16),
    sets: ((listed && listed.sets) || []).map((s) => `${s.name}:${s.enabled ? "on" : "off"}`),
  };
}

function print(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function need(args, n, form) {
  if (args.length !== n) throw new UsageError(`usage: node toolkit/b4-mcp.mjs ${form}`);
}

async function restore(session, file, opt) {
  let want;
  try {
    want = JSON.parse(fs.readFileSync(file, "utf8"));
  } catch (e) {
    throw new UsageError(`cannot read the fingerprint in ${file}: ${e.code || e.message}`);
  }
  if (!want || typeof want.hash !== "string") throw new UsageError(`${file} holds no fingerprint from snap`);
  const address = shownAddress(session.server.url);
  if (want.address !== address) {
    throw new UsageError(`${file} was taken from ${want.address || "an unknown server"}, not ${address}; nothing was undone`);
  }
  const undone = [];
  for (;;) {
    const now = await fingerprint(session);
    if (now.hash === want.hash) {
      print({ restored: true, undone, sets: now.sets });
      return 0;
    }
    const stop = (reason) => {
      print({ restored: false, reason, undone, sets_now: now.sets, sets_wanted: want.sets });
      return 1;
    };
    if (undone.length >= opt.max) {
      return stop(`the configuration still differs after undoing ${opt.max} ${opt.max === 1 ? "change" : "changes"}`);
    }
    let r;
    try {
      r = await callTool(session, "b4_revert_last_change", {});
    } catch (e) {
      if (e instanceof ToolError) return stop(e.message);
      throw e;
    }
    if (!r.reverted) return stop(r.note || "b4 undid nothing");
    undone.push({ path: r.path, undone_value: r.undone_value, restored_to: r.restored_to, remaining: r.remaining_changes });
  }
}

async function main(argv) {
  const { opt, args } = parseArgs(argv);
  const [command, ...rest] = args;
  switch (command) {
    case undefined:
      process.stderr.write(usage);
      return 2;
    case "help":
      process.stdout.write(usage);
      return 0;
    case "servers": {
      need(rest, 0, "servers");
      const file = configFile(opt);
      const servers = readServers(file);
      print(
        Object.entries(servers).map(([name, entry]) => ({
          name,
          type: entry.type || (entry.url ? "http" : "stdio"),
          url: entry.url ? shownAddress(entry.url) : undefined,
        })),
      );
      return 0;
    }
    case "tools": {
      need(rest, 1, "tools <server>");
      const session = await connect(rest[0], opt);
      const tools = (await listTools(session)).map((t) => ({ name: t.name, bytes: Buffer.byteLength(JSON.stringify(t)) }));
      print({
        server: session.info.name,
        version: session.info.version,
        protocol: session.protocol,
        count: tools.length,
        total_bytes: tools.reduce((sum, t) => sum + t.bytes, 0),
        tools,
      });
      return 0;
    }
    case "call": {
      if (rest.length < 2 || rest.length > 3) throw new UsageError("usage: node toolkit/b4-mcp.mjs call <server> <tool> [json]");
      let toolArgs = {};
      if (rest[2] !== undefined) {
        try {
          toolArgs = JSON.parse(rest[2]);
        } catch (e) {
          throw new UsageError(`the arguments are not JSON: ${e.message}`);
        }
        if (!toolArgs || typeof toolArgs !== "object" || Array.isArray(toolArgs)) throw new UsageError("the arguments must be a JSON object");
      }
      const session = await connect(rest[0], opt);
      print(await callTool(session, rest[1], toolArgs));
      return 0;
    }
    case "snap": {
      need(rest, 2, "snap <server> <file>");
      const session = await connect(rest[0], opt);
      const fp = await fingerprint(session);
      fs.writeFileSync(rest[1], `${JSON.stringify(fp)}\n`);
      print(fp);
      return 0;
    }
    case "restore": {
      need(rest, 2, "restore <server> <file>");
      const session = await connect(rest[0], opt);
      return restore(session, rest[1], opt);
    }
    default:
      throw new UsageError(`unknown command ${command}; run with --help for the list`);
  }
}

main(process.argv.slice(2)).then(
  (code) => {
    process.exitCode = code;
  },
  (e) => {
    process.stderr.write(`b4-mcp: ${e.message}\n`);
    process.exitCode = e instanceof UsageError ? 2 : 1;
  },
);
