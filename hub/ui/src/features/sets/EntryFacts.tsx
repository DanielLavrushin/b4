import { Box, Button, Collapse, Link, Stack, Typography } from "@mui/material";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EntryView } from "@/models/api";
import { Facts, type Fact } from "@/shared/components/Facts";
import { CodeBlock } from "@/shared/components/CodeBlock";
import { Mono } from "@/shared/components/Mono";
import { formatStamp } from "@/shared/utils/format";
import { targetsPreview, techniqueText } from "@/shared/utils/terms";
import { FlagChips } from "./components/TechniqueChips";

const list = (items: string[]) => (items.length ? items.join(", ") : "");

export function Origin({ entry }: { entry: EntryView }) {
  const { t } = useTranslation();
  const parts = [t("entry.author") + " " + entry.author];
  if (entry.asn_observed) {
    parts.push(t("entry.seenFrom", { asn: entry.asn_observed, country: entry.country_observed ?? "" }).trim());
  }
  if (entry.asn_hint) {
    parts.push(t("entry.claims", { asn: entry.asn_hint, country: entry.country_hint ?? "" }).trim());
  }
  if (entry.b4_version) {
    parts.push(
      entry.engine
        ? t("entry.sharedFrom", { version: entry.b4_version, engine: entry.engine })
        : t("entry.sharedFromVersion", { version: entry.b4_version }),
    );
  }
  return (
    <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
      {parts.join(" · ")}
    </Typography>
  );
}

export function EntryFacts({ entry, blobBase = "/b4/hub/blob/" }: { entry: EntryView; blobBase?: string }) {
  const { t } = useTranslation();
  const [showProjection, setShowProjection] = useState(false);
  const projectionId = useId();

  const facts: Fact[] = [
    { label: t("entry.targets"), value: targetsPreview(t, entry.targets) },
    {
      label: t("entry.strategy"),
      value: (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {entry.strategy.map((term, i) => (
            <li key={`${term.code}-${String(i)}`}>{techniqueText(t, term)}</li>
          ))}
        </Stack>
      ),
    },
    {
      label: t("entry.flags"),
      value: entry.flags.length ? <FlagChips flags={entry.flags} /> : t("entry.noFlags"),
    },
    {
      label: t("entry.emitted"),
      value: entry.emitted.length ? (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {entry.emitted.map((e) => (
            <li key={e.name + e.source}>
              {e.unreadable ? t("emitted.unreadable") : e.name}{" "}
              <Typography component="span" variant="caption" sx={{ color: colors.text.secondary }}>
                ({t(`emitted.${e.source}`, { defaultValue: e.source })})
              </Typography>
            </li>
          ))}
        </Stack>
      ) : (
        t("entry.noEmitted")
      ),
    },
    {
      label: t("entry.pins"),
      value: entry.pins.length ? (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {entry.pins.map((p) => (
            <li key={p.domain}>
              {p.domain}: <Mono>{list(p.addresses)}</Mono>
            </li>
          ))}
        </Stack>
      ) : (
        ""
      ),
    },
    { label: t("entry.doh"), value: entry.doh_host ?? "" },
    {
      label: t("entry.payloads"),
      value: entry.payloads.length ? (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {entry.payloads.map((p) => (
            <li key={p.sha256}>
              {t("entry.payloadLine", { protocol: p.protocol, domain: p.domain ?? "", size: p.size })}{" "}
              <Link href={blobBase + p.sha256} rel="noreferrer" underline="hover" color="inherit" sx={{ color: colors.text.primary, "&:hover": { color: colors.text.primary } }}>
                <Mono>{p.sha256.slice(0, 16)}</Mono>
              </Link>
            </li>
          ))}
        </Stack>
      ) : (
        ""
      ),
    },
    {
      label: t("entry.votes"),
      value:
        entry.votes.works + entry.votes.broken > 0
          ? t("entry.votesLine", { works: entry.votes.works, broken: entry.votes.broken })
          : "",
    },
    {
      label: t("entry.reports"),
      value: entry.reports.length ? (
        <Box>
          {t("entry.reportsLine", { count: entry.reports.length, independent: entry.independent_reports })}
          <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
            {entry.reports.map((r) => (
              <li key={r.id}>
                {formatStamp(r.received_at)}
                {r.asn_observed ? ` AS${r.asn_observed}` : ""}: {r.reason || t("reports.noReason")}
                {r.state !== "open" ? ` (${t(`reports.state.${r.state}`)})` : ""}
              </li>
            ))}
          </Stack>
        </Box>
      ) : (
        ""
      ),
    },
    { label: t("entry.fingerprint"), value: entry.fp, mono: true },
    { label: t("entry.needsB4"), value: entry.b4_min ?? "" },
    { label: t("entry.decodeError"), value: entry.decode_error ? t("entry.decodeErrorValue") : "" },
  ];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, minWidth: 0 }}>
      {entry.description && <Typography variant="body2">{entry.description}</Typography>}
      <Facts items={facts} />
      <Box>
        <Button
          size="small"
          aria-expanded={showProjection}
          aria-controls={showProjection ? projectionId : undefined}
          endIcon={showProjection ? <ExpandLessIcon /> : <ExpandMoreIcon />}
          onClick={() => setShowProjection((v) => !v)}
          sx={{ ml: -1, color: colors.text.secondary }}
        >
          {t("entry.projection")}
        </Button>
        <Collapse in={showProjection} unmountOnExit>
          <Box id={projectionId} sx={{ mt: 1 }}>
            <CodeBlock value={entry.projection} />
          </Box>
        </Collapse>
      </Box>
    </Box>
  );
}
