// Texts of the template catalog: ready-made apps created with their database and volumes.

export const en = {
  template: 'Template',
  pick: 'Templates',
  what: 'templates',
  search: 'Search templates…',
  category: 'Category',
  all: (n: number) => `All ${n}`,
  noMatch: 'No template matches.',
  categories: { cms: 'Websites', dev: 'Developer tools', automation: 'Automation', monitoring: 'Monitoring', analytics: 'Analytics', data: 'Data stores', files: 'Files', collaboration: 'Collaboration', security: 'Security', media: 'Reading & media', ai: 'AI' },
  withDatabase: (engine: string, version: string) => `${engine} ${version}`,
  volumes: (n: number) => (n === 1 ? '1 volume' : `${n} volumes`),
  port: (port: number) => `port ${port}`,
  create: (name: string) => `Create ${name}`,
  failed: 'Could not create the app from the template',
  apps: (n: number) => `${n} apps`,
  alsoCreates: 'Also creates',
  // Shown instead of the catalog's English description, by template id.
  descriptions: {
  } as Record<string, string>,
}

export const fr: typeof en = {
  template: 'Modèle',
  pick: 'Modèles',
  what: 'modèles',
  search: 'Rechercher un modèle…',
  category: 'Catégorie',
  all: (n) => `Tous ${n}`,
  noMatch: 'Aucun modèle ne correspond.',
  categories: { cms: 'Sites web', dev: 'Outils de développement', automation: 'Automatisation', monitoring: 'Supervision', analytics: 'Analytique', data: 'Stockage de données', files: 'Fichiers', collaboration: 'Collaboration', security: 'Sécurité', media: 'Lecture et médias', ai: 'IA' },
  withDatabase: (engine, version) => `${engine} ${version}`,
  volumes: (n) => (n === 1 ? '1 volume' : `${n} volumes`),
  port: (port) => `port ${port}`,
  create: (name) => `Créer ${name}`,
  failed: "Impossible de créer l'application depuis le modèle",
  apps: (n) => `${n} applications`,
  alsoCreates: 'Crée aussi',
  descriptions: {
    wordpress: 'La plateforme de sites et de blogs la plus utilisée, avec sa base MariaDB.',
    jenkins: "Serveur d'automatisation pour les builds, tests et déploiements. Le premier mot de passe admin est dans les journaux de l'application.",
    clickhouse: "Base en colonnes pour l'analytique en temps réel, sur son interface HTTP. Utilisateur default ; le mot de passe est dans les variables de l'application.",
    signoz: "Traces, métriques et journaux via OpenTelemetry, en quatre applications. Déployez keeper, clickhouse, collector, puis celle-ci. L'OTLP va au collector.",
  },
}

export const ar: typeof en = {
  template: 'قالب',
  pick: 'القوالب',
  what: 'القوالب',
  search: 'ابحث في القوالب…',
  category: 'الفئة',
  all: (n) => `الكل ${n}`,
  noMatch: 'لا يوجد قالب مطابق.',
  categories: { cms: 'مواقع', dev: 'أدوات المطورين', automation: 'الأتمتة', monitoring: 'المراقبة', analytics: 'التحليلات', data: 'مخازن البيانات', files: 'الملفات', collaboration: 'العمل الجماعي', security: 'الأمان', media: 'القراءة والوسائط', ai: 'الذكاء الاصطناعي' },
  withDatabase: (engine, version) => `${engine} ${version}`,
  volumes: (n) => (n === 1 ? 'وحدة تخزين واحدة' : `${n} وحدات تخزين`),
  port: (port) => `المنفذ ${port}`,
  create: (name) => `إنشاء ${name}`,
  failed: 'تعذر إنشاء التطبيق من القالب',
  apps: (n) => `${n} تطبيقات`,
  alsoCreates: 'ينشئ أيضًا',
  descriptions: {
    wordpress: 'أكثر منصات المواقع والمدونات استخدامًا، مع قاعدة بيانات MariaDB.',
    jenkins: 'خادم أتمتة للبناء والاختبار والنشر. كلمة مرور المدير الأولى موجودة في سجلات التطبيق.',
    clickhouse: 'قاعدة بيانات عمودية للتحليلات الفورية، عبر واجهة HTTP. المستخدم default؛ كلمة المرور في متغيرات التطبيق.',
    signoz: 'تتبعات ومقاييس وسجلات عبر OpenTelemetry، في أربعة تطبيقات. انشر keeper ثم clickhouse ثم collector ثم هذا التطبيق. بيانات OTLP تُرسل إلى collector.',
  },
}

export const es: typeof en = {
  template: 'Plantilla',
  pick: 'Plantillas',
  what: 'plantillas',
  search: 'Buscar plantillas…',
  category: 'Categoría',
  all: (n) => `Todas ${n}`,
  noMatch: 'Ninguna plantilla coincide.',
  categories: { cms: 'Sitios web', dev: 'Herramientas de desarrollo', automation: 'Automatización', monitoring: 'Monitorización', analytics: 'Analítica', data: 'Almacenes de datos', files: 'Archivos', collaboration: 'Colaboración', security: 'Seguridad', media: 'Lectura y medios', ai: 'IA' },
  withDatabase: (engine, version) => `${engine} ${version}`,
  volumes: (n) => (n === 1 ? '1 volumen' : `${n} volúmenes`),
  port: (port) => `puerto ${port}`,
  create: (name) => `Crear ${name}`,
  failed: 'No se pudo crear la app desde la plantilla',
  apps: (n) => `${n} apps`,
  alsoCreates: 'También crea',
  descriptions: {
    wordpress: 'La plataforma de sitios y blogs más usada, con su base de datos MariaDB.',
    jenkins: 'Servidor de automatización para builds, pruebas y despliegues. La primera contraseña de administrador está en los registros de la app.',
    clickhouse: 'Base de datos columnar para analítica en tiempo real, en su interfaz HTTP. Usuario default; la contraseña está en las variables de la app.',
    signoz: 'Trazas, métricas y registros con OpenTelemetry, en cuatro apps. Despliega keeper, clickhouse, collector y luego esta. El OTLP va al collector.',
  },
}
