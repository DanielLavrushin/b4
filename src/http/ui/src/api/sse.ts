export interface SSEEvent {
  event: string;
  data: string;
}

export function* parseSSEBlocks(buffer: string): Generator<{ event: SSEEvent; rest: string }> {
  let rest = buffer;
  let idx = rest.indexOf("\n\n");
  while (idx !== -1) {
    const block = rest.slice(0, idx);
    rest = rest.slice(idx + 2);
    let event = "message";
    const dataLines: string[] = [];
    for (const line of block.split("\n")) {
      if (line.startsWith("event: ")) {
        event = line.slice(7).trim();
      } else if (line.startsWith("data: ")) {
        dataLines.push(line.slice(6));
      }
    }
    if (dataLines.length > 0) {
      yield { event: { event, data: dataLines.join("\n") }, rest };
    }
    idx = rest.indexOf("\n\n");
  }
}

export async function readSSE(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: SSEEvent) => boolean,
): Promise<boolean> {
  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) return false;
      buffer += decoder.decode(value, { stream: true });
      for (const block of parseSSEBlocks(buffer)) {
        buffer = block.rest;
        if (onEvent(block.event)) return true;
      }
    }
  } finally {
    reader.releaseLock();
  }
}
