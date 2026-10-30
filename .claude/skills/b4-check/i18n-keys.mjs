import { readFileSync } from "node:fs";

const PLURAL = /_(zero|one|two|few|many|other)$/;
const REQUIRED_FORMS = { en: ["one", "other"], ru: ["one", "few", "many"] };

const flatten = (node, prefix, out) => {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object") flatten(value, path, out);
    else out.push(path);
  }
  return out;
};

const load = (file) => flatten(JSON.parse(readFileSync(file, "utf8")), "", []);

const pluralGroups = (keys) => {
  const all = new Set(keys);
  const forms = new Map();
  for (const key of keys) {
    const match = key.match(PLURAL);
    if (!match) continue;
    const base = key.slice(0, -match[0].length);
    if (!forms.has(base)) forms.set(base, new Set());
    forms.get(base).add(match[1]);
  }
  const groups = new Map();
  for (const [base, set] of forms) {
    const bare = all.has(base);
    if (set.size >= 2 || bare || set.has("one")) groups.set(base, { forms: set, bare });
  }
  return groups;
};

const [enFile, ruFile] = process.argv.slice(2);
if (!enFile || !ruFile) {
  console.error("usage: node i18n-keys.mjs <en.json> <ru.json>");
  process.exit(2);
}

const normalized = (keys, groups) =>
  new Set(
    keys.map((key) => {
      const base = key.replace(PLURAL, "");
      return base !== key && groups.has(base) ? base : key;
    }),
  );

const locales = { en: load(enFile), ru: load(ruFile) };
const groups = { en: pluralGroups(locales.en), ru: pluralGroups(locales.ru) };
const enKeys = normalized(locales.en, groups.en);
const ruKeys = normalized(locales.ru, groups.ru);
const problems = [];

for (const key of enKeys) if (!ruKeys.has(key)) problems.push(`missing in ru: ${key}`);
for (const key of ruKeys) if (!enKeys.has(key)) problems.push(`missing in en: ${key}`);

for (const lang of ["en", "ru"]) {
  for (const [base, { forms, bare }] of groups[lang]) {
    const missing = REQUIRED_FORMS[lang].filter(
      (form) => !forms.has(form) && !(form === "other" && bare),
    );
    if (missing.length) problems.push(`${lang} plural ${base} lacks _${missing.join(", _")}`);
  }
}

for (const line of problems) console.log(line);
process.exit(problems.length ? 1 : 0);
