import { useState, useEffect } from "react";

const changelogUrl = (file: string) =>
  file === "changelog_ru.md"
    ? "/api/system/changelog?lang=ru"
    : "/api/system/changelog";

const parseChangelog = (markdown: string): Record<string, string> => {
  const sections: Record<string, string> = {};
  const lines = markdown.split("\n");
  let version: string | null = null;
  let buffer: string[] = [];

  const flush = () => {
    if (version) sections[version] = buffer.join("\n").trim();
    buffer = [];
  };

  for (const line of lines) {
    const match = /^##\s*\[([^\]]+)\]/.exec(line);
    if (match) {
      flush();
      version = match[1].trim();
    }
    if (version) buffer.push(line);
  }
  flush();
  return sections;
};

const cache: Record<string, Record<string, string>> = {};

export const useLocalizedChangelog = (
  file: string,
  enabled: boolean,
): Record<string, string> => {
  const [sections, setSections] = useState<Record<string, string>>(
    () => cache[file] || {},
  );

  useEffect(() => {
    if (!enabled) return;
    if (cache[file]) {
      setSections(cache[file]);
      return;
    }

    let cancelled = false;
    fetch(changelogUrl(file))
      .then((res) => (res.ok ? res.text() : ""))
      .then((text) => {
        if (cancelled || !text) return;
        const parsed = parseChangelog(text);
        cache[file] = parsed;
        setSections(parsed);
      })
      .catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [file, enabled]);

  return sections;
};

export const changelogNotesForTag = (
  sections: Record<string, string>,
  tag: string,
): string | null => {
  const version = tag
    .replace(/^v/, "")
    .replace(/-?(alpha|beta|rc|dev).*$/i, "");
  return sections[version] ?? null;
};
