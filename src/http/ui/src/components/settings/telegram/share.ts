import type { TFunction } from "i18next";
import type { AlertColor } from "@mui/material";
import { SystemAddresses } from "@models/settings";

export interface ShareTarget {
  name: string;
  secret: string;
}

export type ShareScope = "local" | "internet";

export interface HostCandidate {
  ip: string;
  iface?: string;
}

export interface ShareNote {
  id: string;
  severity: AlertColor;
  text: string;
}

interface ShareNoteInput {
  addrs?: SystemAddresses;
  addrError: string | null;
  publicError: string | null;
  port: number;
  bindAddress: string;
  hostIsV6: boolean;
  proxyRunning: boolean;
}

export const webSecretForm = (secret: string) =>
  "dd" + secret.trim().slice(2, 34).toLowerCase();

export const isAnyAddress = (addr: string) =>
  !addr || addr === "0.0.0.0" || addr === "::";

export const internetCandidates = (
  addrs: SystemAddresses | undefined,
  publicV4: string,
): HostCandidate[] => {
  const list: HostCandidate[] = [];
  const add = (ip: string | undefined, iface?: string) => {
    if (ip && !list.some((c) => c.ip === ip)) list.push({ ip, iface });
  };
  const wan4 = addrs?.wan_v4;
  const wan6 = addrs?.wan_v6;
  if (wan4?.scope === "public") add(wan4.ip, wan4.iface);
  add(publicV4);
  if (wan6?.scope === "public") add(wan6.ip, wan6.iface);
  if (wan4 && wan4.scope !== "public") add(wan4.ip, wan4.iface);
  return list;
};

export const shareNotes = (
  t: TFunction,
  input: ShareNoteInput,
): ShareNote[] => {
  const {
    addrs,
    addrError,
    publicError,
    port,
    bindAddress,
    hostIsV6,
    proxyRunning,
  } = input;
  const notes: ShareNote[] = [];
  const add = (id: string, severity: AlertColor, text: string) =>
    notes.push({ id, severity, text });

  if (addrError !== null) {
    add(
      "addresses",
      "warning",
      t("settings.MTProto.shareAddressesFailed", { error: addrError }),
    );
  }
  if (!addrs) return notes;

  const { exposure, wan_v4: wan4, wan_v6: wan6 } = addrs;
  const block = (exposure.blocked ?? []).find((b) => b.service === "mtproto");
  const exposed = (exposure.ports ?? []).some(
    (p) => p.service === "mtproto" && p.port === port,
  );
  const blockExplained = !exposure.skip_setup && block !== undefined;

  if (!proxyRunning) {
    add("stopped", "info", t("settings.MTProto.shareNotSaved"));
  } else if (exposure.skip_setup) {
    add("skip", "warning", t("settings.MTProto.shareSkipSetup", { port }));
  } else if (block?.reason === "loopback") {
    add("blocked", "error", t("settings.MTProto.shareBlockedLoopback"));
  } else if (block?.reason === "not_listening") {
    add(
      "blocked",
      "error",
      t("settings.MTProto.shareBlockedNotListening", { port }),
    );
  } else if (block) {
    add(
      "blocked",
      "error",
      t("settings.MTProto.shareBlockedBind", { bind: bindAddress }),
    );
  } else if (!exposed) {
    add("closed", "warning", t("settings.MTProto.shareNotExposed", { port }));
  } else if (exposure.error) {
    add(
      "rule",
      "warning",
      t("settings.MTProto.shareExposeError", { error: exposure.error }),
    );
  }

  if (
    !blockExplained &&
    !isAnyAddress(bindAddress) &&
    bindAddress !== wan4?.ip &&
    bindAddress !== wan6?.ip
  ) {
    add(
      "bind",
      "warning",
      t("settings.MTProto.shareBindNotWan", { bind: bindAddress }),
    );
  }

  if (!wan4 && !wan6) {
    add("nowan", "warning", t("settings.MTProto.shareNoWan"));
  }
  if (wan4?.scope === "cgnat") {
    add(
      "cgnat",
      "warning",
      t("settings.MTProto.shareWanCgnat", { ip: wan4.ip }),
    );
  } else if (wan4 && !hostIsV6) {
    if (wan4.scope === "private") {
      add(
        "private",
        "warning",
        t("settings.MTProto.shareWanPrivate", { ip: wan4.ip, port }),
      );
    } else if (wan4.scope === "other") {
      add(
        "other",
        "warning",
        t("settings.MTProto.shareWanOther", { ip: wan4.ip }),
      );
    }
  }
  if (publicError !== null) {
    add(
      "public",
      "warning",
      t("settings.MTProto.sharePublicFailed", { error: publicError }),
    );
  }
  if (hostIsV6) {
    add("v6", "info", t("settings.MTProto.shareIPv6Note"));
  }
  return notes;
};
