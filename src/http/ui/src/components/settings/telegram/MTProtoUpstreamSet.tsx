import { useTranslation } from "react-i18next";
import { Box, Button, Stack } from "@mui/material";
import { AddIcon } from "@b4.icons";
import { B4FieldMessage, B4Select } from "@b4.elements";
import { B4Config } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { SettingsPropHandlerType } from "@models/settings";
import {
  carriesTelegram,
  telegramSetProblem,
  telegramSetProblemText,
} from "@models/telegramSet";

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

  const problemText = telegramSetProblemText(t, problem);

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
            helperText={t(
              chosen === ""
                ? "settings.MTProto.upstreamSetNoneHelp"
                : "settings.MTProto.upstreamSetHelp",
            )}
            message={
              problemText ? (
                <B4FieldMessage>{problemText}</B4FieldMessage>
              ) : undefined
            }
            info={t("settings.MTProto.upstreamSetInfo")}
            learnMore="telegram/upstream"
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
    </Stack>
  );
};
