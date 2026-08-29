import { useState, useEffect, useCallback } from "react";

export interface GitHubRelease {
  tag_name: string;
  name: string;
  body: string;
  html_url: string;
  published_at: string;
  prerelease: boolean;
}

interface UseGitHubReleaseResult {
  releases: GitHubRelease[];
  latestRelease: GitHubRelease | null;
  isNewVersionAvailable: boolean;
  isLoading: boolean;
  error: string | null;
  currentVersion: string;
  includePrerelease: boolean;
  setIncludePrerelease: (include: boolean) => void;
}

const RELEASES_URL = "/api/system/releases";
const DISMISSED_VERSIONS_KEY = "b4_dismissed_versions";
const INCLUDE_PRERELEASE_KEY = "b4_include_prerelease";

export const compareVersions = (v1: string, v2: string): number => {
  const normalize = (v: string) =>
    v
      .replace(/^v/, "")
      .replace(/-(alpha|beta|rc|dev).*$/i, "")
      .split(".")
      .map((s) => {
        const n = Number.parseInt(s, 10);
        return Number.isNaN(n) ? 0 : n;
      });
  const preTag = (v: string) => {
    const m = /-?(alpha|beta|rc|dev)/i.exec(v.replace(/^v/, ""));
    return m ? m[1].toLowerCase() : "";
  };
  const [a, b] = [normalize(v1), normalize(v2)];
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    if ((a[i] || 0) > (b[i] || 0)) return 1;
    if ((a[i] || 0) < (b[i] || 0)) return -1;
  }
  const [pa, pb] = [preTag(v1), preTag(v2)];
  if (pa && !pb) return -1;
  if (!pa && pb) return 1;
  return 0;
};

const isVersionLower = (version1: string, version2: string): boolean => {
  if (version1.replace(/^v/, "") === "dev") return true;
  return compareVersions(version1, version2) < 0;
};

const getDismissedVersions = (): string[] => {
  try {
    const dismissed = localStorage.getItem(DISMISSED_VERSIONS_KEY);
    return dismissed ? (JSON.parse(dismissed) as string[]) : [];
  } catch {
    return [];
  }
};

export const isVersionDismissed = (version: string): boolean => {
  return getDismissedVersions().includes(version);
};

export const dismissVersion = (version: string): void => {
  try {
    const dismissed = getDismissedVersions();
    if (!dismissed.includes(version)) {
      dismissed.push(version);
      localStorage.setItem(DISMISSED_VERSIONS_KEY, JSON.stringify(dismissed));
    }
  } catch (error) {
    console.error("Failed to save dismissed version:", error);
  }
};

export const useGitHubRelease = (): UseGitHubReleaseResult => {
  const [allReleases, setAllReleases] = useState<GitHubRelease[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [includePrerelease, setIncludePrerelease] = useState<boolean>(() => {
    try {
      return localStorage.getItem(INCLUDE_PRERELEASE_KEY) === "true";
    } catch {
      return false;
    }
  });

  const currentVersion = import.meta.env.VITE_APP_VERSION || "dev";

  const togglePrerelease = useCallback((include: boolean) => {
    setIncludePrerelease(include);
    try {
      localStorage.setItem(INCLUDE_PRERELEASE_KEY, String(include));
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    const fetchReleases = async () => {
      try {
        setIsLoading(true);
        setError(null);

        const response = await fetch(RELEASES_URL);

        if (!response.ok) {
          throw new Error(`Release list unavailable (${response.status})`);
        }

        const data = (await response.json()) as GitHubRelease[];
        setAllReleases(data);
      } catch (err) {
        console.error("Failed to fetch releases:", err);
        setError(err instanceof Error ? err.message : "Unknown error");
      } finally {
        setIsLoading(false);
      }
    };

    void fetchReleases();
    const interval = setInterval(
      () => void fetchReleases(),
      6 * 60 * 60 * 1000,
    );
    return () => clearInterval(interval);
  }, []);

  const releases = includePrerelease
    ? allReleases
    : allReleases.filter((r) => !r.prerelease);

  const latestRelease = releases.length > 0 ? releases[0] : null;

  const latestStableRelease = allReleases.find((r) => !r.prerelease) || null;

  const isNewVersionAvailable =
    latestStableRelease !== null &&
    isVersionLower(currentVersion, latestStableRelease.tag_name) &&
    !isVersionDismissed(latestStableRelease.tag_name);
  return {
    releases,
    latestRelease,
    isNewVersionAvailable,
    isLoading,
    error,
    currentVersion,
    includePrerelease,
    setIncludePrerelease: togglePrerelease,
  };
};
