import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate from "@docusaurus/Translate";
import { ArrowRightIcon } from "@phosphor-icons/react/dist/csr/ArrowRight";
import InstallCommand from "./InstallCommand";
import Reveal from "./Reveal";
import shared from "./landing.module.css";
import styles from "./FinalCta.module.css";

export default function FinalCta() {
  return (
    <section className={styles.section} aria-labelledby="final-title">
      <div className={shared.container}>
        <Reveal className={styles.card}>
          <h2 id="final-title" className={clsx(shared.h2, styles.title)}>
            <Translate id="home.final.title">Installation</Translate>
          </h2>
          <p className={clsx(shared.lead, styles.text)}>
            <Translate id="home.final.text">
              One command works on Linux, OpenWrt, ASUS Merlin and Keenetic. MikroTik and Docker run
              b4 as a container, and the guide covers both.
            </Translate>
          </p>
          <div className={styles.actions}>
            <Link to="/docs/install/" className={clsx(shared.btn, shared.btnPrimary)}>
              <Translate id="home.cta.guide">Installation guide</Translate>
              <ArrowRightIcon className={shared.btnIcon} size={16} weight="bold" aria-hidden="true" />
            </Link>
            <InstallCommand />
          </div>
        </Reveal>
      </div>
    </section>
  );
}
