import { useEffect, useState } from "react";
import clsx from "clsx";
import { translate } from "@docusaurus/Translate";
import { CheckIcon } from "@phosphor-icons/react/dist/csr/Check";
import { CopyIcon } from "@phosphor-icons/react/dist/csr/Copy";
import styles from "./InstallCommand.module.css";

const INSTALL_URL = "https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh";
const COMMAND = `curl -fsSL ${INSTALL_URL} | sh`;

type CopyState = "idle" | "copied" | "failed";

function hint(state: CopyState) {
  if (state === "copied") {
    return translate({
      id: "home.install.copiedHint",
      message: "Copied. Paste it into an SSH session on the router.",
    });
  }
  if (state === "failed") {
    return translate({
      id: "home.install.failed",
      message: "Copy failed. Select the command and copy it by hand.",
    });
  }
  return translate({ id: "home.install.hint", message: "Run it over SSH as root." });
}

export default function InstallCommand({ className }: { className?: string }) {
  const [state, setState] = useState<CopyState>("idle");

  useEffect(() => {
    if (state !== "copied") return;
    const timer = window.setTimeout(() => setState("idle"), 2400);
    return () => window.clearTimeout(timer);
  }, [state]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(COMMAND);
      setState("copied");
    } catch {
      setState("failed");
    }
  };

  const copied = state === "copied";

  return (
    <div className={clsx(styles.wrap, className)}>
      <div className={styles.command}>
        <code
          className={styles.text}
          tabIndex={0}
          aria-label={translate({ id: "home.install.label", message: "Install command" })}
        >
          <span className={styles.tool}>curl</span> <span className={styles.flag}>-fsSL</span>{" "}
          <span className={styles.url}>{INSTALL_URL}</span> <span className={styles.flag}>|</span>{" "}
          <span className={styles.tool}>sh</span>
        </code>
        <button
          type="button"
          className={clsx(styles.copy, copied && styles.copied)}
          onClick={copy}
        >
          <span className={styles.icons} aria-hidden="true">
            <CopyIcon className={styles.iconCopy} size={16} weight="bold" />
            <CheckIcon className={styles.iconCheck} size={16} weight="bold" />
          </span>
          {copied
            ? translate({ id: "home.install.copied", message: "Copied" })
            : translate({ id: "home.install.copy", message: "Copy" })}
        </button>
      </div>
      <p className={clsx(styles.hint, state === "failed" && styles.hintError)} role="status">
        {hint(state)}
      </p>
    </div>
  );
}
