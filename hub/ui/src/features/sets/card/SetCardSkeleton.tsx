import { Box, Card, Skeleton } from "@mui/material";
import { colors, radius, spacing, typography } from "@design";

const separated = { borderTop: `1px solid ${colors.border.light}` } as const;

export function SetCardSkeleton() {
  return (
    <Card
      elevation={0}
      aria-hidden
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        border: `1px solid ${colors.border.default}`,
        borderRadius: radius.md,
        bgcolor: colors.background.paper,
      }}
    >
      <Box sx={{ height: 16 }} />
      <Box sx={{ display: "flex", justifyContent: "flex-end", alignItems: "center", px: spacing.md, minHeight: 34 }}>
        <Skeleton variant="rounded" width={24} height={16} sx={{ borderRadius: "999px" }} />
      </Box>
      <Box sx={{ px: spacing.md, pb: spacing.sm, flexGrow: 1 }}>
        <Skeleton variant="text" width="62%" sx={{ fontSize: typography.sizes.xl }} />
        <Skeleton variant="text" width="88%" sx={{ fontSize: typography.sizes.sm }} />
        <Skeleton variant="text" width="54%" sx={{ fontSize: typography.sizes.sm }} />
        <Box sx={{ ...separated, mt: spacing.sm, pt: spacing.sm }}>
          <Skeleton variant="text" width="38%" sx={{ fontSize: typography.sizes.sm }} />
        </Box>
      </Box>
      <Box sx={{ ...separated, display: "flex", alignItems: "center", gap: spacing.xs, px: spacing.md, py: spacing.sm }}>
        <Skeleton variant="rounded" width={76} height={24} sx={{ borderRadius: "999px" }} />
        <Box sx={{ flex: 1 }} />
        <Skeleton variant="text" width={72} />
      </Box>
    </Card>
  );
}
