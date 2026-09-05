import { useEffect, useState } from "react";
import { Stack } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4RunBar, B4RunHeader, B4RunLine, B4RunSteps } from "@b4.elements";
import type { DetectorSuite } from "@models/detector";

interface RunPanelProps {
  suite: DetectorSuite;
  onStop: () => void;
}

export const RunPanel = ({ suite, onStop }: RunPanelProps) => {
  const { t } = useTranslation();
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, []);

  const p = suite.progress;
  const scopes = suite.options.scopes ?? [];
  const stopping = !!suite.stopping;
  const phaseIndex = p.phase ? scopes.indexOf(p.phase) : -1;
  const active = phaseIndex >= 0 ? phaseIndex : 0;
  const started = new Date(suite.start_time).getTime();
  const elapsed = Number.isFinite(started) ? Math.max(0, Math.round((Date.now() - started) / 1000)) : 0;
  const sitesTotal = suite.sites?.sites?.length ?? (suite.options.sites?.length || 0);
  const sitesDone = suite.sites?.sites?.filter((s) => s.done).length ?? 0;

  const steps = scopes.map((scope) => ({
    key: scope,
    label: t(`detector.scopes.${scope}.name`),
    note: scope === p.phase && scope === "sites" && sitesTotal > 0 ? t("detector.run.progress", { done: sitesDone, total: sitesTotal }) : undefined,
  }));

  return (
    <Stack spacing={2}>
      <B4RunHeader
        title={t("detector.run.title", { count: sitesTotal })}
        subtitle={
          <>
            {t("detector.run.elapsed", { seconds: elapsed })}
            {" · "}
            {t("detector.run.progress", { done: p.done, total: p.total })}
          </>
        }
        onStop={onStop}
        stopping={stopping}
        stopLabel={t("detector.run.stop")}
        stoppingLabel={t("detector.run.stopping")}
      />
      <B4RunSteps steps={steps} active={active} />
      <B4RunBar value={p.total > 0 ? (p.done / p.total) * 100 : 0} indeterminate={p.total === 0} />
      <B4RunLine text={p.current ? `${t(`detector.scopes.${p.phase || "sites"}.name`)}: ${p.current}` : ""} placeholder={t("detector.run.preparing")} />
    </Stack>
  );
};
