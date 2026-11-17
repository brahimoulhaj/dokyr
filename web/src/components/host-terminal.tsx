import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { ArrowClockwise, CaretDown, Plus, TerminalWindow, X } from '@phosphor-icons/react'
import { useT } from '../lib/i18n'
import { useTheme } from '../lib/theme'
import { cx } from './ui'

type State = 'connecting' | 'connected' | 'disconnected'
type Tab = { id: number; state: State; nonce: number }

const HEIGHT_KEY = 'host-terminal-height'
const MIN_HEIGHT = 140
const MAX_TABS = 6

function maxHeight() {
  return Math.max(MIN_HEIGHT, window.innerHeight - 160)
}
function storedHeight() {
  try {
    const n = Number(localStorage.getItem(HEIGHT_KEY))
    if (n >= MIN_HEIGHT) return Math.min(n, maxHeight())
  } catch { /* storage blocked */ }
  return Math.min(320, maxHeight())
}

/**
 * Root shells on the host, docked under the page like a browser's devtools. Hiding the
 * dock keeps the sessions; closing it (or the last tab) ends them.
 */
export default function HostTerminalDock({ hidden, onHide, onClose }: { hidden: boolean; onHide: () => void; onClose: () => void }) {
  const t = useT()
  const nextID = useRef(2)
  const [tabs, setTabs] = useState<Tab[]>([{ id: 1, state: 'connecting', nonce: 0 }])
  const [active, setActive] = useState(1)
  const [height, setHeight] = useState(storedHeight)

  useEffect(() => {
    try { localStorage.setItem(HEIGHT_KEY, String(height)) } catch { /* storage blocked */ }
  }, [height])
  useEffect(() => {
    const clamp = () => setHeight((h) => Math.min(h, maxHeight()))
    window.addEventListener('resize', clamp)
    return () => window.removeEventListener('resize', clamp)
  }, [])

  function add() {
    const id = nextID.current++
    setTabs((all) => [...all, { id, state: 'connecting', nonce: 0 }])
    setActive(id)
  }
  function close(id: number) {
    const left = tabs.filter((tab) => tab.id !== id)
    if (!left.length) { onClose(); return }
    if (active === id) setActive(left[Math.max(0, tabs.findIndex((tab) => tab.id === id) - 1)].id)
    setTabs(left)
  }
  const patch = (id: number, change: Partial<Tab>) =>
    setTabs((all) => all.map((tab) => (tab.id === id ? { ...tab, ...change } : tab)))

  function drag(event: React.PointerEvent<HTMLDivElement>) {
    event.preventDefault()
    const handle = event.currentTarget
    handle.setPointerCapture(event.pointerId)
    const move = (e: PointerEvent) => setHeight(Math.min(maxHeight(), Math.max(MIN_HEIGHT, window.innerHeight - e.clientY)))
    const stop = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', stop)
      handle.removeEventListener('pointercancel', stop)
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', stop)
    handle.addEventListener('pointercancel', stop)
  }

  const tool = 'flex size-7 shrink-0 items-center justify-center text-muted hover:bg-raised hover:text-fg disabled:opacity-40'

  return (
    <section aria-label={t.shell.hostTerminal} style={{ height }} className={cx('relative flex shrink-0 flex-col border-t border-line bg-bg', hidden && 'hidden')}>
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label={t.shell.resizeTerminal}
        aria-valuenow={height}
        aria-valuemin={MIN_HEIGHT}
        aria-valuemax={maxHeight()}
        tabIndex={0}
        onPointerDown={drag}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
          e.preventDefault()
          setHeight((h) => Math.min(maxHeight(), Math.max(MIN_HEIGHT, h + (e.key === 'ArrowUp' ? 24 : -24))))
        }}
        className="absolute inset-x-0 -top-1 z-10 h-2 cursor-row-resize touch-none hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
      />
      <div className="flex h-9 shrink-0 items-center gap-1 border-b border-line bg-surface ps-3 pe-1">
        <span className="flex shrink-0 items-center gap-2 pe-2 text-xs text-muted">
          <TerminalWindow size={16} aria-hidden />
          <span className="hidden sm:inline">{t.terminal.title}</span>
        </span>
        <div role="tablist" aria-label={t.shell.hostTerminal} className="flex min-w-0 items-center gap-1 overflow-x-auto">
          {tabs.map((tab, i) => (
            <div
              key={tab.id}
              className={cx(
                'flex h-7 shrink-0 items-center border text-xs',
                tab.id === active ? 'border-line bg-bg text-fg' : 'border-transparent text-muted hover:bg-raised hover:text-fg',
              )}
            >
              <button
                type="button"
                role="tab"
                aria-selected={tab.id === active}
                title={t.terminal[tab.state]}
                onClick={() => setActive(tab.id)}
                className="flex h-full items-center gap-2 ps-2 pe-1"
              >
                <span
                  aria-hidden
                  className={cx(
                    'size-1.5 rounded-full',
                    tab.state === 'connected' ? 'bg-accent' : tab.state === 'connecting' ? 'animate-pulse bg-warn' : 'bg-muted',
                  )}
                />
                <span dir="ltr">{t.shell.hostTab(i + 1)}</span>
              </button>
              <button type="button" aria-label={t.shell.closeTab} title={t.shell.closeTab} onClick={() => close(tab.id)} className="flex h-full items-center px-1 text-muted hover:text-fg">
                <X size={12} aria-hidden />
              </button>
            </div>
          ))}
          <button type="button" aria-label={t.shell.newTab} title={t.shell.newTab} disabled={tabs.length >= MAX_TABS} onClick={add} className={tool}>
            <Plus size={14} aria-hidden />
          </button>
        </div>
        <div className="ms-auto flex shrink-0 items-center">
          <button
            type="button"
            aria-label={t.terminal.reconnect}
            title={t.terminal.reconnect}
            onClick={() => patch(active, { nonce: (tabs.find((tab) => tab.id === active)?.nonce ?? 0) + 1 })}
            className={tool}
          >
            <ArrowClockwise size={14} aria-hidden />
          </button>
          <button type="button" aria-label={t.shell.hideTerminal} title={t.shell.hideTerminal} onClick={onHide} className={tool}>
            <CaretDown size={14} aria-hidden />
          </button>
          <button type="button" aria-label={t.shell.closeTerminal} title={t.shell.closeTerminal} onClick={onClose} className={tool}>
            <X size={14} aria-hidden />
          </button>
        </div>
      </div>
      <div className="relative min-h-0 flex-1">
        {tabs.map((tab) => (
          <HostSession
            key={tab.id}
            visible={!hidden && tab.id === active}
            nonce={tab.nonce}
            onState={(state) => patch(tab.id, { state })}
            onExit={() => close(tab.id)}
          />
        ))}
      </div>
    </section>
  )
}

