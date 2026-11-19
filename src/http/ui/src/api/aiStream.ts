import { parseSSEBlocks, type SSEEvent } from "./sse";

export interface AIStreamUsage {
  input_tokens?: number;
  output_tokens?: number;
}

export interface AIStreamHandlers {
  onDelta: (text: string) => void;
  onDone?: (usage?: AIStreamUsage) => void;
  onError?: (message: string) => void;
}

export async function streamAi(
  url: string,
  body: unknown,
  handlers: AIStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  let finished = false;
  const finish = (usage?: AIStreamUsage) => {
    if (finished) return;
    finished = true;
    handlers.onDone?.(usage);
  };

  try {
    const reader = await openStream(url, body, handlers, signal);
    if (!reader) return;
    await consumeStream(reader, handlers, finish);
  } catch (err) {
    if ((err as { name?: string }).name !== "AbortError") {
      handlers.onError?.(err instanceof Error ? err.message : String(err));
    }
  } finally {
    finish();
  }
}

async function openStream(
  url: string,
  body: unknown,
  handlers: AIStreamHandlers,
  signal?: AbortSignal,
): Promise<ReadableStreamDefaultReader<Uint8Array> | null> {
  let resp: Response;
  try {
    resp = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify(body),
      signal,
    });
  } catch (err) {
    if ((err as { name?: string }).name !== "AbortError") {
      handlers.onError?.(err instanceof Error ? err.message : String(err));
    }
    return null;
  }
  if (!resp.ok) {
    handlers.onError?.(await readErrorDetail(resp));
    return null;
  }
  if (!resp.body) {
    handlers.onError?.("empty response body");
    return null;
  }
  return resp.body.getReader();
}

async function readErrorDetail(resp: Response): Promise<string> {
  let text = "";
  try {
    text = await resp.text();
  } catch {
    /* ignore */
  }
  const trimmed = text.trim();
  try {
    const parsed = JSON.parse(text) as { error?: string };
    if (parsed.error) return parsed.error;
  } catch {
    /* not json */
  }
  return trimmed || `${resp.status} ${resp.statusText}`;
}

async function consumeStream(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  handlers: AIStreamHandlers,
  finish: (usage?: AIStreamUsage) => void,
): Promise<void> {
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    buffer += decoder.decode(value, { stream: true });
    const result = drainBuffer(buffer, handlers, finish);
    buffer = result.rest;
    if (result.terminated) return;
  }
}

interface DrainResult {
  rest: string;
  terminated: boolean;
}

function drainBuffer(
  buffer: string,
  handlers: AIStreamHandlers,
  finish: (usage?: AIStreamUsage) => void,
): DrainResult {
  let rest = buffer;
  for (const block of parseSSEBlocks(buffer)) {
    rest = block.rest;
    if (dispatchEvent(block.event, handlers, finish)) {
      return { rest, terminated: true };
    }
  }
  return { rest, terminated: false };
}

function dispatchEvent(
  event: SSEEvent,
  handlers: AIStreamHandlers,
  finish: (usage?: AIStreamUsage) => void,
): boolean {
  let payload: Record<string, unknown>;
  try {
    payload = event.data ? (JSON.parse(event.data) as Record<string, unknown>) : {};
  } catch (err) {
    handlers.onError?.(err instanceof Error ? err.message : "parse error");
    finish();
    return true;
  }
  switch (event.event) {
    case "delta":
      if (typeof payload.text === "string") handlers.onDelta(payload.text);
      return false;
    case "done":
      finish(payload.usage as AIStreamUsage | undefined);
      return true;
    case "error":
      handlers.onError?.(typeof payload.message === "string" ? payload.message : "stream error");
      finish();
      return true;
    default:
      return false;
  }
}
