import { captureApi } from "@api/capture";
import { Capture } from "@b4.capture";
import { useCallback, useState } from "react";

export function useCaptures() {
  const [captures, setCaptures] = useState<Capture[]>([]);
  const [loading, setLoading] = useState(false);

  const loadCaptures = useCallback(async () => {
    try {
      setLoading(true);
      const list = await captureApi.list();
      setCaptures(list);
      return list;
    } catch (e) {
      console.error("Failed to load captures:", e);
      return [];
    } finally {
      setLoading(false);
    }
  }, []);

  const generate = useCallback(
    async (domain: string, protocol: string) => {
      setLoading(true);
      try {
        const result = await captureApi.generate(domain, protocol);

        await loadCaptures();

        return result;
      } finally {
        setLoading(false);
      }
    },
    [loadCaptures],
  );

  const probe = useCallback(async (domain: string, protocol: string) => {
    setLoading(true);
    try {
      const result = await captureApi.probe(domain, protocol);

      if (result.already_captured) {
        return result;
      }

      const normalizedDomain = domain.toLowerCase().trim();
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        const list = await captureApi.list();
        const found = list.some(
          (c) =>
            c.domain === normalizedDomain &&
            (protocol === "both" || c.protocol === protocol),
        );
        if (found) {
          setCaptures(list);
          return result;
        }
      }

      return result;
    } finally {
      setLoading(false);
    }
  }, []);

  const deleteCapture = useCallback(
    async (protocol: string, domain: string) => {
      await captureApi.delete(protocol, domain);
      await loadCaptures();
    },
    [loadCaptures],
  );

  const clearAll = useCallback(async () => {
    await captureApi.clear();
    await loadCaptures();
  }, [loadCaptures]);

  const upload = useCallback(
    async (file: File, domain: string, protocol: string) => {
      setLoading(true);
      try {
        const result = await captureApi.upload(file, domain, protocol);
        await loadCaptures();
        return result;
      } finally {
        setLoading(false);
      }
    },
    [loadCaptures],
  );

  const download = useCallback((capture: Capture) => {
    const url = `/api/capture/download?file=${encodeURIComponent(
      capture.filepath,
    )}`;
    const link = document.createElement("a");
    link.href = url;
    link.download = `tls_${capture.domain.replace(/\./g, "_")}.bin`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  }, []);

  return {
    captures,
    loading,
    loadCaptures,
    generate,
    probe,
    deleteCapture,
    clearAll,
    upload,
    download,
  };
}
