import { useState } from "react";
import { Button, Stack } from "@mui/material";
import { useTranslation } from "react-i18next";
import { fonts } from "@design";
import { ClearIcon, SetsIcon } from "@b4.icons";
import { B4Hint, B4Section, B4TextField } from "@b4.elements";
import { B4Config, SetTemplate } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { formatSetTemplate } from "@models/setTemplate";

interface NewSetTemplateSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const FIELD = "ui.new_set_template";

const isTemplate = (v: unknown): v is SetTemplate =>
  typeof v === "object" && v !== null && !Array.isArray(v);

export const NewSetTemplateSettings = ({
  config,
  onChange,
}: NewSetTemplateSettingsProps) => {
  const { t } = useTranslation();
  const template = config.ui?.new_set_template ?? null;
  const templateKey = JSON.stringify(template);
  const [text, setText] = useState(() => formatSetTemplate(template));
  const [syncedKey, setSyncedKey] = useState(templateKey);
  const [invalid, setInvalid] = useState(false);

  if (templateKey !== syncedKey) {
    setSyncedKey(templateKey);
    setText(formatSetTemplate(template));
    setInvalid(false);
  }

  const apply = (next: SetTemplate | null) => {
    setInvalid(false);
    setSyncedKey(JSON.stringify(next));
    onChange(FIELD, next);
  };

  const handleText = (value: string) => {
    setText(value);
    if (value.trim() === "") {
      apply(null);
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      setInvalid(true);
      return;
    }
    if (!isTemplate(parsed)) {
      setInvalid(true);
      return;
    }
    apply(parsed);
  };

  return (
    <B4Section title={t("settings.NewSetTemplate.title")} icon={<SetsIcon />}>
      <B4Hint>{t("settings.NewSetTemplate.info")}</B4Hint>
      <Stack spacing={1.5}>
        <B4TextField
          label={t("settings.NewSetTemplate.label")}
          value={text}
          path={FIELD}
          onChange={(e) => handleText(e.target.value)}
          placeholder={t("settings.NewSetTemplate.placeholder")}
          error={invalid}
          helperText={
            invalid
              ? t("settings.NewSetTemplate.invalid")
              : t("settings.NewSetTemplate.help")
          }
          aiTopic={FIELD}
          multiline
          minRows={6}
          slotProps={{ htmlInput: { spellCheck: false } }}
          sx={{ "& textarea": { fontFamily: fonts.mono, fontSize: "0.8rem" } }}
        />
        <Stack direction="row" justifyContent="flex-end">
          <Button
            startIcon={<ClearIcon />}
            onClick={() => handleText("")}
            disabled={text === ""}
          >
            {t("settings.NewSetTemplate.clear")}
          </Button>
        </Stack>
      </Stack>
    </B4Section>
  );
};
