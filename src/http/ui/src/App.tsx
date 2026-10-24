import {
  AppBar,
  Button,
  CircularProgress,
  Box,
  CssBaseline,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  ThemeProvider,
  Toolbar,
  Typography,
  useMediaQuery,
} from "@mui/material";
import { Suspense, lazy, useState } from "react";
import {
  Navigate,
  Outlet,
  useLocation,
  useNavigate,
  useRouteError,
} from "react-router";
import type { RouteObject } from "react-router";
import { useTranslation } from "react-i18next";

import {
  CommunityIcon,
  ConnectionIcon,
  CoreIcon,
  DashboardIcon,
  DiscoveryIcon,
  LogsIcon,
  LogoutIcon,
  MenuIcon,
  SecurityIcon,
  SetsIcon,
  WatchdogIcon,
} from "@b4.icons";
import { colors, theme } from "@design";
import { useAuth } from "@context/AuthProvider";
import { useDocumentTitle } from "@hooks/usePageTitle";
import { LoginPage } from "@components/auth/LoginPage";

import { Logo } from "@common/Logo";
import Version from "@components/version/Version";

import {
  useStreamControls,
  useUnseenDomainsCount,
} from "./context/B4WsProvider";

import { DashboardPage } from "@b4.dashboard";
import { B4Alert } from "@b4.elements";
import { useHubStatus } from "@hooks/useHub";
import { SnackbarProvider } from "@context/SnackbarProvider";

const ConnectionsPage = lazy(() =>
  import("@b4.connections").then((m) => ({ default: m.ConnectionsPage })),
);
const DetectorPage = lazy(() =>
  import("@b4.detector").then((m) => ({ default: m.DetectorPage })),
);
const DiscoveryPage = lazy(() =>
  import("@b4.discovery").then((m) => ({ default: m.DiscoveryPage })),
);
const HubPage = lazy(() =>
  import("@b4.hub").then((m) => ({ default: m.HubPage })),
);
const LogsPage = lazy(() =>
  import("@b4.logs").then((m) => ({ default: m.LogsPage })),
);
const SetsPage = lazy(() =>
  import("@b4.sets").then((m) => ({ default: m.SetsPage })),
);
const SettingsPage = lazy(() =>
  import("@b4.settings").then((m) => ({ default: m.SettingsPage })),
);
const WatchdogPage = lazy(() =>
  import("@b4.watchdog").then((m) => ({ default: m.WatchdogPage })),
);

const DRAWER_WIDTH = 240;
const HEADER_HEIGHT = 64;

interface NavItem {
  path: string;
  labelKey: string;
  icon: React.ReactNode;
}

const navItems: NavItem[] = [
  { path: "/dashboard", labelKey: "core.nav.dashboard", icon: <DashboardIcon /> },
  { path: "/sets", labelKey: "core.nav.sets", icon: <SetsIcon /> },
  { path: "/discovery", labelKey: "core.nav.discovery", icon: <DiscoveryIcon /> },
  { path: "/hub", labelKey: "core.nav.hub", icon: <CommunityIcon /> },
  { path: "/watchdog", labelKey: "core.nav.watchdog", icon: <WatchdogIcon /> },
  { path: "/detector", labelKey: "core.nav.detector", icon: <SecurityIcon /> },
  { path: "/traffic", labelKey: "core.nav.connections", icon: <ConnectionIcon /> },
  { path: "/logs", labelKey: "core.nav.logs", icon: <LogsIcon /> },
  { path: "/settings", labelKey: "core.nav.settings", icon: <CoreIcon /> },
];
function PageLoadError() {
  const { t } = useTranslation();
  const error = useRouteError();
  return (
    <Box sx={{ p: 3 }}>
      <B4Alert
        severity="error"
        noWrapper
        action={
          <Button
            color="inherit"
            size="small"
            onClick={() => window.location.reload()}
          >
            {t("core.reload")}
          </Button>
        }
      >
        {t("core.pageLoadFailed")}
        {error instanceof Error && error.message && (
          <Typography
            variant="caption"
            component="div"
            sx={{ mt: 0.5, opacity: 0.8 }}
          >
            {error.message}
          </Typography>
        )}
      </B4Alert>
    </Box>
  );
}

export const appRoutes: RouteObject[] = [
  {
    errorElement: <PageLoadError />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: "dashboard", element: <DashboardPage /> },
      { path: "sets/*", element: <SetsPage /> },
      { path: "traffic", element: <ConnectionsPage /> },
      { path: "connections", element: <Navigate to="/traffic" replace /> },
      { path: "discovery", element: <DiscoveryPage /> },
      { path: "hub", element: <HubPage /> },
      { path: "watchdog", element: <WatchdogPage /> },
      { path: "detector", element: <DetectorPage /> },
      { path: "logs", element: <LogsPage /> },
      { path: "settings/*", element: <SettingsPage /> },
      { path: "*", element: <Navigate to="/dashboard" replace /> },
    ],
  },
];

function pageTitleKey(pathname: string) {
  return (
    navItems.find((item) => pathname.startsWith(item.path))?.labelKey ??
    "core.nav.dashboard"
  );
}

