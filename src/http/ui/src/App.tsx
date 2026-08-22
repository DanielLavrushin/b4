import {
  AppBar,
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
import { useState } from "react";
import {
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router";
import { useTranslation } from "react-i18next";

import {
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
import { LoginPage } from "@components/auth/LoginPage";

import { Logo } from "@common/Logo";
import Version from "@components/version/Version";

import { useWebSocket } from "./context/B4WsProvider";

import { ConnectionsPage } from "@b4.connections";
import { DashboardPage } from "@b4.dashboard";
import { DetectorPage } from "@b4.detector";
import { DiscoveryPage } from "@b4.discovery";
import { LogsPage } from "@b4.logs";
import { SetsPage } from "@b4.sets";
import { SettingsPage } from "@b4.settings";
import { WatchdogPage } from "@b4.watchdog";
import { SnackbarProvider } from "@context/SnackbarProvider";

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
  { path: "/watchdog", labelKey: "core.nav.watchdog", icon: <WatchdogIcon /> },
  { path: "/detector", labelKey: "core.nav.detector", icon: <SecurityIcon /> },
  { path: "/traffic", labelKey: "core.nav.connections", icon: <ConnectionIcon /> },
  { path: "/logs", labelKey: "core.nav.logs", icon: <LogsIcon /> },
  { path: "/settings", labelKey: "core.nav.settings", icon: <CoreIcon /> },
];

export default function App() {
  const [desktopDrawerOpen, setDesktopDrawerOpen] = useState<boolean>(true);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState<boolean>(false);
  const isCompact = useMediaQuery(theme.breakpoints.down("md"));
  const navigate = useNavigate();
  const location = useLocation();
  const { unseenDomainsCount, resetDomainsBadge } = useWebSocket();
  const { isAuthenticated, isLoading, authRequired, logout } = useAuth();
  const { t } = useTranslation();

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

  if (authRequired && !isAuthenticated) {
    return (
      <ThemeProvider theme={theme}>
        <CssBaseline />
        <LoginPage />
      </ThemeProvider>
    );
  }

  const getPageTitle = () => {
    const path = location.pathname;
    if (path.startsWith("/dashboard")) return t("core.nav.dashboard");
    if (path.startsWith("/sets")) return t("core.nav.sets");
    if (path.startsWith("/traffic")) return t("core.nav.connections");
    if (path.startsWith("/discovery")) return t("core.nav.discovery");
    if (path.startsWith("/watchdog")) return t("core.nav.watchdog");
    if (path.startsWith("/logs")) return t("core.nav.logs");
    if (path.startsWith("/detector")) return t("core.nav.detector");
    if (path.startsWith("/settings")) return t("core.nav.settings");
    return t("core.nav.dashboard");
  };

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
                let targetCount = 0;
                if (item.path === "/traffic" && unseenDomainsCount > 0) {
                  targetCount = unseenDomainsCount;
                }
                const badgeLabel =
                  targetCount > 999 ? "999+" : String(targetCount);

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
                      {targetCount > 0 && (
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
                          {badgeLabel}
                        </Box>
                      )}
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
                  {getPageTitle()}
                </Typography>
                {authRequired && (
                  <IconButton color="inherit" onClick={logout} title={t("core.logout")}>
                    <LogoutIcon />
                  </IconButton>
                )}
              </Toolbar>
            </AppBar>

            <Routes>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard" element={<DashboardPage />} />
              <Route path="/sets/*" element={<SetsPage />} />
              <Route path="/traffic" element={<ConnectionsPage />} />
              <Route path="/connections" element={<Navigate to="/traffic" replace />} />
              <Route path="/discovery" element={<DiscoveryPage />} />
              <Route path="/watchdog" element={<WatchdogPage />} />
              <Route path="/detector" element={<DetectorPage />} />
              <Route path="/logs" element={<LogsPage />} />
              <Route path="/settings/*" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/dashboard" replace />} />
            </Routes>
          </Box>
        </Box>
      </SnackbarProvider>
    </ThemeProvider>
  );
}
