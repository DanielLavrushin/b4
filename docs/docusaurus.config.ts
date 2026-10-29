import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { themes as prismThemes } from "prism-react-renderer";
import type { Config, LoadContext, Plugin } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

type LatestRelease = { version: string; date: string } | null;

function latestReleasePlugin(context: LoadContext): Plugin<LatestRelease> {
  const changelog = join(context.siteDir, "..", "changelog.md");
  return {
    name: "b4-latest-release",
    getPathsToWatch: () => [changelog],
    async loadContent() {
      const text = await readFile(changelog, "utf8");
      const match = text.match(/^## \[(\d+\.\d+\.\d+)\] - (\d{4}-\d{2}-\d{2})/m);
      return match ? { version: match[1], date: match[2] } : null;
    },
    async contentLoaded({ content, actions }) {
      actions.setGlobalData(content);
    },
  };
}

const config: Config = {
  title: "B4 - Bye Bye Big Bro",
  tagline: "Censorship bypass and traffic routing for Linux and routers",
  favicon: "img/favicon.ico",
  url: "https://docs.b4core.app",
  baseUrl: "/",

  onBrokenLinks: "throw",
  onBrokenAnchors: "throw",

  markdown: {
    mermaid: true,
  },
  themes: [
    "@docusaurus/theme-mermaid",
    [
      "@easyops-cn/docusaurus-search-local",
      {
        hashed: true,
        language: ["en", "ru"],
        indexBlog: false,
        docsRouteBasePath: "/docs",
        highlightSearchTermsOnTargetPage: true,
      },
    ],
  ],

  plugins: [
    latestReleasePlugin,
    [
      "@docusaurus/plugin-client-redirects",
      {
        redirects: [{ from: "/docs/advanced", to: "/docs/reference" }],
      },
    ],
  ],

  i18n: {
    defaultLocale: "en",
    locales: ["en", "ru"],
    localeConfigs: {
      en: { label: "English" },
      ru: { label: "Русский" },
    },
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          editUrl: "https://github.com/DanielLavrushin/b4/tree/main/docs/",
        },
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      title: "",
      logo: {
        alt: "B4 Logo",
        src: "img/favicon.svg",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "tutorialSidebar",
          position: "left",
          label: "Docs",
        },
        {
          to: "/swagger",
          label: "API",
          position: "left",
        },
        {
          to: "/docs/changelog",
          label: "Changelog",
          position: "left",
        },
        {
          type: "localeDropdown",
          position: "right",
        },
        {
          href: "https://github.com/DanielLavrushin/b4",
          label: "GitHub",
          position: "right",
        },
      ],
    },
    footer: {
      style: "dark",
      copyright: `Copyright © ${new Date().getFullYear()} B4 Project`,
    },
    prism: {
      theme: prismThemes.dracula,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
