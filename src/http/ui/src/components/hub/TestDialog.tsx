import { useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  DialogContent,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Alert, B4Dialog, B4ResultCard, B4TextField } from "@b4.elements";
import { TestIcon } from "@b4.icons";
import { colors } from "@design";
import { HubProbeResult, HubSet, HubTestResponse } from "@models/hub";
import { describeApiError } from "@utils";
import { useHubTest } from "@hooks/useHub";

interface TestDialogProps {
  set: HubSet;
  defaultDomain: string;
  onClose: () => void;
}

interface ProbeCardProps {
  title: string;
  result: HubProbeResult;
}

const ProbeCard = ({ title, result }: ProbeCardProps) => {
  const { t } = useTranslation();
  return (
    <B4ResultCard
      status={result.ok ? "ok" : "error"}
      title={title}
      subtitle={result.detail || undefined}
      badge={
        <Typography
          variant="caption"
          sx={{
            fontWeight: 700,
            color: result.ok ? colors.state.success : colors.state.error,
          }}
        >
          {result.ok ? t("hub.test.ok") : t("hub.test.fail")}
          {result.status ? ` · ${result.status}` : ""}
        </Typography>
      }
    />
  );
};

const verdictKey = (r: HubTestResponse): string => {
  if (r.through_b4.ok && r.bypassed.ok) return "bothOk";
  if (r.through_b4.ok) return "bypassNeeded";
  if (r.bypassed.ok) return "setBreaks";
  return "bothFail";
};

export const TestDialog = ({ set, defaultDomain, onClose }: TestDialogProps) => {
  const { t } = useTranslation();
  const [domain, setDomain] = useState(defaultDomain);
  const test = useHubTest();

  const run = () => {
    const trimmed = domain.trim();
    test.mutate({ id: set.id, domain: trimmed || undefined });
  };

  const result = test.data;

  return (
    <B4Dialog
      open
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      icon={<TestIcon />}
      title={t("hub.test.title", { name: set.title })}
      subtitle={t("hub.test.subtitle")}
      actions={
        <>
          <Button onClick={onClose}>{t("core.close")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            variant="contained"
            startIcon={
              test.isPending ? (
                <CircularProgress size={14} color="inherit" />
              ) : (
                <TestIcon />
              )
            }
            disabled={test.isPending}
            onClick={run}
          >
            {t("hub.test.run")}
          </Button>
        </>
      }
    >
      <DialogContent sx={{ p: 0 }}>
        <Stack spacing={2}>
          <B4TextField
            label={t("hub.test.domain")}
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") run();
            }}
            helperText={t("hub.test.domainHelp")}
            disabled={test.isPending}
          />
          {test.isError && (
            <B4Alert severity="error">
              {t("hub.test.failed", { error: describeApiError(test.error) })}
            </B4Alert>
          )}
          {result && (
            <Stack spacing={1.5}>
              <Typography variant="body2" sx={{ color: colors.text.primary }}>
                {t(`hub.test.verdict.${verdictKey(result)}`, {
                  domain: result.domain,
                })}
              </Typography>
              <ProbeCard
                title={t("hub.test.throughB4")}
                result={result.through_b4}
              />
              <ProbeCard
                title={t("hub.test.bypassed")}
                result={result.bypassed}
              />
            </Stack>
          )}
        </Stack>
      </DialogContent>
    </B4Dialog>
  );
};
