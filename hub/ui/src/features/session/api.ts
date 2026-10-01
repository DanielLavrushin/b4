import { get, post } from "@/api/client";
import type { SessionState } from "@/models/api";

export const fetchSession = () => get<SessionState>("/session");
export const login = (password: string) => post<SessionState>("/login", { password });
export const logout = () => post<void>("/logout");
