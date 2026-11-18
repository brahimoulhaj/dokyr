// Texts of the Compose source: a pasted Compose file converted into one app for each service.

export const en = {
  compose: 'Compose',
  file: 'Compose file',
  checking: 'Checking…',
  valid: (apps: number, notes: number) => `Valid · ${apps === 1 ? '1 app' : `${apps} apps`}${notes > 0 ? ` · ${notes === 1 ? '1 note' : `${notes} notes`} in the summary` : ''}`,
  summary: (port: number, variables: number, volumes: number) =>
    [port > 0 && `port ${port}`, variables > 0 && (variables === 1 ? '1 variable' : `${variables} variables`), volumes > 0 && (volumes === 1 ? '1 volume' : `${volumes} volumes`)].filter(Boolean).join(' · '),
  kinds: { linked: 'Linked', check: 'Check', skipped: 'Not converted' },
  create: (n: number) => (n === 0 ? 'Create apps' : n === 1 ? 'Create 1 app' : `Create ${n} apps`),
  creating: (n: number) => (n === 1 ? 'Creating 1 app…' : `Creating ${n} apps…`),
  created: (n: number) => (n === 1 ? '1 app created, not deployed yet' : `${n} apps created, not deployed yet`),
  failed: 'Could not create the apps',
}

export const fr: typeof en = {
  compose: 'Compose',
  file: 'Fichier Compose',
  checking: 'Vérification…',
  valid: (apps, notes) => `Valide · ${apps === 1 ? '1 application' : `${apps} applications`}${notes > 0 ? ` · ${notes === 1 ? '1 remarque' : `${notes} remarques`} dans le résumé` : ''}`,
  summary: (port, variables, volumes) =>
    [port > 0 && `port ${port}`, variables > 0 && (variables === 1 ? '1 variable' : `${variables} variables`), volumes > 0 && (volumes === 1 ? '1 volume' : `${volumes} volumes`)].filter(Boolean).join(' · '),
  kinds: { linked: 'Lié', check: 'À vérifier', skipped: 'Non converti' },
  create: (n) => (n === 0 ? 'Créer les applications' : n === 1 ? 'Créer 1 application' : `Créer ${n} applications`),
  creating: (n) => (n === 1 ? 'Création de 1 application…' : `Création de ${n} applications…`),
  created: (n) => (n === 1 ? '1 application créée, pas encore déployée' : `${n} applications créées, pas encore déployées`),
  failed: 'Impossible de créer les applications',
}

export const ar: typeof en = {
  compose: 'Compose',
  file: 'ملف Compose',
  checking: 'جارٍ التحقق…',
  valid: (apps, notes) => `صالح · ${apps === 1 ? 'تطبيق واحد' : `${apps} تطبيقات`}${notes > 0 ? ` · ${notes === 1 ? 'ملاحظة واحدة' : `${notes} ملاحظات`} في الملخص` : ''}`,
  summary: (port, variables, volumes) =>
    [port > 0 && `المنفذ ${port}`, variables > 0 && (variables === 1 ? 'متغير واحد' : `${variables} متغيرات`), volumes > 0 && (volumes === 1 ? 'وحدة تخزين واحدة' : `${volumes} وحدات تخزين`)].filter(Boolean).join(' · '),
  kinds: { linked: 'مرتبط', check: 'للمراجعة', skipped: 'لم يُحوَّل' },
  create: (n) => (n === 0 ? 'إنشاء التطبيقات' : n === 1 ? 'إنشاء تطبيق واحد' : `إنشاء ${n} تطبيقات`),
  creating: (n) => (n === 1 ? 'جارٍ إنشاء تطبيق واحد…' : `جارٍ إنشاء ${n} تطبيقات…`),
  created: (n) => (n === 1 ? 'أُنشئ تطبيق واحد، لم يُنشر بعد' : `أُنشئت ${n} تطبيقات، لم تُنشر بعد`),
  failed: 'تعذر إنشاء التطبيقات',
}

export const es: typeof en = {
  compose: 'Compose',
  file: 'Archivo Compose',
  checking: 'Comprobando…',
  valid: (apps, notes) => `Válido · ${apps === 1 ? '1 app' : `${apps} apps`}${notes > 0 ? ` · ${notes === 1 ? '1 nota' : `${notes} notas`} en el resumen` : ''}`,
  summary: (port, variables, volumes) =>
    [port > 0 && `puerto ${port}`, variables > 0 && (variables === 1 ? '1 variable' : `${variables} variables`), volumes > 0 && (volumes === 1 ? '1 volumen' : `${volumes} volúmenes`)].filter(Boolean).join(' · '),
  kinds: { linked: 'Enlazado', check: 'Revisar', skipped: 'No convertido' },
  create: (n) => (n === 0 ? 'Crear apps' : n === 1 ? 'Crear 1 app' : `Crear ${n} apps`),
  creating: (n) => (n === 1 ? 'Creando 1 app…' : `Creando ${n} apps…`),
  created: (n) => (n === 1 ? '1 app creada, aún sin desplegar' : `${n} apps creadas, aún sin desplegar`),
  failed: 'No se pudieron crear las apps',
}
