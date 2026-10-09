import { memo, useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { DomainIcon } from "@b4.icons";
import { CREATE_SET_SENTINEL } from "@models/config";
import type { TopEntry } from "@models/metrics";
import { AddSniModal } from "@components/connections/AddSniModal";
import { useDomainActions } from "@hooks/useDomainActions";
import { generateDomainVariants } from "@utils";
import { serverNow, useMetricsFrame } from "@/stores/useMetrics";
import { PanelCard } from "./PanelCard";
import { TopEntries, useDialogConfig, type PendingSet } from "./TopEntries";
import { formatSince } from "./format";

const EMPTY_ENTRIES: readonly TopEntry[] = [];

interface PendingDomain extends PendingSet {
  entry: string;
  at: number;
}

const coveredBy = (domain: string, entry: string): boolean =>
  domain === entry || domain.endsWith(`.${entry}`);

function DomainsPanelView() {
  const { t, i18n } = useTranslation();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const entries = useMetricsFrame((f) => f.top_domains?.items) ?? EMPTY_ENTRIES;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;
  const since = formatSince(statsSince, now, i18n.language);
  const [pending, setPending] = useState<readonly PendingDomain[]>([]);
  const { modalState, openModal, closeModal, selectVariant, addDomain } = useDomainActions();
  const dialog = useDialogConfig(modalState.open);

  const onAdd = useCallback(
    (domain: string) => openModal(domain, generateDomainVariants(domain)),
    [openModal],
  );

  const pendingFor = (entry: TopEntry): PendingDomain | undefined =>
    pending.find((p) => entry.last <= p.at && coveredBy(entry.key, p.entry));

  const submit = (setId: string, setName?: string) => {
    const entry = modalState.selected;
    const created = setId === CREATE_SET_SENTINEL;
    const name = created
      ? (setName ?? "")
      : (dialog.sets.find((s) => s.id === setId)?.name ?? setName ?? setId);
    void addDomain(setId, setName).then((added) => {
      if (!added) return;
      setPending((prev) => [
        ...prev.filter((p) => p.entry !== entry),
        { entry, setId: created ? "" : setId, setName: name, at: serverNow() },
      ]);
      dialog.refresh();
    });
  };

  return (
    <>
      <PanelCard
        title={t("dashboard.domains.title")}
        subtitle={t("dashboard.domains.subtitle", { time: since })}
        icon={<DomainIcon />}
        waiting={!hasFrame}
      >
        <TopEntries
          entries={entries}
          empty={t("dashboard.domains.empty", { time: since })}
          addTip={t("dashboard.domains.add")}
          addLabel={(domain) => t("dashboard.domains.addTo", { domain })}
          pendingFor={pendingFor}
          onAdd={onAdd}
        />
      </PanelCard>
      <AddSniModal
        open={modalState.open}
        domain={modalState.domain}
        variants={modalState.variants}
        selected={modalState.selected}
        sets={dialog.sets}
        onClose={closeModal}
        onSelectVariant={selectVariant}
        onAdd={submit}
      />
    </>
  );
}

export const DomainsPanel = memo(DomainsPanelView);
