// Texts of the DNS hint shown next to a hostname.

// Address lists are left-to-right; in Arabic they are isolated so a list keeps its order.
const ltr = (s: string) => `⁦${s}⁩`

export const en = {
  failed: 'DNS check failed.',
  checking: 'Checking DNS…',
  checkAgain: 'Check again',
  verified: 'Verified', elsewhere: 'Other server', unconfirmed: 'Unconfirmed', noRecord: 'No DNS record',
  checkFailed: 'Check failed', checkingShort: 'Checking', expected: 'expected', status: 'DNS status', about: 'About this status',
  notResolving: 'Does not resolve yet. Create an A record pointing at this server.',
  pointsHere: (addresses: string) => `Resolves to ${addresses} — this server.`,
  pointsElsewhere: (addresses: string, server: string) =>
    `Resolves to ${addresses}, not to this server (${server}). Fine behind a CDN or proxy.`,
  unknown: (addresses: string) => `Resolves to ${addresses}. The server cannot tell whether that is itself.`,
  cloudflare: 'Cloudflare',
  viaCloudflare: (addresses: string) => `Resolves to ${addresses}: Cloudflare’s proxy. Cloudflare forwards to the origin set in its DNS record, which the panel cannot see from here.`,
}

export const fr: typeof en = {
  failed: 'La vérification DNS a échoué.',
  checking: 'Vérification du DNS…',
  checkAgain: 'Vérifier à nouveau',
  verified: 'Vérifié', elsewhere: 'Autre serveur', unconfirmed: 'Non confirmé', noRecord: 'Aucun enregistrement',
  checkFailed: 'Échec', checkingShort: 'Vérification', expected: 'attendu', status: 'État DNS', about: 'À propos de cet état',
  notResolving: 'Ne se résout pas encore. Créez un enregistrement A pointant vers ce serveur.',
  pointsHere: (addresses) => `Se résout vers ${addresses} — ce serveur.`,
  pointsElsewhere: (addresses, server) =>
    `Se résout vers ${addresses}, pas vers ce serveur (${server}). Normal derrière un CDN ou un proxy.`,
  unknown: (addresses) => `Se résout vers ${addresses}. Le serveur ne peut pas déterminer s’il s’agit de lui-même.`,
  cloudflare: 'Cloudflare',
  viaCloudflare: (addresses) => `Se résout vers ${addresses} : le proxy de Cloudflare. Cloudflare transmet à l’origine définie dans son enregistrement DNS, que le panneau ne peut pas voir d’ici.`,
}

export const ar: typeof en = {
  failed: 'فشل فحص DNS.',
  checking: 'جارٍ فحص DNS…',
  checkAgain: 'إعادة الفحص',
  verified: 'تم التحقق', elsewhere: 'خادم آخر', unconfirmed: 'غير مؤكد', noRecord: 'لا يوجد سجل',
  checkFailed: 'فشل الفحص', checkingShort: 'جارٍ الفحص', expected: 'المتوقع', status: 'حالة DNS', about: 'حول هذه الحالة',
  notResolving: 'لا يُحلّ بعد. أنشئ سجل A يشير إلى هذا الخادم.',
  pointsHere: (addresses) => `يُحلّ إلى ${ltr(addresses)} — هذا الخادم.`,
  pointsElsewhere: (addresses, server) =>
    `يُحلّ إلى ${ltr(addresses)}، وليس إلى هذا الخادم (${ltr(server)}). لا بأس بذلك خلف CDN أو وسيط.`,
  unknown: (addresses) => `يُحلّ إلى ${ltr(addresses)}. لا يستطيع الخادم معرفة ما إذا كان هذا عنوانه.`,
  cloudflare: 'Cloudflare',
  viaCloudflare: (addresses) => `يُحلّ إلى ${ltr(addresses)}: وسيط Cloudflare. يحوّل Cloudflare الطلبات إلى الأصل المحدد في سجل DNS لديه، ولا تستطيع اللوحة رؤيته من هنا.`,
}

export const es: typeof en = {
  failed: 'La comprobación de DNS ha fallado.',
  checking: 'Comprobando DNS…',
  checkAgain: 'Comprobar de nuevo',
  verified: 'Verificado', elsewhere: 'Otro servidor', unconfirmed: 'Sin confirmar', noRecord: 'Sin registro',
  checkFailed: 'Error', checkingShort: 'Comprobando', expected: 'esperado', status: 'Estado DNS', about: 'Sobre este estado',
  notResolving: 'Aún no se resuelve. Crea un registro A que apunte a este servidor.',
  pointsHere: (addresses) => `Se resuelve a ${addresses} — este servidor.`,
  pointsElsewhere: (addresses, server) =>
    `Se resuelve a ${addresses}, no a este servidor (${server}). Es normal detrás de un CDN o un proxy.`,
  unknown: (addresses) => `Se resuelve a ${addresses}. El servidor no puede saber si es él mismo.`,
  cloudflare: 'Cloudflare',
  viaCloudflare: (addresses) => `Se resuelve a ${addresses}: el proxy de Cloudflare. Cloudflare reenvía al origen definido en su registro DNS, que el panel no puede ver desde aquí.`,
}
