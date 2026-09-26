import { useState, useCallback } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@context/SnackbarProvider";
import { apiPut } from "@api/apiClient";
import { useAsnCache } from "@hooks/useAsn";
import { formatAsn } from "@models/asn";
import { stripPort } from "@utils";

export type IpTarget =
  | { kind: "cidr"; value: string; label: string }
  | { kind: "asn"; id: string };

export interface IpTargetRequest {
  target: IpTarget;
  setId: string;
  newSetName?: string;
  setLabel: string;
}

export interface AddIpTargetsResponse {
  success: boolean;
  message: string;
  set_id: string;
  total_cidrs: number;
  total_asns: number;
  added_cidrs: number;
  added_asns: number;
  unresolved_asns?: string[];
}

export interface IpDialogState {
  open: boolean;
  session: number;
  ip: string;
  asn?: string;
  asnName?: string;
}

export const ipTargetLabel = (target: IpTarget): string =>
  target.kind === "asn" ? formatAsn(target.id) : target.label;

export function useIpActions() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { showSuccess, showSnackbar } = useSnackbar();
  const { invalidate: invalidateAsnViews } = useAsnCache();
  const [dialog, setDialog] = useState<IpDialogState>({
    open: false,
    session: 0,
    ip: "",
  });

  const openDialog = useCallback(
    (ip: string, asn?: string, asnName?: string) => {
      setDialog((prev) => ({
        open: true,
        session: prev.session + 1,
        ip: stripPort(ip),
        asn,
        asnName,
      }));
    },
    [],
  );

  const closeDialog = useCallback(() => {
    setDialog((prev) => ({ ...prev, open: false }));
  }, []);

  const addTarget = useCallback(
    async ({ target, setId, newSetName, setLabel }: IpTargetRequest) => {
      const res = await apiPut<AddIpTargetsResponse>("/api/geoip", {
        ...(target.kind === "asn"
          ? { asns: [target.id] }
          : { cidr: [target.value] }),
        set_id: setId,
        set_name: newSetName?.trim() || undefined,
      });

      const params = { target: ipTargetLabel(target), set: setLabel };
      const openSet = res.set_id
        ? {
            label: t("connections.addIp.openSet"),
            onClick: () => {
              navigate(`/sets/${res.set_id}`)?.catch(() => {});
            },
          }
        : undefined;

      if (res.added_cidrs + res.added_asns === 0) {
        showSnackbar(t("connections.addIp.alreadyInSet", params), "info", openSet);
      } else if (target.kind === "asn" && res.unresolved_asns?.length) {
        showSuccess(t("connections.addIp.addedPending", params), openSet);
      } else {
        showSuccess(t("connections.addIp.added", params), openSet);
      }
      if (target.kind === "asn") void invalidateAsnViews();
      closeDialog();
      return res;
    },
    [closeDialog, invalidateAsnViews, navigate, showSnackbar, showSuccess, t],
  );

  return {
    dialog,
    openDialog,
    closeDialog,
    addTarget,
  };
}
