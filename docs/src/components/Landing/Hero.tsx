import type { CSSProperties } from "react";
import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import InstallCommand from "./InstallCommand";
import { lines } from "./text";
import useScreenshot from "./useScreenshot";
import shared from "./landing.module.css";
import styles from "./Hero.module.css";

const delay = (ms: number) => ({ "--enter-delay": `${ms}ms` }) as CSSProperties;

export default function Hero() {
  const screenshot = useScreenshot("/img/dashboard/20261003220000.png");
  const title = translate({
    id: "home.hero.title",
    message: "DPI bypass\nfor Linux routers",
  });

  return (
    <section className={styles.hero} aria-labelledby="hero-title">
      <div className={shared.plateWrap}>
        <div className={clsx(shared.plate, shared.field, styles.plate)}>
          <div className={styles.text}>
            <h1 id="hero-title" className={clsx(styles.title, shared.enter)}>
              {lines(title).join(" ")}
            </h1>
            <p className={clsx(styles.sentence, shared.enter)} style={delay(80)}>
              <Translate id="home.hero.subtitle">
                b4 changes how packets of selected sites look to the provider’s DPI.
              </Translate>
            </p>
            <div className={clsx(styles.actions, shared.enter)} style={delay(160)}>
              <Link to="/docs/install/" className={clsx(shared.btn, shared.btnPrimary)}>
                <Translate id="home.cta.guide">Installation guide</Translate>
              </Link>
              <InstallCommand />
            </div>
          </div>
          <figure className={clsx(styles.shot, shared.enter)} style={delay(240)}>
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
      </div>
    </section>
  );
}
