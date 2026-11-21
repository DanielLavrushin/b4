const DOCS_BASE = "https://docs.b4core.app";

export const docsUrl = (path: string, language?: string): string => {
  if (/^https?:\/\//.test(path)) return path;
  const [page, anchor] = path.split("#");
  const locale = language?.toLowerCase().startsWith("ru") ? "/ru" : "";
  const trimmed = page.replace(/^\/+|\/+$/g, "");
  return `${DOCS_BASE}${locale}/docs/${trimmed}/${anchor ? `#${anchor}` : ""}`;
};
