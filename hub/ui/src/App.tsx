import { useState } from "react";
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
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, Logo, theme } from "@design";
import { useSession } from "@/context/SessionProvider";
import { SnackbarProvider } from "@/context/SnackbarProvider";
import { useSets } from "@/api/hub";
import { LoginPage } from "@/components/LoginPage";
import { LanguageMenu } from "@/components/LanguageMenu";
import { SidePanel } from "@/components/SidePanel";
import { OverviewPage } from "@/pages/OverviewPage";
import { QueuePage } from "@/pages/QueuePage";
import { SetsPage } from "@/pages/SetsPage";
import { KeysPage } from "@/pages/KeysPage";
import { MirrorsPage } from "@/pages/MirrorsPage";
import { FeedbackPage } from "@/pages/FeedbackPage";
import { CataloguePage } from "@/pages/CataloguePage";

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
];

function Shell() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { logout } = useSession();
  const isCompact = useMediaQuery(theme.breakpoints.down("md"));
  const [desktopOpen, setDesktopOpen] = useState(true);
  const [mobileOpen, setMobileOpen] = useState(false);
  const sets = useSets();
  const pending = sets.data?.pending.length ?? 0;

  const drawerOpen = isCompact ? mobileOpen : desktopOpen;
  const toggleDrawer = () => {
    if (isCompact) setMobileOpen((o) => !o);
    else setDesktopOpen((o) => !o);
  };

  const current = navItems.find((item) => location.pathname.startsWith(item.path)) ?? navItems[0];

  return (
    <Box sx={{ display: "flex", height: "100vh" }}>
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
                selected={location.pathname.startsWith(item.path)}
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
                {item.path === "/queue" && pending > 0 && (
                  <Badge badgeContent={pending > 999 ? "999+" : pending} color="secondary" sx={{ mr: 1 }} />
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
          <Routes>
            <Route path="/" element={<Navigate to="/overview" replace />} />
            <Route path="/overview" element={<OverviewPage />} />
            <Route path="/queue" element={<QueuePage />} />
            <Route path="/sets" element={<SetsPage />} />
            <Route path="/keys" element={<KeysPage />} />
            <Route path="/mirrors" element={<MirrorsPage />} />
            <Route path="/feedback" element={<FeedbackPage />} />
            <Route path="/catalogue" element={<CataloguePage />} />
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Routes>
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
      <SnackbarProvider>{loading ? null : authenticated ? <Shell /> : <LoginPage />}</SnackbarProvider>
    </ThemeProvider>
  );
}
