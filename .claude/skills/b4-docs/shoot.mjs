#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";

const CHROME = process.env.CHROME_BIN || "/usr/bin/google-chrome";

function usage() {
  console.error("usage: node shoot.mjs <jobs.json> [--keep] [--verbose]");
  console.error("       node shoot.mjs --example");
  process.exit(2);
}

const EXAMPLE = {
  baseUrl: "http://localhost:5173",
  outDir: "docs/static/img/dns",
  width: 1440,
  height: 900,
  jobs: [
    {
      out: "20260820120000.png",
      url: "/settings/dns",
      lang: "en",
      actions: [{ waitFor: ".MuiPaper-root" }, { wait: 400 }],
      clip: { selector: ".MuiPaper-root", contains: "DNS", pad: 8 },
    },
    {
      out: "ru/20260820120001.png",
      url: "/settings/dns",
      lang: "ru",
      actions: [{ waitFor: ".MuiPaper-root" }, { wait: 400 }],
      clip: { selector: ".MuiPaper-root", contains: "DNS", pad: 8 },
    },
  ],
};

async function freePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitForTarget(port, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/list`);
      const targets = await res.json();
      const page = targets.find((t) => t.type === "page" && t.webSocketDebuggerUrl);
      if (page) return page.webSocketDebuggerUrl;
    } catch {}
    await sleep(150);
  }
  throw new Error(`chrome devtools did not come up on port ${port}`);
}

class CDP {
  constructor(ws, verbose) {
    this.ws = ws;
    this.verbose = verbose;
    this.id = 0;
    this.pending = new Map();
    this.listeners = new Map();
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.id !== undefined) {
        const p = this.pending.get(msg.id);
        if (!p) return;
        this.pending.delete(msg.id);
        if (msg.error) p.reject(new Error(`${p.method}: ${msg.error.message}`));
        else p.resolve(msg.result);
      } else {
        const hs = this.listeners.get(msg.method);
        if (hs) for (const h of hs) h(msg.params);
      }
    });
  }
  on(method, handler) {
    if (!this.listeners.has(method)) this.listeners.set(method, []);
    this.listeners.get(method).push(handler);
  }
  once(method, timeoutMs = 30000) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`timeout waiting for ${method}`)), timeoutMs);
      this.on(method, (params) => {
        clearTimeout(timer);
        resolve(params);
      });
    });
  }
  send(method, params = {}) {
    const id = ++this.id;
    if (this.verbose) console.error(`  -> ${method}`);
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject, method }));
  }
  async eval(expression, awaitPromise = false) {
    const r = await this.send("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise,
    });
    if (r.exceptionDetails) {
      const t = r.exceptionDetails.exception?.description || r.exceptionDetails.text;
      throw new Error(`page eval failed: ${t}`);
    }
    return r.result.value;
  }
}

const HIDE_OVERLAYS = `
(() => {
  let n = 0;
  for (const el of document.querySelectorAll('*')) {
    const pos = getComputedStyle(el).position;
    if (pos === 'fixed' || pos === 'sticky') {
      el.style.setProperty('visibility', 'hidden', 'important');
      n++;
    }
  }
  return n;
})()`;

const clickExpr = (selector) => `
(() => {
  const el = document.querySelector(${JSON.stringify(selector)});
  if (!el) throw new Error('no element for ' + ${JSON.stringify(selector)});
  const opts = { bubbles: true, cancelable: true, composed: true };
  el.dispatchEvent(new PointerEvent('pointerdown', opts));
  el.dispatchEvent(new MouseEvent('mousedown', opts));
  el.dispatchEvent(new PointerEvent('pointerup', opts));
  el.dispatchEvent(new MouseEvent('mouseup', opts));
  el.click();
  return true;
})()`;

const existsExpr = (selector) =>
  `!!document.querySelector(${JSON.stringify(selector)})`;

function clipExpr(clip) {
  const selector = clip.selector || ".MuiPaper-root";
  const contains = clip.contains ?? null;
  const pad = clip.pad ?? 0;
  return `
(() => {
  const sel = ${JSON.stringify(selector)};
  const needle = ${JSON.stringify(contains)};
  const pad = ${pad};
  let els = Array.from(document.querySelectorAll(sel));
  if (needle !== null) els = els.filter(e => (e.textContent || '').includes(needle));
  if (!els.length) throw new Error('clip matched nothing: ' + sel + (needle === null ? '' : ' containing ' + JSON.stringify(needle)));
  els.sort((a, b) => {
    const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect();
    return (ra.width * ra.height) - (rb.width * rb.height);
  });
  const r = els[0].getBoundingClientRect();
  const sx = window.scrollX, sy = window.scrollY;
  return {
    x: Math.max(0, r.left + sx - pad),
    y: Math.max(0, r.top + sy - pad),
    width: r.width + pad * 2,
    height: r.height + pad * 2,
    scale: 1,
  };
})()`;
}

async function runActions(cdp, actions, verbose) {
  for (const a of actions || []) {
    if (a.wait !== undefined) {
      await sleep(a.wait);
    } else if (a.waitFor !== undefined) {
      const deadline = Date.now() + (a.timeout ?? 15000);
      let ok = false;
      while (Date.now() < deadline) {
        if (await cdp.eval(existsExpr(a.waitFor))) { ok = true; break; }
        await sleep(100);
      }
      if (!ok) throw new Error(`waitFor timed out: ${a.waitFor}`);
    } else if (a.click !== undefined) {
      await cdp.eval(clickExpr(a.click));
      await sleep(a.settle ?? 250);
    } else if (a.eval !== undefined) {
      await cdp.eval(a.eval, true);
    } else {
      throw new Error(`unknown action: ${JSON.stringify(a)}`);
    }
    if (verbose) console.error(`  action ok: ${Object.keys(a)[0]}`);
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.includes("--example")) {
    console.log(JSON.stringify(EXAMPLE, null, 2));
    return;
  }
  const jobsFile = args.find((a) => !a.startsWith("--"));
  if (!jobsFile) usage();
  const keep = args.includes("--keep");
  const verbose = args.includes("--verbose");

  const spec = JSON.parse(await readFile(jobsFile, "utf8"));
  const baseUrl = (spec.baseUrl || "").replace(/\/$/, "");
  const outDir = spec.outDir || ".";
  const width = spec.width ?? 1440;
  const height = spec.height ?? 900;

  const port = await freePort();
  const profile = await mkdtemp(path.join(tmpdir(), "b4shoot-"));
  const chrome = spawn(
    CHROME,
    [
      "--headless=new",
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${profile}`,
      "--hide-scrollbars",
      "--disable-gpu",
      "--disable-dev-shm-usage",
      "--no-first-run",
      "--no-default-browser-check",
      "about:blank",
    ],
    { stdio: verbose ? "inherit" : "ignore" }
  );

  let failures = 0;
  try {
    const wsUrl = await waitForTarget(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((resolve, reject) => {
      ws.addEventListener("open", resolve, { once: true });
      ws.addEventListener("error", () => reject(new Error("devtools websocket failed")), { once: true });
    });
    const cdp = new CDP(ws, verbose);
    await cdp.send("Page.enable");
    await cdp.send("Runtime.enable");
    await cdp.send("Emulation.setDeviceMetricsOverride", {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: false,
    });

    let langScript = null;
    for (const job of spec.jobs) {
      const label = `${job.out}`;
      try {
        const lang = job.lang || spec.lang;
        if (langScript) {
          await cdp.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: langScript }).catch(() => {});
          langScript = null;
        }
        if (lang) {
          const added = await cdp.send("Page.addScriptToEvaluateOnNewDocument", {
            source: `try { localStorage.setItem('b4-language', ${JSON.stringify(lang)}); } catch (e) {}`,
          });
          langScript = added?.identifier ?? null;
        }

        const url = /^https?:/.test(job.url) ? job.url : baseUrl + job.url;
        const loaded = cdp.once("Page.loadEventFired", job.loadTimeout ?? 30000);
        await cdp.send("Page.navigate", { url });
        await loaded;

        await runActions(cdp, job.actions, verbose);

        const hidden = await cdp.eval(HIDE_OVERLAYS);
        if (verbose) console.error(`  hid ${hidden} fixed/sticky elements`);

        const params = { format: "png" };
        if (job.fullPage) {
          params.captureBeyondViewport = true;
          const m = await cdp.send("Page.getLayoutMetrics");
          const c = m.cssContentSize || m.contentSize;
          params.clip = { x: 0, y: 0, width: c.width, height: c.height, scale: 1 };
        } else if (job.clip) {
          params.captureBeyondViewport = true;
          params.clip = await cdp.eval(clipExpr(job.clip));
        }
        if (params.clip && (params.clip.width < 1 || params.clip.height < 1)) {
          throw new Error(`clip resolved to an empty rect for ${label}`);
        }

        const shot = await cdp.send("Page.captureScreenshot", params);
        const outPath = path.resolve(outDir, job.out);
        await mkdir(path.dirname(outPath), { recursive: true });
        const buf = Buffer.from(shot.data, "base64");
        await writeFile(outPath, buf);
        const kb = Math.round(buf.length / 1024);
        console.log(`${outPath}  ${kb} KB`);
        if (kb > 250) console.error(`  warning: ${kb} KB is well above the 10-150 KB repo norm`);
      } catch (err) {
        failures++;
        console.error(`FAILED ${label}: ${err.message}`);
      }
    }
    ws.close();
  } finally {
    chrome.kill("SIGTERM");
    await Promise.race([
      new Promise((resolve) => chrome.once("exit", resolve)),
      sleep(5000),
    ]);
    if (!keep) await rm(profile, { recursive: true, force: true }).catch(() => {});
  }
  process.exit(failures ? 1 : 0);
}

main().catch((err) => {
  console.error(err.stack || String(err));
  process.exit(1);
});
