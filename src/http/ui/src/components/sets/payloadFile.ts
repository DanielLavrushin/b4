import { useEffect, useState } from "react";
import { Capture, useCaptures } from "@b4.capture";

export type ChosenPayloadFile =
  | { state: "none" }
  | { state: "unknown"; name: string }
  | { state: "missing"; name: string }
  | { state: "listed"; capture: Capture };

export const samePayloadFile = (a: string, b: string) =>
  a.replace(/^\.\//, "") === b.replace(/^\.\//, "");

export const payloadFileName = (path: string) =>
  path.split(/[\\/]/).pop() ?? path;

export function chosenPayloadFile(
  captures: Capture[],
  listed: boolean,
  file: string,
): ChosenPayloadFile {
  if (!file) return { state: "none" };
  const capture = captures.find((c) => samePayloadFile(c.filepath, file));
  if (capture && !capture.missing) return { state: "listed", capture };
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
