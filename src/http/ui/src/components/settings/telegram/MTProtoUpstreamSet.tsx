import { useTranslation } from "react-i18next";
import { Box, Button, Stack } from "@mui/material";
import { AddIcon } from "@b4.icons";
import { B4Alert, B4Select } from "@b4.elements";
import { B4Config, B4SetConfig } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { SettingsPropHandlerType } from "@models/settings";

type TelegramSetProblem =
  | { kind: "missing" }
  | {
      kind: "disabled" | "unusable" | "incomplete" | "tun" | "mark";
      name: string;
    };

const TELEGRAM_SET_MARK = 0x4040000;
const DEFAULT_QUEUE_MARK = 0x8000;

const carriesTelegramSetMark = (mark: number) =>
  (mark & TELEGRAM_SET_MARK) === TELEGRAM_SET_MARK;

const telegramSetMarkConflict = (config: B4Config) => {
  const queue = config.queue.mark ?? 0;
  if (queue !== 0 && (queue & ~TELEGRAM_SET_MARK) === 0) return true;
  const main = queue || DEFAULT_QUEUE_MARK;
  const checker = config.system.checker;
  return (
    carriesTelegramSetMark(checker?.discovery_flow_mark || main + 1) ||
    carriesTelegramSetMark(checker?.discovery_injected_mark || main + 2)
  );
};

const routingIncomplete = (set: B4SetConfig) => {
  if (!set.routing?.enabled) return false;
  const mode = set.routing.mode || "interface";
  if (mode === "interface") return !set.routing.egress_interface;
  if (mode === "proxy") return !(set.routing.upstream?.port > 0);
  return false;
};

const carriesTelegram = (set: B4SetConfig) =>
  !set.routing?.enabled ||
  (set.routing.mode !== "mtproto-ws" && set.routing.mode !== "block");

export const telegramSetProblem = (
  config: B4Config,
): TelegramSetProblem | null => {
  const id = config.system.mtproto?.upstream_set ?? "";
  if (id === "") return null;
  const set = config.sets.find((s) => s.id === id);
  if (!set) return { kind: "missing" };
  if (!set.enabled) return { kind: "disabled", name: set.name };
  if (!carriesTelegram(set)) return { kind: "unusable", name: set.name };
  if (routingIncomplete(set)) return { kind: "incomplete", name: set.name };
  if (config.queue.mode === "tun") return { kind: "tun", name: set.name };
  if (telegramSetMarkConflict(config)) return { kind: "mark", name: set.name };
  return null;
};

export const telegramSetUsable = (config: B4Config) =>
  (config.system.mtproto?.upstream_set ?? "") !== "" &&
  telegramSetProblem(config) === null;

export const telegramSetRoutes = (config: B4Config) =>
  telegramSetUsable(config) &&
  config.sets.some(
    (s) =>
      s.id === config.system.mtproto?.upstream_set && !!s.routing?.enabled,
  );

interface MTProtoUpstreamSetProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const MTProtoUpstreamSet = ({
  config,
  onChange,
}: MTProtoUpstreamSetProps) => {
  const { t } = useTranslation();
  const chosen = config.system.mtproto?.upstream_set ?? "";
  const problem = telegramSetProblem(config);

  const options = [
    {
      value: "",
      label: t("settings.MTProto.upstreamSetNone"),
      note: t("settings.MTProto.upstreamSetNoneNote"),
    },
    ...config.sets
      .filter((set) => set.id === chosen || carriesTelegram(set))
      .map((set) => ({
        value: set.id,
        label: set.name,
        note: set.enabled ? undefined : t("settings.MTProto.upstreamSetOff"),
        disabled: set.id !== chosen && (!set.enabled || !carriesTelegram(set)),
      })),
  ];

  const createTelegramSet = () => {
    const set = createDefaultSet(config.sets.length);
    const base = t("settings.MTProto.upstreamSetNewName");
    const taken = new Set(config.sets.map((s) => s.name));
    let name = base;
    for (let n = 2; taken.has(name); n++) name = `${base} ${n}`;
    set.name = name;
    onChange("sets", [...config.sets, set]);
    onChange("system.mtproto.upstream_set", set.id);
  };

  let problemText = "";
  switch (problem?.kind) {
    case "missing":
      problemText = t("settings.MTProto.upstreamSetMissing");
      break;
    case "disabled":
      problemText = t("settings.MTProto.upstreamSetDisabled", {
        name: problem.name,
      });
      break;
    case "unusable":
      problemText = t("settings.MTProto.upstreamSetUnusable", {
        name: problem.name,
      });
      break;
    case "incomplete":
      problemText = t("settings.MTProto.upstreamSetIncomplete", {
        name: problem.name,
      });
      break;
    case "tun":
      problemText = t("settings.MTProto.upstreamSetTun", {
        name: problem.name,
      });
      break;
    case "mark":
      problemText = t("settings.MTProto.upstreamSetMark", {
        name: problem.name,
      });
      break;
  }

  return (
    <Stack spacing={1}>
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "flex-start",
          gap: 1,
        }}
      >
        <Box sx={{ flex: "1 1 260px", minInlineSize: 0 }}>
          <B4Select
            value={chosen}
            path="system.mtproto.upstream_set"
            aiTopic="system.mtproto.upstream_set"
            onChange={(e) =>
              onChange("system.mtproto.upstream_set", String(e.target.value))
            }
            options={options}
            helperText={t("settings.MTProto.upstreamSetHelp")}
          />
        </Box>
        {chosen === "" && (
          <Button
            variant="outlined"
            startIcon={<AddIcon />}
            onClick={createTelegramSet}
            sx={{ blockSize: 40, flex: "none", whiteSpace: "nowrap" }}
          >
            {t("settings.MTProto.upstreamSetCreate")}
          </Button>
        )}
      </Box>
      {problemText && <B4Alert severity="warning">{problemText}</B4Alert>}
    </Stack>
  );
};
