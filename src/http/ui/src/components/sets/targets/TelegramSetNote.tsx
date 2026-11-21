import { Box, Link, Stack } from "@mui/material";
import { Trans, useTranslation } from "react-i18next";
import { B4FieldMessage, B4Help } from "@b4.elements";
import { B4SetConfig } from "@models/config";
import {
  TelegramSetProblem,
  telegramSetProblemText,
} from "@models/telegramSet";
import { hasTargets } from "../facets";

export const TelegramSetNote = ({
  set,
  problem,
}: {
  set: B4SetConfig;
  problem: TelegramSetProblem | null;
}) => {
  const { t } = useTranslation();
  const strategyOnly = !set.routing?.enabled;
  const ports = set.tcp?.dport_filter?.trim() ?? "";
  const caveats = [
    strategyOnly && ports !== "" && t("sets.editor.telegramSetPortFilter"),
    strategyOnly &&
      ports !== "" &&
      !hasTargets(set) &&
      t("sets.editor.telegramSetPortFilterNoTargets"),
  ].filter(Boolean);

  return (
    <Stack spacing={0.5}>
      {problem && (
        <Box>
          <B4FieldMessage>{telegramSetProblemText(t, problem)}</B4FieldMessage>
        </Box>
      )}
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <B4FieldMessage severity="info">
            <Trans
              i18nKey={
                problem
                  ? "sets.editor.telegramSetNoteInactive"
                  : "sets.editor.telegramSetNote"
              }
              components={{
                a: (
                  <Link
                    href={t("sets.editor.telegramSetDocsUrl")}
                    target="_blank"
                    rel="noopener noreferrer"
                  />
                ),
              }}
            />
          </B4FieldMessage>
        </Box>
        {!problem && (
          <B4Help
            label={t("sets.card.telegramSet")}
            info={t("sets.editor.telegramSetNoteInfo")}
            mt="6px"
          />
        )}
      </Box>
      {!problem && caveats.length > 0 && (
        <Box>
          <B4FieldMessage>{caveats.join(" ")}</B4FieldMessage>
        </Box>
      )}
    </Stack>
  );
};
