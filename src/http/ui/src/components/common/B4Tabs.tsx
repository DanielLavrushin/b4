import {
  Tabs,
  TabsProps,
  Tab,
  TabProps,
  Stack,
  Box,
  Fade,
  useTheme,
} from "@mui/material";
import { useEffect, useRef } from "react";
import { RestartIcon } from "@b4.icons";
import { colors } from "@design";
import type { ReactNode } from "react";
import type { SxProps, Theme } from "@mui/material/styles";

const SCROLL_SETTLE_MARGIN_MS = 100;

const revealSelectedTab = (scroller: HTMLElement) => {
  const tab = scroller.querySelector<HTMLElement>(
    '[role="tab"][aria-selected="true"]',
  );
  if (!tab) return;
  const start = tab.offsetLeft;
  const end = start + tab.offsetWidth;
  if (start < scroller.scrollLeft) {
    scroller.scrollLeft = start;
  } else if (end > scroller.scrollLeft + scroller.clientWidth) {
    scroller.scrollLeft = end - scroller.clientWidth;
  }
};

const USER_SCROLL_EVENTS = ["wheel", "pointerdown", "keydown"] as const;

export const B4Tabs = ({ sx, ...props }: TabsProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const userScrolled = useRef(false);
  const settleMs =
    useTheme().transitions.duration.standard + SCROLL_SETTLE_MARGIN_MS;

  useEffect(() => {
    userScrolled.current = false;
  }, [props.value]);

  useEffect(() => {
    const root = rootRef.current;
    const scroller = root?.querySelector<HTMLElement>(".MuiTabs-scroller");
    if (!root || !scroller || typeof ResizeObserver === "undefined") return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const markUserScroll = () => {
      userScrolled.current = true;
      clearTimeout(timer);
    };
    const observer = new ResizeObserver(() => {
      clearTimeout(timer);
      if (userScrolled.current) return;
      timer = setTimeout(() => {
        if (!userScrolled.current) revealSelectedTab(scroller);
      }, settleMs);
    });
    observer.observe(scroller);
    USER_SCROLL_EVENTS.forEach((type) =>
      root.addEventListener(type, markUserScroll, { passive: true }),
    );
    return () => {
      observer.disconnect();
      clearTimeout(timer);
      USER_SCROLL_EVENTS.forEach((type) =>
        root.removeEventListener(type, markUserScroll),
      );
    };
  }, [settleMs]);

  return (
    <Tabs
      ref={rootRef}
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
};

interface B4TabProps extends Omit<TabProps, "label" | "icon"> {
  icon?: React.ReactElement;
  label: string;
  inline?: boolean;
  hasChanges?: boolean;
  needsRestart?: boolean;
  needsRestartLabel?: string;
  index?: number;
  idPrefix?: string;
}

export const B4Tab = ({
  icon,
  label,
  inline,
  hasChanges,
  needsRestart,
  needsRestartLabel,
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
  const marker = needsRestart ? (
    <RestartIcon
      titleAccess={needsRestartLabel}
      sx={{ fontSize: 14, color: colors.state.warning }}
    />
  ) : (
    <Box
      sx={{
        width: 6,
        height: 6,
        borderRadius: "50%",
        bgcolor: colors.secondary,
      }}
    />
  );
  return (
    <Tab
      icon={icon}
      iconPosition={inline ? "start" : undefined}
      label={
        hasChanges ? (
          <Stack direction="row" spacing={1} alignItems="center">
            <span>{label}</span>
            {marker}
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
