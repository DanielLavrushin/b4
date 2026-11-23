import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "b4-mcp.mjs");

const initResult = { protocolVersion: "2025-11-25", capabilities: {}, serverInfo: { name: "stand-in", version: "0" } };

function run(args, limitMs = 10000, env = process.env) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(process.execPath, [script, ...args], { stdio: ["ignore", "pipe", "pipe"], env });
    let out = "";
    let err = "";
    child.stdout.on("data", (c) => (out += c));
    child.stderr.on("data", (c) => (err += c));
    const killer = setTimeout(() => child.kill("SIGKILL"), limitMs);
    child.on("close", (code) => {
      clearTimeout(killer);
      resolve({ code, ms: Date.now() - started, out, err });
    });
  });
}

function readBody(req) {
  return new Promise((resolve) => {
    let b = "";
    req.on("data", (c) => (b += c));
    req.on("end", () => resolve(JSON.parse(b)));
  });
}

async function standIn(t, answer) {
  const srv = http.createServer(async (req, res) => {
    const msg = await readBody(req);
    if (msg.method === "initialize") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ jsonrpc: "2.0", id: msg.id, result: initResult }));
      return;
    }
    answer(req, res, msg);
  });
  await new Promise((resolve) => srv.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    srv.closeAllConnections();
    srv.close();
  });
  return `http://127.0.0.1:${srv.address().port}/api/mcp`;
}

test("--timeout bounds the whole wait, even while the server keeps sending events", async (t) => {
  const url = await standIn(t, (req, res) => {
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    const beat = setInterval(() => res.write(": keepalive\n\n"), 100);
    res.on("close", () => clearInterval(beat));
  });
  const r = await run(["--timeout", "1", "tools", url], 5000);
  assert.equal(r.code, 1, `exit ${r.code} after ${r.ms} ms`);
  assert.ok(r.ms < 3000, `took ${r.ms} ms`);
  assert.match(r.err, /no answer within 1 s/);
});

function answerJSON(res, id, result) {
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ jsonrpc: "2.0", id, result }));
}

test("restore refuses a fingerprint from the same address with another query string", async (t) => {
  const reverts = { a: 0, b: 0 };
  const url = await standIn(t, (req, res, msg) => {
    const router = new URL(req.url, "http://stand-in").searchParams.get("router");
    const structured = {
      b4_get_config: { config: { system: { router }, sets: [] } },
      b4_list_sets: { sets: [] },
      b4_revert_last_change: { reverted: false, note: "nothing to undo", remaining_changes: 0 },
    }[msg.params.name];
    if (msg.params.name === "b4_revert_last_change") reverts[router]++;
    answerJSON(res, msg.id, { content: [], structuredContent: structured });
  });
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "b4-mcp-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const snap = path.join(dir, "a.json");

  assert.equal((await run(["snap", `${url}?router=a`, snap])).code, 0);

  const other = await run(["restore", `${url}?router=b`, snap]);
  assert.equal(other.code, 2, other.err);
  assert.match(other.err, /another query string, not this address; nothing was undone/);
  assert.deepEqual(reverts, { a: 0, b: 0 });

  const same = await run(["restore", `${url}?router=a`, snap]);
  assert.equal(same.code, 0, same.err);
  assert.equal(JSON.parse(same.out).restored, true);
  assert.deepEqual(reverts, { a: 0, b: 0 });
});

test("servers expands ${VAR} in addresses and never reads the headers", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "b4-mcp-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const config = path.join(dir, "mcp.json");
  fs.writeFileSync(
    config,
    JSON.stringify({
      mcpServers: {
        env: { type: "http", url: "${B4_MCP_TEST_URL}", headers: { Authorization: "Bearer ${B4_MCP_TEST_UNSET}" } },
        fallback: { type: "http", url: "${B4_MCP_TEST_UNSET:-https://fallback.example/api/mcp}" },
        unset: { type: "http", url: "${B4_MCP_TEST_UNSET}" },
        plain: { type: "http", url: "https://plain.example/api/mcp?token=secret" },
      },
    }),
  );
  const env = { ...process.env, B4_MCP_TEST_URL: "https://env.example:7000/api/mcp?token=secret" };
  delete env.B4_MCP_TEST_UNSET;
  const r = await run(["--config", config, "servers"], 10000, env);
  assert.equal(r.code, 0, r.err);
  const listed = JSON.parse(r.out);
  assert.deepEqual(
    listed.map((s) => [s.name, s.url]),
    [
      ["env", "https://env.example:7000/api/mcp"],
      ["fallback", "https://fallback.example/api/mcp"],
      ["unset", null],
      ["plain", "https://plain.example/api/mcp"],
    ],
  );
  assert.match(listed[2].note, /B4_MCP_TEST_UNSET is not set/);
  assert.doesNotMatch(r.out, /secret/);
});

