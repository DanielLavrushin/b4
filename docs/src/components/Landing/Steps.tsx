import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import { ArrowRightIcon } from "@phosphor-icons/react/dist/csr/ArrowRight";
import Reveal from "./Reveal";
import { rich } from "./text";
import shared from "./landing.module.css";
import styles from "./Steps.module.css";

export default function Steps() {
  const steps = [
    {
      title: translate({ id: "home.steps.install.title", message: "Install" }),
      text: translate({
        id: "home.steps.install.text",
        message:
          "Run the install command over SSH as root. It detects the platform and the CPU architecture.",
      }),
      to: "/docs/install/",
      link: translate({ id: "home.steps.install.link", message: "Installation guide" }),
    },
    {
      title: translate({ id: "home.steps.discover.title", message: "Find a strategy" }),
      text: translate({
        id: "home.steps.discover.text",
        message:
          "Open the web interface on port 7000, go to **Discovery**, enter a site that doesn't open and press **Start**.",
      }),
      to: "/docs/quickstart",
      link: translate({ id: "home.steps.discover.link", message: "First steps" }),
    },
    {
      title: translate({ id: "home.steps.apply.title", message: "Apply it" }),
      text: translate({
        id: "home.steps.apply.text",
        message:
          "Press **Apply as a set** on the result. The **Traffic** page then shows the set's name next to that site's connections.",
      }),
      to: "/docs/sets/",
      link: translate({ id: "home.steps.apply.link", message: "Sets" }),
    },
  ];

  return (
    <section className={shared.section} aria-labelledby="steps-title">
      <div className={shared.container}>
        <Reveal as="header" className={shared.head}>
          <h2 id="steps-title" className={shared.h2}>
            <Translate id="home.steps.title">Getting started</Translate>
          </h2>
        </Reveal>
        <ol className={styles.steps}>
          {steps.map((step, index) => (
            <Reveal as="li" key={step.to} className={styles.step} delay={index * 80}>
              <span className={styles.number} aria-hidden="true">
                {index + 1}
              </span>
              <h3 className={styles.title}>{step.title}</h3>
              <p className={styles.text}>{rich(step.text)}</p>
              <Link to={step.to} className={shared.textLink}>
                {step.link}
                <ArrowRightIcon className={shared.btnIcon} size={16} weight="bold" aria-hidden="true" />
              </Link>
            </Reveal>
          ))}
        </ol>
      </div>
    </section>
  );
}
