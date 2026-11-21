import { ApiError } from "@api/apiClient";
import { captureApi } from "@api/capture";
import { readErrorBody } from "@api/errorBody";
import { Capture } from "@b4.capture";
import { describeApiError } from "@utils";
import { useCallback, useState } from "react";

export function useCaptures() {
  const [captures, setCaptures] = useState<Capture[]>([]);
  const [loading, setLoading] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [generating, setGenerating] = useState(false);

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
      setUploading(true);
      try {
        const result = await captureApi.upload(file, domain, protocol);
        await loadCaptures();
        return result;
      } finally {
        setUploading(false);
      }
    },
    [loadCaptures],
  );

  const download = useCallback(async (capture: Capture) => {
    const filename = capture.filepath.split(/[\\/]/).pop() || capture.filepath;
    const url = `/api/capture/download?file=${encodeURIComponent(filename)}`;
    const response = await fetch(url);
    if (!response.ok) {
      throw new ApiError(
        url,
        response.status,
        response.statusText,
        await readErrorBody(response),
      );
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
    listError,
    uploading,
    generating,
    loadCaptures,
    refreshCaptures,
    generate,
    deleteCapture,
    clearAll,
    upload,
    download,
  };
}
