import { useEffect, useState } from "react";

export function useGeoCategories(endpoint: string, enabled: boolean) {
  const [categories, setCategories] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      try {
        const response = await fetch(endpoint);
        if (response.ok) {
          const data = (await response.json()) as { tags: string[] };
          if (!cancelled) setCategories(data.tags || []);
        }
      } catch (error) {
        console.error(`Failed to load categories from ${endpoint}:`, error);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [endpoint, enabled]);

  return { categories, loading };
}
