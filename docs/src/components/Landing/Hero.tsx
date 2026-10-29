import type { CSSProperties } from "react";
import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import { usePluginData } from "@docusaurus/useGlobalData";
import { ArrowRightIcon } from "@phosphor-icons/react/dist/csr/ArrowRight";
import InstallCommand from "./InstallCommand";
import { lines } from "./text";
import useScreenshot from "./useScreenshot";
import shared from "./landing.module.css";
import styles from "./Hero.module.css";

type Release = { version: string; date: string } | null | undefined;

const delay = (ms: number) => ({ "--enter-delay": `${ms}ms` }) as CSSProperties;

function ReleasePill() {
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
    <Link to="/docs/changelog" className={clsx(styles.pill, shared.enter)}>
      <span className={styles.pillVersion}>{release.version}</span>
      <span>{translate({ id: "home.hero.released", message: "Released {date}" }, { date })}</span>
      <ArrowRightIcon className={shared.btnIcon} size={14} weight="bold" aria-hidden="true" />
    </Link>
  );
}

export default function Hero() {
  const screenshot = useScreenshot("/img/dashboard/20261003220000.png");
  const title = translate({
    id: "home.hero.title",
    message: "DPI bypass\nfor Linux routers",
  });

  return (
    <section className={styles.hero} aria-labelledby="hero-title">
      <div className={clsx(shared.container, styles.inner)}>
        <ReleasePill />
        <h1 id="hero-title" className={clsx(styles.title, shared.enter)} style={delay(60)}>
          {lines(title).map((line) => (
            <span key={line} className={styles.titleLine}>
              {line}{" "}
            </span>
          ))}
        </h1>
        <p className={clsx(styles.subtitle, shared.enter)} style={delay(120)}>
          <Translate id="home.hero.subtitle">
            b4 is a Linux service that changes how the packets of selected sites look to the
            provider's DPI, and can send them through a VPN or a proxy instead. On a router it works
            for every device on the network, with nothing to install on phones or TVs.
          </Translate>
        </p>
        <div className={clsx(styles.actions, shared.enter)} style={delay(180)}>
          <Link to="/docs/install/" className={clsx(shared.btn, shared.btnPrimary)}>
            <Translate id="home.cta.guide">Installation guide</Translate>
            <ArrowRightIcon className={shared.btnIcon} size={16} weight="bold" aria-hidden="true" />
          </Link>
          <InstallCommand />
        </div>
      </div>
      <div className={clsx(shared.container, styles.shotWrap)}>
        <figure className={clsx(styles.shot, shared.enter)} style={delay(320)}>
          <img
            src={screenshot}
            width={1440}
            height={900}
            fetchPriority="high"
            alt={translate({
              id: "home.hero.shot.alt",
              message:
                "The b4 dashboard: service status, connections per minute and the sets that matched them",
            })}
          />
        </figure>
      </div>
    </section>
  );
}