function UnseenDomainsBadge() {
  const count = useUnseenDomainsCount();
  if (count <= 0) return null;

  return (
    <Box
      component="span"
      sx={{
        ml: "auto",
        backgroundColor: colors.secondary,
        color: colors.text.tertiary,
        fontSize: 11,
        fontWeight: 700,
        px: "6px",
        height: 18,
        minWidth: 18,
        borderRadius: "9px",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {count > 999 ? "999+" : String(count)}
    </Box>
  );
}

export default function App() {
  const [desktopDrawerOpen, setDesktopDrawerOpen] = useState<boolean>(true);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState<boolean>(false);
  const isCompact = useMediaQuery(theme.breakpoints.down("md"));
  const navigate = useNavigate();
  const location = useLocation();
  const { resetDomainsBadge } = useStreamControls();
  const { isAuthenticated, isLoading, authRequired, logout } = useAuth();
  const { t } = useTranslation();
  const hubStatus = useHubStatus(!authRequired || isAuthenticated);
  const hubEnabled = Boolean(hubStatus.data?.enabled);
  const showLogin = authRequired && !isAuthenticated;
  const pageTitle = showLogin
    ? t("login.signIn")
    : t(pageTitleKey(location.pathname));

  useDocumentTitle(isLoading ? "" : pageTitle);

  const drawerOpen = isCompact ? mobileDrawerOpen : desktopDrawerOpen;
  const toggleDrawer = () => {
    if (isCompact) {
      setMobileDrawerOpen((open) => !open);
    } else {
      setDesktopDrawerOpen((open) => !open);
    }
  };

  if (isLoading) {
    return null;
  }

  if (showLogin) {
    return (
      <ThemeProvider theme={theme}>
        <CssBaseline />
        <LoginPage />
      </ThemeProvider>
    );
  }

  const isNavItemSelected = (navPath: string) => {
    if (navPath === "/settings" || navPath === "/sets") {
      return location.pathname.startsWith(navPath);
    }
    return location.pathname === navPath;
  };

  return (
    <ThemeProvider theme={theme}>
      <SnackbarProvider>
        <CssBaseline />
        <Box sx={{ display: "flex", height: "100vh" }}>
          <Drawer
            variant={isCompact ? "temporary" : "persistent"}
            open={drawerOpen}
            onClose={() => setMobileDrawerOpen(false)}
            ModalProps={{ keepMounted: true }}
            sx={{
              ...(isCompact ? {} : { width: DRAWER_WIDTH, flexShrink: 0 }),
              "& .MuiDrawer-paper": {
                width: DRAWER_WIDTH,
                boxSizing: "border-box",
              },
            }}
          >
            <Box
              sx={{
                height: HEADER_HEIGHT,
                boxSizing: "border-box",
                display: "flex",
                alignItems: "center",
                px: "16px",
              }}
            >
              <Logo />
            </Box>
            <Divider sx={{ borderColor: colors.border.default }} />
            <List sx={{ py: 1 }}>
              {navItems.map((item) => {
                if (item.path === "/hub" && !hubEnabled) return null;

                return (
                  <ListItem key={item.path} disablePadding>
                    <ListItemButton
                      selected={isNavItemSelected(item.path)}
                      onClick={() => {
                        if (item.path === "/traffic") {
                          resetDomainsBadge();
                        }
                        if (isCompact) {
                          setMobileDrawerOpen(false);
                        }
                        navigate(item.path)?.catch(() => {});
                      }}
                      sx={{
                        gap: "14px",
                        py: "8px",
                        px: "16px",
                        "&:hover": {
                          backgroundColor: "rgba(255, 255, 255, 0.04)",
                        },
                        "&.Mui-selected": {
                          backgroundColor: colors.accent.primary,
                          "&:hover": {
                            backgroundColor: colors.accent.primaryHover,
                          },
                        },
                      }}
                    >
                      <ListItemIcon
                        sx={{ color: "inherit", minWidth: 0, fontSize: 22 }}
                      >
                        {item.icon}
                      </ListItemIcon>
                      <ListItemText primary={t(item.labelKey)} />
                      {item.path === "/traffic" && <UnseenDomainsBadge />}
                    </ListItemButton>
                  </ListItem>
                );
              })}
            </List>
            <Version />
          </Drawer>

          <Box
            component="main"
            sx={{
              flexGrow: 1,
              minWidth: 0,
              display: "flex",
              flexDirection: "column",
              height: "100vh",
              ml: isCompact || drawerOpen ? 0 : `-${DRAWER_WIDTH}px`,
              transition: theme.transitions.create("margin", {
                easing: theme.transitions.easing.sharp,
                duration: theme.transitions.duration.leavingScreen,
              }),
            }}
          >
            <AppBar
              position="static"
              elevation={0}
              sx={{ boxShadow: "0 2px 0 rgba(0, 0, 0, 0.2)" }}
            >
              <Toolbar sx={{ minHeight: { xs: HEADER_HEIGHT }, px: "16px" }}>
                <IconButton
                  color="inherit"
                  onClick={toggleDrawer}
                  edge="start"
                >
                  <MenuIcon />
                </IconButton>
                <Typography
                  sx={{
                    flexGrow: 1,
                    fontSize: 18,
                    fontWeight: 600,
                    letterSpacing: "0.01em",
                    ml: "12px",
                    color: "#fff",
                  }}
                >
                  {pageTitle}
                </Typography>
                {authRequired && (
                  <IconButton color="inherit" onClick={logout} title={t("core.logout")}>
                    <LogoutIcon />
                  </IconButton>
                )}
              </Toolbar>
            </AppBar>

            <Suspense
              fallback={
                <Box
                  sx={{
                    flex: 1,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  <CircularProgress />
                </Box>
              }
            >
              <Outlet />
            </Suspense>
          </Box>
        </Box>
      </SnackbarProvider>
    </ThemeProvider>
  );
}
