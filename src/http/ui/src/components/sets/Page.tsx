import { useSnackbar } from "@context/SnackbarProvider";
import { colors } from "@design";
import { usePageTitleDetail } from "@hooks/usePageTitle";
import { useSets } from "@hooks/useSets";
import { isStaleWriteError, reportSaveError, reportStaleWrite } from "@utils";
import { B4Config, B4SetConfig } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import {
  Backdrop,
  Box,
  CircularProgress,
  Container,
  Stack,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate, Route, Routes, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { SetEditorPage } from "./Editor";
import { asnOverlapKey } from "./targets/overlap";
import {
  SetStats,
  SetWithStats,
  SetsManager,
  setItemId,
  setItemWithSaved,
} from "./Manager";

interface SetEditorRouteProps {
  config: B4Config & { sets?: SetWithStats[] };
  onRefresh: () => void;
  onReload: () => void;
  reloadToken: number;
  onSaved: (set: B4SetConfig) => void;
}

function SetEditorRoute({
  config,
  onRefresh,
  onReload,
  reloadToken,
  onSaved,
}: Readonly<SetEditorRouteProps>) {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { showSuccess, showError, showSnackbar } = useSnackbar();
  const { createSet, updateSet, loading: saving } = useSets();
  const allowNavigationRef = useRef(false);
  const isNew = id === "new";
  const setsData = config.sets || [];
  const sets = useMemo(
    () =>
      (config.sets ?? []).map((s) => ("set" in s ? s.set : s)) as B4SetConfig[],
    [config.sets],
  );
  const setsStats = setsData.map((s) =>
    "stats" in s ? s.stats : null,
  ) as (SetStats | null)[];

  const existingIndex = isNew ? -1 : sets.findIndex((s) => s.id === id);
  const existingSet = existingIndex >= 0 ? sets[existingIndex] : null;
  const otherSetsTargets = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const s of sets) {
      if (s.id === id) continue;
      for (const domain of s.targets.sni_domains) {
        const key = domain.toLowerCase();
        const existing = map.get(key);
        if (existing) existing.push(s.name);
        else map.set(key, [s.name]);
      }
      for (const ip of s.targets.ip) {
        const existing = map.get(ip);
        if (existing) existing.push(s.name);
        else map.set(ip, [s.name]);
      }
      for (const asn of s.targets.asns ?? []) {
        const key = asnOverlapKey(asn);
        const existing = map.get(key);
        if (existing) existing.push(s.name);
        else map.set(key, [s.name]);
      }
    }
    return map;
  }, [sets, id]);
  const defaultSet = useMemo(() => {
    const created = createDefaultSet(sets.length);
    created.name = i18n.t("sets.editor.defaultName", { n: sets.length + 1 });
    return created;
  }, [sets.length]);
  const set = isNew ? defaultSet : existingSet;

  usePageTitleDetail(isNew ? t("sets.editor.newSet") : existingSet?.name);

  const stats =
    existingIndex >= 0 ? (setsStats[existingIndex] ?? undefined) : undefined;

  const handleSave = async (
    editedSet: B4SetConfig,
  ): Promise<B4SetConfig | undefined> => {
    const { id: _, ...setWithoutId } = editedSet;
    const result = isNew
      ? await createSet(setWithoutId)
      : await updateSet(editedSet);

    if (result.success) {
      showSuccess(isNew ? t("sets.setCreated") : t("sets.setUpdated"));
      if (!isNew && result.data) onSaved(result.data);
      onRefresh();
      if (isNew && result.data) {
        allowNavigationRef.current = true;
        await navigate(`/sets/${result.data.id}`, { replace: true });
      }
      return result.data ?? undefined;
    } else if (isStaleWriteError(result.error)) {
      reportStaleWrite(result.error, showSnackbar, t, onReload);
    } else {
      reportSaveError(result.error, showError, t);
    }
    return undefined;
  };

  if (!set) {
    return <Navigate to="/sets" replace />;
  }

  return (
    <SetEditorPage
      key={isNew ? "new" : `${set.id}:${reloadToken}`}
      settings={config.system}
      set={set}
      config={config}
      stats={stats}
      otherSetsTargets={otherSetsTargets}
      isNew={isNew}
      saving={saving}
      onSave={handleSave}
      onRefresh={onRefresh}
      navigationBypassRef={allowNavigationRef}
    />
  );
}

export function SetsPage() {
  const { t } = useTranslation();
  const { showError } = useSnackbar();
  const [config, setConfig] = useState<
    (B4Config & { sets?: SetWithStats[] }) | null
  >(null);
  const [loading, setLoading] = useState(true);
  const [editorReload, setEditorReload] = useState(0);
  const initialLoadDone = useRef(false);

  const loadConfig = useCallback(async (resetEditor = false) => {
    try {
      if (!initialLoadDone.current) setLoading(true);
      const response = await fetch("/api/config");
      if (!response.ok) throw new Error("Failed to load");
      const data = (await response.json()) as B4Config & {
        sets?: SetWithStats[];
      };
      setConfig(data);
      if (resetEditor) setEditorReload((n) => n + 1);
    } catch {
      showError(i18n.t("core.configLoadError"));
    } finally {
      if (!initialLoadDone.current) {
        setLoading(false);
        initialLoadDone.current = true;
      }
    }
  }, [showError]);

  useEffect(() => {
    loadConfig().catch(() => {});
  }, [loadConfig]);

  const handleSaved = useCallback((saved: B4SetConfig) => {
    setConfig((prev) => {
      if (!prev) return prev;
      const current: SetWithStats[] = prev.sets ?? [];
      const sets = current.map((s) =>
        setItemId(s) === saved.id ? setItemWithSaved(s, saved) : s,
      );
      return { ...prev, sets };
    });
  }, []);

  if (loading || !config) {
    return (
      <Backdrop open sx={{ zIndex: 9999 }}>
        <Stack alignItems="center" spacing={2}>
          <CircularProgress sx={{ color: colors.secondary }} />
          <Typography sx={{ color: colors.text.primary }}>
            {t("core.loading")}
          </Typography>
        </Stack>
      </Backdrop>
    );
  }

  return (
    <Container
      maxWidth={false}
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        py: 3,
      }}
    >
      <Box sx={{ flex: 1, overflow: "auto" }}>
        <Routes>
          <Route
            index
            element={
              <SetsManager
                config={config}
                onRefresh={() => {
                  loadConfig().catch(() => {});
                }}
              />
            }
          />
          <Route
            path=":id"
            element={
              <SetEditorRoute
                config={config}
                onRefresh={() => {
                  loadConfig().catch(() => {});
                }}
                onReload={() => {
                  void loadConfig(true);
                }}
                reloadToken={editorReload}
                onSaved={handleSaved}
              />
            }
          />
        </Routes>
      </Box>
    </Container>
  );
}
