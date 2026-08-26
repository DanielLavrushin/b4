import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const docsDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const rootDir = resolve(docsDir, "..");

const targets = [
  {
    source: resolve(rootDir, "changelog.md"),
    dest: resolve(docsDir, "docs/changelog.md"),
    title: "Changelog",
  },
  {
    source: resolve(rootDir, "changelog_ru.md"),
    dest: resolve(
      docsDir,
      "i18n/ru/docusaurus-plugin-content-docs/current/changelog.md"
    ),
    title: "История изменений",
  },
];

const stripLeadingH1 = (text) =>
  text.replace(/^﻿?\s*#\s+.*\r?\n+/, "");

for (const { source, dest, title } of targets) {
  const body = stripLeadingH1(await readFile(source, "utf8")).trimStart();
  const frontMatter = `---\nsidebar_position: 20\ntitle: ${title}\n---\n\n`;
  await mkdir(dirname(dest), { recursive: true });
  await writeFile(dest, frontMatter + body, "utf8");
  console.log(`synced ${source} -> ${dest}`);
}
