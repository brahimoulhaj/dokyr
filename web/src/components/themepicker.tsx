import { useEffect, useRef, useState } from 'react'
import { useT } from '../lib/i18n'
import { setTheme, themes, themeStyle, useTheme, type Theme } from '../lib/theme'
import { cx } from './ui'

// A full-screen carousel. Browsing only moves the cards; Enter or a click on the middle
// card applies the theme, Escape or a click on the backdrop leaves it unchanged.
export function ThemePicker({ brand, onClose }: { brand: string; onClose: () => void }) {
  const active = useTheme()
  const t = useT()
  const [index, setIndex] = useState(() => Math.max(0, themes.indexOf(active)))
  const dialog = useRef<HTMLDivElement>(null)
  const n = themes.length
  const go = (d: number) => setIndex((i) => (i + d + n) % n)
  const choose = (t: Theme) => {
    setTheme(t.id)
    onClose()
  }

  useEffect(() => {
    dialog.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      else if (e.key === 'ArrowLeft') go(-1)
      else if (e.key === 'ArrowRight') go(1)
      else if (e.key === 'Enter' && e.target === dialog.current) choose(themes[index])
      else return
      e.preventDefault()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  })

  const shown = themes[index]

  return (
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-label={t.theme.choose}
      // The carousel is laid out by position, so it keeps one direction in every language.
      dir="ltr"
      tabIndex={-1}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      className="fixed inset-0 z-50 flex flex-col items-center justify-center gap-6 bg-black/80 backdrop-blur-sm outline-none"
    >
      {/* perspective (without preserve-3d) gives each tilted card its depth while z-index
          still decides which card sits on top. */}
      <div className="pointer-events-none relative h-[min(28.75rem,48.75vw)] w-full [perspective:1600px]">
        {themes.map((th, i) => {
          let o = i - index
          if (o > n / 2) o -= n
          if (o < -n / 2) o += n
          const d = Math.abs(o)
          return (
            <button
              key={th.id}
              type="button"
              tabIndex={o === 0 ? 0 : -1}
              aria-label={o === 0 ? t.theme.use(th.name) : t.theme.show(th.name)}
              aria-hidden={d > 2}
              onClick={() => (o === 0 ? choose(th) : go(o))}
              style={{
                // Side cards turn away from the middle one, their inner edge receding behind it.
                transform: `translate(-50%, -50%) translateX(${o * 34}%) rotateY(${-Math.sign(o) * 38}deg) scale(${o === 0 ? 1 : 0.82 - (d - 1) * 0.06})`,
                zIndex: 10 - d,
                opacity: d > 2 ? 0 : o === 0 ? 1 : 0.5 - (d - 1) * 0.15,
              }}
              className={cx(
                'absolute top-1/2 left-1/2 w-[min(46rem,78vw)] border-2 shadow-2xl motion-safe:transition-[transform,opacity] motion-safe:duration-300',
                d > 2 ? 'pointer-events-none' : 'pointer-events-auto',
                o === 0 ? 'border-white/70' : 'border-transparent',
              )}
            >
              <Preview theme={th} brand={brand} />
            </button>
          )
        })}
      </div>

      <div className="text-center text-white">
        <p className="text-2xl font-medium">{shown.name}</p>
        <p className="mt-1 text-xs text-white/60">
          {t.theme[shown.mode]} · {index + 1}/{n}
          {shown.id === active.id && ` · ${t.theme.current}`}
        </p>
        <p className="mt-3 hidden text-xs text-white/40 sm:block">{t.theme.keys}</p>
      </div>

      <Arrow side="left" onClick={() => go(-1)} />
      <Arrow side="right" onClick={() => go(1)} />
    </div>
  )
}

