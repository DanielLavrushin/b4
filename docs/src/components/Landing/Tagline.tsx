import { translate } from "@docusaurus/Translate";
import { lines } from "./text";
import shared from "./landing.module.css";
import styles from "./Tagline.module.css";

export default function Tagline() {
  const text = translate({
    id: "home.tagline",
    message: "Sites that aren’t in a set\nwork exactly as they did\nbefore b4 was installed.",
  });

  return (
    <section className={styles.section}>
      <div className={shared.container}>
        <p className={styles.tagline}>{lines(text).join(" ")}</p>
      </div>
    </section>
  );
}
