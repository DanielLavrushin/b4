import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { ActionResult, EntryView, MirrorView, SetAction } from "@/models/api";
import { useMirrorAction } from "@/features/mirrors/api";
import { EditSetDialog } from "@/features/sets/EditSetDialog";
import { EditTextDialog } from "@/features/sets/EditTextDialog";
import { ReasonDialog, type ReasonPrompt } from "@/shared/components/ReasonDialog";
import { setRef } from "@/shared/utils/format";
import { useKeyAction, useReinstate, useSetDelete, useVersionReports, useWithdraw } from "./api";
import { KeyImpactSummary } from "./KeyImpactSummary";
import { VersionActionDialog, type VersionTarget } from "./VersionActionDialog";

interface VersionPrompt {
  action: SetAction;
  target: VersionTarget;
  reason?: string;
}

export function useModeration() {
  const { t } = useTranslation();
  const { notifyResult, notifyError } = useSnackbar();
  const [prompt, setPrompt] = useState<ReasonPrompt | null>(null);
  const [versionPrompt, setVersionPrompt] = useState<VersionPrompt | null>(null);
  const [editing, setEditing] = useState<EntryView | null>(null);
  const [retitling, setRetitling] = useState<EntryView | null>(null);
  const keyAction = useKeyAction();
  const mirrorAction = useMirrorAction();
  const setDelete = useSetDelete();
  const withdrawSet = useWithdraw();
  const reinstateSet = useReinstate();
  const versionReports = useVersionReports();

  const run = useCallback(
    async (work: () => Promise<ActionResult>) => {
      try {
        notifyResult(await work());
      } catch (err) {
        notifyError(err);
        throw err;
      }
    },
    [notifyResult, notifyError],
  );

  const version = useCallback(
    (action: SetAction) => (e: EntryView, reason?: string) =>
      setVersionPrompt({ action, target: { set_id: e.set_id, version: e.version, title: e.title, status: e.status }, reason }),
    [],
  );

  const approve = version("approve");
  const reject = version("reject");
  const hide = version("hide");
  const restore = version("restore");

  const edit = useCallback((e: EntryView) => setEditing(e), []);
  const editText = useCallback((e: EntryView) => setRetitling(e), []);

  const withdraw = useCallback(
    (id: string, title: string) =>
      setPrompt({
        title: t("sets.withdrawTitle", { title }),
        text: t("sets.withdrawText"),
        confirmLabel: t("sets.withdraw"),
        reason: "optional",
        scope: "withdraw",
        destructive: true,
        onConfirm: (reason) => run(() => withdrawSet.mutateAsync({ id, reason })),
      }),
    [run, withdrawSet, t],
  );

  const reinstate = useCallback(
    (id: string, title: string) =>
      setPrompt({
        title: t("sets.reinstateTitle", { title }),
        text: t("sets.reinstateText"),
        confirmLabel: t("sets.reinstate"),
        reason: "none",
        onConfirm: () => run(() => reinstateSet.mutateAsync(id)),
      }),
    [run, reinstateSet, t],
  );

  const remove = useCallback(
    (id: string, onDone?: () => void) =>
      setPrompt({
        title: t("detail.deleteTitle", { id }),
        text: t("detail.deleteText"),
        confirmLabel: t("detail.delete"),
        reason: "none",
        destructive: true,
        confirmText: id,
        onConfirm: async () => {
          await run(() => setDelete.mutateAsync(id));
          onDone?.();
        },
      }),
    [run, setDelete, t],
  );

  const reports = useCallback(
    (e: EntryView, action: "dismiss" | "resolve") =>
      setPrompt({
        title: t(`reports.version.${action}Title`, { ref: setRef(e.set_id, e.version) }),
        text: t(`reports.version.${action}Text`, { count: e.open_reports }),
        confirmLabel: t(`reports.actions.${action}`),
        reason: "optional",
        scope: action === "dismiss" ? "report_dismiss" : undefined,
        reasonLabel: t("reports.note"),
        onConfirm: (note) => run(() => versionReports.mutateAsync({ id: e.set_id, version: e.version, action, note })),
      }),
    [run, versionReports, t],
  );

  const ban = useCallback(
    (keyHmac: string, label: string) =>
      setPrompt({
        title: t("keys.banTitle", { key: label }),
        text: t("keys.banText"),
        details: <KeyImpactSummary keyHmac={keyHmac} mode="ban" />,
        confirmLabel: t("keys.ban"),
        reason: "optional",
        scope: "ban",
        destructive: true,
        onConfirm: (reason) => run(() => keyAction.mutateAsync({ key: keyHmac, action: "ban", reason })),
      }),
    [keyAction, run, t],
  );

  const unban = useCallback(
    (keyHmac: string, label: string) =>
      setPrompt({
        title: t("keys.unbanTitle", { key: label }),
        text: t("keys.unbanText"),
        details: <KeyImpactSummary keyHmac={keyHmac} mode="unban" />,
        confirmLabel: t("keys.unban"),
        reason: "none",
        onConfirm: () => run(() => keyAction.mutateAsync({ key: keyHmac, action: "unban" })),
      }),
    [keyAction, run, t],
  );

  const trust = useCallback(
    (keyHmac: string, label: string) =>
      setPrompt({
        title: t("keys.trustTitle", { key: label }),
        text: t("keys.trustText"),
        confirmLabel: t("keys.trust"),
        reason: "none",
        onConfirm: () => run(() => keyAction.mutateAsync({ key: keyHmac, action: "trust" })),
      }),
    [keyAction, run, t],
  );

  const untrust = useCallback(
    (keyHmac: string, label: string) =>
      setPrompt({
        title: t("keys.untrustTitle", { key: label }),
        text: t("keys.untrustText"),
        confirmLabel: t("keys.untrust"),
        reason: "none",
        onConfirm: () => run(() => keyAction.mutateAsync({ key: keyHmac, action: "untrust" })),
      }),
    [keyAction, run, t],
  );

  const approveMirror = useCallback(
    (m: MirrorView) =>
      setPrompt({
        title: t("mirrors.approveTitle"),
        text: `${m.url}. ${t("mirrors.approveText")}`,
        confirmLabel: t("mirrors.approve"),
        reason: "none",
        onConfirm: () => run(() => mirrorAction.mutateAsync({ id: m.id, action: "approve" })),
      }),
    [mirrorAction, run, t],
  );

  const rejectMirror = useCallback(
    (m: MirrorView) =>
      setPrompt({
        title: t("mirrors.rejectTitle"),
        text: `${m.url}. ${t("mirrors.rejectText")}`,
        confirmLabel: t("mirrors.reject"),
        reason: "required",
        scope: "mirror_reject",
        destructive: true,
        onConfirm: (reason) => run(() => mirrorAction.mutateAsync({ id: m.id, action: "reject", reason })),
      }),
    [mirrorAction, run, t],
  );

  const removeMirror = useCallback(
    (m: MirrorView) =>
      setPrompt({
        title: t("mirrors.removeTitle"),
        text: `${m.url}. ${t("mirrors.removeText")}`,
        confirmLabel: t("mirrors.remove"),
        reason: "none",
        destructive: true,
        onConfirm: () => run(() => mirrorAction.mutateAsync({ id: m.id, action: "remove" })),
      }),
    [mirrorAction, run, t],
  );

  const dialog = (
    <>
      <ReasonDialog prompt={prompt} onClose={() => setPrompt(null)} />
      <VersionActionDialog
        action={versionPrompt?.action ?? "approve"}
        target={versionPrompt?.target ?? null}
        initialReason={versionPrompt?.reason}
        onClose={() => setVersionPrompt(null)}
      />
      <EditSetDialog entry={editing} onClose={() => setEditing(null)} />
      <EditTextDialog entry={retitling} onClose={() => setRetitling(null)} />
    </>
  );
  const busy =
    keyAction.isPending ||
    mirrorAction.isPending ||
    setDelete.isPending ||
    withdrawSet.isPending ||
    reinstateSet.isPending ||
    versionReports.isPending;
  const open = prompt !== null || versionPrompt !== null || editing !== null || retitling !== null;

  return {
    approve,
    reject,
    hide,
    restore,
    edit,
    editText,
    withdraw,
    reinstate,
    remove,
    reports,
    ban,
    unban,
    trust,
    untrust,
    approveMirror,
    rejectMirror,
    removeMirror,
    dialog,
    busy,
    open,
  };
}

export type Moderation = ReturnType<typeof useModeration>;