function Arrow({ side, onClick }: { side: 'left' | 'right'; onClick: () => void }) {
  const t = useT()
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={side === 'left' ? t.theme.previous : t.theme.next}
      className={cx(
        'absolute top-1/2 z-20 flex size-11 -translate-y-1/2 items-center justify-center border border-white/15 bg-white/5 text-white/80 hover:bg-white/15 hover:text-white',
        side === 'left' ? 'left-3 sm:left-6' : 'right-3 sm:right-6',
      )}
    >
      <svg viewBox="0 0 16 16" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
        <path d={side === 'left' ? 'M10 3 5 8l5 5' : 'm6 3 5 5-5 5'} />
      </svg>
    </button>
  )
}

// A miniature of the panel's overview, drawn with the theme's own tokens. Sizes are in em
// off a container-relative font size, so it scales with the card.
function Preview({ theme, brand }: { theme: Theme; brand: string }) {
  return (
    <div style={themeStyle(theme)} className="@container aspect-[16/10] w-full overflow-hidden bg-bg text-left text-fg">
      <div className="flex h-full flex-col font-mono text-[1.4cqw] leading-tight whitespace-nowrap">
        <div className="flex h-[3em] shrink-0 border-b border-line bg-surface">
          <div className="flex w-[21%] shrink-0 items-center justify-between border-r border-line px-[1em]">
            <span className="font-pixel text-[1.2em] text-accent">{brand}</span>
            <span className="text-[0.8em] text-muted">dev</span>
          </div>
          <div className="flex min-w-0 flex-1 items-center justify-between px-[1em]">
            <span className="flex w-[18em] items-center justify-between border border-line bg-bg px-[0.7em] py-[0.35em] text-muted">
              Search anything…
              <span className="border border-line px-[0.4em] text-[0.75em]">⌘ K</span>
            </span>
            <span className="flex items-center gap-[1.1em]">
              {[0, 1, 2, 3].map((i) => (
                <Glyph key={i} className="text-muted" />
              ))}
              <span className="flex size-[1.9em] items-center justify-center border border-line bg-raised font-pixel text-accent">b</span>
            </span>
          </div>
        </div>
        <div className="flex min-h-0 flex-1">
          <div className="w-[21%] shrink-0 border-r border-line bg-surface p-[0.6em]">
            <NavItem label="Overview" active />
            <NavItem label="Activity" />
            <NavLabel>Workloads</NavLabel>
            <NavItem label="Projects" count="2" />
            <div className="ml-[1.2em] border-l border-line pl-[0.8em] text-[0.9em] text-muted">
              {['demo2', 'demo'].map((l) => (
                <p key={l} className="flex items-center gap-[0.6em] py-[0.25em]">
                  <span className="size-[0.4em] rounded-full bg-accent" />
                  {l}
                </p>
              ))}
            </div>
            <NavItem label="Databases" count="2" />
            <NavLabel>Infrastructure</NavLabel>
            <NavItem label="Domains" count="3" />
            <NavItem label="Storage" />
            <NavItem label="Registry" count="0" />
            <NavLabel>System</NavLabel>
            <NavItem label="Backup" />
            <NavItem label="Settings" />
          </div>
          <div className="min-w-0 flex-1 space-y-[1.1em] p-[1.6em]">
            <div>
              <p className="font-pixel text-[1.15em] uppercase">Overview</p>
              <p className="mt-[0.2em] text-muted">Signed in as admin@example.com.</p>
            </div>
            <Card
              title="Host metrics"
              aside={
                <span className="flex items-center gap-[0.5em]">
                  <span className="size-[0.45em] rounded-full bg-accent" />
                  Live · every 5s
                </span>
              }
            >
              <Tile label="CPU" value="15.7%" sub="12 logical CPUs" meter={[16, 'bg-accent']} spark="0,7 8,6 14,8 22,5 30,7 38,6 46,8 54,4 62,7 70,6 78,3 86,7 100,6" />
              <Tile label="RAM" value="83%" sub="19.9 / 24 GiB used" meter={[83, 'bg-warn']} spark="0,4 20,4 40,3 60,4 80,3 100,3" />
              <Tile label="Data disk" value="92.9%" sub="32.8 GiB available" meter={[93, 'bg-danger']} />
              <Tile label="Network" value="↓ 383 KiB/s" sub="↑ 15.8 KiB/s" spark="0,5 8,4 14,6 22,4 30,6 38,3 46,5 50,1 54,6 62,4 70,5 78,3 86,6 94,4 100,5" />
            </Card>
            <Card title="Docker" aside={<span className="border border-accent px-[0.5em] py-[0.1em] font-pixel text-[0.8em] text-accent uppercase">Connected</span>}>
              <Tile label="Containers" value="22" sub="22 running · 0 stopped" />
              <Tile label="Images" value="59" sub="14.6 GiB used" />
              <Tile label="Volumes" value="137" sub="8 GiB used" />
              <Tile label="Build cache" value="6.2 GiB" sub="4.7 GiB reclaimable" />
            </Card>
            <Card title="Projects" aside="2 projects · 9 apps" rows>
              {[
                ['demo', '7 apps'],
                ['demo2', '2 apps'],
              ].map(([name, apps]) => (
                <div key={name} className="flex items-center justify-between border-b border-line px-[1.1em] py-[0.6em] last:border-b-0">
                  {name}
                  <span className="border border-line-strong px-[0.5em] py-[0.1em] font-pixel text-[0.75em] text-muted uppercase">{apps}</span>
                </div>
              ))}
            </Card>
          </div>
        </div>
      </div>
    </div>
  )
}

