import { useState, useCallback } from "react";
import { B4SetConfig } from "@models/config";
import { ApiError, ApiResponse } from "@api/apiClient";
import { setsApi } from "@b4.sets";

export function useSets() {
  const [loading, setLoading] = useState(false);

  const createSet = useCallback(
    async (set: Omit<B4SetConfig, "id">): Promise<ApiResponse<B4SetConfig>> => {
      setLoading(true);
      try {
        const data = await setsApi.createSet(set);
        return { success: true, data };
      } catch (e) {
        if (e instanceof ApiError) {
          const msg = JSON.stringify(e.body ?? e.message);
          return { success: false, error: msg };
        }
        return { success: false, error: String(e) };
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  const updateSet = useCallback(
    async (set: B4SetConfig): Promise<ApiResponse<B4SetConfig>> => {
      setLoading(true);
      try {
        const data = await setsApi.updateSet(set.id, set);
        return { success: true, data };
      } catch (e) {
        if (e instanceof ApiError) {
          const msg = JSON.stringify(e.body ?? e.message);
          return { success: false, error: msg };
        }
        return { success: false, error: String(e) };
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  const deleteSet = useCallback(
    async (id: string): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.deleteSet(id);
        return { success: true };
      } catch (e) {
        if (e instanceof ApiError) {
          const msg = JSON.stringify(e.body ?? e.message);
          return { success: false, error: msg };
        }
        return { success: false, error: String(e) };
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  const duplicateSet = useCallback(
    async (set: B4SetConfig): Promise<ApiResponse<B4SetConfig>> => {
      const { id: _, ...rest } = structuredClone(set);
      return createSet({ ...rest, name: `${set.name} (copy)` });
    },
    [createSet],
  );

  const reorderSets = useCallback(
    async (setIds: string[]): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.reorderSets(setIds);
        return { success: true };
      } catch (e) {
        if (e instanceof ApiError) {
          const msg = JSON.stringify(e.body ?? e.message);
          return { success: false, error: msg };
        }
        return { success: false, error: String(e) };
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  const addDomainToSet = useCallback(
    async (setId: string, domain: string): Promise<ApiResponse<void>> => {
      try {
        await setsApi.addDomainToSet(setId, domain);
        return { success: true };
      } catch (e) {
        if (e instanceof ApiError) {
          return {
            success: false,
            error: JSON.stringify(e.body ?? e.message),
          };
        }
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  return {
    createSet,
    updateSet,
    deleteSet,
    duplicateSet,
    reorderSets,
    addDomainToSet,
    loading,
  };
}
