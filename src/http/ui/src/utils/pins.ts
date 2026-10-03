const isAddress = (token: string) =>
  /^\d{1,3}(\.\d{1,3}){3}$/.test(token) ||
  /^[0-9a-f:]+:[0-9a-f:]*$/i.test(token);

export const pinsToText = (pins?: Record<string, string[]>) => {
  const byAddress = new Map<string, string[]>();
  for (const [domain, addresses] of Object.entries(pins ?? {})) {
    for (const address of addresses) {
      byAddress.set(address, [...(byAddress.get(address) ?? []), domain]);
    }
  }
  return [...byAddress]
    .map(([address, domains]) => `${address} ${domains.join(" ")}`)
    .join("\n");
};

export const parsePins = (text: string) => {
  const parsed: Record<string, string[]> = {};
  for (const line of text.split("\n")) {
    const tokens = line
      .trim()
      .split(/[\s,=]+/)
      .filter(Boolean);
    const addresses = tokens.filter(isAddress);
    const domains = tokens
      .filter((token) => !isAddress(token))
      .map((token) => token.toLowerCase().replace(/^\*\./, ""));
    if (addresses.length === 0 || domains.length === 0) continue;
    for (const domain of domains) {
      parsed[domain] = [...new Set([...(parsed[domain] ?? []), ...addresses])];
    }
  }
  return parsed;
};
