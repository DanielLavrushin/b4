import { Box } from "@mui/material";
import { colors } from "@design";

interface DataRowProps {
  leading?: React.ReactNode;
  right?: React.ReactNode;
  onClick?: () => void;
  indent?: boolean;
  hover?: boolean;
  children: React.ReactNode;
}

export const DataRow = ({
  leading,
  right,
  onClick,
  indent = false,
  hover = true,
  children,
}: DataRowProps) => (
  <Box
    role={onClick ? "button" : undefined}
    tabIndex={onClick ? 0 : undefined}
    onClick={onClick}
    onKeyDown={
      onClick
        ? (e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onClick();
            }
          }
        : undefined
    }
    sx={{
      display: "flex",
      alignItems: "center",
      gap: "10px",
      p: indent ? "8px 14px 8px 28px" : "8px 14px",
      borderBottom: `1px solid ${colors.border.light}`,
      "&:last-of-type": { borderBottom: 0 },
      cursor: onClick ? "pointer" : undefined,
      transition: "background-color 120ms ease",
      ...(hover ? { "&:hover": { bgcolor: colors.background.hover } } : null),
    }}
  >
    {leading}
    {children}
    {right}
  </Box>
);
