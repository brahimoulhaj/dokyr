// Texts of the setup guide shown after the owner account is created.

export const en = {
  skip: 'Skip setup',
  stepsLabel: 'Setup steps',
  domainTitle: 'Panel domain',
  domainIntro:
    'Serve this panel on its own hostname over HTTPS. Without one it stays on http://<server-ip>, which is fine to start with.',
  storageTitle: 'Storage',
  storageIntro:
    'Database and panel backups go to an S3 bucket. Add one you already have: AWS S3, Cloudflare R2, Backblaze B2 or any S3-compatible service.',
  storageBuiltin: 'No bucket yet? <a>Run storage on this server</a> (experimental).',
  registryTitle: 'Registry',
  registryIntro:
    'Apps can pull private images from a registry you use (GHCR, Docker Hub…), or push to one on this server that deploys on every push.',
  notificationsTitle: 'Notifications',
  notificationsIntro:
    'Be told when a deploy fails, an app goes down, a backup fails or the disk fills up: add a channel, then a rule that sends events to it.',
  deployTitle: 'First deploy',
  deployIntro: 'That is the setup. Next: a project, an app from an image, a Git repository or a template, and a domain for it.',
  projectLine: '<b>Project</b> — a folder for related apps.',
  appLine: '<b>App</b> — one container image run as one container, with CPU and memory limits.',
  domainLine: '<b>Domain</b> — attach one to an app and it is served over HTTPS with an automatic certificate.',
  back: 'Back',
  next: 'Next',
  goToOverview: 'Go to overview',
  createProject: 'Create a project',
}

export const fr: typeof en = {
  skip: 'Passer la configuration',
  stepsLabel: 'Étapes de configuration',
  domainTitle: 'Domaine du panneau',
  domainIntro:
    "Servez ce panneau sur son propre nom d'hôte en HTTPS. Sans domaine, il reste sur http://<server-ip>, ce qui convient pour commencer.",
  storageTitle: 'Stockage',
  storageIntro:
    "Les sauvegardes des bases de données et du panneau vont dans un bucket S3. Ajoutez-en un que vous avez déjà : AWS S3, Cloudflare R2, Backblaze B2 ou tout service compatible S3.",
  storageBuiltin: 'Pas encore de bucket ? <a>Lancez un stockage sur ce serveur</a> (expérimental).',
  registryTitle: 'Registre',
  registryIntro:
    "Les applications peuvent tirer des images privées d'un registre que vous utilisez (GHCR, Docker Hub…), ou pousser vers un registre sur ce serveur qui déploie à chaque push.",
  notificationsTitle: 'Notifications',
  notificationsIntro:
    "Soyez prévenu quand un déploiement échoue, qu'une application tombe, qu'une sauvegarde échoue ou que le disque se remplit : ajoutez un canal, puis une règle qui lui envoie les événements.",
  deployTitle: 'Premier déploiement',
  deployIntro:
    "C'est toute la configuration. Ensuite : un projet, une application à partir d'une image, d'un dépôt Git ou d'un modèle, et un domaine pour elle.",
  projectLine: '<b>Projet</b> — un dossier pour des applications liées.',
  appLine: '<b>Application</b> — une image de conteneur exécutée dans un seul conteneur, avec des limites de CPU et de mémoire.',
  domainLine: '<b>Domaine</b> — associez-en un à une application et elle est servie en HTTPS avec un certificat automatique.',
  back: 'Retour',
  next: 'Suivant',
  goToOverview: "Aller à la vue d'ensemble",
  createProject: 'Créer un projet',
}

export const ar: typeof en = {
  skip: 'تخطي الإعداد',
  stepsLabel: 'خطوات الإعداد',
  domainTitle: 'نطاق لوحة التحكم',
  domainIntro:
    'اجعل لوحة التحكم متاحة على اسم نطاق خاص بها عبر HTTPS. من دونه تبقى على ⁦http://<server-ip>⁩، وهذا كافٍ للبداية.',
  storageTitle: 'التخزين',
  storageIntro:
    'تُرسل نسخ قواعد البيانات ولوحة التحكم الاحتياطية إلى حاوية S3. أضف حاوية لديك: AWS S3 أو Cloudflare R2 أو Backblaze B2 أو أي خدمة متوافقة مع S3.',
  storageBuiltin: 'لا تملك حاوية بعد؟ <a>شغّل تخزيناً على هذا الخادم</a> (تجريبي).',
  registryTitle: 'السجل',
  registryIntro:
    'يمكن للتطبيقات سحب صور خاصة من سجل تستخدمه (GHCR وDocker Hub…)، أو الدفع إلى سجل على هذا الخادم ينشر مع كل دفع.',
  notificationsTitle: 'الإشعارات',
  notificationsIntro:
    'اعرف عند فشل نشر أو توقف تطبيق أو فشل نسخة احتياطية أو امتلاء القرص: أضف قناة، ثم قاعدة ترسل الأحداث إليها.',
  deployTitle: 'أول نشر',
  deployIntro: 'انتهى الإعداد. التالي: مشروع، ثم تطبيق من صورة أو مستودع Git أو قالب، ثم نطاق له.',
  projectLine: '<b>مشروع</b> — مجلد لتطبيقات مترابطة.',
  appLine: '<b>تطبيق</b> — صورة حاوية واحدة تعمل كحاوية واحدة، مع حدود للمعالج والذاكرة.',
  domainLine: '<b>نطاق</b> — اربطه بتطبيق فيُتاح عبر HTTPS بشهادة تلقائية.',
  back: 'السابق',
  next: 'التالي',
  goToOverview: 'الانتقال إلى النظرة العامة',
  createProject: 'إنشاء مشروع',
}

export const es: typeof en = {
  skip: 'Omitir configuración',
  stepsLabel: 'Pasos de configuración',
  domainTitle: 'Dominio del panel',
  domainIntro:
    'Sirve este panel en su propio nombre de host por HTTPS. Sin él se queda en http://<server-ip>, que está bien para empezar.',
  storageTitle: 'Almacenamiento',
  storageIntro:
    'Las copias de las bases de datos y del panel van a un bucket S3. Añade uno que ya tengas: AWS S3, Cloudflare R2, Backblaze B2 o cualquier servicio compatible con S3.',
  storageBuiltin: '¿Aún no tienes un bucket? <a>Ejecuta un almacenamiento en este servidor</a> (experimental).',
  registryTitle: 'Registro',
  registryIntro:
    'Las aplicaciones pueden descargar imágenes privadas de un registro que uses (GHCR, Docker Hub…), o subir a uno en este servidor que despliega con cada push.',
  notificationsTitle: 'Notificaciones',
  notificationsIntro:
    'Entérate cuando falla un despliegue, se cae una aplicación, falla una copia o se llena el disco: añade un canal y luego una regla que le envíe los eventos.',
  deployTitle: 'Primer despliegue',
  deployIntro:
    'Esa es la configuración. Siguiente: un proyecto, una aplicación desde una imagen, un repositorio Git o una plantilla, y un dominio para ella.',
  projectLine: '<b>Proyecto</b> — una carpeta para aplicaciones relacionadas.',
  appLine: '<b>Aplicación</b> — una imagen de contenedor ejecutada como un solo contenedor, con límites de CPU y memoria.',
  domainLine: '<b>Dominio</b> — asocia uno a una aplicación y se sirve por HTTPS con un certificado automático.',
  back: 'Atrás',
  next: 'Siguiente',
  goToOverview: 'Ir al resumen',
  createProject: 'Crear un proyecto',
}
