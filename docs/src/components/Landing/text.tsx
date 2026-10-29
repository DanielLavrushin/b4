import type { ReactNode } from "react";

const MARKUP = /(\*\*[^*]+\*\*|`[^`]+`)/g;

export function rich(text: string): ReactNode[] {
  return text
    .split(MARKUP)
    .filter(Boolean)
    .map((part, index) => {
      if (part.startsWith("**")) return <strong key={index}>{part.slice(2, -2)}</strong>;
      if (part.startsWith("`")) return <code key={index}>{part.slice(1, -1)}</code>;
      return part;
    });
}

export function plain(text: string): string {
  return text.replace(/\*\*|`/g, "");
}

export function lines(text: string): string[] {
  return text.split("\n");
}
