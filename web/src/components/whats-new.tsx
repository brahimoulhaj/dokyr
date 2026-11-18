import { Popover } from '@base-ui/react/popover'
import { useEffect, useState } from 'react'
import { Gift } from '@phosphor-icons/react/dist/csr/Gift'
import { changelog } from '../changelog'
import { useLang, useT } from '../lib/i18n'
import { Badge, cx } from './ui'

const KEY = 'whats-new'
const OPEN_EVENT = 'whats-new:open'
/** How many versions the menu lists. */
const SHOWN = 3
/** A new version stays marked this long after this browser first saw it. */
const FRESH_FOR = 7 * 24 * 60 * 60 * 1000

/** Opens the menu from elsewhere (the command palette). */
export function openWhatsNew() {
  window.dispatchEvent(new Event(OPEN_EVENT))
}

// What this browser knows: the version it last ran, when it first saw it (0: nothing to announce),
// and whether the menu was opened since.
type Seen = { version: string; since: number; opened: boolean }

function load(version: string): Seen {
  let seen: Seen | null = null
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? 'null')
    if (raw && typeof raw.version === 'string') seen = { version: raw.version, since: Number(raw.since) || 0, opened: !!raw.opened }
  } catch { /* storage blocked */ }
  if (seen?.version === version) return seen
  // A first visit announces nothing; a version other than the last one seen is an update.
  const next: Seen = seen ? { version, since: Date.now(), opened: false } : { version, since: 0, opened: true }
  save(next)
  return next
}

function save(seen: Seen) {
  try { localStorage.setItem(KEY, JSON.stringify(seen)) } catch { /* storage blocked */ }
}

/**
 * The last versions and what changed in them, the running one first.
 * After an update the icon carries a dot until the menu is opened, and the icon and the running
 * version stay marked as new until the menu was opened and 7 days have passed.
 */
export function WhatsNew({ version }: { version: string }) {
  const t = useT()
  const lang = useLang()
  const [open, setOpen] = useState(false)
  const [seen, setSeen] = useState(() => load(version))
  const fresh = seen.since > 0 && (!seen.opened || Date.now() - seen.since < FRESH_FOR)
  const unread = seen.since > 0 && !seen.opened

  function changeOpen(value: boolean) {
    setOpen(value)
    if (value && !seen.opened) {
      const next = { ...seen, opened: true }
      save(next)
      setSeen(next)
    }
  }
  useEffect(() => {
    const onOpen = () => changeOpen(true)
    window.addEventListener(OPEN_EVENT, onOpen)
    return () => window.removeEventListener(OPEN_EVENT, onOpen)
  })

  // The running version first; a build without an entry of its own (dev, a patch) takes the newest.
  const running = version.replace(/^v/, '')
  const at = Math.max(0, changelog.findIndex((r) => r.version === running))
  const releases = changelog.slice(at, at + SHOWN)
  const date = new Intl.DateTimeFormat(lang, { dateStyle: 'medium', timeZone: 'UTC' })

  return (
    <Popover.Root open={open} onOpenChange={changeOpen}>
      <Popover.Trigger
        aria-label={unread ? t.shell.whatsNewUnread : t.shell.whatsNew}
        title={t.shell.whatsNew}
        className={cx(
          'relative flex size-8 items-center justify-center border hover:border-line hover:text-fg',
          open ? 'border-line text-fg' : 'border-transparent',
          !open && (fresh ? 'text-accent' : 'text-muted'),
        )}
      >
        <Gift size={16} weight={fresh ? 'fill' : 'regular'} aria-hidden />
        {unread && <span aria-hidden className="absolute end-1 top-1 size-1.5 rounded-full bg-accent" />}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="end" sideOffset={8} className="isolate z-50">
          <Popover.Popup className="w-96 max-w-(--available-width) origin-(--transform-origin) border border-line-strong bg-surface text-fg shadow-lg shadow-black/30 transition-[opacity,scale] duration-100 outline-none data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0">
            <div className="border-b border-line px-4 py-2">
              <Popover.Title className="font-pixel text-xs text-muted uppercase">{t.shell.whatsNew}</Popover.Title>
            </div>
            <ol className="max-h-96 divide-y divide-line overflow-y-auto">
              {releases.map((release, i) => {
                const current = i === 0
                return (
                  <li
                    key={release.version}
                    aria-current={current || undefined}
                    className={cx('border-s-2 px-4 py-3', current ? 'border-s-accent' : 'border-s-transparent', current && fresh && 'bg-accent/10')}
                  >
                    <div className="flex items-center gap-2">
                      <span dir="ltr" className={cx('font-pixel text-sm', current ? 'text-accent' : 'text-fg')}>
                        {current ? version : release.version}
                      </span>
                      {current && <Badge tone={fresh ? 'accent' : 'muted'}>{fresh ? t.shell.newVersion : t.shell.currentVersion}</Badge>}
                      {release.date && !(current && release.version !== running) && (
                        <time dateTime={release.date} className="ms-auto text-xs text-muted">{date.format(new Date(release.date))}</time>
                      )}
                    </div>
                    <ul className="mt-2 space-y-1.5 text-xs text-muted">
                      {(release.changes[lang] ?? release.changes.en).map((line) => (
                        <li key={line} className="flex gap-2">
                          <span aria-hidden className={current ? 'text-accent' : undefined}>+</span>
                          <span className="min-w-0 break-words">{line}</span>
                        </li>
                      ))}
                    </ul>
                  </li>
                )
              })}
            </ol>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
