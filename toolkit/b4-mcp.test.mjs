import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import http from "node:http";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "b4-mcp.mjs");

const initResult = { protocolVersion: "2025-11-25", capabilities: {}, serverInfo: { name: "stand-in", version: "0" } };

function run(args, limitMs = 10000) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(process.execPath, [script, ...args], { stdio: ["ignore", "pipe", "pipe"] });
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
