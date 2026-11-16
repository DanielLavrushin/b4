import Link from "@docusaurus/Link";
import { translate } from "@docusaurus/Translate";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import { usePluginData } from "@docusaurus/useGlobalData";
import styles from "./ReleaseLink.module.css";

type Release = { version: string; date: string } | null | undefined;

export default function ReleaseLink() {
  const release = usePluginData("b4-latest-release") as Release;
  const { i18n } = useDocusaurusContext();
  if (!release) return null;

  const date = new Intl.DateTimeFormat(i18n.currentLocale, {
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  }).format(new Date(`${release.date}T00:00:00Z`));

  return (
    <Link to="/docs/changelog" className={styles.release}>
      <span className={styles.version}>{release.version}</span>
      {translate({ id: "home.hero.released", message: "Released {date}" }, { date })}
    </Link>
  );
}
