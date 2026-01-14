import { ParsedLog } from "@b4.logs";

export const SORT_STORAGE_KEY = "b4_domains_sort";

export interface DomainSortState {
  column: string | null;
  direction: "asc" | "desc" | null;
}

export function loadSortState(): DomainSortState {
  try {
    const stored = localStorage.getItem(SORT_STORAGE_KEY);
    if (stored) {
      return JSON.parse(stored) as DomainSortState;
    }
  } catch (e) {
    console.error("Failed to load sort state:", e);
  }
  return { column: null, direction: null };
}

export function saveSortState(
  column: string | null,
  direction: "asc" | "desc" | null,
): void {
  try {
    localStorage.setItem(
      SORT_STORAGE_KEY,
      JSON.stringify({ column, direction }),
    );
  } catch (e) {
    console.error("Failed to save sort state:", e);
  }
}

export function parseSniLogLine(line: string): ParsedLog | null {
  const tokens = line.trim().trim().split(",");
  if (tokens.length < 7) {
    return null;
  }

  const [timestamp, protocol, hostSet, domain, source, ipSet, destination] =
    tokens;

  return {
    timestamp: timestamp.replaceAll(" [INFO]", "").trim().split(".")[0],
    protocol: protocol as "TCP" | "UDP",
    hostSet,
    domain,
    source,
    ipSet,
    destination,
    raw: line,
  };
}

// Generate domain variants from most specific to least specific
export function generateDomainVariants(domain: string): string[] {
  const parts = domain.split(".");
  const variants: string[] = [];

  // Generate from full domain to TLD+1 (e.g., example.com)
  for (let i = 0; i < parts.length - 1; i++) {
    variants.push(parts.slice(i).join("."));
  }

  return variants;
}

export function generateIpVariants(ip: string): string[] {
  // IPv6 with port: [2001:db8::1]:443
  if (ip.startsWith("[")) {
    const addr = ip.split("]")[0].substring(1);
    return generateIpv6Variants(addr);
  }

  // IPv6 without port
  if (ip.includes(":") && ip.split(":").length > 2) {
    return generateIpv6Variants(ip);
  }

  // IPv4 (with or without port: 1.1.1.1:443 or 1.1.1.1)
  const parts = ip.split(":")[0].split(".");

  if (
    parts.length !== 4 ||
    parts.some((p) => {
      const num = parseInt(p, 10);
      return isNaN(num) || num < 0 || num > 255;
    })
  ) {
    return [];
  }

  const variants: string[] = [];
  variants.push(`${parts.join(".")}/32`);
  variants.push(`${parts.slice(0, 3).join(".")}.0/24`);
  variants.push(`${parts.slice(0, 2).join(".")}.0.0/16`);
  variants.push(`${parts[0]}.0.0.0/8`);

  return variants;
}

function generateIpv6Variants(ip: string): string[] {
  const variants: string[] = [];
  variants.push(`${ip}/128`);
  variants.push(`${ip}/64`);
  variants.push(`${ip}/48`);
  variants.push(`${ip}/32`);

  return variants;
}

// Local storage utilities
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
