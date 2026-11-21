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

  const download = useCallback(async (capture: Capture) => {
    const filename = capture.filepath.split(/[\\/]/).pop() || capture.filepath;
    const url = `/api/capture/download?file=${encodeURIComponent(filename)}`;
    const response = await fetch(url);
    if (!response.ok) {
      throw new Error(`Download failed: ${response.statusText}`);
    }
    const blob = await response.blob();
    const blobUrl = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = blobUrl;
    link.download = `${capture.protocol}_${capture.domain.replaceAll(".", "_")}.bin`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(blobUrl);
  }, []);

  return {
    captures,
    loading,
    loadCaptures,
    generate,
    deleteCapture,
    clearAll,
    upload,
    download,
  };
}
