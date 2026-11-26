import { B4SetConfig, FakingPayloadType } from "@models/config";

export function payloadBaseName(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}

export function collectPayloadRefs(cfg: B4SetConfig): string[] {
  const refs: string[] = [];
  if (
    cfg.faking?.sni_type === FakingPayloadType.CAPTURE &&
    cfg.faking.payload_file
  ) {
    refs.push(cfg.faking.payload_file);
  }
  const udpFile = cfg.udp?.fake_payload_file ?? "";
  if (cfg.udp?.mode === "fake" && udpFile && !udpFile.startsWith("@")) {
    refs.push(udpFile);
  }
  return [...new Set(refs)];
}

export function missingPayloadRefs(
  sets: B4SetConfig[],
  files: string[],
): string[] {
  const available = new Set(files.map(payloadBaseName));
  return [...new Set(sets.flatMap(collectPayloadRefs))].filter(
    (ref) => !available.has(payloadBaseName(ref)),
  );
}
