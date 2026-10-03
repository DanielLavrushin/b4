import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Chip,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Grid,
  LinearProgress,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { TFunction } from "i18next";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { ApiError } from "@/api/client";
import { useGeoCategories, useSetEdit, useSetPreview } from "@/features/sets/api";
import { Facts, type Fact } from "@/shared/components/Facts";
import { Mono } from "@/shared/components/Mono";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { EditPreview, EntryView, Projection, SuggestionView, TidyView, WarningView } from "@/models/api";
import { setRef } from "@/shared/utils/format";
import { errorText } from "@/shared/utils/notices";
import { flagHint, flagText, targetsPreview, techniqueText } from "@/shared/utils/terms";
import { FlagChips } from "./components/TechniqueChips";
import {
  EMPTY_TARGETS,
  FIELD_LABELS,
  IP_VERSIONS,
  TLS_OFFERED,
  asnList,
  clientWarnings,
  draftOf,
  fieldErrorText,
  fieldOfPath,
  filterCategories,
  hasTargets,
  invalidAsns,
  invalidFields,
  ipKey,
  ipVersionUnknown,
  isObject,
  lines,
  normalizeCategories,
  sameField,
  siteKey,
  splitWarnings,
  tlsUnknown,
  tokens,
  warningItems,
  warningText,
  withTargets,
  type FieldKey,
  type KnownCategories,
  type TargetsDraft,
} from "./editTargets";
import { ProjectionDiff } from "./ProjectionDiff";
import { revisionOf } from "./revision";

interface EditSetDialogProps {
  entry: EntryView | null;
  onClose: () => void;
}

interface Draft {
  title: string;
  description: string;
  projection: Projection;
}

interface Checked {
  key: string;
  draft: Draft;
  result: EditPreview | null;
  error: unknown;
}

type Parsed = { projection: Projection; error: null } | { projection: null; error: string };

interface Note {
  key: string;
  text: string;
  items: string[];
}

const ATTENTION_FLAGS = new Set(["block", "catch_all", "blanket"]);
const NOTE_ITEMS = 12;
const NO_TARGETS_MESSAGE = "the set has no targets";

const pretty = (projection: Projection): string => JSON.stringify(projection, null, 2);

const errorMessage = (err: unknown): string => (err instanceof Error ? err.message : String(err));

const parse = (text: string, notObject: string): Parsed => {
  try {
    const value: unknown = JSON.parse(text);
    return isObject(value) ? { projection: value, error: null } : { projection: null, error: notObject };
  } catch (err) {
    return { projection: null, error: errorMessage(err) };
  }
};

const suggestionText = (t: TFunction, s: SuggestionView): string => {
  switch (s.kind) {
    case "dead_wildcard":
      if (s.replacement) return t("edit.suggestion.deadWildcardReplace", { entry: s.entry, replacement: s.replacement });
      if (s.by) return t("edit.suggestion.deadWildcardCovered", { entry: s.entry, by: s.by });
      return t("edit.suggestion.deadWildcard", { entry: s.entry });
    case "covered":
      return t("edit.suggestion.covered", { entry: s.entry, by: s.by ?? "" });
    case "duplicate":
      return t("edit.suggestion.duplicate", { entry: s.entry, by: s.by ?? "" });
    case "www_only":
      return t("edit.suggestion.wwwOnly", { entry: s.entry, replacement: s.replacement ?? "" });
  }
};

const toNotes = (t: TFunction, warnings: WarningView[]): Note[] =>
  warnings.map((warning, index) => ({
    key: `${warning.code}:${String(index)}`,
    text: warningText(t, warning),
    items: warningItems(warning),
  }));

const monoInput = (fontSize: number) => ({ input: { sx: { fontFamily: fonts.mono, fontSize } } });

const noteSx = {
  py: 0,
  px: 1.5,
  "& .MuiAlert-icon": { fontSize: 18, py: "7px", mr: 1 },
  "& .MuiAlert-message": { py: "7px", minWidth: 0, overflowWrap: "anywhere" },
};

const chipSx = { fontFamily: fonts.mono, fontWeight: 500 };

const listSx = { m: 0, pl: "1.1em" };

