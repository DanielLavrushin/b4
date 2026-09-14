import { Box, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useSets } from "@/api/hub";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { QueueCard } from "@/components/sets/QueueCard";
import { useModeration } from "@/components/sets/useModeration";

export function QueuePage() {
  const { t } = useTranslation();
  const sets = useSets();
  const moderation = useModeration();

  if (sets.isLoading) return <Loading />;
  if (sets.error) return <ErrorState error={sets.error} />;
  const pending = sets.data?.pending ?? [];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Typography variant="sectionHeader">{t("queue.title", { count: pending.length })}</Typography>
      {pending.length === 0 ? (
        <EmptyState text={t("queue.empty")} />
      ) : (
        pending.map((entry) => <QueueCard key={`${entry.set_id}/${String(entry.version)}`} entry={entry} moderation={moderation} />)
      )}
      {moderation.dialog}
    </Box>
  );
}
