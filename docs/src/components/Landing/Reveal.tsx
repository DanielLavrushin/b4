import { createElement, useEffect, useRef } from "react";
import type { CSSProperties, HTMLAttributes, ReactNode } from "react";
import clsx from "clsx";
import shared from "./landing.module.css";

type RevealTag = "div" | "header" | "article" | "figure" | "li" | "ul" | "ol";

type RevealProps = HTMLAttributes<HTMLElement> & {
  as?: RevealTag;
  delay?: number;
  children: ReactNode;
};

function useReveal<T extends HTMLElement>() {
  const ref = useRef<T>(null);

  useEffect(() => {
    const element = ref.current;
    if (!element || !("IntersectionObserver" in window)) return;
    if (element.getBoundingClientRect().top < window.innerHeight * 0.9) return;

    element.dataset.reveal = "hidden";
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        element.dataset.reveal = "shown";
        observer.disconnect();
      },
      { rootMargin: "0px 0px -12% 0px" },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return ref;
}

export default function Reveal({
  as = "div",
  delay = 0,
  className,
  style,
  children,
  ...rest
}: RevealProps) {
  const ref = useReveal<HTMLElement>();
  const revealStyle = { ...style, "--reveal-delay": `${delay}ms` } as CSSProperties;
  return createElement(
    as,
    { ...rest, ref, className: clsx(shared.reveal, className), style: revealStyle },
    children,
  );
}
