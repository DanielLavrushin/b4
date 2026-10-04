import { useEffect, useState } from "react";
import { ModerationProvider } from "@/features/moderation/ModerationProvider";
import { KnownKeysProvider } from "@/features/keys/KnownKeys";
import { KeyDrawerHost } from "@/features/keys/KeyDrawerHost";
import { SetDrawerHost } from "@/features/sets/SetDrawerHost";
import { backgroundOf } from "@/shared/hooks/useOverlay";
import {
  AppBar,
  Badge,
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
import MenuIcon from "@mui/icons-material/Menu";
import LogoutIcon from "@mui/icons-material/Logout";
import DashboardIcon from "@mui/icons-material/DashboardOutlined";
import InboxIcon from "@mui/icons-material/InboxOutlined";
import LayersIcon from "@mui/icons-material/LayersOutlined";
import KeyIcon from "@mui/icons-material/KeyOutlined";
import HubIcon from "@mui/icons-material/HubOutlined";
import ForumIcon from "@mui/icons-material/ForumOutlined";
import InventoryIcon from "@mui/icons-material/Inventory2Outlined";
import TuneIcon from "@mui/icons-material/TuneOutlined";
import InsightsIcon from "@mui/icons-material/InsightsOutlined";
import HistoryIcon from "@mui/icons-material/HistoryOutlined";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, Logo, theme } from "@design";
import { useSession } from "@/features/session/SessionProvider";
import { SnackbarProvider } from "@/app/SnackbarProvider";
import { useCounts } from "./api";
import { LoginPage } from "@/features/session/LoginPage";
import { LanguageMenu } from "@/shared/components/LanguageMenu";
import { useBuildStatus, useRefreshOnPublish } from "@/features/catalogue/api";
import { SidePanel } from "@/app/SidePanel";
import { OverviewPage } from "@/features/overview/OverviewPage";
import { QueuePage } from "@/features/queue/QueuePage";
import { SetsPage } from "@/features/sets/SetsPage";
import { KeysPage } from "@/features/keys/KeysPage";
import { MirrorsPage } from "@/features/mirrors/MirrorsPage";
import { FeedbackPage } from "@/features/feedback/FeedbackPage";
import { CataloguePage } from "@/features/catalogue/CataloguePage";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { StatsPage } from "@/features/stats/StatsPage";
import { AuditPage } from "@/features/audit/AuditPage";
import { useHealth } from "@/features/overview/api";
import { severityColor } from "@/features/overview/HealthCard";

const DRAWER_WIDTH = 240;
const HEADER_HEIGHT = 64;

interface NavItem {
  path: string;
  labelKey: string;
  icon: React.ReactNode;
}

const navItems: NavItem[] = [
  { path: "/overview", labelKey: "nav.overview", icon: <DashboardIcon /> },
  { path: "/queue", labelKey: "nav.queue", icon: <InboxIcon /> },
  { path: "/sets", labelKey: "nav.sets", icon: <LayersIcon /> },
  { path: "/keys", labelKey: "nav.keys", icon: <KeyIcon /> },
  { path: "/mirrors", labelKey: "nav.mirrors", icon: <HubIcon /> },
  { path: "/feedback", labelKey: "nav.feedback", icon: <ForumIcon /> },
  { path: "/catalogue", labelKey: "nav.catalogue", icon: <InventoryIcon /> },
  { path: "/stats", labelKey: "nav.stats", icon: <InsightsIcon /> },
  { path: "/audit", labelKey: "nav.audit", icon: <HistoryIcon /> },
  { path: "/settings", labelKey: "nav.settings", icon: <TuneIcon /> },
];

function PublishWatcher() {
  const status = useBuildStatus();
  useRefreshOnPublish(status.data);
  return null;
}

