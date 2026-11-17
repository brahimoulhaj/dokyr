import { AlertDialog } from '@base-ui/react/alert-dialog'
import { useMutation } from '@tanstack/react-query'
import { LockKey } from '@phosphor-icons/react'
import { useEffect, useId, useRef, useState } from 'react'
import { useT } from '../lib/i18n'
import { Button, ErrorNote } from './ui'

export type PasswordAction = {
  title: string
  confirm: string
  detail?: string
  tone?: 'primary' | 'danger'
  pendingLabel?: string
  onConfirm: (password: string) => Promise<unknown>
}

/** Mounted only for the selected action, so closing discards the password and errors. */
export function PasswordConfirmDialog({ action, onClose, onComplete }: {
  action: PasswordAction
  onClose: () => void
  onComplete: () => void
}) {
  const t = useT()
  const input = useRef<HTMLInputElement>(null)
  const inputID = useId()
  const errorID = useId()
  const [password, setPassword] = useState('')
  const confirmation = useMutation({
    mutationFn: action.onConfirm,
    onSuccess: () => { setPassword(''); onComplete() },
  })
  const busy = confirmation.isPending
  useEffect(() => {
    if (confirmation.error) { input.current?.focus(); input.current?.select() }
  }, [confirmation.error])

  return <AlertDialog.Root open onOpenChange={(open) => { if (!open && !busy) onClose() }}>
    <AlertDialog.Portal>
      <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-bg/70 backdrop-blur-[2px] transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0 motion-reduce:transition-none" />
      <AlertDialog.Popup initialFocus={input} className="fixed start-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-sm -translate-x-1/2 -translate-y-1/2 border border-line-strong bg-surface text-fg shadow-lg outline-none transition-[opacity,scale] duration-150 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none rtl:translate-x-1/2">
        <div className="flex items-center gap-3 border-b border-line px-4 py-3">
          <LockKey size={18} className="shrink-0 text-muted" aria-hidden />
          <AlertDialog.Title className="font-pixel text-xs uppercase">{action.title}</AlertDialog.Title>
        </div>
        <form onSubmit={(event) => { event.preventDefault(); if (password && !busy) confirmation.mutate(password) }}>
          <div className="space-y-4 p-4">
            <div className="space-y-2">
              <AlertDialog.Description className="text-sm leading-relaxed text-muted">{t.security.confirmPasswordHint}</AlertDialog.Description>
              {action.detail && <p className="break-words border-s-2 border-line-strong ps-3 text-sm">{action.detail}</p>}
            </div>
            <div className="space-y-1.5">
              <label htmlFor={inputID} className="block text-xs text-muted">{t.security.currentPassword}</label>
              <input ref={input} id={inputID} name="confirm-password" type="password" autoComplete="current-password" required dir="ltr"
                value={password} disabled={busy} aria-invalid={!!confirmation.error} aria-describedby={confirmation.error ? errorID : undefined}
                onChange={(event) => { setPassword(event.target.value); if (confirmation.error) confirmation.reset() }}
                className="block h-9 w-full border border-line bg-bg px-3 text-sm text-fg focus:border-accent focus:outline-none disabled:opacity-50" />
            </div>
            {confirmation.error && <div id={errorID}><ErrorNote>{confirmation.error instanceof DOMException && confirmation.error.name === 'NotAllowedError' ? t.security.cancelled : confirmation.error.message}</ErrorNote></div>}
          </div>
          <div className="flex flex-wrap justify-end gap-2 border-t border-line bg-raised/40 px-4 py-3">
            <AlertDialog.Close disabled={busy} render={<Button type="button" variant="ghost" />}>{t.common.cancel}</AlertDialog.Close>
            <Button type="submit" variant={action.tone ?? 'primary'} disabled={!password || busy}>
              {busy ? action.pendingLabel ?? t.common.working : action.confirm}
            </Button>
          </div>
        </form>
      </AlertDialog.Popup>
    </AlertDialog.Portal>
  </AlertDialog.Root>
}
