import type { ReactNode } from "react";
import clsx from "clsx";
import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import { ArrowRightIcon } from "@phosphor-icons/react/dist/csr/ArrowRight";
import { ArrowsClockwiseIcon } from "@phosphor-icons/react/dist/csr/ArrowsClockwise";
import { CheckIcon } from "@phosphor-icons/react/dist/csr/Check";
import { CheckCircleIcon } from "@phosphor-icons/react/dist/csr/CheckCircle";
import { DeviceMobileIcon } from "@phosphor-icons/react/dist/csr/DeviceMobile";
import { FlaskIcon } from "@phosphor-icons/react/dist/csr/Flask";
import { GameControllerIcon } from "@phosphor-icons/react/dist/csr/GameController";
import { LaptopIcon } from "@phosphor-icons/react/dist/csr/Laptop";
import { MagnifyingGlassIcon } from "@phosphor-icons/react/dist/csr/MagnifyingGlass";
import { MinusIcon } from "@phosphor-icons/react/dist/csr/Minus";
import { TelevisionSimpleIcon } from "@phosphor-icons/react/dist/csr/TelevisionSimple";
import { XCircleIcon } from "@phosphor-icons/react/dist/csr/XCircle";
import Reveal from "./Reveal";
import useScreenshot from "./useScreenshot";
import shared from "./landing.module.css";
import styles from "./Features.module.css";

type TileProps = {
  className?: string;
  visual: ReactNode;
  decorative?: boolean;
  title: ReactNode;
  text: ReactNode;
  to: string;
  link: ReactNode;
  delay?: number;
};

function Tile({ className, visual, decorative = true, title, text, to, link, delay }: TileProps) {
  return (
    <Reveal as="article" className={clsx(styles.tile, className)} delay={delay}>
      <div className={styles.visual} aria-hidden={decorative || undefined}>
        {visual}
      </div>
      <div className={styles.body}>
        <h3 className={styles.title}>{title}</h3>
        <p className={styles.text}>{text}</p>
        <Link to={to} className={clsx(shared.textLink, styles.link)}>
          {link}
          <ArrowRightIcon className={shared.btnIcon} size={16} weight="bold" aria-hidden="true" />
        </Link>
      </div>
    </Reveal>
  );
}

