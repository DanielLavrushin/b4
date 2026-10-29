import clsx from "clsx";
import Head from "@docusaurus/Head";
import Link from "@docusaurus/Link";
import Translate, { translate } from "@docusaurus/Translate";
import { PlusIcon } from "@phosphor-icons/react/dist/csr/Plus";
import Reveal from "./Reveal";
import { plain, rich } from "./text";
import shared from "./landing.module.css";
import styles from "./Faq.module.css";

function useQuestions() {
  return [
    {
      question: translate({ id: "home.faq.vpn.q", message: "Is b4 a VPN?" }),
      answer: translate({
        id: "home.faq.vpn.a",
        message:
          "No. Traffic still goes straight to the site. b4 only changes how the first packets of each connection look to the provider's DPI. If some sites need a VPN, a route set can send them through one that's already running on the router.",
      }),
    },
    {
      question: translate({ id: "home.faq.where.q", message: "Where does it run?" }),
      answer: translate({
        id: "home.faq.where.a",
        message:
          "OpenWrt 19.07+, ASUS Merlin, Keenetic, MikroTik with RouterOS 7.21.1+ (as a container), Linux computers and servers, and Docker on Linux. There are builds for 17 CPU architectures.",
      }),
    },
    {
      question: translate({ id: "home.faq.needs.q", message: "What does the router need?" }),
      answer: translate({
        id: "home.faq.needs.a",
        message:
          "Linux kernel 3.13 or newer with NFQUEUE support, or the TUN engine where NFQUEUE is missing. About 64 MB of free RAM, 30 MB of disk and root access.",
      }),
    },
    {
      question: translate({
        id: "home.faq.untouched.q",
        message: "Does b4 touch sites that aren't in a set?",
      }),
      answer: translate({
        id: "home.faq.untouched.a",
        message: "No. Connections that don't match any set pass through unchanged.",
      }),
    },
    {
      question: translate({ id: "home.faq.blocked.q", message: "What if a site still doesn't open?" }),
      answer: translate({
        id: "home.faq.blocked.a",
        message:
          "If Discovery reports **address blocked**, the provider blocks the site's IP addresses and changing packets won't help. Such a site needs a set that sends it through a VPN or a SOCKS5 proxy.",
      }),
    },
    {
      question: translate({
        id: "home.faq.changes.q",
        message: "What happens when the provider changes its blocking?",
      }),
      answer: translate({
        id: "home.faq.changes.a",
        message:
          "Turn on the watchdog. It re-checks the sites of selected sets on a schedule, and when they stop opening it runs Discovery again and switches the set to a new strategy.",
      }),
    },
    {
      question: translate({ id: "home.faq.telegram.q", message: "Does it help with Telegram?" }),
      answer: translate({
        id: "home.faq.telegram.a",
        message:
          "Yes. b4 has a built-in MTProto proxy, and a bridge that works with the regular Telegram apps.",
      }),
    },
    {
      question: translate({ id: "home.faq.restart.q", message: "Do changes need a restart?" }),
      answer: translate({
        id: "home.faq.restart.a",
        message:
          "Changes to sets don't. They apply to the running service as soon as they're saved.",
      }),
    },
    {
      question: translate({ id: "home.faq.ads.q", message: "Can it block ads?" }),
      answer: translate({
        id: "home.faq.ads.a",
        message: "Yes. A block set blocks ads and unwanted sites for the whole network.",
      }),
    },
    {
      question: translate({ id: "home.faq.remove.q", message: "How do I remove it?" }),
      answer: translate({
        id: "home.faq.remove.a",
        message:
          "Run the installer with `--remove`. It stops b4, removes its firewall and routing rules and the service, and asks before deleting the config.",
      }),
    },
    {
      question: translate({ id: "home.faq.free.q", message: "Is it free?" }),
      answer: translate({
        id: "home.faq.free.a",
        message: "Yes. It's open source under GPLv3, and the code is on GitHub.",
      }),
    },
  ];
}

function FaqSchema({ questions }: { questions: ReturnType<typeof useQuestions> }) {
  const schema = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: questions.map(({ question, answer }) => ({
      "@type": "Question",
      name: question,
      acceptedAnswer: { "@type": "Answer", text: plain(answer) },
    })),
  };
  return (
    <Head>
      <script type="application/ld+json">{JSON.stringify(schema)}</script>
    </Head>
  );
}

export default function Faq() {
  const questions = useQuestions();

  return (
    <section className={shared.section} aria-labelledby="faq-title">
      <FaqSchema questions={questions} />
      <div className={clsx(shared.container, styles.grid)}>
        <Reveal as="header" className={styles.head}>
          <h2 id="faq-title" className={shared.h2}>
            <Translate id="home.faq.title">Common questions</Translate>
          </h2>
          <p className={shared.lead}>
            <Translate
              id="home.faq.lead"
              values={{
                link: (
                  <Link to="/docs/troubleshooting/" className={styles.leadLink}>
                    <Translate id="home.faq.lead.link">Troubleshooting</Translate>
                  </Link>
                ),
              }}
            >
              {"Something not working after install? See {link}."}
            </Translate>
          </p>
        </Reveal>
        <Reveal className={styles.list}>
          {questions.map(({ question, answer }) => (
            <details key={question} className={styles.item}>
              <summary className={styles.question}>
                <span>{question}</span>
                <PlusIcon className={styles.icon} size={20} weight="bold" aria-hidden="true" />
              </summary>
              <p className={styles.answer}>{rich(answer)}</p>
            </details>
          ))}
        </Reveal>
      </div>
    </section>
  );
}
