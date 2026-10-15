import { memo, useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { IpIcon } from "@b4.icons";
import type { TopEntry } from "@models/metrics";
import { AddIpDialog } from "@components/connections/AddIpDialog";
import { useIpActions, type IpTarget } from "@hooks/useIpActions";
import { asnStorage, cidrCovers, type AsnLabels } from "@utils";
import { serverNow, useMetricsFrame } from "@/stores/useMetrics";
import { PanelCard } from "./PanelCard";
import { TopEntries, useDialogConfig, type PendingSet } from "./TopEntries";
import { formatSince } from "./format";

const EMPTY_ENTRIES: readonly TopEntry[] = [];

interface PendingAddress extends PendingSet {
  target: IpTarget;
  at: number;
}

const targetCovers = (target: IpTarget, ip: string, labels: AsnLabels): boolean =>
  target.kind === "asn" ? labels.find(ip)?.id === target.id : cidrCovers(target.value, ip);

function AddressesPanelView() {
  const { t, i18n } = useTranslation();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const entries = useMetricsFrame((f) => f.top_addresses?.items) ?? EMPTY_ENTRIES;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;
  const since = formatSince(statsSince, now, i18n.language);
  const asnLabels = useSyncExternalStore(asnStorage.subscribe, asnStorage.getLabels);
  const [pending, setPending] = useState<readonly PendingAddress[]>([]);
  const { dialog, openDialog, closeDialog, addTarget } = useIpActions();
  const config = useDialogConfig(dialog.open);

  useEffect(() => {
    void asnStorage.init();
  }, []);

  const onAdd = useCallback((ip: string) => openDialog(ip), [openDialog]);

  const pendingFor = (entry: TopEntry): PendingAddress | undefined =>
    pending.find((p) => entry.last <= p.at && targetCovers(p.target, entry.key, asnLabels));

  return (
    <>
      <PanelCard
        title={t("dashboard.addresses.title")}
        subtitle={t("dashboard.addresses.subtitle", { time: since })}
        icon={<IpIcon />}
        waiting={!hasFrame}
      >
        <TopEntries
          entries={entries}
          empty={t("dashboard.addresses.empty", { time: since })}
          addTip={t("dashboard.addresses.add")}
          addLabel={(address) => t("dashboard.addresses.addTo", { address })}
          detail={(ip) => asnLabels.find(ip)?.name}
          pendingFor={pendingFor}
          onAdd={onAdd}
        />
      </PanelCard>
      <AddIpDialog
        key={dialog.session}
        open={dialog.open}
        ip={dialog.ip}
        asn={dialog.asn}
        asnName={dialog.asnName}
        sets={config.sets}
        ipInfoToken={config.ipInfoToken}
        onClose={closeDialog}
        onSubmit={async (request) => {
          const res = await addTarget(request);
          setPending((prev) => [
            ...prev,
            { target: request.target, setId: res.set_id, setName: request.setLabel, at: serverNow() },
          ]);
          config.refresh();
        }}
      />
    </>
  );
}

export const AddressesPanel = memo(AddressesPanelView);
