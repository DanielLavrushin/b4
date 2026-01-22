import {
  ConnectionIcon,
  CoreIcon,
  DashboardIcon,
  DescriptionIcon,
  DiscoveryIcon,
  GitHubIcon,
  LogsIcon,
  SetsIcon,
} from "@b4.icons";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@composed/sidebar";
import { Button } from "@primitives/button";
import { Separator } from "@primitives/separator";
import { useLocation, useNavigate } from "react-router-dom";
import Version from "../version/Version";
import { Logo } from "./Logo";

const menuItems = [
  {
    title: "Dashboard",
    icon: DashboardIcon,
    url: "/dashboard",
  },
  {
    title: "Connections",
    icon: ConnectionIcon,
    url: "/connections",
  },
  {
    title: "Sets",
    icon: SetsIcon,
    url: "/sets",
  },
  {
    title: "Settings",
    icon: CoreIcon,
    url: "/settings",
  },
  {
    title: "Discovery",
    icon: DiscoveryIcon,
    url: "/discovery",
  },
  {
    title: "Logs",
    icon: LogsIcon,
    url: "/logs",
  },
];

export function AppSidebar() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <Sidebar variant="inset">
      <SidebarHeader>
        <Logo />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Navigation</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {menuItems.map((item) => {
                const Icon = item.icon;
                const isActive =
                  location.pathname === item.url ||
                  (item.url !== "/dashboard" &&
                    location.pathname.startsWith(item.url));
                return (
                  <SidebarMenuItem key={item.url}>
                    <SidebarMenuButton
                      isActive={isActive}
                      onClick={() => navigate(item.url)}
                      tooltip={item.title}
                    >
                      <Icon />
                      <span>{item.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <Separator />
      <SidebarFooter>
        <Button variant="link" asChild>
          <a
            href="https://github.com/daniellavrushin/b4"
            target="_blank"
            rel="noopener noreferrer"
          >
            <GitHubIcon />
            DanielLavrushin/b4
          </a>
        </Button>
        <Button variant="link" asChild>
          <a
            href="https://daniellavrushin.github.io/b4/"
            target="_blank"
            rel="noopener noreferrer"
          >
            <DescriptionIcon />
            Documentation
          </a>
        </Button>
        <Version />
      </SidebarFooter>
    </Sidebar>
  );
}
