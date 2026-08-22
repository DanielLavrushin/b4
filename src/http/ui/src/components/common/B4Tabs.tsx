import { Tabs, TabsProps, Tab, TabProps, Stack, Box, Fade } from "@mui/material";
import { colors } from "@design";
import type { ReactNode } from "react";
import type { SxProps, Theme } from "@mui/material/styles";

export const B4Tabs = ({ sx, ...props }: TabsProps) => (
  <Tabs
    variant="scrollable"
    scrollButtons="auto"
    allowScrollButtonsMobile
    sx={{
      borderBottom: `1px solid ${colors.border.light}`,
      minHeight: 38,
      "& .MuiTabs-flexContainer": {
        gap: "4px",
      },
      "& .MuiTab-root": {
        color: colors.text.secondary,
        textTransform: "none",
        fontSize: 13,
        minHeight: 38,
        padding: "10px 12px",
        "&.Mui-selected": {
          color: colors.secondary,
        },
      },
      "& .MuiTabs-indicator": {
        bgcolor: colors.secondary,
        height: 2,
      },
      ...sx,
    }}
    {...props}
  />
);

interface B4TabProps extends Omit<TabProps, "label" | "icon"> {
  icon?: React.ReactElement;
  label: string;
  inline?: boolean;
  hasChanges?: boolean;
  index?: number;
  idPrefix?: string;
}

export const B4Tab = ({
  icon,
  label,
  inline,
  hasChanges,
  index,
  idPrefix = "b4-tab",
  ...props
}: B4TabProps) => {
  const ariaProps =
    index === undefined
      ? {}
      : {
          id: `${idPrefix}-${index}`,
          "aria-controls": `${idPrefix}panel-${index}`,
        };
  return (
    <Tab
      icon={icon}
      iconPosition={inline ? "start" : undefined}
      label={
        hasChanges ? (
          <Stack direction="row" spacing={1} alignItems="center">
            <span>{label}</span>
            <Box
              sx={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                bgcolor: colors.secondary,
              }}
            />
          </Stack>
        ) : (
          label
        )
      }
      {...ariaProps}
      {...props}
    />
  );
};

export interface B4TabPanelProps {
  children?: ReactNode;
  index: number;
  value: number;
  idPrefix?: string;
  sx?: SxProps<Theme>;
}

export const B4TabPanel = ({
  children,
  value,
  index,
  idPrefix = "b4-tab",
  sx,
}: Readonly<B4TabPanelProps>) => (
  <div
    role="tabpanel"
    hidden={value !== index}
    id={`${idPrefix}panel-${index}`}
    aria-labelledby={`${idPrefix}-${index}`}
  >
    {value === index && (
      <Fade in>
        <Box sx={sx}>{children}</Box>
      </Fade>
    )}
  </div>
);
