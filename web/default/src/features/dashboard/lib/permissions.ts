import { ROLE } from '@/lib/roles'

export function canViewAdminData(
  user: { role?: number } | null | undefined,
  site: { tenant_id?: number } | null | undefined
): boolean {
  return site?.tenant_id === 0 && (user?.role ?? ROLE.GUEST) >= ROLE.ADMIN
}
