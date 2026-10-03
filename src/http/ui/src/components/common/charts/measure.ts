import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import { fonts } from "@design";
import { AXIS_FONT_PX } from "./geometry";

const widths = new Map<string, number>();
let context: CanvasRenderingContext2D | null | undefined;

function canvasContext(): CanvasRenderingContext2D | null {
  if (context !== undefined) return context;
  try {
    context =
      typeof document === "undefined"
        ? null
        : document.createElement("canvas").getContext("2d");
  } catch {
    context = null;
  }
  return context;
}

export function measureText(
  text: string,
  fontPx: number = AXIS_FONT_PX,
  family: string = fonts.mono,
): number {
  const key = `${fontPx}|${family}|${text}`;
  const cached = widths.get(key);
  if (cached !== undefined) return cached;
  const ctx = canvasContext();
  let width = text.length * fontPx * 0.62;
  if (ctx) {
    ctx.font = `${fontPx}px ${family}`;
    width = ctx.measureText(text).width;
  }
  if (widths.size > 512) widths.clear();
  widths.set(key, width);
  return width;
}

export function useElementWidth<T extends HTMLElement>(): [
  RefObject<T | null>,
  number,
] {
  const ref = useRef<T | null>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const read = () => {
      const next = Math.floor(element.clientWidth);
      setWidth((prev) => (prev === next ? prev : next));
    };
    read();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(read);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return [ref, width];
}
