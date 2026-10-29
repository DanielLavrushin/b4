import { useEffect, useRef } from "react";
import { translate } from "@docusaurus/Translate";
import { lines } from "./text";
import shared from "./landing.module.css";
import styles from "./Tagline.module.css";

const STEP_MS = 40;

function useWordReveal() {
  const ref = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    const element = ref.current;
    if (!element || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const words = Array.from(element.querySelectorAll<HTMLElement>("[data-word]"));
    let lit = 0;
    let frame = 0;

    function update() {
      frame = 0;
      const rect = element!.getBoundingClientRect();
      const start = window.innerHeight * 0.85;
      const end = window.innerHeight * 0.4;
      const progress = (start - rect.top) / (start - end + rect.height);
      const target = Math.min(words.length, Math.max(0, Math.ceil(progress * words.length)));
      for (let index = lit; index < target; index += 1) {
        words[index].style.transitionDelay = `${(index - lit) * STEP_MS}ms`;
        words[index].dataset.on = "true";
      }
      lit = Math.max(lit, target);
      if (lit === words.length) stop();
    }

    function schedule() {
      if (!frame) frame = window.requestAnimationFrame(update);
    }

    function stop() {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    }

    element.dataset.armed = "true";
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    schedule();
    return stop;
  }, []);

  return ref;
}

export default function Tagline() {
  const ref = useWordReveal();
  const text = translate({
    id: "home.tagline",
    message: "Sites that aren't in a set\nwork exactly as they did\nbefore b4 was installed.",
  });

  return (
    <section className={styles.section}>
      <div className={shared.container}>
        <p ref={ref} className={styles.tagline}>
          <span className={styles.readable}>{text.replace(/\n/g, " ")}</span>
          <span aria-hidden="true">
            {lines(text).map((line, lineIndex) => (
              <span key={lineIndex} className={styles.line}>
                {line.split(" ").map((word, wordIndex) => (
                  <span key={wordIndex} data-word="" className={styles.word}>
                    {word}{" "}
                  </span>
                ))}
              </span>
            ))}
          </span>
        </p>
      </div>
    </section>
  );
}
