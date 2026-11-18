// What changed in each version, for the "What's new" menu of the navbar.
//
// Newest first. Add an entry at the top with every release; a few short lines a user understands,
// not the detail of Roadmap.md. `en` is required, the other languages fall back to it.
// The menu shows the first entries only (SHOWN in components/whats-new.tsx). A build whose version
// has no entry here (a `dev` build, a patch release) shows the first entry under its own version.

export type Release = {
  /** As the panel reports it, without a leading "v". */
  version: string
  /** Release day, YYYY-MM-DD; left out while the version is not released. */
  date?: string
  changes: { en: string[]; fr?: string[]; ar?: string[]; es?: string[] }
}

export const changelog: Release[] = [
  {
    version: '0.27.0',
    date: '2026-11-18',
    changes: {
      en: [
        'Cron jobs: schedule bash scripts on the server from the main menu, instead of crontab -e.',
        'The host terminal asks for your password before a shell opens.',
        'The host terminal can open as a chosen user instead of root, and can be switched off in Settings → Security.',
        'The whole panel is more compact.',
        'Theme previews show the new Overview.',
        'This menu: what changed in each version.',
      ],
      fr: [
        'Tâches cron : planifiez des scripts bash sur le serveur depuis le menu principal, au lieu de crontab -e.',
        'Le terminal de l’hôte demande votre mot de passe avant d’ouvrir un shell.',
        'Le terminal de l’hôte peut s’ouvrir avec un utilisateur choisi plutôt que root, et se désactive dans Paramètres → Sécurité.',
        'Tout le panneau est plus compact.',
        'Les aperçus des thèmes montrent la nouvelle Vue d’ensemble.',
        'Ce menu : ce qui a changé dans chaque version.',
      ],
      ar: [
        'مهام cron: جدولة سكربتات Bash على الخادم من القائمة الرئيسية بدل crontab -e.',
        'طرفية الخادم تطلب كلمة المرور قبل فتح الصدفة.',
        'يمكن فتح طرفية الخادم بمستخدم تختاره بدل root، ويمكن إيقافها من الإعدادات ← الأمان.',
        'اللوحة كلها أصبحت أكثر إحكامًا.',
        'معاينات السمات تعرض صفحة النظرة العامة الجديدة.',
        'هذه القائمة: ما تغيّر في كل إصدار.',
      ],
      es: [
        'Tareas cron: programa scripts de bash en el servidor desde el menú principal, en lugar de crontab -e.',
        'El terminal del host pide tu contraseña antes de abrir un shell.',
        'El terminal del host puede abrirse con un usuario elegido en lugar de root, y se desactiva en Ajustes → Seguridad.',
        'Todo el panel es más compacto.',
        'Las vistas previas de los temas muestran el nuevo Resumen.',
        'Este menú: qué cambió en cada versión.',
      ],
    },
  },
  {
    version: '0.26.0',
    date: '2026-11-18',
    changes: {
      en: [
        'Users and roles: owner, admin and maintainer.',
        'Invite people from Settings → Users with a one-time link valid 7 days.',
        'Maintainers create, deploy and configure, but cannot delete.',
        'Git connections, Backup and API tokens moved from Settings to the main menu.',
      ],
      fr: [
        'Utilisateurs et rôles : propriétaire, administrateur et mainteneur.',
        'Invitez des personnes depuis Paramètres → Utilisateurs avec un lien à usage unique valable 7 jours.',
        'Les mainteneurs créent, déploient et configurent, mais ne peuvent pas supprimer.',
        'Connexions Git, Sauvegarde et Jetons d’API sont passés des Paramètres au menu principal.',
      ],
      ar: [
        'المستخدمون والأدوار: مالك ومدير ومشرف صيانة.',
        'ادعُ أشخاصًا من الإعدادات ← المستخدمون برابط يُستعمل مرة واحدة وصالح 7 أيام.',
        'مشرفو الصيانة ينشئون وينشرون ويضبطون، لكن لا يستطيعون الحذف.',
        'اتصالات Git والنسخ الاحتياطي ورموز API انتقلت من الإعدادات إلى القائمة الرئيسية.',
      ],
      es: [
        'Usuarios y roles: propietario, administrador y mantenedor.',
        'Invita a personas desde Ajustes → Usuarios con un enlace de un solo uso válido 7 días.',
        'Los mantenedores crean, despliegan y configuran, pero no pueden eliminar.',
        'Conexiones Git, Copia de seguridad y Tokens de API pasaron de Ajustes al menú principal.',
      ],
    },
  },
  {
    version: '0.25.0',
    date: '2026-11-18',
    changes: {
      en: [
        'A new app has a page of its own, with four sources: image, Git repository, Compose file or template.',
        '41 templates: WordPress, Gitea, Keycloak, n8n, Grafana, Nextcloud, Redis and more.',
        'Paste a Compose file: each service becomes an app, with notes on what changed.',
        'Apps of a project reach each other by name: ${app.NAME.HOST} and ${app.NAME.PORT}.',
        'An app says when its settings changed since the running deployment.',
      ],
      fr: [
        'Une nouvelle application a sa propre page, avec quatre sources : image, dépôt Git, fichier Compose ou modèle.',
        '41 modèles : WordPress, Gitea, Keycloak, n8n, Grafana, Nextcloud, Redis et d’autres.',
        'Collez un fichier Compose : chaque service devient une application, avec des notes sur ce qui a changé.',
        'Les applications d’un projet se joignent par leur nom : ${app.NAME.HOST} et ${app.NAME.PORT}.',
        'Une application signale que ses réglages ont changé depuis le déploiement en cours.',
      ],
      ar: [
        'للتطبيق الجديد صفحة خاصة به، بأربعة مصادر: صورة، مستودع Git، ملف Compose أو قالب.',
        '41 قالبًا: WordPress وGitea وKeycloak وn8n وGrafana وNextcloud وRedis وغيرها.',
        'الصق ملف Compose: كل خدمة تصير تطبيقًا، مع ملاحظات عمّا تغيّر.',
        'تطبيقات المشروع تصل إلى بعضها بالاسم: ${app.NAME.HOST} و${app.NAME.PORT}.',
        'التطبيق يُعلمك حين تتغيّر إعداداته منذ النشر الجاري.',
      ],
      es: [
        'Una app nueva tiene su propia página, con cuatro orígenes: imagen, repositorio Git, archivo Compose o plantilla.',
        '41 plantillas: WordPress, Gitea, Keycloak, n8n, Grafana, Nextcloud, Redis y más.',
        'Pega un archivo Compose: cada servicio se convierte en una app, con notas sobre lo que cambió.',
        'Las apps de un proyecto se alcanzan por nombre: ${app.NAME.HOST} y ${app.NAME.PORT}.',
        'Una app avisa cuando sus ajustes cambiaron desde el despliegue en ejecución.',
      ],
    },
  },
]
