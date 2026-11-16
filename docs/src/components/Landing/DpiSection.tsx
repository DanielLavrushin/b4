import type { ReactNode } from "react";
import clsx from "clsx";
import Translate, { translate } from "@docusaurus/Translate";
import { CheckCircleIcon } from "@phosphor-icons/react/dist/csr/CheckCircle";
import { DeviceMobileIcon } from "@phosphor-icons/react/dist/csr/DeviceMobile";
import { GlobeSimpleIcon } from "@phosphor-icons/react/dist/csr/GlobeSimple";
import { XCircleIcon } from "@phosphor-icons/react/dist/csr/XCircle";
import shared from "./landing.module.css";
import styles from "./DpiSection.module.css";

function Node({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={clsx(styles.node, className)}>{children}</span>;
}

export default function DpiSection() {
  const fake = translate({ id: "home.dpi.fake", message: "fake" });

  return (
    <section className={shared.sectionXL} aria-labelledby="dpi-title">
      <div className={shared.container}>
        <header className={shared.headSplit}>
          <h2 id="dpi-title" className={shared.h2}>
            <Translate id="home.dpi.title">How it works</Translate>
          </h2>
          <p className={shared.lead}>
            <Translate id="home.dpi.lead">
              Providers that block sites with DPI read the site name from the first packets of a
              connection, and reset or slow down connections to sites on their list. b4 changes how
              those first packets look: it splits them, reorders them or sends a fake one ahead.
            </Translate>
          </p>
        </header>

        <div className={styles.panel}>
          <article className={styles.row}>
            <div className={styles.copy}>
              <p className={styles.label}>
                <Translate id="home.dpi.before.label">Without b4</Translate>
              </p>
              <p className={clsx(styles.verdict, styles.bad)}>
                <XCircleIcon size={24} weight="fill" aria-hidden="true" />
                <Translate id="home.dpi.before.verdict">
                  DPI reads the name and resets the connection
                </Translate>
              </p>
            </div>
            <div className={styles.lane} aria-hidden="true">
              <Node>
                <DeviceMobileIcon size={24} />
              </Node>
              <span className={styles.wire} />
              <span className={styles.packet}>youtube.com</span>
              <span className={styles.wire} />
              <Node className={clsx(styles.dpi, styles.hit)}>DPI</Node>
              <span className={clsx(styles.wire, styles.cut)} />
              <Node className={styles.siteOff}>
                <GlobeSimpleIcon size={24} />
              </Node>
            </div>
          </article>

          <article className={styles.row}>
            <div className={styles.copy}>
              <p className={styles.label}>
                <Translate id="home.dpi.after.label">With b4</Translate>
              </p>
              <p className={clsx(styles.verdict, styles.ok)}>
                <CheckCircleIcon size={24} weight="fill" aria-hidden="true" />
                <Translate id="home.dpi.after.verdict">
                  The name arrives in pieces and the site gets the same request
                </Translate>
              </p>
            </div>
            <div className={styles.lane} aria-hidden="true">
              <Node className={styles.b4}>b4</Node>
              <span className={styles.wire} />
              <span className={styles.fragments}>
                <span className={clsx(styles.packet, styles.fake)}>{fake}</span>
                <span className={styles.packet}>you</span>
                <span className={styles.packet}>tube</span>
                <span className={styles.packet}>.com</span>
              </span>
              <span className={styles.wire} />
              <Node className={styles.dpi}>DPI</Node>
              <span className={styles.wire} />
              <Node className={styles.siteOn}>
                <GlobeSimpleIcon size={24} />
              </Node>
            </div>
          </article>
        </div>
      </div>
    </section>
  );
}
