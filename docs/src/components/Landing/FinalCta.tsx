import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate from "@docusaurus/Translate";
import InstallCommand from "./InstallCommand";
import ReleaseLink from "./ReleaseLink";
import Steps from "./Steps";
import shared from "./landing.module.css";
import styles from "./FinalCta.module.css";

export default function FinalCta() {
  return (
    <section className={styles.section} aria-labelledby="start-title">
      <div className={shared.plateWrap}>
        <div className={clsx(shared.plate, shared.field, styles.plate)}>
          <div className={styles.start}>
            <h2 id="start-title" className={clsx(shared.h2, styles.title)}>
              <Translate id="home.steps.title">Getting started</Translate>
            </h2>
            <div className={styles.actions}>
              <Link to="/docs/install/" className={clsx(shared.btn, shared.btnPrimary)}>
                <Translate id="home.cta.guide">Installation guide</Translate>
              </Link>
              <InstallCommand />
            </div>
            <p className={styles.note}>
              <Translate id="home.final.text">
                MikroTik and Docker run b4 as a container, and the guide covers both.
              </Translate>
            </p>
            <ReleaseLink />
          </div>
          <Steps />
        </div>
      </div>
    </section>
  );
}
