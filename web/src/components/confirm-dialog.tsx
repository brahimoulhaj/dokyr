import { AlertDialog } from '@base-ui/react/alert-dialog'
import { useState, type ReactNode } from 'react'
import { useT } from '../lib/i18n'
import { Button } from './ui'

/** A button that opens a confirmation dialog explaining the consequences first. */
export function ConfirmDialog(props: {
  label: string
  title: string
  message: ReactNode
  confirm: string
  onConfirm: () => void
  disabled?: boolean
  pending?: boolean
  /** danger for destructive actions; primary confirms a non-destructive one. */
  tone?: 'danger' | 'primary'
  className?: string
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const danger = props.tone !== 'primary'
  return <>
    <Button type="button" variant={danger ? 'danger' : 'ghost'} className={props.className} disabled={props.disabled || props.pending} onClick={() => setOpen(true)}>
      {props.pending ? t.common.working : props.label}
    </Button>
    <ConfirmModal open={open} onOpenChange={setOpen} title={props.title} message={props.message} confirm={props.confirm} tone={props.tone} onConfirm={props.onConfirm} />
  </>
}

/** The confirmation dialog alone, opened by its caller (e.g. from a menu item). */
export function ConfirmModal(props: { open: boolean; onOpenChange: (open: boolean) => void; title: string; message: ReactNode; confirm: string; onConfirm: () => void; tone?: 'danger' | 'primary' }) {
  const t = useT()
  const danger = props.tone !== 'primary'
  return (
    <AlertDialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-bg/70 backdrop-blur-[2px] transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0 motion-reduce:transition-none" />
        <AlertDialog.Popup className="fixed start-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 border border-line bg-surface p-5 shadow-2xl outline-none transition-[opacity,scale] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none rtl:translate-x-1/2">
          <AlertDialog.Title className="font-pixel text-sm uppercase">{props.title}</AlertDialog.Title>
          <AlertDialog.Description className="mt-3 text-sm leading-relaxed text-muted">{props.message}</AlertDialog.Description>
          <div className="mt-5 flex flex-wrap justify-end gap-2">
            <AlertDialog.Close render={<Button type="button" variant="ghost" />}>{t.common.cancel}</AlertDialog.Close>
            <Button type="button" variant={danger ? 'danger' : 'primary'} onClick={() => { props.onOpenChange(false); props.onConfirm() }}>{props.confirm}</Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  )
}
