import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { createContext, type ReactNode, useCallback, useContext, useMemo } from "react";

import { type Me, logout as requestLogout } from "./api";

interface AuthContextValue {
  /** `GET /auth/me` result for the current session — always defined inside
   * `AuthProvider`, which only mounts once the route guard has resolved it. */
  me: Me;
  isOwner: boolean;
  /** Checks a `me.permissions` capability string (ADR-010). The UI hides
   * what a role cannot do; the API stays the enforcement point. */
  can: (permission: string) => boolean;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ me, children }: { me: Me; children: ReactNode }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const logout = useCallback(async () => {
    await requestLogout();
    queryClient.clear();
    await navigate({ to: "/login" });
  }, [queryClient, navigate]);

  const value = useMemo<AuthContextValue>(
    () => ({
      me,
      isOwner: me.user.role === "owner",
      can: (permission) => me.permissions.includes(permission),
      logout,
    }),
    [me, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return value;
}
