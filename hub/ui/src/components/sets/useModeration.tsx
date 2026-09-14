import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { useKeyAction, useMirrorAction, useSetAction } from "@/api/hub";
import { useSnackbar } from "@/context/SnackbarProvider";
import type { EntryView, MirrorView } from "@/models/api";
import { setRef } from "@/utils/format";
import { ReasonDialog, type ReasonPrompt } from "@/components/common/ReasonDialog";

export function useModeration() {
  const { t } = useTranslation();
  const { notify, notifyError } = useSnackbar();
  const [prompt, setPrompt] = useState<ReasonPrompt | null>(null);
  const setAction = useSetAction();
  const keyAction = useKeyAction();
  const mirrorAction = useMirrorAction();

  const run = useCallback(
    async (work: () => Promise<{ notice: string }>) => {
      try {
        const result = await work();
        notify(result.notice, "success");
      } catch (err) {
        notifyError(err);
        throw err;
      }
    },
    [notify, notifyError],
  );

  const approve = useCallback(
    (e: EntryView) =>
      setPrompt({
        title: t("queue.approveTitle", { ref: setRef(e.set_id, e.version) }),
        text: t("queue.approveText"),
        confirmLabel: t("queue.approve"),
        reason: "none",
        onConfirm: () => run(() => setAction.mutateAsync({ id: e.set_id, version: e.version, action: "approve" })),
      }),
    [run, setAction, t],
  );

  const reject = useCallback(
    (e: EntryView) =>
      setPrompt({
        title: t("queue.rejectTitle", { ref: setRef(e.set_id, e.version) }),
        text: t("queue.rejectText"),
        confirmLabel: t("queue.reject"),
        reason: "required",
        destructive: true,
        onConfirm: (reason) => run(() => setAction.mutateAsync({ id: e.set_id, version: e.version, action: "reject", reason })),
      }),
    [run, setAction, t],
  );

  const hide = useCallback(
    (e: EntryView) =>
      setPrompt({
        title: t("queue.hideTitle", { ref: setRef(e.set_id, e.version) }),
        text: t("queue.hideText"),
        confirmLabel: t("queue.hide"),
        reason: "optional",
        destructive: true,
        onConfirm: (reason) => run(() => setAction.mutateAsync({ id: e.set_id, version: e.version, action: "hide", reason })),
      }),
    [run, setAction, t],
  );

  const ban = useCallback(
    (keyHmac: string, label: string) =>
      setPrompt({
        title: t("keys.banTitle", { key: label }),
        text: t("keys.banText"),
        confirmLabel: t("keys.ban"),
        reason: "optional",
        destructive: true,
        onConfirm: (reason) => run(() => keyAction.mutateAsync({ key: keyHmac, action: "ban", reason })),
      }),
    [keyAction, run, t],
  );

  const unban = useCallback(
    (keyHmac: string) => run(() => keyAction.mutateAsync({ key: keyHmac, action: "unban" })),
    [keyAction, run],
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
    (keyHmac: string) => run(() => keyAction.mutateAsync({ key: keyHmac, action: "untrust" })),
    [keyAction, run],
  );

  const approveMirror = useCallback(
    (m: MirrorView) => run(() => mirrorAction.mutateAsync({ id: m.id, action: "approve" })),
    [mirrorAction, run],
  );

  const rejectMirror = useCallback(
    (m: MirrorView) =>
      setPrompt({
        title: t("mirrors.rejectTitle"),
        text: `${m.url}. ${t("mirrors.rejectText")}`,
        confirmLabel: t("mirrors.reject"),
        reason: "required",
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

  const dialog = <ReasonDialog prompt={prompt} onClose={() => setPrompt(null)} />;
  const busy = setAction.isPending || keyAction.isPending || mirrorAction.isPending;

  return { approve, reject, hide, ban, unban, trust, untrust, approveMirror, rejectMirror, removeMirror, dialog, busy };
}

export type Moderation = ReturnType<typeof useModeration>;
