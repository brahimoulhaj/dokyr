// Texts for connecting a database to an app made from a template that needs one.

export const en = {
  afterCreate: (engine: string) => `${engine} — connect one after creating`,
  needs: (app: string, engine: string) => `${app} needs a ${engine} database. Connect one before deploying.`,
  connect: 'Connect database',
  title: (engine: string) => `Connect a ${engine} database`,
  instance: 'Database instance',
  database: 'Database',
  user: 'User',
  starting: 'starting',
  manage: 'Add a database or a user in this instance',
  newInstance: 'New instance',
  newTitle: (engine: string) => `New ${engine} instance`,
  none: (engine: string) => `This project has no ${engine} instance yet.`,
  back: 'Back',
  connected: (name: string) => `Connected to ${name}`,
}

export const fr: typeof en = {
  afterCreate: (engine) => `${engine} — à connecter après la création`,
  needs: (app, engine) => `${app} a besoin d'une base ${engine}. Connectez-en une avant de déployer.`,
  connect: 'Connecter une base',
  title: (engine) => `Connecter une base ${engine}`,
  instance: 'Instance de base de données',
  database: 'Base de données',
  user: 'Utilisateur',
  starting: 'démarrage',
  manage: 'Ajouter une base ou un utilisateur dans cette instance',
  newInstance: 'Nouvelle instance',
  newTitle: (engine) => `Nouvelle instance ${engine}`,
  none: (engine) => `Ce projet n'a pas encore d'instance ${engine}.`,
  back: 'Retour',
  connected: (name) => `Connectée à ${name}`,
}

export const ar: typeof en = {
  afterCreate: (engine) => `${engine} — تُربط بعد الإنشاء`,
  needs: (app, engine) => `يحتاج ${app} إلى قاعدة بيانات ${engine}. اربط واحدة قبل النشر.`,
  connect: 'ربط قاعدة بيانات',
  title: (engine) => `ربط قاعدة بيانات ${engine}`,
  instance: 'مثيل قاعدة البيانات',
  database: 'قاعدة البيانات',
  user: 'المستخدم',
  starting: 'قيد البدء',
  manage: 'أضف قاعدة بيانات أو مستخدمًا في هذا المثيل',
  newInstance: 'مثيل جديد',
  newTitle: (engine) => `مثيل ${engine} جديد`,
  none: (engine) => `لا يوجد في هذا المشروع مثيل ${engine} بعد.`,
  back: 'رجوع',
  connected: (name) => `تم الربط بـ ${name}`,
}

export const es: typeof en = {
  afterCreate: (engine) => `${engine} — conéctala después de crear`,
  needs: (app, engine) => `${app} necesita una base de datos ${engine}. Conecta una antes de desplegar.`,
  connect: 'Conectar base de datos',
  title: (engine) => `Conectar una base de datos ${engine}`,
  instance: 'Instancia de base de datos',
  database: 'Base de datos',
  user: 'Usuario',
  starting: 'iniciando',
  manage: 'Añadir una base de datos o un usuario en esta instancia',
  newInstance: 'Nueva instancia',
  newTitle: (engine) => `Nueva instancia ${engine}`,
  none: (engine) => `Este proyecto aún no tiene una instancia ${engine}.`,
  back: 'Volver',
  connected: (name) => `Conectada a ${name}`,
}
