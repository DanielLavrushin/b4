import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogProps,
  Stack,
  Box,
  Typography,
  Divider,
} from "@mui/material";
import { colors, radius, radiusPx } from "@design";

interface B4DialogProps extends Omit<DialogProps, "title"> {
  title: string;
  subtitle?: string;
  icon?: React.ReactNode;
  headerAlert?: React.ReactNode;
  actions?: React.ReactNode;
  onClose: () => void;
}

export const B4Dialog = ({
  title,
  subtitle,
  icon,
  headerAlert,
  children,
  actions,
  onClose,
  ...props
}: B4DialogProps) => (
  <Dialog
    onClose={onClose}
    slotProps={{
      paper: {
        sx: {
          m: { xs: 1.5, sm: 4 },
          width: { xs: "calc(100% - 24px)", sm: "auto" },
          maxWidth: { xs: "calc(100% - 24px)", sm: undefined },
          maxHeight: { xs: "calc(100% - 24px)", sm: "calc(100% - 64px)" },
          bgcolor: colors.background.default,
          border: `2px solid ${colors.border.default}`,
          borderRadius: radius.md,
          boxShadow: `0 24px 80px rgba(0,0,0,0.55), 0 0 0 1px rgba(245,173,24,0.04)`,
        },
      },
    }}
    {...props}
  >
    <DialogTitle
      sx={{
        bgcolor: colors.background.dark,
        color: colors.text.primary,
        borderBottom: `1px solid ${colors.border.default}`,
        p: "14px 18px",
      }}
    >
      <Stack direction="row" alignItems="center" gap="14px">
        {icon && (
          <Box
            sx={{
              width: 38,
              height: 38,
              borderRadius: `${radiusPx.sm}px`,
              bgcolor: colors.accent.secondary,
              color: colors.secondary,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              flexShrink: 0,
            }}
          >
            {icon}
          </Box>
        )}
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Typography
            component="div"
            sx={{
              fontSize: 15,
              fontWeight: 600,
              lineHeight: 1.25,
              letterSpacing: "0.02em",
              textTransform: "uppercase",
              color: colors.text.primary,
            }}
          >
            {title}
          </Typography>
          {subtitle && (
            <Typography
              component="div"
              sx={{
                fontSize: 11,
                lineHeight: 1.3,
                color: colors.text.secondary,
                mt: "2px",
              }}
            >
              {subtitle}
            </Typography>
          )}
        </Box>
      </Stack>
    </DialogTitle>

    <DialogContent
      sx={{
        bgcolor: colors.background.default,
        display: "flex",
        flexDirection: "column",
        mt: 2,
      }}
    >
      {headerAlert}
      {children}
    </DialogContent>

    {actions && (
      <>
        <Divider sx={{ borderColor: colors.border.default }} />
        <DialogActions
          sx={{
            p: "12px 14px",
            bgcolor: colors.background.paper,
            gap: 1,
          }}
        >
          {actions}
        </DialogActions>
      </>
    )}
  </Dialog>
);
