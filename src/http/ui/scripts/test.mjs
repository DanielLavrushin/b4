import { spawnSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const root = fileURLToPath(new URL("..", import.meta.url));
const sources = join(root, "src");

const findTests = () =>
  readdirSync(sources, { recursive: true })
    .filter((name) => name.endsWith(".test.ts"))
    .sort()
    .map((name) => join(sources, name));

const requested = process.argv.slice(2).map((file) => resolve(file));
const entries = requested.length > 0 ? requested : findTests();
if (entries.length === 0) {
  console.error("no *.test.ts files under src");
  process.exit(1);
}

const outdir = mkdtempSync(join(tmpdir(), "b4-ui-test-"));
let status = 1;
try {
  const outputs = entries.map((entry, index) =>
    join(outdir, `${index}-${basename(entry, ".ts")}.mjs`),
  );
  await Promise.all(
    entries.map((entry, index) =>
      build({
        entryPoints: [entry],
        outfile: outputs[index],
        bundle: true,
        platform: "node",
        format: "esm",
        target: `node${process.versions.node}`,
        tsconfig: join(root, "tsconfig.json"),
        sourcemap: "inline",
        logLevel: "warning",
      }),
    ),
  );
  const run = spawnSync(
    process.execPath,
    ["--enable-source-maps", "--test", ...outputs],
    { stdio: "inherit" },
  );
  status = run.status ?? 1;
} finally {
  rmSync(outdir, { recursive: true, force: true });
}
process.exit(status);