/** One shell: an xterm bridged to /api/host/terminal. Connects on mount and when nonce changes. */
function HostSession({ visible, nonce, onState, onExit }: { visible: boolean; nonce: number; onState: (state: State) => void; onExit: () => void }) {
  const t = useT().terminal
  const theme = useTheme()
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const socket = useRef<WebSocket | null>(null)
  const [message, setMessage] = useState('')
  const [ended, setEnded] = useState(false)
  // The latest props, for callbacks that outlive a render.
  const live = useRef({ t, visible, onState, onExit })
  live.current = { t, visible, onState, onExit }

  function send(value: object) {
    if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify(value))
  }
  function drop() {
    const ws = socket.current
    socket.current = null
    ws?.close()
    if (term.current) term.current.options.disableStdin = true
    setEnded(true)
    live.current.onState('disconnected')
  }
  function connect() {
    const terminal = term.current
    if (!terminal) return
    const previous = socket.current
    socket.current = null
    previous?.close()
    terminal.reset()
    if (host.current?.clientWidth) fit.current?.fit()
    setMessage('')
    setEnded(false)
    live.current.onState('connecting')
    const url = new URL('/api/host/terminal', window.location.href)
    url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    url.search = new URLSearchParams({ cols: String(terminal.cols), rows: String(terminal.rows) }).toString()
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    socket.current = ws
    let failed = false
    const fail = (text: string) => { failed = true; setMessage(text) }
    const timeout = window.setTimeout(() => {
      if (socket.current === ws && ws.readyState !== WebSocket.CLOSED) { fail(live.current.t.hostFailed); drop() }
    }, 20000)
    ws.onmessage = (event) => {
      if (socket.current !== ws) return
      if (event.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(event.data), () => { if (socket.current === ws) send({ type: 'ack' }) })
        return
      }
      const msg = JSON.parse(event.data) as { type: string; message?: string }
      if (msg.type === 'ready') {
        window.clearTimeout(timeout)
        terminal.options.disableStdin = false
        live.current.onState('connected')
        if (live.current.visible) terminal.focus()
        send({ type: 'resize', cols: terminal.cols, rows: terminal.rows })
      } else if (msg.type === 'error') {
        fail(msg.message ?? live.current.t.hostFailed)
      }
    }
    ws.onerror = () => { if (socket.current === ws) fail(live.current.t.hostFailed) }
    ws.onclose = (event) => {
      window.clearTimeout(timeout)
      if (socket.current !== ws) return
      socket.current = null
      // `exit` in the shell closes its tab, as a desktop terminal does.
      if (event.code === 1000 && !failed) { live.current.onExit(); return }
      terminal.options.disableStdin = true
      setEnded(true)
      live.current.onState('disconnected')
    }
  }

  useEffect(() => {
    if (!host.current) return
    const terminal = new Terminal({
      fontFamily: '"JetBrains Mono", monospace', fontSize: 13, lineHeight: 1.25,
      cursorBlink: true, cursorStyle: 'bar', scrollback: 5000,
      disableStdin: true, allowProposedApi: false, screenReaderMode: true,
    })
    const addon = new FitAddon()
    terminal.loadAddon(addon)
    terminal.open(host.current)
    terminal.textarea?.setAttribute('aria-label', live.current.t.hostOutput)
    term.current = terminal
    fit.current = addon
    let frame = 0
    const resize = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => { if (host.current?.clientWidth) addon.fit() })
    }
    const observer = new ResizeObserver(resize)
    observer.observe(host.current)
    const input = terminal.onData((data) => {
      const bytes = new TextEncoder().encode(data)
      const ws = socket.current
      if (ws?.readyState !== WebSocket.OPEN) return
      if (ws.bufferedAmount + bytes.byteLength > 1024 * 1024) { setMessage(live.current.t.hostFailed); drop(); return }
      for (let offset = 0; offset < bytes.length; offset += 16384) ws.send(bytes.subarray(offset, offset + 16384))
    })
    const resized = terminal.onResize(({ cols, rows }) => send({ type: 'resize', cols, rows }))
    const ping = window.setInterval(() => send({ type: 'ping' }), 20000)
    if (host.current.clientWidth) addon.fit()
    connect()
    return () => {
      const ws = socket.current; socket.current = null; ws?.close()
      window.clearInterval(ping); cancelAnimationFrame(frame)
      observer.disconnect(); input.dispose(); resized.dispose(); terminal.dispose()
      term.current = null; fit.current = null
    }
  }, [])

  const firstNonce = useRef(nonce)
  useEffect(() => {
    if (nonce !== firstNonce.current) connect()
  }, [nonce])

  useEffect(() => {
    if (!visible) return
    const frame = requestAnimationFrame(() => {
      if (host.current?.clientWidth) fit.current?.fit()
      term.current?.focus()
    })
    return () => cancelAnimationFrame(frame)
  }, [visible])

  useEffect(() => {
    if (!term.current) return
    term.current.options.theme = {
      background: theme.tokens.bg, foreground: theme.tokens.fg, cursor: theme.tokens.accent,
      selectionBackground: theme.tokens.raised, black: '#1b1f27', red: '#f7768e', green: '#9ece6a',
      yellow: '#e0af68', blue: '#7aa2f7', magenta: '#bb9af7', cyan: '#7dcfff', white: '#d5d9e2',
    }
  }, [theme])

  return (
    <div role="tabpanel" className={cx('absolute inset-0 flex flex-col', !visible && 'hidden')}>
      {(message || ended) && (
        <p role="alert" className="flex shrink-0 items-center justify-between gap-3 border-b border-line px-3 py-1.5 text-xs text-warn">
          <span>{message || t.ended}</span>
          <button type="button" onClick={connect} className="shrink-0 text-fg underline underline-offset-2 hover:text-accent">{t.reconnect}</button>
        </p>
      )}
      <div className="relative min-h-0 flex-1">
        <div ref={host} dir="ltr" className="absolute inset-x-3 inset-y-2" />
      </div>
    </div>
  )
}