test("tools counts each tool's bytes as Go's json.Marshal writes them", async (t) => {
  const tool = { name: "t", description: "<a> & b\u2028", inputSchema: { type: "object", properties: {} } };
  const url = await standIn(t, (req, res, msg) => answerJSON(res, msg.id, { tools: [tool] }));
  const r = await run(["tools", url]);
  assert.equal(r.code, 0, r.err);
  const goJSON = '{"name":"t","description":"\\u003ca\\u003e \\u0026 b\\u2028","inputSchema":{"type":"object","properties":{}}}';
  assert.deepEqual(JSON.parse(r.out).tools, [{ name: "t", bytes: Buffer.byteLength(goJSON) }]);
});

test("a result larger than a pipe's buffer reaches the reader whole", async (t) => {
  const lines = Array.from({ length: 4000 }, (_, i) => `line ${i} ${"x".repeat(40)}`);
  const url = await standIn(t, (req, res, msg) => answerJSON(res, msg.id, { content: [], structuredContent: { lines } }));
  const r = await run(["call", url, "b4_logs_tail"]);
  assert.equal(r.code, 0, r.err);
  assert.ok(r.out.length > 200000, `got ${r.out.length} bytes`);
  assert.equal(JSON.parse(r.out).lines.length, lines.length);
});

function boxEnv(t, files, config) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "b4-mcp-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  for (const [name, lines] of Object.entries(files)) fs.writeFileSync(path.join(dir, `${name}.env`), `${lines.join("\n")}\n`);
  const env = { ...process.env, BOXES_DIR: dir };
  delete env.B4_MCP_CONFIG;
  delete env.B4_MCP_TOKEN;
  if (!config) return { dir, env };
  const file = path.join(dir, "mcp.json");
  fs.writeFileSync(file, JSON.stringify({ mcpServers: config }));
  return { dir, env, file };
}

async function tokenSeen(t) {
  const seen = [];
  const url = await standIn(t, (req, res, msg) => {
    seen.push(req.headers.authorization);
    answerJSON(res, msg.id, { tools: [] });
  });
  return { url, seen };
}

test("a box name takes its address and token from the box file", async (t) => {
  const { url, seen } = await tokenSeen(t);
  const forms = {
    plain: [`B4_MCP_ENDPOINT=${url}`, "B4_MCP_TOKEN=box-secret"],
    spaced: [`B4_MCP_ENDPOINT = ${url} `, "B4_MCP_TOKEN := box-secret"],
    commented: ["#B4_MCP_TOKEN=old", `B4_MCP_ENDPOINT=${url} # tunnel`, "B4_MCP_TOKEN ?= box-secret"],
    last: ["B4_MCP_TOKEN=old", `B4_MCP_ENDPOINT=${url}`, "B4_MCP_TOKEN=box-secret", "B4_MCP_TOKEN_OLD=older"],
  };
  const { env } = boxEnv(t, forms);
  for (const name of Object.keys(forms)) {
    const r = await run(["tools", name], 10000, { ...env, B4_MCP_TOKEN: "from-the-environment" });
    assert.equal(r.code, 0, `${name}: ${r.err}`);
    assert.equal(seen.pop(), "Bearer box-secret", name);
    assert.doesNotMatch(r.out + r.err, /box-secret/);
  }
});

test("make reads a box's values the way b4-mcp.mjs does", async (t) => {
  const { url, seen } = await tokenSeen(t);
  const { dir, env } = boxEnv(t, { odd: [`B4_MCP_ENDPOINT = ${url} # note`, "B4_MCP_TOKEN = a$$b\\#c # note"] });
  const mk = path.join(dir, "show.mk");
  fs.writeFileSync(mk, `include ${path.join(dir, "odd.env")}\n$(info [$(strip $(B4_MCP_ENDPOINT))][$(strip $(B4_MCP_TOKEN))])\nall: ;\n`);
  const made = await new Promise((resolve) => {
    const child = spawn("make", ["-s", "-f", mk], { stdio: ["ignore", "pipe", "pipe"] });
    let out = "";
    child.stdout.on("data", (c) => (out += c));
    child.on("error", () => resolve(null));
    child.on("close", () => resolve(out.split("\n")[0]));
  });
  if (made === null) return t.skip("make is not installed");
  const r = await run(["tools", "odd"], 10000, env);
  assert.equal(r.code, 0, r.err);
  assert.equal(`[${url}][${seen.pop().replace("Bearer ", "")}]`, made);
});

