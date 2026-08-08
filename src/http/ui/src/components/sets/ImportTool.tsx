import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Divider,
  LinearProgress,
  MenuItem,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";

import { B4Alert, B4Dialog, B4TextField } from "@b4.elements";
import { ImportExportIcon } from "@b4.icons";
import { colors } from "@design";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  CONVERT_STATUS_ORDER,
  ConvertNote,
  ConvertRequest,
  ConvertResult,
  ConvertSetPlan,
  ConvertStatus,
  ConvertToolInfo,
  convertApi,
} from "@api/convert";

const AUTO = "auto";

const statusColor: Record<ConvertStatus, string> = {
  mapped: colors.state.success,
  approximated: colors.state.info,
  degenerate: colors.state.warning,
  unsupported: colors.state.warning,
  not_applicable: colors.text.secondary,
  unknown: colors.state.error,
  invalid: colors.state.error,
};

interface ImportToolDialogProps {
  open: boolean;
  onClose: () => void;
  onImported: () => void;
}

export function ImportToolDialog({
  open,
  onClose,
  onImported,
}: Readonly<ImportToolDialogProps>) {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();

  const [tools, setTools] = useState<ConvertToolInfo[]>([]);
  const [text, setText] = useState("");
  const [domains, setDomains] = useState("");
  const [tool, setTool] = useState(AUTO);
  const [version, setVersion] = useState(AUTO);
  const [profileDomains, setProfileDomains] = useState<Record<string, string>>(
    {},
  );
  const [result, setResult] = useState<ConvertResult | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    convertApi
      .getTools()
      .then(setTools)
      .catch(() => setTools([]));
  }, [open]);

  useEffect(() => {
    if (!open) {
      setText("");
      setDomains("");
      setProfileDomains({});
      setTool(AUTO);
      setVersion(AUTO);
      setResult(null);
      setError("");
    }
  }, [open]);

  const versions = useMemo(
    () => tools.find((x) => x.tool === tool)?.versions ?? [],
    [tools, tool],
  );

  const request = useCallback((): ConvertRequest => {
    const perProfile: Record<string, string[]> = {};
    for (const [key, value] of Object.entries(profileDomains)) {
      perProfile[key] = splitDomains(value);
    }
    return {
      text,
      tool: tool === AUTO ? undefined : tool,
      version: version === AUTO ? undefined : version,
      domains: splitDomains(domains),
      profile_domains: perProfile,
    };
  }, [text, tool, version, domains, profileDomains]);

  const handleAnalyze = useCallback(async () => {
    setAnalyzing(true);
    setError("");
    try {
      setResult(await convertApi.analyze(request()));
    } catch (e) {
      setResult(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setAnalyzing(false);
    }
  }, [request]);

  useEffect(() => {
    if (!result) return;
    const id = setTimeout(() => {
      handleAnalyze().catch(() => {});
    }, 400);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [domains, profileDomains]);

  const handleApply = useCallback(async () => {
    setApplying(true);
    try {
      const applied = await convertApi.apply(request());
      showSuccess(
        t("sets.convert.applied", {
          count: applied.sets.length,
          tool: applied.tool_label,
        }),
      );
      onImported();
      onClose();
    } catch (e) {
      showError(e instanceof Error ? e.message : String(e));
    } finally {
      setApplying(false);
    }
  }, [request, showSuccess, showError, onImported, onClose, t]);

  const grouped = useMemo(() => groupNotes(result), [result]);

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      icon={<ImportExportIcon />}
      title={t("sets.convert.title")}
      subtitle={t("sets.convert.subtitle")}
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Button
            onClick={() => {
              handleAnalyze().catch(() => {});
            }}
            disabled={!text.trim() || analyzing}
            variant="outlined"
            startIcon={analyzing ? <CircularProgress size={16} /> : undefined}
          >
            {t("sets.convert.analyze")}
          </Button>
          <Button
            onClick={() => {
              handleApply().catch(() => {});
            }}
            disabled={!result || result.sets.length === 0 || applying}
            variant="contained"
            startIcon={applying ? <CircularProgress size={16} /> : undefined}
          >
            {t("sets.convert.createSets", { count: result?.sets.length ?? 0 })}
          </Button>
        </>
      }
    >
      <Stack gap={2}>
        <B4Alert severity="info" noWrapper>
          {t("sets.convert.disclaimer")}
        </B4Alert>

        <B4TextField
          label={t("sets.convert.inputLabel")}
          helperText={t("sets.convert.inputHelper")}
          value={text}
          onChange={(e) => setText(e.target.value)}
          multiline
          minRows={4}
          slotProps={{
            input: { sx: { fontFamily: "monospace", fontSize: 13 } },
          }}
        />

        <Stack direction={{ xs: "column", sm: "row" }} gap={2}>
          <B4TextField
            select
            label={t("sets.convert.tool")}
            value={tool}
            onChange={(e) => {
              setTool(e.target.value);
              setVersion(AUTO);
            }}
          >
            <MenuItem value={AUTO}>{t("sets.convert.autoDetect")}</MenuItem>
            {tools.map((x) => (
              <MenuItem key={x.tool} value={x.tool}>
                {x.label}
              </MenuItem>
            ))}
          </B4TextField>
          <B4TextField
            select
            label={t("sets.convert.version")}
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            disabled={versions.length === 0}
          >
            <MenuItem value={AUTO}>{t("sets.convert.autoDetect")}</MenuItem>
            {versions.map((v) => (
              <MenuItem key={v} value={v}>
                {v}
              </MenuItem>
            ))}
          </B4TextField>
          <B4TextField
            label={t("sets.convert.sharedDomainsLabel")}
            helperText={t("sets.convert.domainsHelper")}
            value={domains}
            onChange={(e) => setDomains(e.target.value)}
          />
        </Stack>

        {error && (
          <B4Alert severity="error" noWrapper>
            {error}
          </B4Alert>
        )}

        {result && (
          <>
            <Divider />
            <Summary result={result} />

            <PlanEditor
              plan={result.plan}
              values={profileDomains}
              shared={domains}
              onChange={(profile, value) =>
                setProfileDomains((prev) => ({ ...prev, [profile]: value }))
              }
            />

            {result.warnings.map((wrn) => (
              <B4Alert key={wrn.code} severity="warning" noWrapper>
                {t(`sets.convert.warning.${wrn.code}`, {
                  ...wrn.params,
                  defaultValue: wrn.code,
                })}
              </B4Alert>
            ))}

            {result.unresolved.length > 0 && (
              <B4Alert severity="warning" noWrapper>
                {t("sets.convert.unresolvedFiles", {
                  files: result.unresolved.map((u) => u.path).join(", "),
                })}
              </B4Alert>
            )}

            {grouped.map(({ profile, name, notes }) => (
              <Box key={profile}>
                <Typography
                  variant="subtitle2"
                  sx={{ color: colors.text.secondary, mb: 0.5 }}
                >
                  {t("sets.convert.profileHeading", {
                    index: profile + 1,
                    name,
                  })}
                </Typography>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell width="24%">
                        {t("sets.convert.columnOption")}
                      </TableCell>
                      <TableCell width="16%">
                        {t("sets.convert.columnStatus")}
                      </TableCell>
                      <TableCell>{t("sets.convert.columnEffect")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {notes.map((n, i) => (
                      <TableRow key={`${n.token}-${i}`}>
                        <TableCell
                          sx={{ fontFamily: "monospace", fontSize: 12.5 }}
                        >
                          {n.token}
                        </TableCell>
                        <TableCell>
                          <Chip
                            size="small"
                            label={t(`sets.convert.status.${n.status}`)}
                            sx={{
                              bgcolor: "transparent",
                              border: `1px solid ${statusColor[n.status]}`,
                              color: statusColor[n.status],
                              height: 20,
                              fontSize: 11,
                            }}
                          />
                        </TableCell>
                        <TableCell>
                          <Typography variant="body2">
                            {t(`sets.convert.reason.${n.reason}`, {
                              ...n.params,
                              defaultValue: n.reason,
                            })}
                          </Typography>
                          {n.fields && n.fields.length > 0 && (
                            <Typography
                              variant="caption"
                              sx={{
                                color: colors.text.secondary,
                                fontFamily: "monospace",
                              }}
                            >
                              {n.fields.join("  ")}
                            </Typography>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            ))}
          </>
        )}
      </Stack>
    </B4Dialog>
  );
}

function splitDomains(value: string): string[] {
  return value
    .split(/[\s,;]+/)
    .map((d) => d.trim())
    .filter(Boolean);
}

interface PlanEditorProps {
  plan: ConvertSetPlan[];
  values: Record<string, string>;
  shared: string;
  onChange: (profile: number, value: string) => void;
}

function PlanEditor({
  plan,
  values,
  shared,
  onChange,
}: Readonly<PlanEditorProps>) {
  const { t } = useTranslation();
  if (plan.length === 0) return null;

  return (
    <Stack gap={1.5}>
      <Typography variant="subtitle2">{t("sets.convert.planTitle")}</Typography>
      {plan.map((p) => {
        const key = String(p.profile);
        const value = values[key] ?? (p.role === "entry" ? shared : "");
        return (
          <Stack
            key={key}
            direction={{ xs: "column", md: "row" }}
            gap={1.5}
            alignItems={{ md: "flex-start" }}
            sx={{
              border: `1px solid ${colors.border.default}`,
              borderRadius: 1,
              p: 1.5,
            }}
          >
            <Stack gap={0.5} sx={{ minWidth: 220 }}>
              <Stack direction="row" gap={0.75} alignItems="center">
                <Typography variant="body2">
                  <strong>{p.name}</strong>
                </Typography>
                <Chip
                  size="small"
                  label={t(`sets.convert.role.${p.role}`, {
                    index: p.fallback_for + 1,
                  })}
                  sx={{
                    height: 20,
                    fontSize: 11,
                    bgcolor: "transparent",
                    border: `1px solid ${
                      p.role === "entry"
                        ? colors.state.success
                        : colors.text.secondary
                    }`,
                    color:
                      p.role === "entry"
                        ? colors.state.success
                        : colors.text.secondary,
                  }}
                />
              </Stack>
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary }}
              >
                {p.strategy}
                {p.faking ? " + fake" : ""}
                {p.enabled ? "" : ` - ${t("sets.convert.willBeDisabled")}`}
              </Typography>
            </Stack>

            {p.accepts_targets ? (
              <B4TextField
                label={t("sets.convert.domainsLabel")}
                value={value}
                onChange={(e) => onChange(p.profile, e.target.value)}
                helperText={
                  values[key] === undefined && shared
                    ? t("sets.convert.usingShared")
                    : undefined
                }
              />
            ) : (
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary, pt: 1 }}
              >
                {t("sets.convert.fallbackNoTargets", {
                  index: p.fallback_for + 1,
                })}
              </Typography>
            )}
          </Stack>
        );
      })}
    </Stack>
  );
}

function Summary({ result }: Readonly<{ result: ConvertResult }>) {
  const { t } = useTranslation();
  const counts = CONVERT_STATUS_ORDER.filter(
    (s) => countFor(result, s) > 0,
  ).map((s) => ({ status: s, count: countFor(result, s) }));

  return (
    <Stack gap={1}>
      <Stack direction="row" gap={1} alignItems="center" flexWrap="wrap">
        <Chip
          size="small"
          label={`${result.tool_label} ${result.version_label}`}
          color="primary"
        />
        {result.version_inferred && (
          <Chip
            size="small"
            variant="outlined"
            label={t("sets.convert.versionGuessed")}
          />
        )}
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("sets.convert.summary", {
            sets: result.sets.length,
            options: result.fidelity.total,
          })}
        </Typography>
      </Stack>

      <Box>
        <Stack
          direction="row"
          justifyContent="space-between"
          alignItems="center"
        >
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("sets.convert.fidelity")}
          </Typography>
          <Typography variant="caption">{result.fidelity.score}%</Typography>
        </Stack>
        <LinearProgress
          variant="determinate"
          value={result.fidelity.score}
          sx={{ height: 6, borderRadius: 3 }}
        />
      </Box>

      <Stack direction="row" gap={0.75} flexWrap="wrap">
        {counts.map(({ status, count }) => (
          <Chip
            key={status}
            size="small"
            label={`${t(`sets.convert.status.${status}`)}: ${count}`}
            sx={{
              bgcolor: "transparent",
              border: `1px solid ${statusColor[status]}`,
              color: statusColor[status],
              height: 22,
              fontSize: 11,
            }}
          />
        ))}
      </Stack>
    </Stack>
  );
}

function countFor(result: ConvertResult, status: ConvertStatus): number {
  const f = result.fidelity;
  switch (status) {
    case "mapped":
      return f.mapped;
    case "approximated":
      return f.approximated;
    case "unsupported":
      return f.unsupported;
    case "not_applicable":
      return f.not_applicable;
    case "degenerate":
      return f.degenerate;
    case "unknown":
      return f.unknown;
    case "invalid":
      return f.invalid;
    default:
      return 0;
  }
}

interface NoteGroup {
  profile: number;
  name: string;
  notes: ConvertNote[];
}

function groupNotes(result: ConvertResult | null): NoteGroup[] {
  if (!result) return [];
  const groups = new Map<number, ConvertNote[]>();
  for (const note of result.notes) {
    const list = groups.get(note.profile) ?? [];
    list.push(note);
    groups.set(note.profile, list);
  }
  return [...groups.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([profile, notes]) => ({
      profile,
      name: result.sets[profile]?.name ?? "",
      notes,
    }));
}