function DiscoveryVisual() {
  const confirmed = translate({ id: "home.features.discovery.confirmed", message: "confirmed 3 of 3" });
  const rows = [
    { domain: "youtube.com", verdict: confirmed, ok: true },
    { domain: "discord.com", verdict: confirmed, ok: true },
    {
      domain: "github.com",
      verdict: translate({ id: "home.features.discovery.none", message: "no bypass needed" }),
      ok: false,
    },
  ];
  return (
    <div className={styles.stack}>
      <div className={styles.search}>
        <MagnifyingGlassIcon size={16} />
        <span className={styles.searchText}>youtube.com, discord.com, github.com</span>
        <span className={styles.start}>
          <Translate id="home.features.discovery.start">Start</Translate>
        </span>
      </div>
      <ul className={styles.rows}>
        {rows.map((row) => (
          <li key={row.domain} className={styles.row}>
            <span className={shared.mono}>{row.domain}</span>
            <span className={clsx(styles.chip, row.ok ? styles.chipOk : styles.chipMuted)}>
              {row.ok ? <CheckIcon size={12} weight="bold" /> : <MinusIcon size={12} weight="bold" />}
              {row.verdict}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function DevicesVisual() {
  const devices = [
    {
      icon: <TelevisionSimpleIcon size={18} />,
      name: translate({ id: "home.features.devices.tv", message: "Living room TV" }),
      on: true,
    },
    { icon: <DeviceMobileIcon size={18} />, name: "iPhone", on: true },
    {
      icon: <GameControllerIcon size={18} />,
      name: translate({ id: "home.features.devices.console", message: "Game console" }),
      on: true,
    },
    {
      icon: <LaptopIcon size={18} />,
      name: translate({ id: "home.features.devices.work", message: "Work laptop" }),
      on: false,
    },
  ];
  return (
    <div className={styles.stack}>
      <p className={styles.visualHead}>
        <span>YouTube</span>
        <span className={clsx(styles.chip, styles.kindBypass)}>
          <Translate id="home.features.kind.bypass">Bypass</Translate>
        </span>
      </p>
      <ul className={styles.rows}>
        {devices.map((device) => (
          <li key={device.name} className={clsx(styles.row, !device.on && styles.rowOff)}>
            <span className={styles.rowLabel}>
              {device.icon}
              {device.name}
            </span>
            <span className={clsx(styles.switch, device.on && styles.switchOn)} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function WatchdogVisual() {
  const events = [
    {
      icon: <XCircleIcon size={20} weight="fill" />,
      tone: styles.toneBad,
      text: translate({ id: "home.features.watchdog.failed", message: "3 checks failed in a row" }),
      time: "14:02",
    },
    {
      icon: <FlaskIcon size={20} weight="fill" />,
      tone: styles.toneAccent,
      text: translate({ id: "home.features.watchdog.discovery", message: "Discovery for the set YouTube" }),
      time: "14:03",
    },
    {
      icon: <ArrowsClockwiseIcon size={20} weight="bold" />,
      tone: styles.toneInfo,
      text: translate({ id: "home.features.watchdog.written", message: "New strategy written" }),
      time: "14:08",
    },
    {
      icon: <CheckCircleIcon size={20} weight="fill" />,
      tone: styles.toneOk,
      text: translate({ id: "home.features.watchdog.passed", message: "Check passed" }),
      time: "14:09",
    },
  ];
  return (
    <ol className={styles.timeline}>
      {events.map((event) => (
        <li key={event.time} className={styles.event}>
          <span className={clsx(styles.eventIcon, event.tone)}>{event.icon}</span>
          <span>{event.text}</span>
          <time className={styles.time}>{event.time}</time>
        </li>
      ))}
    </ol>
  );
}

function RoutingVisual() {
  const sets = [
    {
      name: "YouTube",
      kind: translate({ id: "home.features.kind.bypass", message: "Bypass" }),
      tone: styles.kindBypass,
      to: translate({ id: "home.features.routing.direct", message: "direct" }),
    },
    {
      name: translate({ id: "home.features.routing.vpn", message: "Work VPN" }),
      kind: translate({ id: "home.features.kind.route", message: "Route" }),
      tone: styles.kindRoute,
      to: "wg0",
    },
    {
      name: "Telegram",
      kind: translate({ id: "home.features.kind.proxy", message: "Proxy" }),
      tone: styles.kindProxy,
      to: "127.0.0.1:1080",
    },
    {
      name: translate({ id: "home.features.routing.ads", message: "Ads and trackers" }),
      kind: translate({ id: "home.features.kind.block", message: "Block" }),
      tone: styles.kindBlock,
      to: translate({ id: "home.features.routing.blocked", message: "blocked" }),
    },
  ];
  return (
    <ul className={styles.rows}>
      {sets.map((set) => (
        <li key={set.name} className={clsx(styles.row, styles.setRow)}>
          <span className={styles.setName}>{set.name}</span>
          <span className={clsx(styles.chip, set.tone)}>{set.kind}</span>
          <span className={clsx(shared.mono, styles.setTarget)}>{set.to}</span>
        </li>
      ))}
    </ul>
  );
}

function InterfaceVisual() {
  const screenshot = useScreenshot("/img/connections/20260923214001.png");
  return (
    <img
      className={styles.shot}
      src={screenshot}
      width={1600}
      height={860}
      loading="lazy"
      alt={translate({
        id: "home.features.ui.alt",
        message: "The Traffic page: connections of each device and the sets they matched",
      })}
    />
  );
}

export default function Features() {
  return (
    <section className={shared.section} aria-labelledby="features-title">
      <div className={shared.container}>
        <Reveal as="header" className={shared.head}>
          <h2 id="features-title" className={shared.h2}>
            <Translate id="home.features.title">What b4 does</Translate>
          </h2>
          <p className={shared.lead}>
            <Translate id="home.features.lead">
              Everything is configured as sets. A set is a list of sites, plus what b4 should do with
              their traffic.
            </Translate>
          </p>
        </Reveal>

        <div className={styles.bento}>
          <Tile
            className={styles.span7}
            visual={<DiscoveryVisual />}
            title={<Translate id="home.features.discovery.title">Discovery</Translate>}
            text={
              <Translate id="home.features.discovery.text">
                Discovery tries b4's strategies on the router's own connection and shows which one
                opens the site. The one that works can be saved as a set. A run takes 1 to 10 minutes.
              </Translate>
            }
            to="/docs/discovery"
            link={<Translate id="home.features.discovery.link">How Discovery works</Translate>}
          />
          <Tile
            className={styles.span5}
            delay={80}
            visual={<DevicesVisual />}
            title={<Translate id="home.features.devices.title">Devices</Translate>}
            text={
              <Translate id="home.features.devices.text">
                Installed on the router, b4 handles traffic from every device on the network: phones,
                TVs, consoles. A set can also apply to specific devices only.
              </Translate>
            }
            to="/docs/guides/devices"
            link={<Translate id="home.features.devices.link">Selecting devices</Translate>}
          />
          <Tile
            className={styles.span5}
            visual={<WatchdogVisual />}
            title={<Translate id="home.features.watchdog.title">Watchdog</Translate>}
            text={
              <Translate id="home.features.watchdog.text">
                The watchdog re-checks a set's sites on a schedule. If they keep failing, it runs
                Discovery for the set and switches it to the strategy it finds, or back to the old one
                if the sites still fail. It's off by default.
              </Translate>
            }
            to="/docs/guides/keep-working"
            link={<Translate id="home.features.watchdog.link">Keeping sets working</Translate>}
          />
          <Tile
            className={styles.span7}
            delay={80}
            visual={<RoutingVisual />}
            title={<Translate id="home.features.routing.title">Routing and blocking</Translate>}
            text={
              <Translate id="home.features.routing.text">
                Sites blocked by IP can go through a VPN interface on the router, or through Xray,
                sing-box or any other SOCKS5 proxy. A block set drops ads and trackers for the whole
                network.
              </Translate>
            }
            to="/docs/sets/routing"
            link={<Translate id="home.features.routing.link">VPN and proxy routing</Translate>}
          />
          <Tile
            className={styles.wide}
            visual={<InterfaceVisual />}
            decorative={false}
            title={<Translate id="home.features.ui.title">Web interface</Translate>}
            text={
              <Translate id="home.features.ui.text">
                Dashboard, live traffic and logs on port 7000, in English or Russian. Changes to sets
                apply without a restart.
              </Translate>
            }
            to="/docs/quickstart"
            link={<Translate id="home.features.ui.link">First steps</Translate>}
          />
        </div>
      </div>
    </section>
  );
}
