import { useEffect, useMemo, useRef, useState } from "react";
import { asnStorage } from "@utils";
import type {
  AggregatorInput,
  AggregatorOutput,
  AggregatorSnapshot,
  ConnectionGroup,
  DeviceSummary,
} from "../workers/connections-types";
import {
  HIDDEN_SNAPSHOT_INTERVAL_MS,
  SNAPSHOT_INTERVAL_MS,
} from "../workers/connections-types";

export interface EnrichedGroup extends ConnectionGroup {
  asnId: string | null;
  asnName: string | null;
  deviceName: string;
  deviceVendor: string;
  deviceIp: string;
  deviceLabel: string;
}

export interface EnrichedDevice extends DeviceSummary {
  deviceName: string;
  deviceVendor: string;
  deviceLabel: string;
}

const NO_VENDORS: Record<string, string> = {};

const labelOf = (name: string, vendor: string, ip: string, mac: string): string => {
  if (name) return name;
  if (vendor) return `${vendor} (${ip || mac})`;
  return ip || mac;
};

export interface GroupsState {
  groups: EnrichedGroup[];
  devices: EnrichedDevice[];
  totalPackets: number;
  unmatchedCount: number;
  ts: number;
}

const EMPTY_STATE: GroupsState = {
  groups: [],
  devices: [],
  totalPackets: 0,
  unmatchedCount: 0,
  ts: 0,
};

export function useConnectionGroups(
  lines: string[],
  deviceMap: Record<string, string>,
  paused: boolean,
  ipToMac: Record<string, string> = {},
  vendorMap: Record<string, string> = NO_VENDORS,
): GroupsState {
  const workerRef = useRef<Worker | null>(null);
  const lastSentinelRef = useRef<string | null>(null);
  const linesRef = useRef<string[]>(lines);
  const pausedRef = useRef<boolean>(paused);
  linesRef.current = lines;
  pausedRef.current = paused;
  const [snapshot, setSnapshot] = useState<AggregatorSnapshot | null>(null);

  useEffect(() => {
    const w = new Worker(
      new URL("../workers/connections-aggregator.worker.ts", import.meta.url),
      { type: "module" },
    );
    workerRef.current = w;
    w.onmessage = (e: MessageEvent<AggregatorOutput>) => {
      if (e.data.type === "snapshot") setSnapshot(e.data.payload);
    };
    return () => {
      w.terminate();
      workerRef.current = null;
    };
  }, []);

  useEffect(() => {
    const applyInterval = () => {
      const w = workerRef.current;
      if (!w) return;
      const msg: AggregatorInput = {
        type: "setSnapshotInterval",
        ms: document.hidden
          ? HIDDEN_SNAPSHOT_INTERVAL_MS
          : SNAPSHOT_INTERVAL_MS,
      };
      w.postMessage(msg);
    };
    applyInterval();
    document.addEventListener("visibilitychange", applyInterval);
    return () => {
      document.removeEventListener("visibilitychange", applyInterval);
    };
  }, []);

  useEffect(() => {
    const w = workerRef.current;
    if (!w) return;
    const setMsg: AggregatorInput = { type: "setIpToMac", map: ipToMac };
    w.postMessage(setMsg);
    if (pausedRef.current) return;
    const clearMsg: AggregatorInput = { type: "clear" };
    w.postMessage(clearMsg);
    const all = linesRef.current;
    if (all.length > 0) {
      const ingestMsg: AggregatorInput = { type: "ingest", lines: all };
      w.postMessage(ingestMsg);
    }
    lastSentinelRef.current = all.at(-1) ?? null;
  }, [ipToMac]);

  useEffect(() => {
    const w = workerRef.current;
    if (!w) return;

    if (lines.length === 0) {
      if (lastSentinelRef.current !== null) {
        const clearMsg: AggregatorInput = { type: "clear" };
        w.postMessage(clearMsg);
        lastSentinelRef.current = null;
      }
      return;
    }

    if (paused) {
      lastSentinelRef.current = lines.at(-1) ?? null;
      return;
    }

    const sentinel = lastSentinelRef.current;
    let newLines: string[];

    if (sentinel === null) {
      newLines = lines;
    } else {
      let idx = -1;
      for (let i = lines.length - 1; i >= 0; i--) {
        if (lines[i] === sentinel) {
          idx = i;
          break;
        }
      }
      if (idx === -1) {
        const clearMsg: AggregatorInput = { type: "clear" };
        w.postMessage(clearMsg);
        newLines = lines;
      } else {
        newLines = lines.slice(idx + 1);
      }
    }

    if (newLines.length > 0) {
      const ingestMsg: AggregatorInput = { type: "ingest", lines: newLines };
      w.postMessage(ingestMsg);
    }
    lastSentinelRef.current = lines.at(-1) ?? null;
  }, [lines, paused]);

  return useMemo<GroupsState>(() => {
    if (!snapshot) return EMPTY_STATE;
    const ipOf = new Map(snapshot.devices.map((d) => [d.mac, d.ip]));
    const groups: EnrichedGroup[] = snapshot.groups.map((g) => {
      const asn = g.destIp ? asnStorage.findAsnForIp(g.destIp) : null;
      const deviceName = deviceMap[g.mac] ?? "";
      const deviceVendor = vendorMap[g.mac] ?? "";
      const deviceIp = ipOf.get(g.mac) ?? "";
      return {
        ...g,
        asnId: asn?.id ?? null,
        asnName: asn?.name ?? null,
        deviceName,
        deviceVendor,
        deviceIp,
        deviceLabel: labelOf(deviceName, deviceVendor, deviceIp, g.mac),
      };
    });
    const devices: EnrichedDevice[] = snapshot.devices.map((d) => {
      const deviceName = deviceMap[d.mac] ?? "";
      const deviceVendor = vendorMap[d.mac] ?? "";
      return {
        ...d,
        deviceName,
        deviceVendor,
        deviceLabel: labelOf(deviceName, deviceVendor, d.ip, d.mac),
      };
    });
    return {
      groups,
      devices,
      totalPackets: snapshot.totalPackets,
      unmatchedCount: snapshot.unmatchedCount,
      ts: snapshot.ts,
    };
  }, [snapshot, deviceMap, vendorMap]);
}

export function clearGroupsWorker(worker: Worker | null): void {
  if (!worker) return;
  const msg: AggregatorInput = { type: "clear" };
  worker.postMessage(msg);
}
