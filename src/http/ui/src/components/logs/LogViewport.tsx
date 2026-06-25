import { RefObject } from "react";
import { Box, IconButton, Typography } from "@mui/material";
import { ArrowDownIcon } from "@b4.icons";
import { colors, fonts, glows } from "@design";
import { useTranslation } from "react-i18next";
import { LogRow } from "./LogRow";
import { ParsedLogLine } from "./parse";

interface LogViewportProps {
  totalCount: number;
  filtered: ParsedLogLine[];
  showScrollBtn: boolean;
  scrollRef: RefObject<HTMLDivElement | null>;
  onScroll: () => void;
  onScrollToBottom: () => void;
}

export function LogViewport({
  totalCount,
  filtered,
  showScrollBtn,
  scrollRef,
  onScroll,
  onScrollToBottom,
}: LogViewportProps) {
  const { t } = useTranslation();

  return (
    <Box
      ref={scrollRef}
      onScroll={onScroll}
      sx={{
        flex: 1,
        overflowY: "auto",
        position: "relative",
        p: 2,
        fontFamily: fonts.mono,
        fontSize: 12.5,
        lineHeight: 1.7,
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
        backgroundColor: colors.background.dark,
        color: colors.text.primary,
      }}
    >
      {(() => {
        if (filtered.length === 0 && totalCount === 0) {
          return (
            <Typography
              sx={{
                color: colors.text.secondary,
                textAlign: "center",
                mt: 4,
                fontStyle: "italic",
              }}
            >
              {t("logs.waitingForLogs")}
            </Typography>
          );
        } else if (filtered.length === 0) {
          return (
            <Typography
              sx={{
                color: colors.text.secondary,
                textAlign: "center",
                mt: 4,
                fontStyle: "italic",
              }}
            >
              {t("logs.noMatch")}
            </Typography>
          );
        } else {
          return filtered.map((line, i) => (
            <LogRow key={line.raw + "_" + i} line={line} />
          ));
        }
      })()}

      {showScrollBtn && (
        <IconButton
          onClick={onScrollToBottom}
          sx={{
            position: "absolute",
            bottom: 16,
            right: 16,
            bgcolor: colors.primary,
            color: colors.text.primary,
            boxShadow: glows.primary,
            "&:hover": { bgcolor: colors.tertiary },
          }}
          size="small"
        >
          <ArrowDownIcon />
        </IconButton>
      )}
    </Box>
  );
}
