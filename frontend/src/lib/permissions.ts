import type { Principal } from "@/types";

/** Admin implies every permission; otherwise check the explicit grant. */
export function can(principal: Principal | undefined, permission: string): boolean {
  if (!principal) return false;
  return principal.permissions.includes("admin") || principal.permissions.includes(permission);
}
