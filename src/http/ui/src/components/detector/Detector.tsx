import { useCallback, useEffect, useState } from "react";
import { Button, Stack } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { SecurityIcon, DomainIcon, DnsIcon, NetworkIcon, TelegramIcon, HistoryIcon, ClearIcon } from "@b4.icons";
import { B4Alert, B4Section } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { useDetector } from "@hooks/useDetector";
import { useSets } from "@hooks/useSets";
import { setsApi } from "@api/sets";
import type { B4SetConfig } from "@models/config";
import type { DetectorSuite } from "@models/detector";
import { copyText } from "@utils";
import { SetupPanel } from "./SetupPanel";
import { RunPanel } from "./RunPanel";
import { VerdictPanel } from "./VerdictPanel";
import { SitesTable } from "./SitesTable";
import { DnsTable } from "./DnsTable";
import { HostingTable } from "./HostingTable";
import { TelegramPanel } from "./TelegramPanel";
import { HistoryTable } from "./HistoryTable";
import { buildReport } from "./report";

export const DetectorRunner = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { showSuccess, showError } = useSnackbar();
  const { running, suite, suiteId, error, history, lists, listsBusy, start, cancel, reset, open, clearHistory, deleteHistoryEntry, updateLists, resetLists } = useDetector();
  const { addDomainsToSet } = useSets();
  const [sets, setSets] = useState<B4SetConfig[]>([]);

  useEffect(() => {
    setsApi
      .getSets()
      .then((s) => setSets(Array.isArray(s) ? s : []))
      .catch(() => setSets([]));
  }, []);

  const goDiscovery = useCallback(
    (sites: string[]) => {
      navigate("/discovery", { state: { urls: sites } })?.catch(() => {});
    },
    [navigate],
  );

  const copyReport = useCallback(
    async (entry: DetectorSuite) => {
      const ok = await copyText(buildReport(entry, t));
      if (ok) showSuccess(t("core.copied"));
      else showError(t("detector.copyFailed"));
    },
    [t, showSuccess, showError],
  );

  const copyPlain = useCallback(
    async (text: string, message: string) => {
      const ok = await copyText(text);
      if (ok) showSuccess(message);
      else showError(t("detector.copyFailed"));
    },
    [t, showSuccess, showError],
  );

  const handleUpdateLists = useCallback(async () => {
    const err = await updateLists();
    if (err) showError(t("detector.setup.updateListsFailed", { error: err }));
    else showSuccess(t("detector.setup.updateListsDone"));
  }, [updateLists, showError, showSuccess, t]);

  const handleAddToSet = useCallback(
    async (setId: string, domain: string) => {
      const res = await addDomainsToSet(setId, [domain]);
      const set = sets.find((s) => s.id === setId);
      if (res.success) showSuccess(t("detector.sites.addedToSet", { domain, set: set?.name ?? setId }));
      else showError(t("detector.sites.addFailed", { domain }));
    },
    [addDomainsToSet, sets, showSuccess, showError, t],
  );

  const showSetup = !running && !suite;
  const both = suite?.options.fetch_mode !== "direct";
  const stale = (scope: string) => suite && suite.options.scopes.indexOf(scope as never) < 0;

  return (
    <Stack spacing={3}>
      <B4Section title={t("detector.title")} description={t("detector.description")} icon={<SecurityIcon />}>
        <Stack spacing={2}>
          {showSetup && (
            <SetupPanel sets={sets} lists={lists} listsBusy={listsBusy} busy={running} onStart={(o) => void start(o)} onUpdateLists={() => void handleUpdateLists()} onResetLists={() => void resetLists()} />
          )}
          {running && suite && <RunPanel suite={suite} onStop={() => void cancel()} />}
          {running && !suite && <B4Alert severity="info">{t("detector.run.starting")}</B4Alert>}
          {error && <B4Alert severity="error">{error}</B4Alert>}
        </Stack>
      </B4Section>

      {suite && (
        <VerdictPanel suite={suite} running={running} onDiscovery={goDiscovery} onCopy={() => void copyReport(suite)} onRunAgain={reset} />
      )}

      {suite?.sites && (
        <B4Section title={t("detector.scopes.sites.name")} icon={<DomainIcon />} description={stale("sites") ? "" : undefined}>
          <SitesTable
            result={suite.sites}
            both={both}
            sets={sets}
            onDiscovery={goDiscovery}
            onOpenSet={(id) => {
              navigate(`/sets/${id}`)?.catch(() => {});
            }}
            onAddToSet={(setId, domain) => void handleAddToSet(setId, domain)}
          />
        </B4Section>
      )}

      {suite?.dns && (
        <B4Section title={t("detector.scopes.dns.name")} icon={<DnsIcon />}>
          <DnsTable result={suite.dns} onUseDoH={(url) => void copyPlain(url, t("detector.dns.dohCopied", { url }))} />
        </B4Section>
      )}

      {suite?.hosting && (
        <B4Section title={t("detector.scopes.hosting.name")} icon={<NetworkIcon />}>
          <HostingTable result={suite.hosting} onCopySNI={(sni) => void copyPlain(sni, t("detector.hosting.sniCopied", { sni }))} />
        </B4Section>
      )}

      {suite?.telegram && (
        <B4Section title={t("detector.scopes.telegram.name")} icon={<TelegramIcon />}>
          <TelegramPanel result={suite.telegram} />
        </B4Section>
      )}

      {history.length > 0 && (
        <B4Section
          title={t("core.history.title")}
          description={t("detector.history.kept", { count: history.length })}
          icon={<HistoryIcon />}
          action={
            <Button size="small" startIcon={<ClearIcon />} onClick={() => void clearHistory()}>
              {t("core.history.clearHistory")}
            </Button>
          }
        >
          <HistoryTable entries={history} currentId={suiteId} onOpen={open} onCopy={(e) => void copyReport(e)} onDelete={(id) => void deleteHistoryEntry(id)} />
        </B4Section>
      )}
    </Stack>
  );
};