function FieldNotes({ notes }: Readonly<{ notes: Note[] }>) {
  const { t } = useTranslation();
  if (notes.length === 0) return null;
  return (
    <Stack spacing={0.75} sx={{ mt: 1 }}>
      {notes.map((note) => {
        const shown = note.items.slice(0, NOTE_ITEMS);
        const more = note.items.length - shown.length;
        return (
          <Alert key={note.key} severity="warning" sx={noteSx}>
            {note.text}
            {shown.length > 0 && (
              <Box sx={{ mt: 0.25 }}>
                <Mono>{shown.join(", ")}</Mono>
                {more > 0 && ` ${t("targets.more", { count: more })}`}
              </Box>
            )}
          </Alert>
        );
      })}
    </Stack>
  );
}

function TidyNote({ tidy, busy, onApply }: Readonly<{ tidy: TidyView; busy: boolean; onApply: () => void }>) {
  const { t } = useTranslation();
  return (
    <Alert severity="warning" sx={{ ...noteSx, mt: 0.75 }}>
      <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
        {t("edit.tidy", { count: tidy.suggestions.length })}
      </Typography>
      <Stack component="ul" sx={listSx} spacing={0.25}>
        {tidy.suggestions.map((s) => (
          <li key={`${s.kind}:${s.entry}`}>{suggestionText(t, s)}</li>
        ))}
      </Stack>
      <Button size="small" color="inherit" variant="outlined" disabled={busy} onClick={onApply} sx={{ mt: 1 }}>
        {t("edit.applyTidy")}
      </Button>
    </Alert>
  );
}

interface ListFieldProps {
  label: string;
  hint: string;
  value: string;
  disabled: boolean;
  minRows: number;
  maxRows: number;
  errors: string[];
  notes: Note[];
  onChange: (value: string) => void;
  children?: ReactNode;
}

