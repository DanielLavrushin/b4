import type { DetectorSuite } from "@models/detector";
import type { TFunction } from "i18next";

const line = (parts: (string | undefined | null | false)[]) =>
  parts.filter(Boolean).join(" ");

export function buildReport(suite: DetectorSuite, t: TFunction): string {
  const out: string[] = [];
  const v = suite.verdict;
  out.push(`# b4 DPI Detector ${new Date(suite.start_time).toISOString()}`);
  if (suite.network?.wan_ip) {
    out.push(
      line([
        `WAN ${suite.network.wan_ip}`,
        suite.network.asn && `AS${suite.network.asn}`,
        suite.network.org,
        suite.network.ipv6 ? "IPv6 yes" : "IPv6 no",
      ]),
    );
  }
  out.push(
    line([
      `scopes: ${(suite.options.scopes ?? []).join(", ")}`,
      `mode: ${suite.options.fetch_mode ?? "both"}`,
      `${suite.options.ip_version ?? "ipv4"}`,
      suite.lists_date && `lists ${suite.lists_date}`,
    ]),
  );
  out.push("");

  if (suite.sites) {
    out.push(
      `## Sites: ${v.blocked_by_isp} blocked by ISP, ${v.fixed_by_b4} fixed by b4, ${v.still_blocked} still blocked, ${v.not_blocked} not blocked`,
    );
    out.push("");
    out.push("| Site | Direct | Through b4 | Outcome | Detail |");
    out.push("|---|---|---|---|---|");
    for (const s of suite.sites.sites ?? []) {
      const d = s.direct;
      const b = s.through_b4;
      const detail = [
        d?.detail,
        d?.tls12 && `TLS1.2 ${d.tls12}`,
        d?.http && d.http !== "OK" && `HTTP ${d.http}`,
        b?.detail && b.status !== "OK" && `via b4: ${b.detail}`,
        s.set_name && `set "${s.set_name}"${s.set_enabled ? "" : " (disabled)"}`,
      ]
        .filter(Boolean)
        .join("; ");
      out.push(
        `| ${s.input}${s.family === "ipv6" ? " (IPv6)" : ""} ${s.ip ?? ""} | ${d?.status ?? "-"} | ${b?.status ?? "-"} | ${t(`detector.outcome.${s.outcome}`)} | ${detail.replace(/\|/g, "/")} |`,
      );
    }
    if (suite.sites.stub_ips?.length) {
      out.push("");
      out.push(`Stub addresses: ${suite.sites.stub_ips.join(", ")}`);
    }
    out.push("");
  }

  if (suite.dns) {
    const d = suite.dns;
    out.push(
      `## DNS: UDP ${d.udp_ok}/${d.udp_total}, DoH ${d.doh_ok}/${d.doh_total}, DoT ${d.dot_ok}/${d.dot_total}, hijacked ${d.hijacked}${d.hijacked_by ? ` (${d.hijacked_by})` : ""}, substituting ${d.substituting}`,
    );
    out.push("");
    out.push("| Provider | UDP | DoH | DoT | Honest | Port 53 answered by |");
    out.push("|---|---|---|---|---|---|");
    const cell = (p?: { status: string; latency_ms?: number }) =>
      !p ? "-" : p.status === "ok" ? `${p.latency_ms ?? 0} ms` : p.status;
    for (const p of d.providers ?? []) {
      const h = p.udp?.honesty ?? p.doh?.honesty ?? p.dot?.honesty ?? "-";
      const by = p.udp?.answered_by
        ? line([
            p.udp.answered_by,
            p.udp.answered_by_asn && `AS${p.udp.answered_by_asn}`,
            p.udp.answered_by_org,
            p.udp.hijacked && "(hijacked)",
          ])
        : "-";
      out.push(`| ${p.name} | ${cell(p.udp)} | ${cell(p.doh)} | ${cell(p.dot)} | ${h} | ${by} |`);
    }
    out.push("");
  }

  if (suite.hosting) {
    const h = suite.hosting;
    out.push(`## Hosting and CDN: ${h.dropped}/${h.total} targets dropped, ${h.dropped_groups} of ${(h.groups ?? []).length} networks affected`);
    out.push("");
    out.push("| Network | Status | Targets | Drops at | Working SNI |");
    out.push("|---|---|---|---|---|");
    for (const g of h.groups ?? []) {
      const drop = g.dropped
        ? g.drop_min_kb === g.drop_max_kb
          ? `${g.drop_min_kb} KB`
          : `${g.drop_min_kb}-${g.drop_max_kb} KB`
        : "-";
      out.push(
        `| ${g.provider} AS${g.asn} | ${g.status} | ${g.ok} ok, ${g.dropped} dropped, ${g.timeouts} no answer | ${drop} | ${(g.working_snis ?? []).join(", ") || "-"} |`,
      );
    }
    out.push("");
  }

  if (suite.telegram) {
    const tg = suite.telegram;
    out.push(`## Telegram: ${tg.verdict}`);
    out.push(`- datacenters ${tg.dc_reachable}/${tg.dc_total}`);
    out.push(`- download ${tg.download.verdict}, ${tg.download.mbps_avg} Mbit/s, ${Math.round(tg.download.bytes / 1024 / 1024)} MB`);
    out.push(`- upload ${tg.upload.verdict}, ${tg.upload.mbps_avg} Mbit/s, ${Math.round(tg.upload.bytes / 1024 / 1024)} MB`);
    out.push("");
  }
  return out.join("\n");
}
