import { api, type User } from './api'

export function passkeysSupported() {
  return window.isSecureContext && typeof PublicKeyCredential !== 'undefined'
}

function decode(value: string): ArrayBuffer {
  const raw = atob(value.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(raw, (c) => c.charCodeAt(0)).buffer
}

function encode(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

type DescriptorJSON = Omit<PublicKeyCredentialDescriptor, 'id'> & { id: string }
type CreationJSON = Omit<PublicKeyCredentialCreationOptions, 'challenge' | 'user' | 'excludeCredentials'> & {
  challenge: string
  user: Omit<PublicKeyCredentialUserEntity, 'id'> & { id: string }
  excludeCredentials?: DescriptorJSON[]
}
type RequestJSON = Omit<PublicKeyCredentialRequestOptions, 'challenge' | 'allowCredentials'> & {
  challenge: string
  allowCredentials?: DescriptorJSON[]
}

function credentialJSON(credential: PublicKeyCredential) {
  const response = credential.response
  const shared = { clientDataJSON: encode(response.clientDataJSON) }
  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: response instanceof AuthenticatorAttestationResponse
      ? { ...shared, attestationObject: encode(response.attestationObject), transports: response.getTransports?.() ?? [] }
      : {
          ...shared,
          authenticatorData: encode((response as AuthenticatorAssertionResponse).authenticatorData),
          signature: encode((response as AuthenticatorAssertionResponse).signature),
          userHandle: (response as AuthenticatorAssertionResponse).userHandle ? encode((response as AuthenticatorAssertionResponse).userHandle!) : null,
        },
  }
}

export async function registerPasskey(name: string, password: string) {
  const { publicKey: options } = await api<{ publicKey: CreationJSON }>('/auth/passkeys/register/begin', { method: 'POST', body: { name, password } })
  const credential = await navigator.credentials.create({ publicKey: {
    ...options, challenge: decode(options.challenge), user: { ...options.user, id: decode(options.user.id) },
    excludeCredentials: options.excludeCredentials?.map((c) => ({ ...c, id: decode(c.id) })),
  } }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey creation was cancelled.')
  await api('/auth/passkeys/register/finish', { method: 'POST', body: credentialJSON(credential) })
}

export async function loginWithPasskey(): Promise<User> {
  const { publicKey: options } = await api<{ publicKey: RequestJSON }>('/auth/passkeys/login/begin', { method: 'POST', body: {} })
  const credential = await navigator.credentials.get({ publicKey: {
    ...options, challenge: decode(options.challenge),
    allowCredentials: options.allowCredentials?.map((c) => ({ ...c, id: decode(c.id) })),
  } }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey sign-in was cancelled.')
  return api<User>('/auth/passkeys/login/finish', { method: 'POST', body: credentialJSON(credential) })
}

/** Confirms with one of the signed-in user's passkeys, in place of the password, to unlock host access. */
export async function unlockHostWithPasskey() {
  const { publicKey: options } = await api<{ publicKey: RequestJSON }>('/auth/passkeys/host-unlock/begin', { method: 'POST', body: {} })
  const credential = await navigator.credentials.get({ publicKey: {
    ...options, challenge: decode(options.challenge),
    allowCredentials: options.allowCredentials?.map((c) => ({ ...c, id: decode(c.id) })),
  } }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey confirmation was cancelled.')
  await api('/auth/passkeys/host-unlock/finish', { method: 'POST', body: credentialJSON(credential) })
}
