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

const GITHUB_REPO = "DanielLavrushin/b4";
const GITHUB_API_URL = `https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=15`;
const DISMISSED_VERSIONS_KEY = "b4_dismissed_versions";
const INCLUDE_PRERELEASE_KEY = "b4_include_prerelease";

export const compareVersions = (v1: string, v2: string): number => {
  const normalize = (v: string) => v.replace(/^v/, "").split(".").map(Number);
  const [a, b] = [normalize(v1), normalize(v2)];
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    if ((a[i] || 0) > (b[i] || 0)) return 1;
    if ((a[i] || 0) < (b[i] || 0)) return -1;
  }
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
  const [includePrerelease, setIncludePrereleaseState] = useState<boolean>(
    () => {
      try {
        return localStorage.getItem(INCLUDE_PRERELEASE_KEY) === "true";
      } catch {
        return false;
      }
    },
  );

  const currentVersion = import.meta.env.VITE_APP_VERSION || "dev";

  const setIncludePrerelease = useCallback((include: boolean) => {
    setIncludePrereleaseState(include);
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

        const response = await fetch(GITHUB_API_URL, {
          headers: { Accept: "application/vnd.github.v3+json" },
        });

        if (!response.ok) {
          throw new Error(`GitHub API returned ${response.status}`);
        }

        const data = (await response.json()) as GitHubRelease[];
        setAllReleases(data.slice(0, 10));
      } catch (err) {
        console.error("Failed to fetch GitHub releases:", err);
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
    setIncludePrerelease,
  };
};
