import { Menu } from '@base-ui/react/menu'
import { cx } from './ui'

export type MenuAction = { label: string; onSelect: () => void; danger?: boolean; disabled?: boolean }

/** One "Actions ▾" button that opens a keyboard-navigable list of row actions. */
export function ActionsMenu(props: { label: string; items: MenuAction[]; disabled?: boolean; ariaLabel?: string }) {
  const items = props.items
  const firstDanger = items.findIndex((i) => i.danger)
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label={props.ariaLabel}
        disabled={props.disabled}
        className="inline-flex h-8 items-center gap-2 border border-line px-3 text-xs text-muted transition-colors hover:border-line-strong hover:text-fg disabled:cursor-not-allowed disabled:opacity-50 data-popup-open:border-line-strong data-popup-open:text-fg"
      >
        {props.label}<span aria-hidden="true" className="text-[10px]">▾</span>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={6} className="isolate z-50 outline-none">
          <Menu.Popup className="min-w-48 origin-(--transform-origin) border border-line-strong bg-surface py-1 text-fg shadow-lg shadow-black/30 outline-none transition-[opacity,scale] duration-100 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none">
            {items.map((item, i) => <div key={item.label}>
              {i === firstDanger && i > 0 && <Menu.Separator className="my-1 h-px bg-line" />}
              <Menu.Item
                disabled={item.disabled}
                // Run after the menu has closed: a dialog opened in the same pointer
                // event would otherwise treat that click as an outside press and close.
                onClick={() => setTimeout(item.onSelect, 0)}
                className={cx('flex cursor-default px-3 py-2 text-xs outline-none select-none data-disabled:opacity-50 data-highlighted:bg-bg', item.danger ? 'text-danger' : 'text-fg')}
              >
                {item.label}
              </Menu.Item>
            </div>)}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
