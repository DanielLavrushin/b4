import { useState, useCallback } from "react";

export function useForm<T extends object>(initialData: T | null = null) {
  const [data, setData] = useState<T | null>(initialData);
  const [isDirty, setIsDirty] = useState(false);

  const handleChange = useCallback((field: string, value: unknown) => {
    setData((prev) => {
      if (!prev) return prev;

      const keys = field.split(".");
      if (keys.length === 1) {
        return { ...prev, [field]: value };
      }

      const newData = { ...prev };
      let current: Record<string, unknown> = newData as Record<string, unknown>;
      for (let i = 0; i < keys.length - 1; i++) {
        current[keys[i]] = { ...(current[keys[i]] as object) };
        current = current[keys[i]] as Record<string, unknown>;
      }
      current[keys[keys.length - 1]] = value;
      return newData;
    });
    setIsDirty(true);
  }, []);

  const setFormData = useCallback((newData: T | null) => {
    setData(newData);
    setIsDirty(false);
  }, []);

  const reset = useCallback(() => {
    setData(null);
    setIsDirty(false);
  }, []);

  return {
    data,
    isDirty,
    handleChange,
    setFormData,
    reset,
  };
}