// A stand-in for an icon at this size.
function Glyph({ className }: { className?: string }) {
  return <span className={cx('size-[0.85em] shrink-0 border border-current', className)} />
}

function NavLabel({ children }: { children: React.ReactNode }) {
  return <p className="px-[0.7em] pt-[1.1em] pb-[0.4em] font-pixel text-[0.7em] text-muted uppercase">{children}</p>
}

function NavItem({ label, count, active }: { label: string; count?: string; active?: boolean }) {
  return (
    <div className={cx('flex items-center gap-[0.7em] border px-[0.7em] py-[0.35em]', active ? 'border-line bg-raised text-fg' : 'border-transparent text-muted')}>
      <Glyph className={active ? 'text-accent' : undefined} />
      <span className="flex-1">{label}</span>
      {count && <span className="border border-line px-[0.4em] text-[0.75em]">{count}</span>}
    </div>
  )
}

function Card({ title, aside, rows, children }: { title: string; aside: React.ReactNode; rows?: boolean; children: React.ReactNode }) {
  return (
    <div className="border border-line bg-surface">
      <div className="flex items-center justify-between border-b border-line px-[1.1em] py-[0.6em] text-[0.9em] text-muted">
        <span className="font-pixel text-[0.9em] uppercase">{title}</span>
        {aside}
      </div>
      {rows ? children : <div className="grid grid-cols-4 gap-px bg-line">{children}</div>}
    </div>
  )
}

function Tile({ label, value, sub, meter, spark }: { label: string; value: string; sub: string; meter?: [number, string]; spark?: string }) {
  return (
    <div className="min-w-0 space-y-[0.6em] overflow-hidden bg-surface px-[1.1em] py-[0.9em]">
      <p className="text-[0.85em] text-muted">{label}</p>
      <p className="text-[1.45em] leading-none">{value}</p>
      {meter && (
        <div className="h-[0.25em] bg-line">
          <div className={cx('h-full', meter[1])} style={{ width: `${meter[0]}%` }} />
        </div>
      )}
      {spark && (
        <svg viewBox="0 0 100 10" preserveAspectRatio="none" className="h-[1.6em] w-full text-accent" aria-hidden>
          <polyline points={spark} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        </svg>
      )}
      <p className="text-[0.85em] text-muted">{sub}</p>
    </div>
  )
}
