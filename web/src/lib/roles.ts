import { useQuery } from '@tanstack/react-query'
import { meQuery } from './api'

// What the signed-in user's role allows. The server enforces the same rules; this only
// keeps the interface from offering what would be refused.
export function useCan() {
  const role = useQuery(meQuery).data?.role
  const admin = role === 'owner' || role === 'admin'
  return {
    /** Invite and remove users, change roles: the owner. */
    manageUsers: role === 'owner',
    /** The host terminal, the panel's backups, sign-in configuration: owner and admins. */
    admin,
    /** Delete apps, databases, domains and other resources: everyone but maintainers. */
    remove: admin,
  }
}
