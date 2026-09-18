import {
  Alert,
  Box,
  Button,
  Chip,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Link,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import type { TFunction } from "i18next";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { useSetEdit, useSetPreview } from "@/api/hub";
import { Facts, type Fact } from "@/components/common/Facts";
import { Mono } from "@/components/common/Mono";
import { useSnackbar } from "@/context/SnackbarProvider";
import type { EditPreview, EntryView, Projection, SuggestionView } from "@/models/api";
import { setRef } from "@/utils/format";
import { ProjectionDiff } from "./ProjectionDiff";

interface EditSetDialogProps {
  entry: EntryView | null;
  onClose: () => void;
}

interface Draft {
  title: string;
  description: string;
  projection: Projection;
}

type Parsed = { projection: Projection; error: null } | { projection: null; error: string };

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const domainsOf = (projection: Projection): string[] => {
  const targets = projection.targets;
  if (!isObject(targets)) return [];
  const list: unknown[] = Array.isArray(targets.sni_domains) ? targets.sni_domains : [];
  return list.filter((item): item is string => typeof item === "string");
};

const withDomains = (projection: Projection, domains: string[]): Projection => {
  const next: Projection = { ...projection };
  const targets: Record<string, unknown> = isObject(next.targets) ? { ...next.targets } : {};
  if (domains.length > 0) targets.sni_domains = domains;
  else delete targets.sni_domains;
  if (Object.keys(targets).length > 0) next.targets = targets;
  else delete next.targets;
  return next;
};

const pretty = (projection: Projection): string => JSON.stringify(projection, null, 2);

const lines = (text: string): string[] =>
  text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");

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

const monoInput = (fontSize: number) => ({ input: { sx: { fontFamily: fonts.mono, fontSize } } });

interface CheckResultProps {
  entry: EntryView;
  result: EditPreview;
  busy: boolean;
  onApplyTidy: () => void;
}

function CheckResult({ entry, result, busy, onApplyTidy }: CheckResultProps) {
  const { t } = useTranslation();
  const facts: Fact[] = [
    { label: t("entry.targets"), value: result.targets.summary },
    {
      label: t("entry.strategy"),
      value: (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {result.strategy.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </Stack>
      ),
    },
    {
      label: t("entry.flags"),
      value: result.flags.length ? (
        <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
          {result.flags.map((flag) => (
            <Chip key={flag} size="small" variant="outlined" color="warning" label={flag} />
          ))}
        </Stack>
      ) : (
        t("entry.noFlags")
      ),
    },
    { label: t("edit.family"), value: result.family },
    { label: t("entry.needsB4"), value: result.b4_min },
    { label: t("entry.fingerprint"), value: result.fp, mono: true },
  ];

  return (
    <Stack spacing={1.5}>
      {result.duplicate && (
        <Alert severity="error">
          {t("edit.duplicate", {
            ref: setRef(result.duplicate.set_id, result.duplicate.version),
            title: result.duplicate.title,
            status: result.duplicate.status,
          })}
        </Alert>
      )}
      {!result.changed ? (
        <Alert severity="info">{t("edit.unchanged")}</Alert>
      ) : result.fp_changed ? (
        <Alert severity="warning">{t("edit.fpChanged")}</Alert>
      ) : (
        <Alert severity="success">{t("edit.targetsOnly")}</Alert>
      )}
      {result.tidy && (
        <Alert
          severity="warning"
          variant="outlined"
          action={
            <Button size="small" color="inherit" disabled={busy} onClick={onApplyTidy}>
              {t("edit.applyTidy")}
            </Button>
          }
        >
          <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
            {t("edit.tidy", { count: result.tidy.suggestions.length })}
          </Typography>
          <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
            {result.tidy.suggestions.map((s) => (
              <li key={`${s.kind}:${s.entry}`}>{suggestionText(t, s)}</li>
            ))}
          </Stack>
        </Alert>
      )}
      {result.warnings.length > 0 && (
        <Box>
          <Typography variant="sectionHeader" sx={{ display: "block", mb: 0.5 }}>
            {t("edit.warnings")}
          </Typography>
          <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
            {result.warnings.map((w, i) => (
              <li key={`${w.code}:${String(i)}`}>
                {t(`edit.warningCodes.${w.code}`, { defaultValue: w.code })}{" "}
                {w.params !== undefined && <Mono>{JSON.stringify(w.params)}</Mono>}
              </li>
            ))}
          </Stack>
        </Box>
      )}
      {result.stripped.length > 0 && (
        <Box>
          <Typography variant="sectionHeader" sx={{ display: "block", mb: 0.5 }}>
            {t("edit.stripped")}
          </Typography>
          <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
            {result.stripped.map((s) => (
              <li key={s.path}>
                <Mono>{s.path}</Mono> {s.reason}
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
        emptyText={t("edit.diffNone")}
        beforeLabel={t("edit.received")}
        afterLabel={t("edit.edited")}
      />
    </Stack>
  );
}

export function EditSetDialog({ entry, onClose }: EditSetDialogProps) {
  const { t } = useTranslation();
  const { notify, notifyError } = useSnackbar();
  const preview = useSetPreview();
  const edit = useSetEdit();
  const previewMutate = preview.mutate;
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [note, setNote] = useState("");
  const [text, setText] = useState("");
  const [domainsText, setDomainsText] = useState("");
  const [showJson, setShowJson] = useState(false);
  const [result, setResult] = useState<EditPreview | null>(null);
  const [checkError, setCheckError] = useState<string | null>(null);
  const [stale, setStale] = useState(false);

  const notObject = t("edit.notObject");
  const parsed = useMemo(() => parse(text, notObject), [text, notObject]);
  const sequence = useRef(0);
  const latestDraft = useRef("");
  const draftKey = parsed.projection ? JSON.stringify({ title, description, projection: parsed.projection }) : "";
  useEffect(() => {
    latestDraft.current = draftKey;
  }, [draftKey]);

  const check = useCallback(
    (draft: Draft) => {
      if (!entry) return;
      const key = JSON.stringify(draft);
      sequence.current += 1;
      const mine = sequence.current;
      const current = () => mine === sequence.current && key === latestDraft.current;
      setCheckError(null);
      setStale(true);
      previewMutate(
        { id: entry.set_id, version: entry.version, body: { ...draft, note: "", approve: false } },
        {
          onSuccess: (data) => {
            if (!current()) return;
            setResult(data);
            setStale(false);
          },
          onError: (err) => {
            if (!current()) return;
            setResult(null);
            setCheckError(errorMessage(err));
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
    setDomainsText(domainsOf(draft.projection).join("\n"));
    setShowJson(false);
    setResult(null);
    check(draft);
  }, [entry, check]);

  const changeDomains = (value: string) => {
    setDomainsText(value);
    if (parsed.projection) setText(pretty(withDomains(parsed.projection, lines(value))));
    setStale(true);
  };

  const changeText = (value: string) => {
    setText(value);
    const next = parse(value, notObject);
    if (next.projection) setDomainsText(domainsOf(next.projection).join("\n"));
    setStale(true);
  };

  const currentDraft = (): Draft | null => (parsed.projection ? { title, description, projection: parsed.projection } : null);

  const applyTidy = () => {
    if (!parsed.projection || !result?.tidy) return;
    const next = withDomains(parsed.projection, result.tidy.domains);
    setText(pretty(next));
    setDomainsText(result.tidy.domains.join("\n"));
    check({ title, description, projection: next });
  };

  const runCheck = () => {
    const draft = currentDraft();
    if (draft) check(draft);
  };

  const save = async (approve: boolean) => {
    const draft = currentDraft();
    if (!entry || !draft) return;
    try {
      const outcome = await edit.mutateAsync({ id: entry.set_id, version: entry.version, body: { ...draft, note, approve } });
      notify(outcome.notice, "success");
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };

  const busy = preview.isPending || edit.isPending;
  const fresh = result !== null && !stale ? result : null;
  const blocked = busy || parsed.projection === null || fresh === null || !fresh.changed || fresh.duplicate !== undefined;
  const invalid = parsed.error !== null;

  return (
    <Dialog open={entry !== null} onClose={busy ? undefined : onClose} fullWidth maxWidth="md">
      <DialogTitle>{entry ? t("edit.title", { ref: setRef(entry.set_id, entry.version) }) : ""}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("edit.intro")}
        </Typography>
        <TextField
          label={t("edit.setTitle")}
          value={title}
          onChange={(e) => {
            setTitle(e.target.value);
            setStale(true);
          }}
          size="small"
          fullWidth
        />
        <TextField
          label={t("edit.description")}
          value={description}
          onChange={(e) => {
            setDescription(e.target.value);
            setStale(true);
          }}
          size="small"
          fullWidth
          multiline
          minRows={2}
        />
        <TextField
          label={t("edit.domains")}
          value={domainsText}
          onChange={(e) => changeDomains(e.target.value)}
          disabled={invalid}
          size="small"
          fullWidth
          multiline
          minRows={6}
          maxRows={16}
          helperText={invalid ? t("edit.domainsLocked") : t("edit.domainsHint", { count: lines(domainsText).length })}
          slotProps={monoInput(13)}
        />
        <Box>
          <Link component="button" type="button" underline="hover" variant="body2" onClick={() => setShowJson((v) => !v)}>
            {showJson ? t("edit.hideJson") : t("edit.showJson")}
          </Link>
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
          {!showJson && parsed.error !== null && (
            <Alert severity="error" sx={{ mt: 1 }}>
              {t("edit.invalidJson", { message: parsed.error })}
            </Alert>
          )}
        </Box>
        <TextField
          label={t("edit.note")}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          size="small"
          fullWidth
          helperText={t("edit.noteHint")}
        />
        {checkError !== null && <Alert severity="error">{checkError}</Alert>}
        {stale && result !== null && (
          <Alert severity="info" variant="outlined">
            {t("edit.stale")}
          </Alert>
        )}
        {fresh !== null && entry !== null && <CheckResult entry={entry} result={fresh} busy={busy} onApplyTidy={applyTidy} />}
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
        <Button onClick={() => void save(true)} disabled={blocked} variant="contained" color="success">
          {t("edit.saveApprove")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
