import { useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { Capture, useCaptures } from "@b4.capture";
import { bytesLabel, kindLabel } from "@components/settings/payloads/facts";

export type ChosenPayloadFile =
  | { state: "none" }
  | { state: "unknown"; name: string }
  | { state: "missing"; name: string }
  | { state: "empty"; capture: Capture }
  | { state: "listed"; capture: Capture };

export const isEmptyPayload = (capture: Capture) =>
  (capture.warnings ?? []).includes("empty");

export const samePayloadFile = (a: string, b: string) =>
  a.replace(/^\.\//, "") === b.replace(/^\.\//, "");

export const payloadFileName = (path: string) =>
  path.split(/[\\/]/).pop() ?? path;

export const byPayloadName = (a: Capture, b: Capture) =>
  a.domain.localeCompare(b.domain);

export function payloadNote(t: TFunction, capture: Capture): string {
  const faults = capture.facts?.faults?.length ?? 0;
  return [
    capture.origin ? t(`settings.Capture.origins.${capture.origin}`) : "",
    capture.facts ? kindLabel(t, capture.facts) : "",
    bytesLabel(t, capture.size),
    faults > 0 ? t("sets.faking.fakeSni.payloadFaults", { count: faults }) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function chosenPayloadFile(
  captures: Capture[],
  listed: boolean,
  file: string,
): ChosenPayloadFile {
  if (!file) return { state: "none" };
  const capture = captures.find((c) => samePayloadFile(c.filepath, file));
  if (capture && !capture.missing) {
    return isEmptyPayload(capture)
      ? { state: "empty", capture }
      : { state: "listed", capture };
  }
  if (!listed) return { state: "unknown", name: payloadFileName(file) };
  return { state: "missing", name: capture?.domain ?? payloadFileName(file) };
}

export function useListedCaptures() {
  const { captures, refreshCaptures } = useCaptures();
  const [listed, setListed] = useState(false);

  useEffect(() => {
    refreshCaptures().then(
      () => setListed(true),
      () => {},
    );
  }, [refreshCaptures]);

  return { captures, listed };
}
