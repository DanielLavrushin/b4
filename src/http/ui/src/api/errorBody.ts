export async function readErrorBody(r: Response): Promise<unknown> {
  const text = await r.text().catch(() => "");
  if (text.trim().length === 0) return undefined;
  const contentType = r.headers.get("Content-Type") ?? "";
  if (contentType.toLowerCase().startsWith("text/html")) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return text;
  }
  return parsed && typeof parsed === "object" ? parsed : text;
}
