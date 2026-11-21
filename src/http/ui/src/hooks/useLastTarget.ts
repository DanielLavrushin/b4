import { useState } from "react";

export function useLastTarget<T>(target: T | null): T | null {
  const [last, setLast] = useState<T | null>(target);
  if (target !== null && target !== last) setLast(target);
  return target ?? last;
}
