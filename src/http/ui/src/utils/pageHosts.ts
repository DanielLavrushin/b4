import { HostContext, PageHosts, PageHostsSkip } from "@models/discovery";
import { SetDomainMatch } from "@models/sets";

export interface OfferedHost {
  host: string;
  context?: HostContext;
  blocking?: boolean;
  tracker?: boolean;
  service?: string;
  picked?: boolean;
}

export interface HostNote {
  key: string;
  params?: Record<string, string>;
}

export interface HostState extends OfferedHost {
  disabled: boolean;
  on: boolean;
  note?: HostNote;
}

export interface HostSkip {
  reason: PageHostsSkip;
  domains: string[];
}

interface PageHostsSource {
  domain?: string;
  page_hosts?: PageHosts;
}

const CONTEXT_ORDER: HostContext[] = [
  "stylesheet",
  "script",
  "image",
  "media",
  "css",
  "preload",
  "redirect",
  "base",
  "frame",
  "icon",
  "preconnect",
  "inline",
];

const TICKED_CONTEXTS = new Set<HostContext>([
  "stylesheet",
  "script",
  "image",
  "media",
  "preload",
  "css",
]);

const COVERING = new Set(["exact", "covered", "regexp"]);

const contextRank = (context?: HostContext): number => {
  const i = context ? CONTEXT_ORDER.indexOf(context) : -1;
  return i < 0 ? CONTEXT_ORDER.length : i;
};

const checksFor = (sources: PageHostsSource[], preset: string): PageHosts[] =>
  sources
    .map((s) => s.page_hosts)
    .filter((ph): ph is PageHosts => !!ph && ph.preset === preset);

export function offeredHosts(
  sources: PageHostsSource[],
  preset: string,
): OfferedHost[] {
  const byHost = new Map<string, OfferedHost>();
  for (const check of checksFor(sources, preset)) {
    for (const h of check.hosts ?? []) {
      if (h.verdict !== "needed") continue;
      const host = h.host.toLowerCase();
      const seen = byHost.get(host);
      if (!seen) {
        byHost.set(host, {
          host,
          context: h.context,
          blocking: h.blocking,
          tracker: h.tracker,
          service: h.service,
        });
        continue;
      }
      seen.blocking = seen.blocking || h.blocking;
      if (contextRank(h.context) < contextRank(seen.context)) {
        seen.context = h.context;
      }
    }
  }
  return [...byHost.values()];
}

export function unfixedHosts(
  sources: PageHostsSource[],
  preset: string,
): string[] {
  const hosts = new Set<string>();
  for (const check of checksFor(sources, preset)) {
    for (const h of check.hosts ?? []) {
      if (h.verdict === "not_fixed") hosts.add(h.host.toLowerCase());
    }
  }
  return [...hosts];
}

export function hostSkips(
  sources: PageHostsSource[],
  preset: string,
): HostSkip[] {
  const byReason = new Map<PageHostsSkip, string[]>();
  for (const s of sources) {
    const check = s.page_hosts;
    if (!check?.skipped || check.preset !== preset) continue;
    const domains = byReason.get(check.skipped) ?? [];
    if (s.domain) domains.push(s.domain);
    byReason.set(check.skipped, domains);
  }
  return [...byReason].map(([reason, domains]) => ({ reason, domains }));
}

export function defaultPicked(h: OfferedHost): boolean {
  return (
    (!h.tracker || !!h.blocking) &&
    !h.service &&
    !!h.context &&
    TICKED_CONTEXTS.has(h.context)
  );
}

function labelNote(h: OfferedHost): HostNote | undefined {
  if (h.service) {
    return { key: "discovery.hosts.service", params: { service: h.service } };
  }
  if (h.tracker) return { key: "discovery.hosts.tracker" };
  return undefined;
}

const coveringName = (host: string, names: string[]): string | undefined =>
  names.find((name) => {
    const d = name.toLowerCase();
    return host === d || host.endsWith(`.${d}`);
  });

export function hostStates(
  hosts: OfferedHost[],
  matches: SetDomainMatch[],
  names: string[],
  targetSetId: string | null,
): HostState[] {
  return hosts.map((h) => {
    const host = h.host.toLowerCase();
    const entry = coveringName(host, names);
    if (entry) {
      return {
        ...h,
        disabled: true,
        on: false,
        note: { key: "discovery.hosts.covered", params: { entry } },
      };
    }
    const own = matches.filter((m) => m.domain.toLowerCase() === host);
    if (
      targetSetId &&
      own.some((m) => m.set_id === targetSetId && COVERING.has(m.relation))
    ) {
      return {
        ...h,
        disabled: true,
        on: false,
        note: { key: "discovery.hosts.inSet" },
      };
    }
    const elsewhere = own.filter(
      (m) =>
        m.enabled &&
        m.set_id !== targetSetId &&
        (COVERING.has(m.relation) || m.handles),
    );
    const handler = elsewhere.find((m) => m.handles) ?? elsewhere[0];
    if (handler) {
      return {
        ...h,
        disabled: false,
        on: h.picked ?? false,
        note: { key: "discovery.hosts.handled", params: { set: handler.set_name } },
      };
    }
    return {
      ...h,
      disabled: false,
      on: h.picked ?? defaultPicked(h),
      note: labelNote(h),
    };
  });
}

export function pickedHosts(
  states: HostState[],
  picks: Record<string, boolean>,
): string[] {
  return states
    .filter((s) => !s.disabled && (picks[s.host] ?? s.on))
    .map((s) => s.host);
}
