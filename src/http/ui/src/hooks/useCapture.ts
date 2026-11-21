import { captureApi } from "@api/capture";
import { Capture } from "@b4.capture";
import { describeApiError } from "@utils";
import { useCallback, useState } from "react";

export function useCaptures() {
  const [captures, setCaptures] = useState<Capture[]>([]);
  const [loading, setLoading] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [regenerating, setRegenerating] = useState(false);

  const refreshCaptures = useCallback(async () => {
    setLoading(true);
    try {
      const list = await captureApi.list();
      setCaptures(list);
      setListError(null);
      return list;
    } catch (e) {
      setListError(describeApiError(e));
      throw e;
    } finally {
      setLoading(false);
    }
  }, []);

  const loadCaptures = useCallback(async (): Promise<Capture[]> => {
    try {
      return await refreshCaptures();
    } catch {
      return [];
    }
  }, [refreshCaptures]);

  const generate = useCallback(
    async (domain: string, protocol: string) => {
      setGenerating(true);
      try {
        const result = await captureApi.generate(domain, protocol);

        await loadCaptures();

        return result;
      } finally {
        setGenerating(false);
      }
    },
    [loadCaptures],
  );

  const regenerate = useCallback(
    async (domain: string) => {
      setRegenerating(true);
      try {
        const result = await captureApi.regenerate(domain);
        await loadCaptures();
        return result;
      } finally {
        setRegenerating(false);
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

  const inspect = useCallback(
    (file: File) => captureApi.inspect(file),
    [],
  );

  const upload = useCallback(
    async (file: File, domain: string, protocol: string, replace = false) => {
      setUploading(true);
      try {
        const result = await captureApi.upload(file, domain, protocol, replace);
        await loadCaptures();
        return result;
      } finally {
        setUploading(false);
      }
    },
    [loadCaptures],
  );

  const readBytes = useCallback(
    (capture: Capture) => captureApi.bytes(capture),
    [],
  );

  const download = useCallback(async (capture: Capture) => {
    const bytes = await captureApi.bytes(capture);
    saveBytes(
      bytes,
      `${capture.protocol}_${capture.domain.replaceAll(".", "_")}.bin`,
    );
  }, []);

  return {
    captures,
    loading,
    listError,
    uploading,
    generating,
    regenerating,
    loadCaptures,
    refreshCaptures,
    generate,
    regenerate,
    deleteCapture,
    clearAll,
    inspect,
    upload,
    readBytes,
    download,
  };
}

function saveBytes(bytes: Uint8Array<ArrayBuffer>, filename: string) {
  const blobUrl = URL.createObjectURL(new Blob([bytes]));
  const link = document.createElement("a");
  link.href = blobUrl;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(blobUrl);
}
