import { useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { ApiError } from "@api/apiClient";
import { captureApi } from "@api/capture";
import {
  Capture,
  PayloadFacts,
  PayloadFileResolution,
  useCaptures,
} from "@b4.capture";
import { bytesLabel, kindLabel } from "@components/settings/payloads/facts";

export type ChosenPayloadFile =
  | { state: "none" }
  | { state: "unknown"; name: string }
  | { state: "missing"; name: string }
  | { state: "empty"; name: string; capture?: Capture }
  | {
      state: "unlisted";
      name: string;
      size: number;
      facts?: PayloadFacts;
      tooLarge: boolean;
    }
  | { state: "listed"; capture: Capture };

export interface ResolvedPayloadFile {
  file: string;
  answer: PayloadFileResolution | null;
}

export const isEmptyPayload = (capture: Capture) =>
  (capture.warnings ?? []).includes("empty");

export const samePayloadFile = (a: string, b: string) =>
  a.replace(/^\.\//, "") === b.replace(/^\.\//, "");

export const payloadFileName = (path: string) =>
  path.split(/[\\/]/).pop() ?? path;

export const byPayloadName = (a: Capture, b: Capture) =>
  a.domain.localeCompare(b.domain);

export function payloadNote(
  t: TFunction,
  capture: Pick<Capture, "origin" | "facts" | "size">,
): string {
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
  resolved?: ResolvedPayloadFile,
): ChosenPayloadFile {
  if (!file) return { state: "none" };
  const name = payloadFileName(file);
  const answer = resolved?.file === file ? resolved.answer : undefined;
  const capture =
    captures.find((c) => samePayloadFile(c.filepath, file)) ??
    (answer
      ? captures.find((c) => samePayloadFile(c.filepath, answer.file))
      : undefined);
  if (capture && !capture.missing) {
    return isEmptyPayload(capture)
      ? { state: "empty", name: capture.domain, capture }
      : { state: "listed", capture };
  }
  if (!listed) return { state: "unknown", name };
  if (capture) return { state: "missing", name: capture.domain };
  if (answer === undefined) return { state: "unknown", name };
  if (!answer?.exists) return { state: "missing", name };
  if (answer.size === 0) return { state: "empty", name };
  return {
    state: "unlisted",
    name,
    size: answer.size,
    facts: answer.facts ?? undefined,
    tooLarge: answer.too_large,
  };
}

export function useResolvedPayloadFile(
  captures: Capture[],
  listed: boolean,
  file: string,
): ResolvedPayloadFile | undefined {
  const unmatched =
    listed &&
    !!file &&
    !captures.some((c) => samePayloadFile(c.filepath, file));
  const [resolved, setResolved] = useState<ResolvedPayloadFile>();

  useEffect(() => {
    if (!unmatched) return;
    let current = true;
    captureApi.resolve(file).then(
      (answer) => {
        if (current) setResolved({ file, answer });
      },
      (error: unknown) => {
        if (current && error instanceof ApiError && error.status === 400) {
          setResolved({ file, answer: null });
        }
      },
    );
    return () => {
      current = false;
    };
  }, [unmatched, file]);

  return resolved?.file === file ? resolved : undefined;
}

export function useListedCaptures() {
  const { captures, listError, refreshCaptures } = useCaptures();
  const [listed, setListed] = useState(false);

  useEffect(() => {
    refreshCaptures().then(
      () => setListed(true),
      () => {},
    );
  }, [refreshCaptures]);

  return { captures, listed, listError };
}
