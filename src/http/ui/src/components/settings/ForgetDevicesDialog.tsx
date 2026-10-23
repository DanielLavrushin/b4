import { useState } from "react";
import { Box, Button, Checkbox, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Alert, B4Dialog } from "@b4.elements";
import { DeleteIcon } from "@b4.icons";
import { B4Config, B4SetConfig } from "@models/config";

interface ForgetCandidate {
  mac: string;
  label: string;
}

interface ForgetDevicesDialogProps {
  open: boolean;
  candidates: ForgetCandidate[];
  config: B4Config;
  onClose: () => void;
  onForget: (macs: string[]) => void;
}

const macKey = (mac: string) => mac.trim().toUpperCase();

const filterLosesEveryDevice = (config: B4Config, drop: Set<string>) => {
  const { enabled, wisb, devices } = config.queue.devices;
  if (!enabled || wisb) return false;
  const selected = devices.filter(
    (d) => d.selected && !d.is_manual && d.mac.trim() !== "",
  );
  return selected.length > 0 && selected.every((d) => drop.has(macKey(d.mac)));
};

const setsLeftWithoutDevices = (config: B4Config, drop: Set<string>) =>
  config.sets.filter((s) => {
    const macs = s.targets.source_devices ?? [];
    return (
      !s.targets.source_devices_exclude &&
      macs.length > 0 &&
      macs.every((m) => drop.has(macKey(m)))
    );
  });

const clampsWithoutAddressTargets = (s: B4SetConfig) =>
  !!s.mss_clamp?.enabled &&
  (s.targets.ip ?? []).length === 0 &&
  (s.targets.geoip_categories ?? []).length === 0 &&
  (s.targets.asns ?? []).length === 0;

export const ForgetDevicesDialog = ({
  open,
  candidates,
  config,
  onClose,
  onForget,
}: ForgetDevicesDialogProps) => {
  const { t } = useTranslation();
  const [checked, setChecked] = useState(
    () => new Set(candidates.map((c) => macKey(c.mac))),
  );
  const several = candidates.length > 1;
  const picked = candidates.filter((c) => checked.has(macKey(c.mac)));
  const emptiedSets = setsLeftWithoutDevices(config, checked);
  const blockedSets = emptiedSets.filter(clampsWithoutAddressTargets);
  const widenedSets = emptiedSets.filter(
    (s) => !clampsWithoutAddressTargets(s),
  );

  const toggle = (key: string) =>
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const details = (mac: string) => {
    const key = macKey(mac);
    const entry = config.queue.devices.devices.find(
      (d) => macKey(d.mac) === key,
    );
    const parts = [mac];
    if (entry?.selected) {
      parts.push(
        t(
          config.queue.devices.wisb
            ? "settings.Devices.forgetDialog.excluded"
            : "settings.Devices.forgetDialog.selected",
        ),
      );
    }
    if (entry?.mss_clamp) parts.push(`MSS ${entry.mss_clamp}`);
    const sets = config.sets
      .filter((s) =>
        (s.targets.source_devices ?? []).some((m) => macKey(m) === key),
      )
      .map((s) => s.name);
    if (sets.length > 0) {
      parts.push(
        t("settings.Devices.forgetDialog.inSets", { names: sets.join(", ") }),
      );
    }
    return parts.join(" · ");
  };

  return (
    <B4Dialog
      open={open}
      title={t(
        several
          ? "settings.Devices.forgetDialog.title"
          : "settings.Devices.forget",
      )}
      subtitle={
        several ? t("settings.Devices.forgetDialog.subtitle") : undefined
      }
      icon={<DeleteIcon />}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            variant="contained"
            disabled={picked.length === 0 || blockedSets.length > 0}
            onClick={() => onForget(picked.map((c) => c.mac))}
          >
            {t("settings.Devices.forgetDialog.confirm", {
              count: picked.length,
            })}
          </Button>
        </>
      }
    >
      <Typography variant="body2" sx={{ mb: 1.5 }}>
        {t("settings.Devices.forgetDialog.intro")}
      </Typography>

      <Stack spacing={1} sx={{ maxHeight: 280, overflow: "auto" }}>
        {candidates.map((c) => {
          const key = macKey(c.mac);
          return (
            <Stack
              key={key}
              direction="row"
              alignItems="center"
              spacing={1}
              onClick={several ? () => toggle(key) : undefined}
              sx={{ cursor: several ? "pointer" : "default" }}
            >
              {several && (
                <Checkbox
                  size="small"
                  color="secondary"
                  checked={checked.has(key)}
                  onClick={(e) => e.stopPropagation()}
                  onChange={() => toggle(key)}
                  slotProps={{ input: { "aria-label": c.label || c.mac } }}
                />
              )}
              <Box sx={{ minWidth: 0 }}>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>
                  {c.label || t("core.unknown")}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ overflowWrap: "anywhere" }}
                >
                  {details(c.mac)}
                </Typography>
              </Box>
            </Stack>
          );
        })}
      </Stack>

      {blockedSets.map((s) => (
        <B4Alert key={s.id} noWrapper severity="error" sx={{ mt: 2 }}>
          {t("settings.Devices.forgetDialog.mssBlocked", { name: s.name })}
        </B4Alert>
      ))}
      {filterLosesEveryDevice(config, checked) && (
        <B4Alert noWrapper severity="warning" sx={{ mt: 2 }}>
          {t("settings.Devices.forgetDialog.filterWarning")}
        </B4Alert>
      )}
      {widenedSets.map((s) => (
        <B4Alert key={s.id} noWrapper severity="warning" sx={{ mt: 2 }}>
          {t("settings.Devices.forgetDialog.setWarning", { name: s.name })}
        </B4Alert>
      ))}
    </B4Dialog>
  );
};
