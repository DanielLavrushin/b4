import { useEffect } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ApiError } from "@api/apiClient";
import { discoveryApi } from "@api/discovery";
import { DiscoverySuite, isSuite } from "@models/discovery";
import { verdictOf } from "@utils";
import { useNotify, type Notice } from "@hooks/useNotify";
import {
  forgetRun,
  observeRun,
  onRunFinished,
  useDiscoveryWatch,
} from "@/stores/discoveryWatch";

const WATCH_POLL_MS = 5000;
const DISCOVERY_PATH = "/discovery";
const MAX_LISTED_SITES = 3;

function listSites(suite: DiscoverySuite): string {
  const sites = (suite.domains ?? []).map((d) => d.domain);
  const shown = sites.slice(0, MAX_LISTED_SITES).join(", ");
  const more = sites.length - MAX_LISTED_SITES;
  return more > 0 ? `${shown} +${more}` : shown;
}

function describeRun(suite: DiscoverySuite, t: TFunction): Notice {
  if (suite.status === "failed") {
    return {
      title: t("discovery.notify.failed"),
      body: listSites(suite),
      severity: "error",
    };
  }
  const results = Object.values(suite.domain_discovery_results ?? {});
  const count = results.length;
  const found = results.filter((r) => verdictOf(r, true) === "found").length;
  return {
    title: t("discovery.notify.finished"),
    body:
      found > 0
        ? t("discovery.notify.found", { found, count })
        : t("discovery.notify.notFound", { count }),
    severity: found > 0 ? "success" : "warning",
  };
}

export function DiscoveryNotifier() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const notify = useNotify();
  const { run, pageRun } = useDiscoveryWatch();

  useEffect(() => {
    let active = true;
    const pickUp = async () => {
      try {
        const current = await discoveryApi.current();
        if (active && isSuite(current)) observeRun(current);
      } catch {
        return;
      }
    };
    void pickUp();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!run || run === pageRun) return;
    let active = true;
    const tick = async () => {
      try {
        const suite = await discoveryApi.status(run);
        if (active) observeRun(suite);
      } catch (e) {
        if (active && e instanceof ApiError && e.status === 404) forgetRun(run);
      }
    };
    void tick();
    const timer = setInterval(() => void tick(), WATCH_POLL_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [run, pageRun]);

  useEffect(
    () =>
      onRunFinished((suite, shown) => {
        notify({
          ...describeRun(suite, t),
          tag: `b4-discovery-${suite.id}`,
          inApp: !shown,
          action: {
            label: t("discovery.notify.open"),
            onClick: () => {
              navigate(DISCOVERY_PATH)?.catch(() => {});
            },
          },
        });
      }),
    [navigate, notify, t],
  );

  return null;
}