test("a box without both variables is refused by the name of the missing one", async (t) => {
  const { env } = boxEnv(t, {
    none: ["ARCH=arm64"],
    endpoint: ["B4_MCP_ENDPOINT=http://127.0.0.1:1/api/mcp"],
    token: ["B4_MCP_TOKEN=box-secret"],
  });
  for (const [name, missing] of [
    ["none", /does not set B4_MCP_ENDPOINT and B4_MCP_TOKEN/],
    ["endpoint", /does not set B4_MCP_TOKEN/],
    ["token", /does not set B4_MCP_ENDPOINT/],
  ]) {
    const r = await run(["tools", name], 10000, env);
    assert.equal(r.code, 2, r.err);
    assert.match(r.err, missing);
    assert.doesNotMatch(r.err, /box-secret/);
  }
});

test("a box's token is never sent over plain http to a public address or a host name", async (t) => {
  const refused = ["http://router.example/api/mcp", "http://203.0.113.5:7000/api/mcp", "http://[2001:db8::1]/api/mcp", "http://127.0.0.1.example/api/mcp", "http://172.32.0.1/api/mcp"];
  const boxes = Object.fromEntries(refused.map((u, i) => [`r${i}`, [`B4_MCP_ENDPOINT=${u}`, "B4_MCP_TOKEN=box-secret"]]));
  const allowed = ["https://router.example/api/mcp", "http://192.168.1.1:7000/api/mcp", "http://10.0.0.1/api/mcp", "http://172.16.0.1/api/mcp", "http://[::1]:7000/api/mcp", "http://[fd00::1]/api/mcp", "http://localhost:7000/api/mcp"];
  allowed.forEach((u, i) => (boxes[`a${i}`] = [`B4_MCP_ENDPOINT=${u}`, "B4_MCP_TOKEN=box-secret"]));
  const { env } = boxEnv(t, boxes);
  for (const i of refused.keys()) {
    const r = await run(["tools", `r${i}`], 10000, env);
    assert.equal(r.code, 2, `${refused[i]}: ${r.err}`);
    assert.match(r.err, /would send the token unencrypted/);
  }
  const r = await run(["servers"], 10000, env);
  const notes = Object.fromEntries(JSON.parse(r.out).map((s) => [s.name, s.note]));
  for (const i of allowed.keys()) assert.equal(notes[`a${i}`], undefined, allowed[i]);
  for (const i of refused.keys()) assert.match(notes[`r${i}`], /unencrypted/);
});

test("an explicit config file leaves the boxes out, and a name is never a path", async (t) => {
  const { url, seen } = await tokenSeen(t);
  const { dir, env, file } = boxEnv(
    t,
    { same: [`B4_MCP_ENDPOINT=${url}`, "B4_MCP_TOKEN=box-secret"] },
    { same: { type: "http", url, headers: { Authorization: "Bearer config-secret" } } },
  );
  assert.equal((await run(["tools", "same"], 10000, env)).code, 0);
  assert.equal(seen.pop(), "Bearer box-secret");
  assert.equal((await run(["--config", file, "tools", "same"], 10000, env)).code, 0);
  assert.equal(seen.pop(), "Bearer config-secret");
  assert.equal((await run(["tools", "same"], 10000, { ...env, B4_MCP_CONFIG: file })).code, 0);
  assert.equal(seen.pop(), "Bearer config-secret");
  const r = await run(["tools", `../${path.basename(dir)}/same`], 10000, env);
  assert.equal(r.code, 2, r.err);
  assert.equal(seen.length, 0);
});

test("servers lists boxes with MCP and their addresses, never a token", async (t) => {
  const { env } = boxEnv(t, {
    on: ["B4_MCP_ENDPOINT=https://on.example/api/mcp?token=query-secret", "B4_MCP_TOKEN=box-secret"],
    off: ["ARCH=arm64", "SFTP_PASS=ssh-secret"],
  });
  const r = await run(["servers"], 10000, env);
  assert.equal(r.code, 0, r.err);
  const listed = JSON.parse(r.out);
  assert.deepEqual(listed.find((s) => s.name === "on"), { name: "on", type: "box", url: "https://on.example/api/mcp" });
  assert.equal(listed.find((s) => s.name === "off"), undefined);
  assert.doesNotMatch(r.out, /secret/);
});
