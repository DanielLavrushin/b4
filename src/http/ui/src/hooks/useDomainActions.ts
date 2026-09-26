import { useState, useCallback, useMemo } from "react";
import { SortDirection } from "@common/SortableTableCell";
import {
  AsnLabels,
  matchesConnectionFilter,
  parseConnectionFilter,
} from "@utils";
import { useSnackbar } from "@context/SnackbarProvider";

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

export function parseSniLogLine(line: string): ParsedLog | null {
  const tokens = line.trim().split(",");
  if (tokens.length < 7) return null;

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

  return result;
}

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

export function useFilteredLogs(
  parsedLogs: ParsedLog[],
  filter: string,
  asnLabels: AsnLabels,
): ParsedLog[] {
  return useMemo(() => {
    const parsed = parseConnectionFilter(filter);
    if (!parsed) return parsedLogs;

    const asnName = (destination: string): string | null =>
      destination ? (asnLabels.find(destination)?.name ?? null) : null;

    const getFieldValue = (log: ParsedLog, field: string): string => {
      if (field === "asn") {
        return asnName(log.destination)?.toLowerCase() || "";
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
      asnName(log.destination),
    ];

    return parsedLogs.filter((log: ParsedLog) =>
      matchesConnectionFilter(
        parsed,
        (field) => getFieldValue(log, field),
        getSearchableValues(log),
      ),
    );
  }, [parsedLogs, filter, asnLabels]);
}

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
