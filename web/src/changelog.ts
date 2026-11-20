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
    version: '0.31.0',
    date: '2026-11-20',
    changes: {
      en: [
        'Deploy on push: a Git app can deploy only when chosen paths change (Settings → Deploy webhook), and a new GitHub App connection reports pushes itself, with no webhook per repository.',
        'Host access: a host shell closes after a time without input (Settings → Security), a passkey can confirm instead of the password, and a cron script that starts with #! runs with that interpreter.',
        'Databases: Sync with server lists the databases, users and permissions made outside the panel.',
        '“Changes not deployed” also follows project variables, database references and other apps’ addresses. The Overview keeps 30 days of host history.',
        'Errors from the server and the panel’s lines in deployment logs follow the interface language.',
      ],
      fr: [
        'Déploiement au push : une app Git peut ne se déployer que si des chemins choisis changent (Paramètres → Webhook de déploiement), et une nouvelle connexion GitHub App signale elle-même les pushs, sans webhook par dépôt.',
        'Accès à l’hôte : un shell de l’hôte se ferme après un temps sans saisie (Paramètres → Sécurité), une clé d’accès peut confirmer à la place du mot de passe, et un script cron qui commence par #! s’exécute avec cet interpréteur.',
        'Bases de données : « Synchroniser avec le serveur » liste les bases, utilisateurs et permissions créés hors du panneau.',
        '« Modifications non déployées » suit aussi les variables du projet, les références de base de données et les adresses des autres apps. La vue d’ensemble garde 30 jours d’historique du serveur.',
        'Les erreurs du serveur et les lignes du panneau dans les journaux de déploiement suivent la langue de l’interface.',
      ],
      ar: [
        'النشر عند الدفع: يمكن لتطبيق Git ألا يُنشر إلا عند تغيّر مسارات محددة (الإعدادات ← Webhook النشر)، واتصال GitHub App الجديد يُبلغ عن عمليات الدفع بنفسه دون webhook لكل مستودع.',
        'الوصول إلى الخادم: تُغلق صدفة الخادم بعد مدة بلا إدخال (الإعدادات ← الأمان)، ويمكن التأكيد بمفتاح مرور بدل كلمة المرور، وسكربت cron الذي يبدأ بـ #! يُنفَّذ بذلك المفسّر.',
        'قواعد البيانات: «المزامنة مع الخادم» تسرد القواعد والمستخدمين والصلاحيات المنشأة خارج اللوحة.',
        '«تغييرات غير منشورة» تتبع أيضًا متغيرات المشروع ومراجع قواعد البيانات وعناوين التطبيقات الأخرى. النظرة العامة تحفظ 30 يومًا من سجل الخادم.',
        'أخطاء الخادم وسطور اللوحة في سجلات النشر تتبع لغة الواجهة.',
      ],
      es: [
        'Despliegue al hacer push: una app Git puede desplegarse solo cuando cambian las rutas elegidas (Ajustes → Webhook de despliegue), y una conexión GitHub App nueva avisa ella misma de los pushes, sin webhook por repositorio.',
        'Acceso al servidor: un shell del servidor se cierra tras un tiempo sin entrada (Ajustes → Seguridad), una llave de acceso puede confirmar en lugar de la contraseña, y un script cron que empieza por #! se ejecuta con ese intérprete.',
        'Bases de datos: «Sincronizar con el servidor» lista las bases, usuarios y permisos creados fuera del panel.',
        '«Cambios sin desplegar» sigue también las variables del proyecto, las referencias de bases de datos y las direcciones de otras apps. El resumen guarda 30 días de historial del servidor.',
        'Los errores del servidor y las líneas del panel en los registros de despliegue siguen el idioma de la interfaz.',
      ],
    },
  },
  {
    version: '0.30.0',
    date: '2026-11-19',
    changes: {
      en: [
        'Security: a maintainer can no longer mount host paths, pick a Buildpacks builder image, change the panel domain, apply updates, or manage notification channels, storage and registry settings: an owner or an admin does. The pages hide those controls for maintainers.',
        'The proxy tells the panel the real client address with a secret, so containers on the same network cannot fake it to get around the sign-in rate limit.',
        'Git hosts, GitLab and S3 on private addresses need an admin; webhooks and Git hosts no longer follow redirects.',
        'Content-Security-Policy and HSTS headers; deploy-hook tokens are hidden in the panel log.',
      ],
      fr: [
        'Sécurité : un mainteneur ne peut plus monter des chemins de l’hôte, choisir une image de builder Buildpacks, changer le domaine du panneau, appliquer des mises à jour ni gérer les canaux de notification, le stockage et le registre : cela revient au propriétaire ou à un admin. Les pages masquent ces commandes aux mainteneurs.',
        'Le proxy transmet l’adresse réelle du client au panneau avec un secret : un conteneur du même réseau ne peut plus la falsifier pour contourner la limite de connexions.',
        'Les hôtes Git, GitLab et S3 sur des adresses privées demandent un admin ; les webhooks et les hôtes Git ne suivent plus les redirections.',
        'En-têtes Content-Security-Policy et HSTS ; les jetons de déploiement sont masqués dans le journal du panneau.',
      ],
      ar: [
        'الأمان: لم يعد بإمكان المشرف (maintainer) ربط مسارات المضيف أو اختيار صورة Buildpacks أو تغيير نطاق اللوحة أو تطبيق التحديثات أو إدارة قنوات الإشعارات والتخزين والسجل؛ هذا للمالك أو للمسؤول. تخفي الصفحات هذه الأزرار عن المشرفين.',
        'يمرّر الوكيل عنوان العميل الحقيقي إلى اللوحة بسر، فلا تستطيع الحاويات على الشبكة نفسها تزييفه للالتفاف على حدّ محاولات الدخول.',
        'مضيفات Git وGitLab وS3 على عناوين خاصة تتطلب مسؤولاً؛ ولا تتبع خطافات الويب ومضيفات Git عمليات إعادة التوجيه.',
        'ترويستا Content-Security-Policy وHSTS؛ ورموز خطاف النشر مخفية في سجل اللوحة.',
      ],
      es: [
        'Seguridad: un mantenedor ya no puede montar rutas del host, elegir una imagen de builder de Buildpacks, cambiar el dominio del panel, aplicar actualizaciones ni gestionar canales de notificación, almacenamiento y registro: lo hace el propietario o un administrador. Las páginas ocultan esos controles a los mantenedores.',
        'El proxy pasa al panel la dirección real del cliente con un secreto, así que un contenedor de la misma red ya no puede falsearla para esquivar el límite de inicios de sesión.',
        'Los hosts Git, GitLab y S3 en direcciones privadas requieren un administrador; los webhooks y los hosts Git ya no siguen redirecciones.',
        'Cabeceras Content-Security-Policy y HSTS; los tokens de despliegue se ocultan en el registro del panel.',
      ],
    },
  },
  {
    version: '0.29.0',
    date: '2026-11-19',
    changes: {
      en: [
        'Three more builders for Git apps: Nixpacks, Buildpacks (Heroku by default) and Static sites served by Caddy.',
        'Static sites: an optional build command, an output folder and a single-page app option.',
        'PHP apps built by Railpack get the MySQL and PostgreSQL drivers: a build-time variable is added on the first build, yours to edit.',
        'Pages have a little less padding.',
      ],
      fr: [
        'Trois builders de plus pour les apps Git : Nixpacks, Buildpacks (Heroku par défaut) et les sites statiques servis par Caddy.',
        'Sites statiques : une commande de build facultative, un dossier de sortie et une option application monopage.',
        'Les apps PHP construites par Railpack ont les pilotes MySQL et PostgreSQL : une variable de build est ajoutée au premier build, modifiable ensuite.',
        'Les pages ont un peu moins de marge.',
      ],
      ar: [
        'ثلاث أدوات بناء إضافية لتطبيقات Git: Nixpacks وBuildpacks (Heroku افتراضياً) والمواقع الثابتة التي يقدّمها Caddy.',
        'المواقع الثابتة: أمر بناء اختياري، مجلد مخرجات، وخيار تطبيق من صفحة واحدة.',
        'تطبيقات PHP التي يبنيها Railpack تحصل على مشغّلات MySQL وPostgreSQL: يُضاف متغير وقت البناء عند أول بناء، ويمكنك تعديله.',
        'هوامش الصفحات أصغر قليلاً.',
      ],
      es: [
        'Tres compiladores más para apps Git: Nixpacks, Buildpacks (Heroku por defecto) y sitios estáticos servidos por Caddy.',
        'Sitios estáticos: un comando de compilación opcional, una carpeta de salida y una opción de aplicación de una sola página.',
        'Las apps PHP compiladas por Railpack tienen los controladores de MySQL y PostgreSQL: se añade una variable de compilación en la primera compilación, que puedes editar.',
        'Las páginas tienen un poco menos de margen.',
      ],
    },
  },
  {
    version: '0.28.0',
    date: '2026-11-18',
    changes: {
      en: [
        'Built-in storage: access keys limited to one bucket, read only or read & write, for your apps and scripts.',
        'The root storage credentials are no longer shown; give each app its own key instead.',
        'The Storage page is split into Overview, Buckets and Access keys tabs.',
        'The setup guide has new steps for storage, a registry and notifications.',
        'Git connections, API tokens, build limits and the panel domain open in side sheets.',
      ],
      fr: [
        "Stockage intégré : des clés d'accès limitées à un bucket, en lecture seule ou en lecture et écriture, pour vos applications et scripts.",
        "Les identifiants root du stockage ne sont plus affichés ; donnez plutôt à chaque application sa propre clé.",
        "La page Stockage est découpée en onglets : Vue d'ensemble, Buckets et Clés d'accès.",
        'Le guide de configuration a de nouvelles étapes pour le stockage, un registre et les notifications.',
        'Les connexions Git, les jetons API, les limites de build et le domaine du panneau s’ouvrent dans des panneaux latéraux.',
      ],
      ar: [
        'التخزين المدمج: مفاتيح وصول محدودة بحاوية واحدة، للقراءة فقط أو للقراءة والكتابة، لتطبيقاتك وسكربتاتك.',
        'لم تعد بيانات الدخول الجذرية للتخزين تُعرض؛ أعطِ كل تطبيق مفتاحه الخاص بدلاً من ذلك.',
        'صفحة التخزين مقسّمة إلى تبويبات: نظرة عامة، الحاويات، مفاتيح الوصول.',
        'دليل الإعداد فيه خطوات جديدة للتخزين والسجل والإشعارات.',
        'اتصالات Git ورموز API وحدود البناء ونطاق اللوحة تُفتح في لوحات جانبية.',
      ],
      es: [
        'Almacenamiento integrado: claves de acceso limitadas a un bucket, de solo lectura o de lectura y escritura, para tus aplicaciones y scripts.',
        'Las credenciales root del almacenamiento ya no se muestran; da a cada aplicación su propia clave.',
        'La página Almacenamiento se divide en pestañas: Resumen, Buckets y Claves de acceso.',
        'La guía de configuración tiene pasos nuevos para el almacenamiento, un registro y las notificaciones.',
        'Las conexiones Git, los tokens de API, los límites de build y el dominio del panel se abren en paneles laterales.',
      ],
    },
  },
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
