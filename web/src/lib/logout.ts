import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, meQuery } from './api'

/** The account menu and command palette use the same sign-out flow. */
export function useLogout() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: () => api<void>('/auth/logout', { method: 'POST' }),
    onSuccess: () => {
      queryClient.setQueryData(meQuery.queryKey, null)
      void navigate({ to: '/login' })
    },
  })
}
