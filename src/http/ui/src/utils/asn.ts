import * as ipaddr from "ipaddr.js";
import { stripPort } from "./logs";

export interface AsnInfo {
  id: string;
  name: string;
  prefixes: string[];
}

class AsnStorage {
  private cache: Record<string, AsnInfo> = {};
  private v4Index: Array<[ipaddr.IPv4, number, AsnInfo]> = [];
  private v6Index: Array<[ipaddr.IPv6, number, AsnInfo]> = [];
  private readonly lookupCache = new Map<string, AsnInfo | null>();
  private readonly MAX_CACHE_SIZE = 10000;
  private loaded = false;
  private loadPromise: Promise<void> | null = null;

  async init(): Promise<void> {
    if (this.loaded) return;
    if (this.loadPromise) return this.loadPromise;
    this.loadPromise = this.fetchAll();
    await this.loadPromise;
  }

  private async fetchAll(): Promise<void> {
    try {
      const oldData = localStorage.getItem("b4_asn_cache");
      if (oldData) {
        await this.migrateFromLocalStorage(oldData);
      }

      const response = await fetch("/api/asn");
      if (response.ok) {
        const data = (await response.json()) as Record<string, AsnInfo> | null;
        this.cache = data ?? {};
      }
    } catch {
      // keep whatever is in cache
    }
    this.loaded = true;
    this.rebuildIndex();
  }

  private rebuildIndex(): void {
    this.v4Index = [];
    this.v6Index = [];
    for (const asn of Object.values(this.cache)) {
      this.indexAsn(asn);
    }
    this.lookupCache.clear();
  }

  private indexAsn(asn: AsnInfo): void {
    for (const prefix of asn.prefixes) {
      try {
        const [addr, bits] = ipaddr.parseCIDR(prefix);
        if (addr.kind() === "ipv4") {
          this.v4Index.push([addr as ipaddr.IPv4, bits, asn]);
        } else {
          this.v6Index.push([addr as ipaddr.IPv6, bits, asn]);
        }
      } catch {
        // skip malformed prefix
      }
    }
  }

  private async migrateFromLocalStorage(data: string): Promise<void> {
    try {
      const parsed = JSON.parse(data) as Record<string, AsnInfo>;
      for (const info of Object.values(parsed)) {
        await fetch("/api/asn", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(info),
        });
      }
      localStorage.removeItem("b4_asn_cache");
    } catch {
      // migration is best-effort
    }
  }

  async addAsn(asnId: string, name: string, prefixes: string[]): Promise<void> {
    const info: AsnInfo = { id: asnId, name, prefixes };
    this.cache[asnId] = info;
    this.rebuildIndex();

    try {
      await fetch("/api/asn", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(info),
      });
    } catch {
      // keep local cache even if server write fails
    }
  }

  async deleteAsn(asnId: string): Promise<void> {
    delete this.cache[asnId];
    this.rebuildIndex();

    try {
      await fetch(`/api/asn?id=${encodeURIComponent(asnId)}`, {
        method: "DELETE",
      });
    } catch {
      // keep local state
    }
  }

  getAll(): Record<string, AsnInfo> {
    return { ...this.cache };
  }

  findAsnForIp(ip: string): AsnInfo | null {
    const cleanIp = stripPort(ip);

    const cached = this.lookupCache.get(cleanIp);
    if (cached !== undefined) {
      this.lookupCache.delete(cleanIp);
      this.lookupCache.set(cleanIp, cached);
      return cached;
    }

    const result = this.scanIndex(cleanIp);

    if (this.lookupCache.size >= this.MAX_CACHE_SIZE) {
      const firstKey = this.lookupCache.keys().next().value;
      if (firstKey) this.lookupCache.delete(firstKey);
    }

    this.lookupCache.set(cleanIp, result);
    return result;
  }

  private scanIndex(cleanIp: string): AsnInfo | null {
    try {
      const addr = ipaddr.process(cleanIp);
      const index = addr.kind() === "ipv4" ? this.v4Index : this.v6Index;
      for (const [range, bits, asn] of index) {
        if (addr.match(range, bits)) {
          return asn;
        }
      }
    } catch {
      // unparseable IP -> no match
    }
    return null;
  }

  async reload(): Promise<void> {
    this.loaded = false;
    this.loadPromise = null;
    await this.init();
  }
}

export const asnStorage = new AsnStorage();
