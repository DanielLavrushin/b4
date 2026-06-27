import { useMemo } from "react";
import { formatNumber } from "@utils";
import { B4SetConfig } from "@models/config";
import { B4ConfidencePill, B4CountPill } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { useDomainTargeting } from "@hooks/useDomainTargeting";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { DomainLabel } from "./DomainLabel";
import { AddToSetButton } from "./AddToSetButton";

interface UnmatchedDomainsProps {
  topDomains: Record<string, number>;
  domainTLS: Record<string, string>;
  sets: B4SetConfig[];
  targetedDomains: Set<string>;
  onRefreshSets: () => void;
}

export const UnmatchedDomains = ({
  topDomains,
  domainTLS,
  sets,
  targetedDomains,
  onRefreshSets,
}: UnmatchedDomainsProps) => {
  const { t } = useTranslation();
  const isDomainTargeted = useDomainTargeting(targetedDomains);

  const unmatched = useMemo(() => {
    return Object.entries(topDomains)
      .filter(([domain]) => !isDomainTargeted(domain))
      .sort((a, b) => b[1] - a[1])
      .slice(0, 15);
  }, [topDomains, isDomainTargeted]);

  if (unmatched.length === 0) return null;

  return (
    <DashboardPanel eyebrow={t("dashboard.unmatchedDomains.title")}>
      {unmatched.map(([domain, count]) => (
        <DataRow
          key={domain}
          leading={domainTLS[domain] ? <B4ConfidencePill score={domainTLS[domain]} /> : undefined}
          right={
            <>
              <B4CountPill value={formatNumber(count)} />
              <AddToSetButton domain={domain} sets={sets} onAdded={onRefreshSets} />
            </>
          }
        >
          <DomainLabel value={domain} />
        </DataRow>
      ))}
    </DashboardPanel>
  );
};
