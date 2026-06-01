import { useState, useCallback, useMemo } from "react";
import { SortDirection } from "@common/SortableTableCell";
import {
  asnStorage,
  matchesConnectionFilter,
  parseConnectionFilter,
} from "@utils";
import { useSnackbar } from "@context/SnackbarProvider";

// Types
export type SortColumn =
  | "timestamp"
  | "set"
  | "protocol"
  | "domain"
  | "source"
  | "destination";

export interface ParsedLog {
  timestamp: string;
  protocol: "TCP" | "UDP";
  hostSet: string;
  ipSet: string;
  domain: string;
  source: string;
  destination: string;
  raw: string;
  sourceAlias: string;
  deviceName: string;
  tls: string;
  flags: string;
}

interface DomainModalState {
  open: boolean;
  domain: string;
  variants: string[];
  selected: string;
}

// Simple LRU Cache for parsed logs
class ParseCache {
  private readonly cache = new Map<string, ParsedLog | null>();
  private readonly maxSize = 5000;

  get(key: string): ParsedLog | null | undefined {
    const value = this.cache.get(key);
    if (value !== undefined) {
      this.cache.delete(key);
      this.cache.set(key, value);
    }
    return value;
  }

  set(key: string, value: ParsedLog | null): void {
    if (this.cache.size >= this.maxSize) {
      const firstKey = this.cache.keys().next().value;
      if (firstKey) this.cache.delete(firstKey);
    }
    this.cache.set(key, value);
  }

  has(key: string): boolean {
    return this.cache.has(key);
  }

  clear(): void {
    this.cache.clear();
  }
}

const parseCache = new ParseCache();

// ASN Lookup cache
const asnLookupCache = new Map<string, string | null>();

export function getAsnForIp(destination: string): string | null {
  if (!destination) return null;

  const cached = asnLookupCache.get(destination);
  if (cached !== undefined) return cached;

  const asn = asnStorage.findAsnForIp(destination);
  const result = asn?.name || null;

  asnLookupCache.set(destination, result);

  if (asnLookupCache.size > 2000) {
    const entries = Array.from(asnLookupCache.entries());
    asnLookupCache.clear();
    entries.slice(-1000).forEach(([k, v]) => asnLookupCache.set(k, v));
  }

  return result;
}

export function clearAsnLookupCache(): void {
  asnLookupCache.clear();
}

// Parse a single log line with caching
export function parseSniLogLine(line: string): ParsedLog | null {
  const cached = parseCache.get(line);
  if (cached !== undefined) return cached;

  const tokens = line.trim().split(",");
  if (tokens.length < 7) {
    parseCache.set(line, null);
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

  const result: ParsedLog = {
    timestamp: timestamp.replaceAll(" [INFO]", "").trim().split(".")[0],
    protocol: protocol as "TCP" | "UDP",
    hostSet,
    domain,
    source,
    ipSet,
    destination,
    raw: line,
    sourceAlias: sourceAlias ?? "",
    deviceName: "",
    tls: tls ?? "",
    flags: flags ?? "",
  };

  parseCache.set(line, result);
  return result;
}

// Domain actions hook
export function useDomainActions() {
  const { showSuccess, showError } = useSnackbar();
  const [modalState, setModalState] = useState<DomainModalState>({
    open: false,
    domain: "",
    variants: [],
    selected: "",
  });

  const openModal = useCallback((domain: string, variants: string[]) => {
    setModalState({
      open: true,
      domain,
      variants,
      selected: variants[0] || domain,
    });
  }, []);

  const closeModal = useCallback(() => {
    setModalState({
      open: false,
      domain: "",
      variants: [],
      selected: "",
    });
  }, []);

  const selectVariant = useCallback((variant: string) => {
    setModalState((prev) => ({ ...prev, selected: variant }));
  }, []);

  const addDomain = useCallback(
    async (setId: string, setName?: string) => {
      if (!modalState.selected) return;

      try {
        const response = await fetch("/api/geosite/domain", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            domain: modalState.selected,
            set_id: setId,
            set_name: setName,
          }),
        });

        if (response.ok) {
          showSuccess(`Domain ${modalState.selected} added successfully`);
          closeModal();
        } else {
          const error = (await response.json()) as { message: string };
          showError(`Failed to add domain: ${error.message}`);
        }
      } catch (error) {
        showError(`Failed to add domain: ${String(error)}`);
      }
    },
    [modalState.selected, closeModal, showError, showSuccess],
  );

  return {
    modalState,
    openModal,
    closeModal,
    selectVariant,
    addDomain,
  };
}

// Enrich logs with device names
export function useEnrichedLogs(
  parsedLogs: ParsedLog[],
  deviceMap: Record<string, string>,
): ParsedLog[] {
  return useMemo(() => {
    if (Object.keys(deviceMap).length === 0) return parsedLogs;

    return parsedLogs.map((log) => {
      const normalized =
        log.sourceAlias?.toUpperCase().replaceAll("-", ":") || "";
      const deviceName = deviceMap[normalized] || "";
      if (deviceName === log.deviceName) return log;
      return { ...log, deviceName };
    });
  }, [parsedLogs, deviceMap]);
}

// Optimized filtering with memoization
export function useFilteredLogs(
  parsedLogs: ParsedLog[],
  filter: string,
): ParsedLog[] {
  return useMemo(() => {
    const parsed = parseConnectionFilter(filter);
    if (!parsed) return parsedLogs;

    const getFieldValue = (log: ParsedLog, field: string): string => {
      if (field === "asn") {
        return getAsnForIp(log.destination)?.toLowerCase() || "";
      }
      if (field === "alias" || field === "device") {
        return `${log.sourceAlias || ""} ${log.deviceName || ""}`.toLowerCase();
      }
      return log[field as keyof typeof log]?.toString().toLowerCase() || "";
    };

    const getSearchableValues = (log: ParsedLog): (string | null)[] => [
      log.hostSet,
      log.ipSet,
      log.domain,
      log.source,
      log.sourceAlias,
      log.deviceName,
      log.protocol,
      log.destination,
      log.flags,
      getAsnForIp(log.destination),
    ];

    return parsedLogs.filter((log: ParsedLog) =>
      matchesConnectionFilter(
        parsed,
        (field) => getFieldValue(log, field),
        getSearchableValues(log),
      ),
    );
  }, [parsedLogs, filter]);
}

// Optimized sorting
export function useSortedLogs(
  filteredLogs: ParsedLog[],
  sortColumn: SortColumn | null,
  sortDirection: SortDirection,
): ParsedLog[] {
  return useMemo(() => {
    if (!sortColumn || !sortDirection) {
      return filteredLogs;
    }

    const sorted = [...filteredLogs].sort((a, b) => {
      let aValue: string | number;
      let bValue: string | number;

      if (sortColumn === "timestamp") {
        aValue = new Date(a.timestamp.replaceAll(/\/+/g, "-")).getTime() || 0;
        bValue = new Date(b.timestamp.replaceAll(/\/+/g, "-")).getTime() || 0;
      } else {
        aValue = (a[sortColumn as keyof ParsedLog] || "")
          .toString()
          .toLowerCase();
        bValue = (b[sortColumn as keyof ParsedLog] || "")
          .toString()
          .toLowerCase();
      }

      if (aValue < bValue) return sortDirection === "asc" ? -1 : 1;
      if (aValue > bValue) return sortDirection === "asc" ? 1 : -1;
      return 0;
    });

    return sorted;
  }, [filteredLogs, sortColumn, sortDirection]);
}
