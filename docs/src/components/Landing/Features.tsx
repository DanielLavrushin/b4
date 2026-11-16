import { useState } from "react";
import type { CSSProperties } from "react";
import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import { PlusIcon } from "@phosphor-icons/react/dist/csr/Plus";
import useScreenshot from "./useScreenshot";
import shared from "./landing.module.css";
import styles from "./Features.module.css";

type Feature = {
  id: string;
  title: string;
  text: string;
  link: string;
  to: string;
  image: string;
  width: number;
  height: number;
  alt: string;
  position: string;
  aspect?: string;
};

function useFeatures(): Feature[] {
  const discovery = useScreenshot("/img/discovery/20260905160200.png");
  const devices = useScreenshot("/img/targets/20261023183500.png");
  const watchdog = useScreenshot("/img/discovery/20261004190000.png");
  const routing = useScreenshot("/img/dashboard/20261003220040.png");
  const traffic = useScreenshot("/img/connections/20260923214001.png");

  return [
    {
      id: "discovery",
      title: translate({ id: "home.features.discovery.title", message: "Discovery" }),
      text: translate({
        id: "home.features.discovery.text",
        message:
          "Discovery tries b4's strategies on the router’s own connection and shows which one opens the site. The one that works can be saved as a set. A run takes 1 to 10 minutes.",
      }),
      link: translate({ id: "home.features.discovery.link", message: "How Discovery works" }),
      to: "/docs/discovery",
      image: discovery,
      width: 1168,
      height: 477,
      alt: translate({
        id: "home.features.discovery.alt",
        message: "The Discovery page during a run: the stages of the search and the sites tested so far",
      }),
      position: "left top",
      aspect: "1168 / 477",
    },
    {
      id: "devices",
      title: translate({ id: "home.features.devices.title", message: "Devices" }),
      text: translate({
        id: "home.features.devices.text",
        message:
          "Installed on the router, b4 handles traffic from every device on the network: phones, TVs, consoles. A set can also apply to specific devices only.",
      }),
      link: translate({ id: "home.features.devices.link", message: "Selecting devices" }),
      to: "/docs/guides/devices",
      image: devices,
      width: 1168,
      height: 733,
      alt: translate({
        id: "home.features.devices.alt",
        message: "The Source Devices tab of a set: the devices the set applies to",
      }),
      position: "left top",
    },
    {
      id: "watchdog",
      title: translate({ id: "home.features.watchdog.title", message: "Watchdog" }),
      text: translate({
        id: "home.features.watchdog.text",
        message:
          "The watchdog re-checks a set’s sites on a schedule. If they keep failing, it runs Discovery for the set and switches it to the strategy it finds, or back to the old one if the sites still fail. It’s off by default.",
      }),
      link: translate({ id: "home.features.watchdog.link", message: "Keeping sets working" }),
      to: "/docs/guides/keep-working",
      image: watchdog,
      width: 1168,
      height: 827,
      alt: translate({
        id: "home.features.watchdog.alt",
        message: "The Watchdog panel in the settings, with the switch that turns monitoring on",
      }),
      position: "left bottom",
    },
    {
      id: "routing",
      title: translate({ id: "home.features.routing.title", message: "Routing and blocking" }),
      text: translate({
        id: "home.features.routing.text",
        message:
          "Sites blocked by IP can go through a VPN interface on the router, or through Xray, sing-box or any other SOCKS5 proxy. A block set drops ads and trackers for the whole network.",
      }),
      link: translate({ id: "home.features.routing.link", message: "VPN and proxy routing" }),
      to: "/docs/sets/routing",
      image: routing,
      width: 780,
      height: 320,
      alt: translate({
        id: "home.features.routing.alt",
        message: "The Sets panel of the dashboard: each set marked Bypass, Proxy, Block or Route",
      }),
      position: "left top",
      aspect: "780 / 320",
    },
    {
      id: "ui",
      title: translate({ id: "home.features.ui.title", message: "Web interface" }),
      text: translate({
        id: "home.features.ui.text",
        message:
          "Dashboard, live traffic and logs on port 7000, in English or Russian. Changes to sets apply without a restart.",
      }),
      link: translate({ id: "home.features.ui.link", message: "First steps" }),
      to: "/docs/quickstart",
      image: traffic,
      width: 1600,
      height: 860,
      alt: translate({
        id: "home.features.ui.alt",
        message: "The Traffic page: connections of each device and the sets they matched",
      }),
      position: "left top",
    },
  ];
}

export default function Features() {
  const features = useFeatures();
  const [active, setActive] = useState(0);

  return (
    <section className={shared.sectionL} aria-labelledby="features-title">
      <div className={shared.container}>
        <header className={shared.headSplit}>
          <h2 id="features-title" className={shared.h2}>
            <Translate id="home.features.title">What b4 does</Translate>
          </h2>
          <p className={shared.lead}>
            <Translate id="home.features.lead">
              Everything is configured as sets. A set is a list of sites, plus what b4 should do with
              their traffic.
            </Translate>
          </p>
        </header>

        <div className={styles.layout}>
          <div className={styles.list}>
            {features.map((feature, index) => {
              const open = index === active;
              return (
                <div key={feature.id} className={clsx(styles.item, open && styles.itemOpen)}>
                  <h3 className={styles.heading}>
                    <button
                      type="button"
                      id={`feature-${feature.id}-trigger`}
                      className={styles.trigger}
                      aria-expanded={open}
                      aria-controls={`feature-${feature.id}-body`}
                      onClick={() => setActive(index)}
                    >
                      <span className={styles.marker} aria-hidden="true" />
                      {feature.title}
                      {!open && (
                        <PlusIcon className={styles.plus} size={20} weight="bold" aria-hidden="true" />
                      )}
                    </button>
                  </h3>
                  <div
                    id={`feature-${feature.id}-body`}
                    role="region"
                    aria-labelledby={`feature-${feature.id}-trigger`}
                    className={styles.body}
                    hidden={!open}
                  >
                    <p className={styles.text}>{feature.text}</p>
                    <Link to={feature.to} className={shared.textLink}>
                      {feature.link}
                    </Link>
                    <figure
                      className={styles.inlineFigure}
                      style={{ "--shot-position": feature.position, ...(feature.aspect && { "--shot-aspect": feature.aspect }) } as CSSProperties}
                    >
                      <img
                        src={feature.image}
                        width={feature.width}
                        height={feature.height}
                        loading="lazy"
                        alt={feature.alt}
                      />
                    </figure>
                  </div>
                </div>
              );
            })}
          </div>

          <div className={styles.stage}>
            {features.map((feature, index) => (
              <figure
                key={feature.id}
                className={clsx(styles.figure, index === active && styles.figureOpen)}
                style={{ "--shot-position": feature.position, ...(feature.aspect && { "--shot-aspect": feature.aspect }) } as CSSProperties}
              >
                <img
                  src={feature.image}
                  width={feature.width}
                  height={feature.height}
                  loading="lazy"
                  alt={feature.alt}
                />
              </figure>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