function ListField({ label, hint, value, disabled, minRows, maxRows, errors, notes, onChange, children }: Readonly<ListFieldProps>) {
  return (
    <>
      <TextField
        label={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        size="small"
        fullWidth
        multiline
        minRows={minRows}
        maxRows={maxRows}
        error={errors.length > 0}
        helperText={errors.length > 0 ? errors.join(" ") : hint}
        slotProps={monoInput(13)}
      />
      <FieldNotes notes={notes} />
      {children}
    </>
  );
}

interface CategoryFieldProps {
  label: string;
  hint: string;
  value: string[];
  options: string[];
  known: Set<string>;
  keyOf: (name: string) => string;
  disabled: boolean;
  errors: string[];
  notes: Note[];
  onChange: (value: string[]) => void;
}

function CategoryField({ label, hint, value, options, known, keyOf, disabled, errors, notes, onChange }: Readonly<CategoryFieldProps>) {
  const { t } = useTranslation();
  return (
    <>
      <Autocomplete
        multiple
        freeSolo
        autoSelect
        filterSelectedOptions
        size="small"
        options={options}
        value={value}
        disabled={disabled}
        filterOptions={filterCategories}
        isOptionEqualToValue={(option, item) => option.toLowerCase() === item.toLowerCase()}
        onChange={(_, next) => onChange(normalizeCategories(next))}
        clearText={t("edit.clear")}
        renderValue={(items, getItemProps) =>
          items.map((item, index) => {
            const { key, ...itemProps } = getItemProps({ index });
            const unknown = known.size > 0 && !known.has(keyOf(item));
            return (
              <Chip
                key={key}
                {...itemProps}
                size="small"
                variant="outlined"
                color={unknown ? "warning" : "default"}
                label={item}
                sx={chipSx}
              />
            );
          })
        }
        renderInput={(params) => (
          <TextField
            {...params}
            label={label}
            error={errors.length > 0}
            helperText={errors.length > 0 ? errors.join(" ") : hint}
          />
        )}
        slotProps={{ listbox: { sx: { fontFamily: fonts.mono, fontSize: 13 } } }}
      />
      <FieldNotes notes={notes} />
    </>
  );
}

const impactOf = (t: TFunction, entry: EntryView, result: EditPreview) => {
  const out: string[] = [];
  if (entry.b4_min && result.b4_min && entry.b4_min !== result.b4_min) {
    out.push(t("edit.b4MinChanged", { from: entry.b4_min, to: result.b4_min }));
  }
  const before = new Set(entry.flags);
  const after = new Set(result.flags);
  const added = result.flags.filter((flag) => !before.has(flag));
  const removed = entry.flags.filter((flag) => !after.has(flag));
  added.forEach((flag) => out.push(t("edit.flagAdded", { flag: flagText(t, flag), hint: flagHint(t, flag) }).trim()));
  removed.forEach((flag) => out.push(t("edit.flagRemoved", { flag: flagText(t, flag) })));
  return { lines: out, attention: added.some((flag) => ATTENTION_FLAGS.has(flag)) };
};

function CheckError({ error, noTargets }: Readonly<{ error: unknown; noTargets: boolean }>) {
  const { t } = useTranslation();
  const fields = invalidFields(error);
  if (fields.length === 0) {
    if (noTargets && error instanceof ApiError && error.code === "invalid_set" && error.message === NO_TARGETS_MESSAGE) return null;
    return <Alert severity="error">{errorText(t, error)}</Alert>;
  }
  return (
    <Alert severity="error">
      <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
        {t("edit.refused")}
      </Typography>
      <Stack component="ul" sx={listSx} spacing={0.25}>
        {fields.map((field, index) => {
          const key = fieldOfPath(field.path);
          return (
            <li key={`${field.path}:${String(index)}`}>
              {key ? t(FIELD_LABELS[key]) : <Mono>{field.path}</Mono>}: {fieldErrorText(t, field)}
            </li>
          );
        })}
      </Stack>
    </Alert>
  );
}

interface CheckResultProps {
  entry: EntryView;
  result: EditPreview;
  warnings: WarningView[];
}

function CheckResult({ entry, result, warnings }: Readonly<CheckResultProps>) {
  const { t } = useTranslation();
  const facts: Fact[] = [
    { label: t("entry.targets"), value: targetsPreview(t, result.targets) },
    {
      label: t("entry.strategy"),
      value: (
        <Stack component="ul" sx={listSx} spacing={0.25}>
          {result.strategy.map((term, i) => (
            <li key={`${term.code}-${String(i)}`}>{techniqueText(t, term)}</li>
          ))}
        </Stack>
      ),
    },
    {
      label: t("entry.flags"),
      value: result.flags.length ? <FlagChips flags={result.flags} /> : t("entry.noFlags"),
    },
    { label: t("entry.needsB4"), value: result.b4_min },
    { label: t("entry.fingerprint"), value: result.fp, mono: true },
  ];
  const impact = impactOf(t, entry, result);
  let outcome = <Alert severity="info">{t("edit.targetsOnly")}</Alert>;
  if (!result.changed) outcome = <Alert severity="info">{t("edit.unchanged")}</Alert>;
  else if (result.fp_changed) outcome = <Alert severity="warning">{t("edit.fpChanged")}</Alert>;

  return (
    <Stack spacing={1.5}>
      {result.duplicate && (
        <Alert severity="error">
          {t("edit.duplicate", {
            ref: setRef(result.duplicate.set_id, result.duplicate.version),
            title: result.duplicate.title,
            status: t(`status.set.${result.duplicate.status}`),
          })}
        </Alert>
      )}
      {outcome}
      {impact.lines.length > 0 && (
        <Alert severity={impact.attention ? "warning" : "info"}>
          {impact.lines.length === 1 ? (
            impact.lines[0]
          ) : (
            <Stack component="ul" sx={listSx} spacing={0.25}>
              {impact.lines.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </Stack>
          )}
        </Alert>
      )}
      {warnings.length > 0 && (
        <Box>
          <Typography variant="sectionHeader" sx={{ display: "block", mb: 0.5 }}>
            {t("edit.warnings")}
          </Typography>
          <Stack component="ul" sx={listSx} spacing={0.25}>
            {warnings.map((w, i) => {
              const items = warningItems(w);
              return (
                <li key={`${w.code}:${String(i)}`}>
                  {warningText(t, w)}
                  {items.length > 0 && (
                    <Mono>
                      {" "}
                      {items.join(", ")}
                    </Mono>
                  )}
                </li>
              );
            })}
          </Stack>
        </Box>
      )}
      {result.stripped.length > 0 && (
        <Box>
          <Typography variant="sectionHeader" sx={{ display: "block", mb: 0.5 }}>
            {t("edit.stripped")}
          </Typography>
          <Stack component="ul" sx={listSx} spacing={0.25}>
            {result.stripped.map((s) => (
              <li key={s.path}>
                <Mono>{s.path}</Mono> {t(`edit.strippedReasons.${s.reason}`, { defaultValue: s.reason })}
              </li>
            ))}
          </Stack>
        </Box>
      )}
      <Facts items={facts} />
      <ProjectionDiff
        before={entry.projection}
        after={result.projection}
        beforeVersion={entry.version}
        title={t("edit.diffTitle")}
        emptyText={t("edit.diffSame")}
        beforeLabel={t("edit.now")}
        afterLabel={t("edit.edited")}
      />
    </Stack>
  );
}

export function EditSetDialog({ entry, onClose }: Readonly<EditSetDialogProps>) {
  const { t } = useTranslation();
  const { notifyResult, notifyError } = useSnackbar();
  const preview = useSetPreview();
  const edit = useSetEdit();
  const categories = useGeoCategories(entry !== null);
  const previewMutate = preview.mutate;
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [note, setNote] = useState("");
  const [text, setText] = useState("");
  const [targets, setTargets] = useState<TargetsDraft>(EMPTY_TARGETS);
  const [showJson, setShowJson] = useState(false);
  const [checked, setChecked] = useState<Checked | null>(null);
  const sequence = useRef(0);

  const notObject = t("edit.notObject");
  const parsed = useMemo(() => parse(text, notObject), [text, notObject]);
  const projection = parsed.projection;
  const draftKey = useMemo(
    () => (projection ? JSON.stringify({ title, description, projection }) : ""),
    [title, description, projection],
  );

  const check = useCallback(
    (draft: Draft) => {
      if (!entry) return;
      sequence.current += 1;
      const mine = sequence.current;
      const key = JSON.stringify(draft);
      previewMutate(
        { id: entry.set_id, version: entry.version, body: { ...draft, note: "", approve: false } },
        {
          onSuccess: (data) => {
            if (mine === sequence.current) setChecked({ key, draft, result: data, error: null });
          },
          onError: (err) => {
            if (mine === sequence.current) setChecked({ key, draft, result: null, error: err });
          },
        },
      );
    },
    [entry, previewMutate],
  );

  useEffect(() => {
    if (!entry) return;
    const draft: Draft = { title: entry.title, description: entry.description ?? "", projection: entry.projection };
    setTitle(draft.title);
    setDescription(draft.description);
    setNote("");
    setText(pretty(draft.projection));
    setTargets(draftOf(draft.projection));
    setShowJson(false);
    setChecked(null);
    check(draft);
  }, [entry, check]);

  const known = useMemo<KnownCategories>(
    () => ({
      geosite: new Set((categories.data?.geosite ?? []).map((name) => name.toLowerCase())),
      geoip: new Set((categories.data?.geoip ?? []).map((name) => name.toLowerCase())),
    }),
    [categories.data],
  );
  const serverWarnings = useMemo(() => splitWarnings(checked?.result?.warnings ?? []), [checked]);
  const serverErrors = useMemo(() => invalidFields(checked?.error), [checked]);
  const localWarnings = useMemo(() => clientWarnings(targets, known), [targets, known]);
  const asnErrors = useMemo(
    () => invalidAsns(targets.asns).map((value) => t("edit.fieldErrors.asn_invalid", { value })),
    [targets.asns, t],
  );

  const stale = checked === null || checked.key !== draftKey;
  const result = checked !== null && !stale ? checked.result : null;
  const fieldFresh = (key: FieldKey) => checked !== null && projection !== null && sameField(checked.draft.projection, projection, key);

  const errorsFor = (key: FieldKey): string[] => {
    const own = key === "asns" ? asnErrors : [];
    const server = fieldFresh(key)
      ? serverErrors.filter((field) => fieldOfPath(field.path) === key).map((field) => fieldErrorText(t, field))
      : [];
    return [...new Set([...own, ...server])];
  };

  const notesFor = (key: FieldKey): Note[] => {
    const fromServer = checked?.result && fieldFresh(key) ? (serverWarnings.byField[key] ?? []) : null;
    return toNotes(t, fromServer ?? localWarnings[key] ?? []);
  };

  const tidy = checked?.result?.tidy && fieldFresh("sni_domains") ? checked.result.tidy : undefined;

  const changeTargets = (patch: Partial<TargetsDraft>) => {
    const next = { ...targets, ...patch };
    setTargets(next);
    if (projection) setText(pretty(withTargets(projection, next, Object.keys(patch) as FieldKey[])));
  };

  const changeText = (value: string) => {
    setText(value);
    const next = parse(value, notObject);
    if (next.projection) setTargets(draftOf(next.projection));
  };

  const applyTidy = () => {
    if (!projection || !tidy) return;
    const next = { ...targets, sni_domains: tidy.domains.join("\n") };
    const nextProjection = withTargets(projection, next, ["sni_domains"]);
    setTargets(next);
    setText(pretty(nextProjection));
    check({ title, description, projection: nextProjection });
  };

  const runCheck = () => {
    if (projection) check({ title, description, projection });
  };

  const save = async (approve: boolean) => {
    if (!entry || !projection) return;
    try {
      const outcome = await edit.mutateAsync({
        id: entry.set_id,
        version: entry.version,
        body: { title, description, projection, note, approve, expect_updated_at: revisionOf(entry) },
      });
      notifyResult(outcome);
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };

  const busy = preview.isPending || edit.isPending;
  const invalid = parsed.error !== null;
  const noTargets = projection !== null && !hasTargets(projection);
  const tlsOptions = TLS_OFFERED.includes(targets.tls) ? TLS_OFFERED : [...TLS_OFFERED, targets.tls];
  const ipOptions = IP_VERSIONS.includes(targets.ip_version) ? IP_VERSIONS : [...IP_VERSIONS, targets.ip_version];
  const tlsOdd = tlsUnknown(targets.tls);
  const ipOdd = ipVersionUnknown(targets.ip_version);
  const blocked =
    busy || invalid || noTargets || tlsOdd || ipOdd || result === null || !result.changed || result.duplicate !== undefined;
  const shrunk = { select: { displayEmpty: true }, inputLabel: { shrink: true } };

  return (
    <Dialog open={entry !== null} onClose={busy ? undefined : onClose} fullWidth maxWidth="md">
      <DialogTitle>{entry ? t("edit.title", { ref: setRef(entry.set_id, entry.version) }) : ""}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("edit.intro")}
        </Typography>
        <TextField label={t("edit.setTitle")} value={title} onChange={(e) => setTitle(e.target.value)} size="small" fullWidth />
        <TextField
          label={t("edit.description")}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          size="small"
          fullWidth
          multiline
          minRows={2}
        />
        <Stack spacing={1.5}>
          <Typography variant="sectionHeader" component="h3">
            {t("edit.targets")}
          </Typography>
          {invalid && <Alert severity="error">{t("edit.targetsLocked", { message: parsed.error })}</Alert>}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <ListField
                label={t("edit.domains")}
                hint={t("edit.domainsHint", { count: lines(targets.sni_domains).length })}
                value={targets.sni_domains}
                disabled={invalid}
                minRows={4}
                maxRows={16}
                errors={errorsFor("sni_domains")}
                notes={notesFor("sni_domains")}
                onChange={(value) => changeTargets({ sni_domains: value })}
              >
                {tidy && <TidyNote tidy={tidy} busy={busy} onApply={applyTidy} />}
              </ListField>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <ListField
                label={t("edit.ips")}
                hint={t("edit.ipsHint", { count: tokens(targets.ip).length })}
                value={targets.ip}
                disabled={invalid}
                minRows={4}
                maxRows={16}
                errors={errorsFor("ip")}
                notes={notesFor("ip")}
                onChange={(value) => changeTargets({ ip: value })}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <ListField
                label={t("edit.asns")}
                hint={t("edit.asnsHint", { count: asnList(targets.asns).length })}
                value={targets.asns}
                disabled={invalid}
                minRows={1}
                maxRows={8}
                errors={errorsFor("asns")}
                notes={notesFor("asns")}
                onChange={(value) => changeTargets({ asns: value })}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <CategoryField
                label={t("edit.geosite")}
                hint={t("edit.geositeHint", { count: targets.geosite_categories.length })}
                value={targets.geosite_categories}
                options={categories.data?.geosite ?? []}
                known={known.geosite}
                keyOf={siteKey}
                disabled={invalid}
                errors={errorsFor("geosite_categories")}
                notes={notesFor("geosite_categories")}
                onChange={(value) => changeTargets({ geosite_categories: value })}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <CategoryField
                label={t("edit.geoip")}
                hint={t("edit.geoipHint", { count: targets.geoip_categories.length })}
                value={targets.geoip_categories}
                options={categories.data?.geoip ?? []}
                known={known.geoip}
                keyOf={ipKey}
                disabled={invalid}
                errors={errorsFor("geoip_categories")}
                notes={notesFor("geoip_categories")}
                onChange={(value) => changeTargets({ geoip_categories: value })}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                select
                size="small"
                fullWidth
                label={t("edit.tls")}
                value={targets.tls}
                disabled={invalid}
                onChange={(e) => changeTargets({ tls: e.target.value })}
                error={tlsOdd}
                helperText={tlsOdd ? t("edit.filterUnknown") : undefined}
                slotProps={shrunk}
              >
                {tlsOptions.map((value) => (
                  <MenuItem key={value} value={value}>
                    {value === "" ? t("edit.any") : `TLS ${value}`}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <TextField
                select
                size="small"
                fullWidth
                label={t("edit.ipVersion")}
                value={targets.ip_version}
                disabled={invalid}
                onChange={(e) => changeTargets({ ip_version: e.target.value })}
                error={ipOdd}
                helperText={ipOdd ? t("edit.filterUnknown") : undefined}
                slotProps={shrunk}
              >
                {ipOptions.map((value) => (
                  <MenuItem key={value} value={value}>
                    {value === "" ? t("edit.any") : `IPv${value}`}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <FormControlLabel
                sx={{ m: 0, alignItems: "flex-start", gap: 1.5 }}
                control={
                  <Switch
                    checked={targets.domain_only}
                    disabled={invalid}
                    onChange={(e) => changeTargets({ domain_only: e.target.checked })}
                    sx={{ mt: 0.25, flexShrink: 0 }}
                  />
                }
                label={
                  <Box>
                    <Typography variant="body2">{t("edit.domainOnly")}</Typography>
                    <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }}>
                      {t("edit.domainOnlyHint")}
                    </Typography>
                  </Box>
                }
              />
            </Grid>
          </Grid>
          {noTargets && <Alert severity="error">{t("edit.noTargets")}</Alert>}
        </Stack>
        <Box>
          <Button
            size="small"
            onClick={() => setShowJson((v) => !v)}
            endIcon={showJson ? <ExpandLessIcon /> : <ExpandMoreIcon />}
            aria-expanded={showJson}
            sx={{ color: colors.text.secondary, px: 1, ml: -1 }}
          >
            {showJson ? t("edit.hideJson") : t("edit.showJson")}
          </Button>
          <Collapse in={showJson} unmountOnExit>
            <TextField
              sx={{ mt: 1.5 }}
              label={t("edit.json")}
              value={text}
              onChange={(e) => changeText(e.target.value)}
              size="small"
              fullWidth
              multiline
              minRows={12}
              maxRows={30}
              error={invalid}
              helperText={parsed.error !== null ? t("edit.invalidJson", { message: parsed.error }) : t("edit.jsonHint")}
              slotProps={monoInput(12)}
            />
          </Collapse>
        </Box>
        <TextField
          label={t("edit.note")}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          size="small"
          fullWidth
          helperText={t("edit.noteHint")}
        />
        {preview.isPending && <LinearProgress color="secondary" />}
        {!preview.isPending && checked !== null && stale && <Alert severity="info">{t("edit.stale")}</Alert>}
        {!stale && checked !== null && checked.result === null && <CheckError error={checked.error} noTargets={noTargets} />}
        {result !== null && entry !== null && <CheckResult entry={entry} result={result} warnings={serverWarnings.rest} />}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy} color="inherit">
          {t("app.cancel")}
        </Button>
        <Button onClick={runCheck} disabled={busy || invalid} variant="outlined">
          {t("edit.check")}
        </Button>
        <Box sx={{ flex: 1 }} />
        <Button onClick={() => void save(false)} disabled={blocked} variant="contained">
          {t("edit.save")}
        </Button>
        <Button onClick={() => void save(true)} disabled={blocked} variant="contained">
          {t("edit.saveApprove")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
