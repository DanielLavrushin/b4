import type { DetectorTestType } from "@models/detector";
import type { TFunction } from "i18next";

export function getTestName(t: TFunction, test: DetectorTestType): string {
  return t(`detector.tests.names.${test}`);
}

export function getTestDescription(t: TFunction, test: DetectorTestType): string {
  return t(`detector.tests.descriptions.${test}`);
}

export const testSequence: DetectorTestType[] = [
  "dns",
  "dns-availability",
  "domains",
  "tcp",
  "sni",
  "telegram",
];

export const staggerContainer = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.1 },
  },
};

export const staggerItem = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.3, ease: "easeOut" as const },
  },
};

export const statusColors = {
  ok: "#4caf50",
  error: "#f44336",
  warning: "#F5AD18",
} as const;
