import { Dialog } from '@base-ui/react/dialog'
import type { ReactNode } from 'react'
import { useT } from '../lib/i18n'

/** A side sheet: a form that slides in from the inline end (the right, or the left in
 * Arabic) over a dimmed page. Escape, the backdrop and the × close it. */
export function Sheet(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
}) {
  const t = useT()
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-bg/70 backdrop-blur-[2px] transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0 motion-reduce:transition-none" />
        <Dialog.Popup
          className={[
            'fixed inset-y-0 end-0 z-50 flex w-full max-w-lg flex-col border-s border-line bg-surface shadow-2xl outline-none',
            'transition-transform duration-250 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none',
            'data-[starting-style]:translate-x-full data-[ending-style]:translate-x-full',
            'rtl:data-[starting-style]:-translate-x-full rtl:data-[ending-style]:-translate-x-full',
          ].join(' ')}
        >
          <div className="flex items-start justify-between gap-4 border-b border-line px-5 py-4">
            <div className="min-w-0">
              <Dialog.Title className="font-pixel text-sm uppercase">{props.title}</Dialog.Title>
              {props.description && <Dialog.Description className="mt-1.5 text-xs text-muted">{props.description}</Dialog.Description>}
            </div>
            <Dialog.Close aria-label={t.common.close} className="-me-1 inline-flex size-8 shrink-0 items-center justify-center border border-transparent text-muted hover:border-line hover:text-fg">
              ×
            </Dialog.Close>
          </div>
          {/* The content fills the sheet so SheetActions can sit at its bottom, short form or long. */}
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto [&>*]:flex [&>*]:grow [&>*]:flex-col">{props.children}</div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/** The action buttons of a sheet's form, kept at the bottom of the sheet and outside the
 * scrolling content. Goes last inside a form padded with p-5. */
export function SheetActions({ children }: { children: ReactNode }) {
  return (
    <div className="sticky bottom-0 z-10 -mx-5 -mb-5 mt-auto flex flex-wrap gap-2 border-t border-line bg-surface px-5 py-4">
      {children}
    </div>
  )
}
