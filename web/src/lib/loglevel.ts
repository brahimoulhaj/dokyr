export type LogLevel = 'error' | 'warn' | 'info' | 'debug'

const NAMES: Record<string, LogLevel> = {
  trace: 'debug', debug: 'debug', dbg: 'debug', verbose: 'debug',
  info: 'info', notice: 'info', log: 'info',
  warn: 'warn', warning: 'warn',
  err: 'error', error: 'error', fatal: 'error', panic: 'error',
  crit: 'error', critical: 'error', alert: 'error', emerg: 'error', emergency: 'error',
}

const WORDS = 'TRACE|DEBUG|DBG|INFO|NOTICE|WARN(?:ING)?|ERR(?:OR)?|FATAL|PANIC|CRIT(?:ICAL)?|ALERT|EMERG(?:ENCY)?'

// Structured logs: {"level":"error"}, {"severity":"WARNING"}, level=warn, lvl="debug", pino's "level":30.
const FIELD = /(?:^|[\s,{"])(?:level|lvl|severity|loglevel)"?\s*[:=]\s*"?([A-Za-z]+|\d{2})\b/i
// A level written as its own upper-case word: "ERROR", "[WARN]", "production.ERROR:", "INFO:".
const UPPER = new RegExp(`(?<![\\w-])(${WORDS})(?![\\w-])`)
// Lower or mixed case counts only where it is plainly a tag: "[error]", "<warn>", "error: …", "npm warn …".
const TAGGED = new RegExp(`(?:[\\[<(](${WORDS})[\\]>)]|^\\s*(?:npm )?(${WORDS})(?::|\\s|$))`, 'i')

const ANSI = /\x1b\[[0-9;]*m/g
/** Levels sit at the front of a line; a level word deep in a message is prose. */
const HEAD = 160

/** pino and bunyan write the level as a number. */
function numeric(n: number): LogLevel | null {
  if (n >= 50) return 'error'
  if (n >= 40) return 'warn'
  if (n >= 30) return 'info'
  if (n >= 10) return 'debug'
  return null
}

/** The severity a log line declares for itself, or null when it declares none. */
export function logLevel(text: string): LogLevel | null {
  const field = FIELD.exec(text.length > 2000 ? text.slice(0, 2000) : text)
  if (field) {
    const v = field[1]
    const level = /^\d/.test(v) ? numeric(Number(v)) : NAMES[v.toLowerCase()]
    if (level) return level
  }
  const head = text.slice(0, HEAD).replace(ANSI, '')
  const word = UPPER.exec(head)?.[1] ?? (() => {
    const m = TAGGED.exec(head)
    return m?.[1] ?? m?.[2]
  })()
  return word ? NAMES[word.toLowerCase()] ?? null : null
}
