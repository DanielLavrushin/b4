import { ParsedLog } from "@b4.logs";

export const SORT_STORAGE_KEY = "b4_domains_sort";
export const AGG_SORT_STORAGE_KEY = "b4_connections_agg_sort";

export interface DomainSortState {
  column: string | null;
  direction: "asc" | "desc" | null;
}

export function loadSortState(key: string = SORT_STORAGE_KEY): DomainSortState {
  try {
    const stored = localStorage.getItem(key);
    if (stored) {
      const parsed = JSON.parse(stored) as Partial<DomainSortState>;
      const column = typeof parsed.column === "string" ? parsed.column : null;
      const direction =
        parsed.direction === "asc" || parsed.direction === "desc"
          ? parsed.direction
          : null;
      return { column, direction };
    }
  } catch (e) {
    console.error("Failed to load sort state:", e);
  }
  return { column: null, direction: null };
}

export function saveSortState(
  column: string | null,
  direction: "asc" | "desc" | null,
  key: string = SORT_STORAGE_KEY,
): void {
  try {
    localStorage.setItem(key, JSON.stringify({ column, direction }));
  } catch (e) {
    console.error("Failed to save sort state:", e);
  }
}

export function parseSniLogLine(line: string): ParsedLog | null {
  const tokens = line.trim().trim().split(",");
  if (tokens.length < 7) {
    return null;
  }

  const [
    timestamp,
    protocol,
    hostSet,
    domain,
    source,
    ipSet,
    destination,
    sourceAlias,
    tls,
    flags,
  ] = tokens;

  return {
    timestamp: timestamp.replaceAll(" [INFO]", "").trim().split(".")[0],
    protocol: protocol as "TCP" | "UDP",
    hostSet,
    domain,
    source,
    ipSet,
    destination,
    sourceAlias: sourceAlias ?? "",
    deviceName: "",
    tls: tls ?? "",
    flags: flags ?? "",
    raw: line,
  };
}

export function generateDomainVariants(domain: string): string[] {
  const parts = domain.split(".");
  const variants: string[] = [];

  for (let i = 0; i < parts.length - 1; i++) {
    variants.push(parts.slice(i).join("."));
  }

  return variants;
}

export function stripPort(addr: string): string {
  if (!addr) return addr;
  if (addr.startsWith("[")) {
    const end = addr.indexOf("]");
    return end > 0 ? addr.slice(1, end) : addr.slice(1);
  }
  if ((addr.match(/:/g) || []).length === 1) return addr.split(":")[0];
  return addr;
}

export function generateIpVariants(ip: string): string[] {
  const stripped = stripPort(ip);
  if (stripped.includes(":")) return generateIpv6Variants(stripped);

  const parts = stripped.split(".");

  if (
    parts.length !== 4 ||
    parts.some((p) => {
      const num = Number.parseInt(p, 10);
      return Number.isNaN(num) || num < 0 || num > 255;
    })
  ) {
    return [];
  }

  return [
    `${parts.join(".")}/32`,
    `${parts.slice(0, 3).join(".")}.0/24`,
    `${parts.slice(0, 2).join(".")}.0.0/16`,
    `${parts[0]}.0.0.0/8`,
  ];
}

function generateIpv6Variants(ip: string): string[] {
  return [`${ip}/128`, `${ip}/64`, `${ip}/48`, `${ip}/32`];
}

export const STORAGE_KEY = "b4_domains_lines";
export const MAX_STORED_LINES = 1000;

export function loadPersistedLines(): string[] {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const parsed = JSON.parse(stored) as unknown;
      return Array.isArray(parsed) ? (parsed as string[]) : [];
    }
  } catch (e) {
    console.error("Failed to load persisted domains:", e);
  }
  return [];
}

export function persistLogLines(lines: string[]): void {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify(lines.slice(-MAX_STORED_LINES)),
    );
  } catch (e) {
    console.error("Failed to persist domains:", e);
  }
}

export function clearLogPersistedLines(): void {
  localStorage.removeItem(STORAGE_KEY);
}
