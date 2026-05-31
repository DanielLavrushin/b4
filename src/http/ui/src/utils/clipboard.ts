export async function copyText(text: string): Promise<boolean> {
  try {
    if (globalThis.isSecureContext && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through to legacy path */
  }

  const active = document.activeElement as HTMLElement | null;

  const ta = document.createElement("textarea");
  ta.value = text;
  ta.contentEditable = "true";
  ta.readOnly = false;
  ta.style.position = "fixed";
  ta.style.top = "0";
  ta.style.left = "0";
  ta.style.width = "1px";
  ta.style.height = "1px";
  ta.style.padding = "0";
  ta.style.border = "none";
  ta.style.outline = "none";
  ta.style.boxShadow = "none";
  ta.style.background = "transparent";

  const host = active?.closest("[role='dialog']") ?? document.body;
  host.appendChild(ta);

  ta.focus();
  ta.select();

  const range = document.createRange();
  range.selectNodeContents(ta);
  const sel = globalThis.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  ta.setSelectionRange(0, text.length);

  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    /* legacy copy failed too */
  }

  ta.remove();
  active?.focus?.();
  return ok;
}
