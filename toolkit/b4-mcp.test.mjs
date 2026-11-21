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

test("a result larger than a pipe's buffer reaches the reader whole", async (t) => {
  const lines = Array.from({ length: 4000 }, (_, i) => `line ${i} ${"x".repeat(40)}`);
  const url = await standIn(t, (req, res, msg) => answerJSON(res, msg.id, { content: [], structuredContent: { lines } }));
  const r = await run(["call", url, "b4_logs_tail"]);
  assert.equal(r.code, 0, r.err);
  assert.ok(r.out.length > 200000, `got ${r.out.length} bytes`);
  assert.equal(JSON.parse(r.out).lines.length, lines.length);
});
