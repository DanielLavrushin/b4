import { useState, useCallback } from "react";
import { B4SetConfig } from "@models/config";
import { ApiResponse } from "@api/apiClient";
import { setsApi } from "@b4.sets";
import { AddDomainsResult } from "@api/sets";

export function useSets() {
  const [loading, setLoading] = useState(false);

  const createSet = useCallback(
    async (set: Omit<B4SetConfig, "id">): Promise<ApiResponse<B4SetConfig>> => {
      setLoading(true);
      try {
        const data = await setsApi.createSet(set);
        return { success: true, data };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const updateSet = useCallback(
    async (set: B4SetConfig): Promise<ApiResponse<B4SetConfig>> => {
      setLoading(true);
      try {
        const data = await setsApi.updateSet(set.id, set);
        return { success: true, data };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const deleteSet = useCallback(
    async (id: string): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.deleteSet(id);
        return { success: true };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const deleteSets = useCallback(
    async (ids: string[]): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.deleteSets(ids);
        return { success: true };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const setEnabledForSets = useCallback(
    async (ids: string[], enabled: boolean): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.setEnabledForSets(ids, enabled);
        return { success: true };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const duplicateSet = useCallback(
    async (set: B4SetConfig): Promise<ApiResponse<B4SetConfig>> => {
      const { id: _, ...rest } = structuredClone(set);
      return createSet({ ...rest, name: `${set.name} (copy)` });
    },
    [createSet]
  );

  const reorderSets = useCallback(
    async (setIds: string[]): Promise<ApiResponse<void>> => {
      setLoading(true);
      try {
        await setsApi.reorderSets(setIds);
        return { success: true };
      } catch (e) {
        return { success: false, error: e };
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const addDomainToSet = useCallback(
    async (setId: string, domain: string): Promise<ApiResponse<void>> => {
      try {
        await setsApi.addDomainToSet(setId, domain);
        return { success: true };
      } catch (e) {
        return { success: false, error: e };
      }
    },
    []
  );

  const addDomainsToSet = useCallback(
    async (
      setId: string,
      domains: string[],
    ): Promise<ApiResponse<AddDomainsResult>> => {
      try {
        const data = await setsApi.addDomainsToSet(setId, domains);
        return { success: true, data };
      } catch (e) {
        return { success: false, error: e };
      }
    },
    []
  );

  return {
    createSet,
    updateSet,
    deleteSet,
    deleteSets,
    setEnabledForSets,
    duplicateSet,
    reorderSets,
    addDomainToSet,
    addDomainsToSet,
    loading,
  };
}
