import * as ipaddr from "ipaddr.js";
import { asnApi } from "@api/asn";
import { AsnView, AsnViews, formatAsn } from "@models/asn";
import { stripPort } from "./logs";

export interface AsnInfo {
  id: string;
  name: string;
}

export function asnLabel(id: string, name?: string): string {
  const tag = formatAsn(id);
  const holder = (name ?? "").trim();
  if (!holder) return tag;
  const startsWithTag =
    holder.toUpperCase().startsWith(tag.toUpperCase()) &&
    !/\d/.test(holder.charAt(tag.length));
  return startsWithTag ? holder : `${tag} ${holder}`;
}

const LOOKUP_CACHE_SIZE = 10000;

const v4Value = (addr: ipaddr.IPv4): number =>
  addr.octets.reduce((acc, octet) => acc * 256 + octet, 0);

const v4Key = (value: number, bits: number): number =>
  Math.floor(value / 2 ** (32 - bits));

const v6Value = (addr: ipaddr.IPv6): bigint =>
  addr.parts.reduce((acc, part) => (acc << 16n) | BigInt(part), 0n);

const v6Key = (value: bigint, bits: number): bigint =>
  value >> BigInt(128 - bits);

function tableFor<K>(
  tables: Map<number, Map<K, AsnInfo>>,
  bits: number,
): Map<K, AsnInfo> {
  let table = tables.get(bits);
  if (!table) {
    table = new Map<K, AsnInfo>();
    tables.set(bits, table);
  }
  return table;
}

const longestFirst = (tables: Map<number, unknown>): number[] =>
  [...tables.keys()].sort((a, b) => b - a);

export class AsnLabels {
  private readonly v4 = new Map<number, Map<number, AsnInfo>>();
  private readonly v6 = new Map<number, Map<bigint, AsnInfo>>();
  private readonly v4Lengths: number[];
  private readonly v6Lengths: number[];
  private readonly lookupCache = new Map<string, AsnInfo | null>();

  constructor(views: AsnViews) {
    const ids = Object.keys(views).sort((a, b) => Number(a) - Number(b));
    for (const id of ids) {
      const view = views[id];
      const info: AsnInfo = { id, name: asnLabel(id, view.name) };
      for (const prefix of view.prefixes ?? []) this.add(prefix, info);
    }
    this.v4Lengths = longestFirst(this.v4);
    this.v6Lengths = longestFirst(this.v6);
  }

  private add(prefix: string, info: AsnInfo): void {
    let parsed: [ipaddr.IPv4 | ipaddr.IPv6, number];
    try {
      parsed = ipaddr.parseCIDR(prefix);
    } catch {
      return;
    }
    const [addr, bits] = parsed;
    if (bits === 0) return;
    if (addr.kind() === "ipv4") {
      const table = tableFor(this.v4, bits);
      const key = v4Key(v4Value(addr as ipaddr.IPv4), bits);
      if (!table.has(key)) table.set(key, info);
      return;
    }
    const table = tableFor(this.v6, bits);
    const key = v6Key(v6Value(addr as ipaddr.IPv6), bits);
    if (!table.has(key)) table.set(key, info);
  }

  find(ip: string): AsnInfo | null {
    const clean = stripPort(ip);
    if (!clean) return null;

    const cached = this.lookupCache.get(clean);
    if (cached !== undefined) {
      this.lookupCache.delete(clean);
      this.lookupCache.set(clean, cached);
      return cached;
    }

    const result = this.scan(clean);
    if (this.lookupCache.size >= LOOKUP_CACHE_SIZE) {
      const oldest = this.lookupCache.keys().next().value;
      if (oldest !== undefined) this.lookupCache.delete(oldest);
    }
    this.lookupCache.set(clean, result);
    return result;
  }

  private scan(clean: string): AsnInfo | null {
    let addr: ipaddr.IPv4 | ipaddr.IPv6;
    try {
      addr = ipaddr.process(clean);
    } catch {
      return null;
    }
    if (addr.kind() === "ipv4") {
      const value = v4Value(addr as ipaddr.IPv4);
      for (const bits of this.v4Lengths) {
        const info = this.v4.get(bits)?.get(v4Key(value, bits));
        if (info) return info;
      }
      return null;
    }
    const value = v6Value(addr as ipaddr.IPv6);
    for (const bits of this.v6Lengths) {
      const info = this.v6.get(bits)?.get(v6Key(value, bits));
      if (info) return info;
    }
    return null;
  }
}

type Listener = () => void;

class AsnStorage {
  private views: AsnViews = {};
  private labels = new AsnLabels({});
  private loaded = false;
  private loadPromise: Promise<void> | null = null;
  private readonly listeners = new Set<Listener>();

  readonly subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  readonly getLabels = (): AsnLabels => this.labels;

  init(): Promise<void> {
    if (this.loaded) return Promise.resolve();
    this.loadPromise ??= this.fetchAll();
    return this.loadPromise;
  }

  reload(): Promise<void> {
    this.loadPromise = this.fetchAll();
    return this.loadPromise;
  }

  private async fetchAll(): Promise<void> {
    try {
      const views = await asnApi.list();
      this.loaded = true;
      this.replace(views);
    } catch {
      this.loadPromise = null;
    }
  }

  findAsnForIp(ip: string): AsnInfo | null {
    return this.labels.find(ip);
  }

  put(view: AsnView): void {
    if (!this.loaded) return;
    this.replace({ ...this.views, [view.id]: view });
  }

  async remove(id: string): Promise<void> {
    await asnApi.remove(id);
    if (!(id in this.views)) return;
    this.replace(
      Object.fromEntries(
        Object.entries(this.views).filter(([key]) => key !== id),
      ),
    );
  }

  private replace(views: AsnViews): void {
    this.views = views;
    this.labels = new AsnLabels(views);
    for (const listener of this.listeners) listener();
  }
}

export const asnStorage = new AsnStorage();
