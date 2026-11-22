import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { ArrowClockwise } from '@phosphor-icons/react/dist/csr/ArrowClockwise'
import type { DnsHint } from '../lib/api'
import { useT } from '../lib/i18n'
import { cx, HelpTip } from './ui'

/** One line saying where a hostname points, from a DNS hint query, with a way to look
 * again (after creating a record) unless it already points at this server. */
export function DnsHintText({ dns }: { dns: UseQueryResult<DnsHint> }): ReactNode {
  const t = useT()
  if (!dns.data && !dns.error) return t.dns.checking
  const d = dns.data
  const again = (
    <button
      type="button"
      onClick={() => dns.refetch()}
      disabled={dns.isFetching}
      className="ms-2 whitespace-nowrap text-muted underline decoration-dotted underline-offset-2 hover:text-fg disabled:no-underline disabled:opacity-60"
    >
      {dns.isFetching ? t.dns.checking : t.dns.checkAgain}
    </button>
  )
  let text: ReactNode
  if (dns.error || !d) text = t.dns.failed
  else if (!d.resolves) text = <span className="text-warn">{t.dns.notResolving}</span>
  else if (d.points_here === true) return <span className="text-accent">{t.dns.pointsHere(d.addresses.join(', '))}</span>
  else if (d.cdn === 'cloudflare') text = t.dns.viaCloudflare(d.addresses.join(', '))
  else if (d.points_here === false)
    text = <span className="text-warn">{t.dns.pointsElsewhere(d.addresses.join(', '), d.server_addresses.join(', '))}</span>
  else text = t.dns.unknown(d.addresses.join(', '))
  return (
    <>
      {text}
      {again}
    </>
  )
}

export type DnsState = 'checking' | 'failed' | 'unresolved' | 'verified' | 'elsewhere' | 'cloudflare' | 'unconfirmed'

/** What a DNS hint query says about a hostname, as one state. */
export function dnsState(dns: UseQueryResult<DnsHint>): DnsState {
  if (!dns.data && !dns.error) return 'checking'
  if (dns.error || !dns.data) return 'failed'
  if (!dns.data.resolves) return 'unresolved'
  if (dns.data.points_here === true) return 'verified'
  // Proxied: the record names Cloudflare, which forwards to wherever it was told to.
  if (dns.data.cdn === 'cloudflare') return 'cloudflare'
  return dns.data.points_here === false ? 'elsewhere' : 'unconfirmed'
}

/** The colour of a state, for a row's status rail. */
export const dnsRail: Record<DnsState, string> = {
  verified: 'bg-accent', elsewhere: 'bg-warn', unresolved: 'bg-danger', failed: 'bg-danger',
  cloudflare: 'bg-accent/50', unconfirmed: 'bg-line-strong', checking: 'bg-line-strong',
}

/** The state as a badge: solid when verified, outlined otherwise, so "verified" reads first.
 * Behind Cloudflare is outlined in the accent: fine, but not checked end to end. */
export function DnsBadge({ state }: { state: DnsState }) {
  const t = useT().dns
  const label = {
    verified: t.verified, elsewhere: t.elsewhere, cloudflare: t.cloudflare, unconfirmed: t.unconfirmed,
    unresolved: t.noRecord, failed: t.checkFailed, checking: t.checkingShort,
  }[state]
  return (
    <span
      role="status"
      aria-label={`${t.status}: ${label}`}
      className={cx(
        'inline-flex h-5 shrink-0 items-center gap-1.5 border px-1.5 font-pixel text-[10px] uppercase leading-none',
        state === 'verified' && 'border-accent bg-accent text-accent-fg',
        state === 'cloudflare' && 'border-accent text-accent',
        state === 'elsewhere' && 'border-warn text-warn',
        (state === 'unresolved' || state === 'failed') && 'border-danger text-danger',
        (state === 'unconfirmed' || state === 'checking') && 'border-line-strong text-muted',
      )}
    >
      <span aria-hidden className={cx('size-1.5 bg-current', state === 'checking' && 'animate-pulse motion-reduce:animate-none')} />
      {label}
    </span>
  )
}

const MAX_ADDRESSES = 2

function Addresses({ list, expected }: { list: string[]; expected?: boolean }) {
  const shown = list.slice(0, MAX_ADDRESSES)
  const rest = list.slice(MAX_ADDRESSES)
  return (
    <>
      {shown.map((address) => (
        <span key={address} dir="ltr" className={cx('border px-1.5 py-0.5 font-mono text-[11px] leading-none', expected ? 'border-dashed border-line-strong text-fg' : 'border-line bg-bg text-muted')}>
          {address}
        </span>
      ))}
      {rest.length > 0 && <span dir="ltr" title={rest.join(', ')} className="cursor-help text-[11px] text-muted">+{rest.length}</span>}
    </>
  )
}

/** Where the hostname points: its addresses as chips, the expected ones when they differ,
 * the long explanation behind a "?", and a way to look again. */
export function DnsRecords({ dns }: { dns: UseQueryResult<DnsHint> }) {
  const t = useT().dns
  const state = dnsState(dns)
  const d = dns.data
  const addresses = d?.addresses.join(', ') ?? ''
  const help = state === 'elsewhere' && d ? t.pointsElsewhere(addresses, d.server_addresses.join(', '))
    : state === 'cloudflare' ? t.viaCloudflare(addresses)
    : state === 'unconfirmed' ? t.unknown(addresses)
    : state === 'verified' ? t.pointsHere(addresses) : null
  return (
    <div className="flex min-h-5 flex-wrap items-center gap-x-2 gap-y-1.5 text-xs text-muted">
      {state === 'unresolved' && <span>{t.notResolving}</span>}
      {state === 'failed' && <span>{t.failed}</span>}
      {d?.resolves && <Addresses list={d.addresses} />}
      {state === 'elsewhere' && d && d.server_addresses.length > 0 && (
        <>
          <span aria-hidden className="rtl:rotate-180">→</span>
          <span>{t.expected}</span>
          <Addresses list={d.server_addresses} expected />
        </>
      )}
      {help && <HelpTip label={t.about}>{help}</HelpTip>}
      {state !== 'checking' && (
        <button
          type="button"
          onClick={() => dns.refetch()}
          disabled={dns.isFetching}
          className="inline-flex items-center gap-1 whitespace-nowrap text-muted hover:text-fg disabled:opacity-60"
        >
          <ArrowClockwise size={12} aria-hidden className={cx(dns.isFetching && 'animate-spin motion-reduce:animate-none')} />
          {dns.isFetching ? t.checking : t.checkAgain}
        </button>
      )}
    </div>
  )
}