function Shell() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const pageLocation = backgroundOf(location) ?? location;
  const { logout } = useSession();
  const isCompact = useMediaQuery(theme.breakpoints.down("md"));
  const [desktopOpen, setDesktopOpen] = useState(true);
  const [mobileOpen, setMobileOpen] = useState(false);
  const counts = useCounts();
  const health = useHealth();
  const worst = health.data?.worst ?? "ok";
  const pending = counts.data?.pending ?? 0;
  const badges: Record<string, number> = {
    "/queue": pending,
    "/feedback": counts.data?.reports_open ?? 0,
    "/sets": counts.data?.attention ?? 0,
    "/mirrors": counts.data?.mirrors_pending ?? 0,
  };

  const drawerOpen = isCompact ? mobileOpen : desktopOpen;
  const toggleDrawer = () => {
    if (isCompact) setMobileOpen((o) => !o);
    else setDesktopOpen((o) => !o);
  };

  const current = navItems.find((item) => pageLocation.pathname.startsWith(item.path)) ?? navItems[0];

  useEffect(() => {
    document.title = pending > 0 ? `(${String(pending)}) ${t("app.title")}` : t("app.title");
  }, [pending, t]);

  return (
    <Box sx={{ display: "flex", height: "100vh" }}>
      <PublishWatcher />
      <Drawer
        variant={isCompact ? "temporary" : "persistent"}
        open={drawerOpen}
        onClose={() => setMobileOpen(false)}
        ModalProps={{ keepMounted: true }}
        sx={{
          ...(isCompact ? {} : { width: DRAWER_WIDTH, flexShrink: 0 }),
          "& .MuiDrawer-paper": { width: DRAWER_WIDTH, boxSizing: "border-box" },
        }}
      >
        <Box sx={{ height: HEADER_HEIGHT, boxSizing: "border-box", display: "flex", alignItems: "center", px: "16px" }}>
          <Logo subtitle={t("app.subtitle")} />
        </Box>
        <Divider sx={{ borderColor: colors.border.default }} />
        <List sx={{ py: 1 }}>
          {navItems.map((item) => (
            <ListItem key={item.path} disablePadding>
              <ListItemButton
                selected={pageLocation.pathname.startsWith(item.path)}
                onClick={() => {
                  if (isCompact) setMobileOpen(false);
                  void navigate(item.path);
                }}
                sx={{
                  gap: "14px",
                  py: "8px",
                  px: "16px",
                  "&:hover": { backgroundColor: colors.background.hover },
                  "&.Mui-selected": {
                    backgroundColor: colors.accent.primary,
                    "&:hover": { backgroundColor: colors.accent.primaryHover },
                  },
                }}
              >
                <ListItemIcon sx={{ color: "inherit", minWidth: 0, fontSize: 22 }}>{item.icon}</ListItemIcon>
                <ListItemText primary={t(item.labelKey)} />
                {item.path === "/overview" && worst !== "ok" && (
                  <Box
                    title={t(`health.worst.${worst}`)}
                    sx={{ width: 8, height: 8, borderRadius: "50%", bgcolor: severityColor(worst), mr: 1, flexShrink: 0 }}
                  />
                )}
                {(badges[item.path] ?? 0) > 0 && (
                  <Badge
                    badgeContent={badges[item.path] > 999 ? "999+" : badges[item.path]}
                    color={item.path === "/queue" ? "secondary" : "default"}
                    sx={{ mr: 1, "& .MuiBadge-badge": item.path === "/queue" ? {} : { bgcolor: colors.background.hover, color: colors.text.primary } }}
                  />
                )}
              </ListItemButton>
            </ListItem>
          ))}
        </List>
        <SidePanel />
      </Drawer>

      <Box
        component="main"
        sx={{
          flexGrow: 1,
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
          height: "100vh",
          ml: isCompact || drawerOpen ? 0 : `-${String(DRAWER_WIDTH)}px`,
          transition: theme.transitions.create("margin", {
            easing: theme.transitions.easing.sharp,
            duration: theme.transitions.duration.leavingScreen,
          }),
        }}
      >
        <AppBar position="static" elevation={0} sx={{ boxShadow: "0 2px 0 rgba(0, 0, 0, 0.2)" }}>
          <Toolbar sx={{ minHeight: { xs: HEADER_HEIGHT }, px: "16px" }}>
            <IconButton color="inherit" onClick={toggleDrawer} edge="start">
              <MenuIcon />
            </IconButton>
            <Typography sx={{ flexGrow: 1, fontSize: 18, fontWeight: 600, letterSpacing: "0.01em", ml: "12px", color: "#fff" }}>
              {t(current.labelKey)}
            </Typography>
            <LanguageMenu color="inherit" />
            <IconButton color="inherit" onClick={() => void logout()} title={t("app.logout")}>
              <LogoutIcon />
            </IconButton>
          </Toolbar>
        </AppBar>

        <Box sx={{ flex: 1, overflow: "auto", p: { xs: 2, md: 3 } }}>
          <Routes location={pageLocation}>
            <Route path="/" element={<Navigate to="/overview" replace />} />
            <Route path="/overview" element={<OverviewPage />} />
            <Route path="/queue" element={<QueuePage />} />
            <Route path="/sets" element={<SetsPage />} />
            <Route path="/sets/:id" element={<SetsPage />} />
            <Route path="/sets/:id/v/:version" element={<SetsPage />} />
            <Route path="/keys" element={<KeysPage />} />
            <Route path="/keys/:hmac" element={<KeysPage />} />
            <Route path="/mirrors" element={<MirrorsPage />} />
            <Route path="/feedback" element={<FeedbackPage />} />
            <Route path="/catalogue" element={<CataloguePage />} />
            <Route path="/stats" element={<StatsPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Routes>
          <SetDrawerHost />
          <KeyDrawerHost />
        </Box>
      </Box>
    </Box>
  );
}

export default function App() {
  const { loading, authenticated } = useSession();
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <SnackbarProvider>
        {loading ? null : authenticated ? (
          <KnownKeysProvider>
            <ModerationProvider>
              <Shell />
            </ModerationProvider>
          </KnownKeysProvider>
        ) : (
          <LoginPage />
        )}
      </SnackbarProvider>
    </ThemeProvider>
  );
}
