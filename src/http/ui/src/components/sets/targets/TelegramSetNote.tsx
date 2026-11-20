import { Link, Stack } from "@mui/material";
import { Trans, useTranslation } from "react-i18next";
import { TelegramIcon } from "@b4.icons";
import { B4Alert } from "@b4.elements";
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
    <Stack spacing={1}>
      {problem && (
        <B4Alert severity="warning">
          {telegramSetProblemText(t, problem)}
        </B4Alert>
      )}
      <B4Alert severity="info" icon={<TelegramIcon />}>
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
      </B4Alert>
      {!problem && caveats.length > 0 && (
        <B4Alert severity="warning">{caveats.join(" ")}</B4Alert>
      )}
    </Stack>
  );
};
