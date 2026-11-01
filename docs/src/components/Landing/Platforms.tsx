import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import { ArrowUpRightIcon } from "@phosphor-icons/react/dist/csr/ArrowUpRight";
import Reveal from "./Reveal";
import shared from "./landing.module.css";
import styles from "./Platforms.module.css";

const ARCHITECTURES = [
  "amd64",
  "386",
  "arm64",
  "armv5",
  "armv6",
  "armv7",
  "mips",
  "mipsle",
  "mips_softfloat",
  "mipsle_softfloat",
  "mips64",
  "mips64le",
  "loong64",
  "ppc64",
  "ppc64le",
  "riscv64",
  "s390x",
];

export default function Platforms() {
  const platforms = [
    {
      name: "OpenWrt",
      detail: translate({
        id: "home.platforms.openwrt",
        message: "18.06 or newer",
      }),
      to: "/docs/install/openwrt",
    },
    {
      name: "ASUS Merlin",
      detail: translate({
        id: "home.platforms.merlin",
        message: "Needs Entware on a USB drive",
      }),
      to: "/docs/install/merlin",
    },
    {
      name: "Keenetic",
      detail: translate({
        id: "home.platforms.keenetic",
        message: "Needs Entware and the Netfilter components",
      }),
      to: "/docs/install/keenetic",
    },
    {
      name: "MikroTik",
      detail: translate({
        id: "home.platforms.mikrotik",
        message: "RouterOS 7.21.1+, as a container",
      }),
      to: "/docs/install/mikrotik",
    },
    {
      name: "Docker",
      detail: translate({
        id: "home.platforms.docker",
        message: "Linux hosts only, host networking",
      }),
      to: "/docs/install/docker",
    },
    {
      name: "Linux",
      detail: translate({
        id: "home.platforms.linux",
        message: "systemd, OpenRC or SysV init",
      }),
      to: "/docs/install/linux",
    },
  ];

  return (
    <section className={shared.section} aria-labelledby="platforms-title">
      <div className={shared.container}>
        <Reveal as="header" className={shared.head}>
          <h2 id="platforms-title" className={shared.h2}>
            <Translate id="home.platforms.title">Supported platforms</Translate>
          </h2>
          <p className={shared.lead}>
            <Translate id="home.platforms.lead">
              OpenWrt, ASUS Merlin, Keenetic and other Linux systems use the
              same install script. On MikroTik and in Docker, b4 runs as a
              container.
            </Translate>
          </p>
        </Reveal>

        <Reveal as="ul" className={styles.grid}>
          {platforms.map((platform) => (
            <li key={platform.name}>
              <Link to={platform.to} className={styles.card}>
                <span className={styles.name}>{platform.name}</span>
                <span className={styles.detail}>{platform.detail}</span>
                <ArrowUpRightIcon
                  className={styles.arrow}
                  size={20}
                  weight="bold"
                  aria-hidden="true"
                />
              </Link>
            </li>
          ))}
        </Reveal>

        <Reveal className={styles.footer}>
          <div className={styles.archs}>
            <p className={styles.archTitle}>
              <Translate id="home.platforms.archs">
                Builds for 17 CPU architectures
              </Translate>
            </p>
            <ul className={styles.archList}>
              {ARCHITECTURES.map((arch) => (
                <li key={arch} className={styles.arch}>
                  {arch}
                </li>
              ))}
            </ul>
          </div>
          <p className={styles.requirements}>
            <Translate id="home.platforms.requirements">
              Needs kernel 3.13 or newer, about 64 MB of free RAM, 30 MB of disk
              and root.
            </Translate>{" "}
            <Link to="/docs/intro#requirements" className={shared.textLink}>
              <Translate id="home.platforms.requirements.link">
                Full requirements
              </Translate>
            </Link>
          </p>
        </Reveal>
      </div>
    </section>
  );
}
