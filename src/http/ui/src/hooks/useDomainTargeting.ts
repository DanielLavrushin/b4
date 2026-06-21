import { useCallback } from "react";

export function useDomainTargeting(targetedDomains: Set<string>) {
  return useCallback(
    (domain: string): boolean => {
      if (targetedDomains.has(domain)) return true;
      const parts = domain.split(".");
      for (let i = 1; i < parts.length; i++) {
        if (targetedDomains.has(parts.slice(i).join("."))) return true;
      }
      return false;
    },
    [targetedDomains],
  );
}
