export const qk = {
  session: ["session"] as const,
  overview: ["overview"] as const,
  buildStatus: ["build-status"] as const,
  builds: {
    all: ["builds"] as const,
    list: (only: string) => ["builds", "list", only] as const,
  },
  counts: ["counts"] as const,
  sets: {
    all: ["sets"] as const,
    list: ["sets", "list"] as const,
    queue: ["sets", "queue"] as const,
    rows: (params: Record<string, unknown>) => ["sets", "rows", params] as const,
    detail: (id: string) => ["sets", "detail", id] as const,
  },
  preview: {
    all: ["preview"] as const,
  },
  keys: {
    all: ["keys"] as const,
    list: ["keys", "list"] as const,
    rows: (params: Record<string, unknown>) => ["keys", "rows", params] as const,
    detail: (key: string) => ["keys", "detail", key] as const,
    notable: ["keys", "notable"] as const,
    impact: (key: string) => ["keys", "impact", key] as const,
  },
  votes: {
    all: ["votes"] as const,
    list: (params: Record<string, unknown>) => ["votes", "list", params] as const,
  },
  mirrors: {
    all: ["mirrors"] as const,
  },
  feedback: {
    all: ["feedback"] as const,
    list: (limit: number) => ["feedback", "list", limit] as const,
  },
  reports: {
    all: ["reports"] as const,
    list: (filter: Record<string, unknown>) => ["reports", "list", filter] as const,
  },
  audit: {
    all: ["audit"] as const,
    list: (filter: Record<string, unknown>) => ["audit", "list", filter] as const,
  },
  health: ["health"] as const,
  stats: (days: number) => ["stats", days] as const,
  reasons: ["reasons"] as const,
  settings: ["settings"] as const,
  notify: ["notify"] as const,
};
