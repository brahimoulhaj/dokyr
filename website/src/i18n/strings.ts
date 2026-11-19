// Every word on the landing page, per language. Log lines and commit messages in the built
// panel stay in English in every language, as they do in the real panel.

import { sitePath } from '../data/site';

export const locales = ['en', 'fr', 'es', 'ar'] as const;
export type Locale = (typeof locales)[number];

export const localeNames: Record<Locale, string> = {
  en: 'English',
  fr: 'Français',
  es: 'Español',
  ar: 'العربية',
};

export const dir = (l: Locale) => (l === 'ar' ? 'rtl' : 'ltr');
export const pathFor = (l: Locale) => sitePath(l === 'en' ? '/' : `/${l}/`);

type Item = { t: string; d: string };

export type Strings = {
  meta: { title: string; description: string };
  nav: {
    skip: string;
    panel: string;
    deploy: string;
    install: string;
    theme: string;
    language: string;
    home: string;
  };
  hero: { h1: string; lede: string; primary: string; secondary: string };
  tour: {
    label: string;
    tabs: { overview: string; deployments: string; metrics: string; newApp: string };
    notes: { overview: string; deployments: string; metrics: string; newApp: string };
    theme: string;
  };
  deploy: {
    h2: string;
    line: string;
    tabsLabel: string;
    tabs: { git: string; image: string; compose: string; template: string };
    notes: { git: string; image: string; compose: string; template: string };
    builders: string;
  };
  included: { h2: string; items: Item[] };
  picker: {
    choose: string;
    use: string;
    show: string;
    alt: string;
    light: string;
    dark: string;
    current: string;
    keys: string[];
    previous: string;
    next: string;
  };
  compare: { h2: string; them: string; us: string; rows: [string, string, string][] };
  faq: { h2: string; items: Item[] };
  install: {
    h2: string;
    steps: string[];
    update: string;
    copy: string;
    copied: string;
    source: string;
    releases: string;
  };
  footer: { license: string; languages: string };
};

const en: Strings = {
  meta: {
    title: 'Dokyr, your own PaaS on one server',
    description:
      'Open source, self-hosted PaaS. Push to deploy, automatic HTTPS, databases with S3 backups, logs and alerts, installed with one command on any VPS.',
  },
  nav: {
    skip: 'Skip to content',
    panel: 'Panel',
    deploy: 'Deploy',
    install: 'Install',
    theme: 'Theme',
    language: 'Language',
    home: 'Dokyr home',
  },
  hero: {
    h1: 'Your own PaaS on one server',
    lede: 'Push to deploy, HTTPS, databases and backups, on a VPS you own.',
    primary: 'Install Dokyr',
    secondary: 'Tour the panel',
  },
  tour: {
    label: 'Screens of the panel',
    tabs: { overview: 'Overview', deployments: 'Deployments', metrics: 'Metrics', newApp: 'New app' },
    notes: {
      overview: 'Host and Docker metrics, live, with every project on one page.',
      deployments: 'Every deployment with its commit, its timeline and a rollback.',
      metrics: 'CPU, memory, network and disk for one app, up to 30 days back.',
      newApp: 'A new app from Git, an image, a Compose file or one of 41 templates.',
    },
    theme: 'Shown in the {theme} theme. Pick another from the palette icon and the screens follow.',
  },
  deploy: {
    h2: 'Traffic moves after the health check',
    line: 'Bring a repository, an image, a Compose file or a template. Each gets the same rollout: build, health check, then traffic.',
    tabsLabel: 'Ways to deploy',
    tabs: { git: 'Git repository', image: 'Docker image', compose: 'Compose file', template: 'Template' },
    notes: {
      git: 'GitHub App, GitLab token or deploy key. Every push builds and ships.',
      image: 'Any public or private registry, including the one built into Dokyr.',
      compose: 'Paste a Compose file. Each service becomes an app you manage on its own.',
      template: '41 ready apps, from WordPress and Gitea to Grafana and Ollama.',
    },
    builders: 'Builders',
  },
  included: {
    h2: 'The paid add‑ons are included',
    items: [
      { t: 'Zero‑downtime deploys', d: 'A health check passes before traffic moves.' },
      { t: 'Automatic HTTPS', d: 'Caddy issues a certificate for every domain.' },
      { t: 'Databases', d: 'PostgreSQL and MariaDB per project, with Adminer.' },
      { t: 'Backups to S3', d: 'Scheduled dumps with retention, restored to a new database.' },
      { t: 'Logs and alerts', d: 'Live logs, metrics, and alerts to Slack or email.' },
      { t: 'Registry and storage', d: 'Private images and S3 buckets on the same server.' },
    ],
  },
  picker: {
    choose: 'Choose a theme',
    use: 'Use {name}',
    show: 'Show {name}',
    alt: 'The Dokyr home page in the {name} theme',
    light: 'light',
    dark: 'dark',
    current: 'current',
    keys: ['← → browse', 'Enter or click to apply', 'Esc to close'],
    previous: 'Previous theme',
    next: 'Next theme',
  },
  compare: {
    h2: 'Rent a platform, or own one',
    them: 'Hosted PaaS',
    us: 'Dokyr',
    rows: [
      ['Price', 'Per app, per seat, per gigabyte', 'Your server bill'],
      ['Your data', 'Their region, their disks', 'Your server, your volumes'],
      ['Databases', 'A paid add-on each', 'Included, with backups'],
      ['Leaving', 'Their config and their CLI', 'Plain Docker images, MIT licence'],
      ['Limits', 'Sleeping apps, build minutes', 'The size of your machine'],
    ],
  },
  faq: {
    h2: 'Before you install',
    items: [
      { t: 'What do I need?', d: 'A fresh Ubuntu or Debian server, amd64 or arm64, with ports 80 and 443 open. The installer adds Docker if it is missing.' },
      { t: 'Is it free?', d: 'Yes. Dokyr is open source under the MIT licence. You pay for the server only.' },
      { t: 'Can my team use it?', d: 'Yes. Invite people as admins or maintainers. They sign in with a password, a passkey or GitHub.' },
      { t: 'Does it run on more than one server?', d: 'Not yet. Dokyr runs on a single node by design.' },
      { t: 'What happens if the panel stops?', d: 'Your apps keep serving. They are plain Docker containers behind Caddy, and the panel adopts them again when it starts.' },
    ],
  },
  install: {
    h2: 'Install on a fresh server',
    steps: [
      'Run this as root on Ubuntu or Debian, amd64 or arm64, with ports 80 and 443 open.',
      'It installs Docker if needed and starts the panel.',
      'Open the setup link it prints and create the owner account.',
    ],
    update: 'Update later from Settings, or over SSH with',
    copy: 'Copy',
    copied: 'Copied',
    source: 'Source on GitHub',
    releases: 'Release notes',
  },
  footer: { license: 'Dokyr is open source under the MIT licence.', languages: 'Languages' },
};

const fr: Strings = {
  meta: {
    title: 'Dokyr, votre PaaS sur un seul serveur',
    description:
      'PaaS open source et auto-hébergé. Déploiement par git push, HTTPS automatique, bases de données sauvegardées sur S3, journaux et alertes, installé en une commande sur n’importe quel VPS.',
  },
  nav: {
    skip: 'Aller au contenu',
    panel: 'Panneau',
    deploy: 'Déployer',
    install: 'Installer',
    theme: 'Thème',
    language: 'Langue',
    home: 'Accueil Dokyr',
  },
  hero: {
    h1: 'Votre PaaS sur un seul serveur',
    lede: 'Déploiement par push, HTTPS, bases de données et sauvegardes, sur votre VPS.',
    primary: 'Installer Dokyr',
    secondary: 'Voir le panneau',
  },
  tour: {
    label: 'Écrans du panneau',
    tabs: { overview: 'Vue d’ensemble', deployments: 'Déploiements', metrics: 'Métriques', newApp: 'Nouvelle app' },
    notes: {
      overview: 'Métriques de l’hôte et de Docker en direct, et tous les projets sur une page.',
      deployments: 'Chaque déploiement avec son commit, sa chronologie et un retour arrière.',
      metrics: 'CPU, mémoire, réseau et disque d’une application, jusqu’à 30 jours.',
      newApp: 'Une application depuis Git, une image, un fichier Compose ou l’un des 41 modèles.',
    },
    theme: 'Affiché avec le thème {theme}. Choisissez-en un autre avec l’icône de palette, les écrans suivent.',
  },
  deploy: {
    h2: 'Le trafic suit le contrôle de santé',
    line: 'Un dépôt, une image, un fichier Compose ou un modèle. Chacun suit le même déploiement : build, contrôle de santé, puis trafic.',
    tabsLabel: 'Façons de déployer',
    tabs: { git: 'Dépôt Git', image: 'Image Docker', compose: 'Fichier Compose', template: 'Modèle' },
    notes: {
      git: 'GitHub App, jeton GitLab ou clé de déploiement. Chaque push construit et déploie.',
      image: 'N’importe quel registre public ou privé, y compris celui intégré à Dokyr.',
      compose: 'Collez un fichier Compose. Chaque service devient une application à part entière.',
      template: '41 applications prêtes, de WordPress et Gitea à Grafana et Ollama.',
    },
    builders: 'Builders',
  },
  included: {
    h2: 'Les options payantes sont incluses',
    items: [
      { t: 'Déploiements sans coupure', d: 'Un contrôle de santé passe avant de basculer le trafic.' },
      { t: 'HTTPS automatique', d: 'Caddy émet un certificat pour chaque domaine.' },
      { t: 'Bases de données', d: 'PostgreSQL et MariaDB par projet, avec Adminer.' },
      { t: 'Sauvegardes sur S3', d: 'Dumps planifiés avec rétention, restaurés dans une nouvelle base.' },
      { t: 'Journaux et alertes', d: 'Journaux en direct, métriques, alertes vers Slack ou e-mail.' },
      { t: 'Registre et stockage', d: 'Images privées et buckets S3 sur le même serveur.' },
    ],
  },
  picker: {
    choose: 'Choisir un thème',
    use: 'Utiliser {name}',
    show: 'Afficher {name}',
    alt: 'La page d’accueil de Dokyr avec le thème {name}',
    light: 'clair',
    dark: 'sombre',
    current: 'actuel',
    keys: ['← → parcourir', 'Entrée ou clic pour appliquer', 'Échap pour fermer'],
    previous: 'Thème précédent',
    next: 'Thème suivant',
  },
  compare: {
    h2: 'Louer une plateforme, ou la posséder',
    them: 'PaaS hébergé',
    us: 'Dokyr',
    rows: [
      ['Prix', 'Par application, par siège, par gigaoctet', 'La facture de votre serveur'],
      ['Vos données', 'Leur région, leurs disques', 'Votre serveur, vos volumes'],
      ['Bases de données', 'Une option payante chacune', 'Incluses, avec sauvegardes'],
      ['Partir', 'Leur configuration et leur CLI', 'Des images Docker, licence MIT'],
      ['Limites', 'Applications en veille, minutes de build', 'La taille de votre machine'],
    ],
  },
  faq: {
    h2: 'Avant d’installer',
    items: [
      { t: 'De quoi ai-je besoin ?', d: 'Un serveur Ubuntu ou Debian neuf, amd64 ou arm64, avec les ports 80 et 443 ouverts. L’installateur ajoute Docker s’il manque.' },
      { t: 'Est-ce gratuit ?', d: 'Oui. Dokyr est open source sous licence MIT. Vous ne payez que le serveur.' },
      { t: 'Mon équipe peut-elle l’utiliser ?', d: 'Oui. Invitez des admins ou des mainteneurs. Ils se connectent par mot de passe, passkey ou GitHub.' },
      { t: 'Fonctionne-t-il sur plusieurs serveurs ?', d: 'Pas encore. Dokyr tourne sur un seul nœud, par choix.' },
      { t: 'Que se passe-t-il si le panneau s’arrête ?', d: 'Vos applications continuent de servir. Ce sont des conteneurs Docker derrière Caddy, que le panneau reprend à son redémarrage.' },
    ],
  },
  install: {
    h2: 'Installer sur un serveur neuf',
    steps: [
      'Lancez cette commande en root sur Ubuntu ou Debian, amd64 ou arm64, ports 80 et 443 ouverts.',
      'Elle installe Docker si besoin et démarre le panneau.',
      'Ouvrez le lien de configuration affiché et créez le compte propriétaire.',
    ],
    update: 'Mettez à jour depuis les Paramètres, ou en SSH avec',
    copy: 'Copier',
    copied: 'Copié',
    source: 'Code sur GitHub',
    releases: 'Notes de version',
  },
  footer: { license: 'Dokyr est open source sous licence MIT.', languages: 'Langues' },
};

const es: Strings = {
  meta: {
    title: 'Dokyr, tu propio PaaS en un solo servidor',
    description:
      'PaaS de código abierto y autoalojado. Despliegue con git push, HTTPS automático, bases de datos con copias en S3, registros y alertas, instalado con un comando en cualquier VPS.',
  },
  nav: {
    skip: 'Ir al contenido',
    panel: 'Panel',
    deploy: 'Desplegar',
    install: 'Instalar',
    theme: 'Tema',
    language: 'Idioma',
    home: 'Inicio de Dokyr',
  },
  hero: {
    h1: 'Tu propio PaaS en un servidor',
    lede: 'Despliegue con push, HTTPS, bases de datos y copias, en tu propio VPS.',
    primary: 'Instalar Dokyr',
    secondary: 'Ver el panel',
  },
  tour: {
    label: 'Pantallas del panel',
    tabs: { overview: 'Resumen', deployments: 'Despliegues', metrics: 'Métricas', newApp: 'Nueva app' },
    notes: {
      overview: 'Métricas del host y de Docker en vivo, con todos los proyectos en una página.',
      deployments: 'Cada despliegue con su commit, su línea de tiempo y la vuelta atrás.',
      metrics: 'CPU, memoria, red y disco de una aplicación, hasta 30 días.',
      newApp: 'Una aplicación desde Git, una imagen, un archivo Compose o una de 41 plantillas.',
    },
    theme: 'Con el tema {theme}. Elige otro desde el icono de paleta y las pantallas lo siguen.',
  },
  deploy: {
    h2: 'El tráfico pasa tras el chequeo',
    line: 'Un repositorio, una imagen, un archivo Compose o una plantilla. Todos siguen el mismo despliegue: build, chequeo de salud y tráfico.',
    tabsLabel: 'Formas de desplegar',
    tabs: { git: 'Repositorio Git', image: 'Imagen Docker', compose: 'Archivo Compose', template: 'Plantilla' },
    notes: {
      git: 'GitHub App, token de GitLab o clave de despliegue. Cada push compila y publica.',
      image: 'Cualquier registro público o privado, incluido el que trae Dokyr.',
      compose: 'Pega un archivo Compose. Cada servicio se convierte en una aplicación propia.',
      template: '41 aplicaciones listas, de WordPress y Gitea a Grafana y Ollama.',
    },
    builders: 'Builders',
  },
  included: {
    h2: 'Los extras de pago vienen incluidos',
    items: [
      { t: 'Despliegues sin cortes', d: 'Un chequeo de salud pasa antes de mover el tráfico.' },
      { t: 'HTTPS automático', d: 'Caddy emite un certificado para cada dominio.' },
      { t: 'Bases de datos', d: 'PostgreSQL y MariaDB por proyecto, con Adminer.' },
      { t: 'Copias en S3', d: 'Volcados programados con retención, restaurados en una base nueva.' },
      { t: 'Registros y alertas', d: 'Registros en vivo, métricas y alertas a Slack o correo.' },
      { t: 'Registro y almacenamiento', d: 'Imágenes privadas y buckets S3 en el mismo servidor.' },
    ],
  },
  picker: {
    choose: 'Elegir un tema',
    use: 'Usar {name}',
    show: 'Mostrar {name}',
    alt: 'La página de inicio de Dokyr con el tema {name}',
    light: 'claro',
    dark: 'oscuro',
    current: 'actual',
    keys: ['← → explorar', 'Enter o clic para aplicar', 'Esc para cerrar'],
    previous: 'Tema anterior',
    next: 'Tema siguiente',
  },
  compare: {
    h2: 'Alquilar una plataforma, o tenerla',
    them: 'PaaS alojado',
    us: 'Dokyr',
    rows: [
      ['Precio', 'Por aplicación, por usuario, por gigabyte', 'La factura de tu servidor'],
      ['Tus datos', 'Su región, sus discos', 'Tu servidor, tus volúmenes'],
      ['Bases de datos', 'Un extra de pago cada una', 'Incluidas, con copias'],
      ['Irte', 'Su configuración y su CLI', 'Imágenes Docker normales, licencia MIT'],
      ['Límites', 'Apps dormidas, minutos de build', 'El tamaño de tu máquina'],
    ],
  },
  faq: {
    h2: 'Antes de instalar',
    items: [
      { t: '¿Qué necesito?', d: 'Un servidor Ubuntu o Debian limpio, amd64 o arm64, con los puertos 80 y 443 abiertos. El instalador añade Docker si falta.' },
      { t: '¿Es gratis?', d: 'Sí. Dokyr es de código abierto con licencia MIT. Solo pagas el servidor.' },
      { t: '¿Puede usarlo mi equipo?', d: 'Sí. Invita a personas como admins o maintainers. Entran con contraseña, passkey o GitHub.' },
      { t: '¿Funciona en varios servidores?', d: 'Todavía no. Dokyr funciona en un solo nodo, por diseño.' },
      { t: '¿Qué pasa si el panel se detiene?', d: 'Tus aplicaciones siguen sirviendo. Son contenedores Docker detrás de Caddy, y el panel los retoma al arrancar.' },
    ],
  },
  install: {
    h2: 'Instalar en un servidor limpio',
    steps: [
      'Ejecútalo como root en Ubuntu o Debian, amd64 o arm64, con los puertos 80 y 443 abiertos.',
      'Instala Docker si hace falta y arranca el panel.',
      'Abre el enlace de configuración que muestra y crea la cuenta de propietario.',
    ],
    update: 'Actualiza después desde Ajustes, o por SSH con',
    copy: 'Copiar',
    copied: 'Copiado',
    source: 'Código en GitHub',
    releases: 'Notas de versión',
  },
  footer: { license: 'Dokyr es de código abierto con licencia MIT.', languages: 'Idiomas' },
};

const ar: Strings = {
  meta: {
    title: 'Dokyr، منصتك الخاصة على خادم واحد',
    description:
      'منصة PaaS مفتوحة المصدر تستضيفها بنفسك. نشر عبر git push، و HTTPS تلقائي، وقواعد بيانات مع نسخ احتياطية على S3، وسجلات وتنبيهات، تُثبَّت بأمر واحد على أي خادم VPS.',
  },
  nav: {
    skip: 'انتقل إلى المحتوى',
    panel: 'اللوحة',
    deploy: 'النشر',
    install: 'التثبيت',
    theme: 'السمة',
    language: 'اللغة',
    home: 'الصفحة الرئيسية لـ Dokyr',
  },
  hero: {
    h1: 'منصتك الخاصة على خادم واحد',
    lede: 'نشر بالدفع، و HTTPS، وقواعد بيانات ونسخ احتياطية، على خادمك الخاص.',
    primary: 'ثبّت Dokyr',
    secondary: 'جولة في اللوحة',
  },
  tour: {
    label: 'شاشات اللوحة',
    tabs: { overview: 'نظرة عامة', deployments: 'عمليات النشر', metrics: 'المقاييس', newApp: 'تطبيق جديد' },
    notes: {
      overview: 'مقاييس الخادم و Docker مباشرة، وكل المشاريع في صفحة واحدة.',
      deployments: 'كل عملية نشر مع الالتزام والمراحل وإمكانية التراجع.',
      metrics: 'المعالج والذاكرة والشبكة والقرص لتطبيق واحد، حتى 30 يومًا.',
      newApp: 'تطبيق جديد من Git أو صورة أو ملف Compose أو أحد 41 قالبًا.',
    },
    theme: 'معروضة بسمة {theme}. اختر سمة أخرى من أيقونة الألوان فتتبعها الشاشات.',
  },
  deploy: {
    h2: 'تنتقل الحركة بعد فحص الصحة',
    line: 'مستودع أو صورة أو ملف Compose أو قالب. كلها تمر بالخطوات نفسها: البناء، ثم فحص الصحة، ثم تحويل الحركة.',
    tabsLabel: 'طرق النشر',
    tabs: { git: 'مستودع Git', image: 'صورة Docker', compose: 'ملف Compose', template: 'قالب' },
    notes: {
      git: 'تطبيق GitHub أو رمز GitLab أو مفتاح نشر. كل دفع يُبنى ويُنشر.',
      image: 'أي سجل عام أو خاص، بما فيه السجل المدمج في Dokyr.',
      compose: 'الصق ملف Compose. تصبح كل خدمة تطبيقًا تديره وحده.',
      template: '41 تطبيقًا جاهزًا، من WordPress و Gitea إلى Grafana و Ollama.',
    },
    builders: 'أدوات البناء',
  },
  included: {
    h2: 'الإضافات المدفوعة مشمولة',
    items: [
      { t: 'نشر بلا انقطاع', d: 'يجتاز فحص الصحة قبل تحويل الحركة.' },
      { t: 'HTTPS تلقائي', d: 'يصدر Caddy شهادة لكل نطاق.' },
      { t: 'قواعد البيانات', d: 'PostgreSQL و MariaDB لكل مشروع، مع Adminer.' },
      { t: 'نسخ احتياطي إلى S3', d: 'نسخ مجدولة مع مدة احتفاظ، تُستعاد في قاعدة جديدة.' },
      { t: 'السجلات والتنبيهات', d: 'سجلات مباشرة ومقاييس وتنبيهات إلى Slack أو البريد.' },
      { t: 'السجل والتخزين', d: 'صور خاصة وحاويات S3 على الخادم نفسه.' },
    ],
  },
  picker: {
    choose: 'اختر سمة',
    use: 'استخدام {name}',
    show: 'عرض {name}',
    alt: 'الصفحة الرئيسية لـ Dokyr بسمة {name}',
    light: 'فاتحة',
    dark: 'داكنة',
    current: 'الحالية',
    keys: ['← → للتصفح', 'Enter أو نقرة للتطبيق', 'Esc للإغلاق'],
    previous: 'السمة السابقة',
    next: 'السمة التالية',
  },
  compare: {
    h2: 'استأجر منصة، أو امتلك منصتك',
    them: 'منصة مستضافة',
    us: 'Dokyr',
    rows: [
      ['السعر', 'لكل تطبيق ولكل مستخدم ولكل غيغابايت', 'فاتورة خادمك فقط'],
      ['بياناتك', 'منطقتهم وأقراصهم', 'خادمك ووحدات تخزينك'],
      ['قواعد البيانات', 'إضافة مدفوعة لكل واحدة', 'مشمولة، مع نسخ احتياطي'],
      ['المغادرة', 'إعداداتهم وأداتهم', 'صور Docker عادية، رخصة MIT'],
      ['الحدود', 'تطبيقات نائمة ودقائق بناء', 'حجم جهازك'],
    ],
  },
  faq: {
    h2: 'قبل التثبيت',
    items: [
      { t: 'ماذا أحتاج؟', d: 'خادم Ubuntu أو Debian جديد، amd64 أو arm64، مع فتح المنفذين 80 و 443. يضيف المثبّت Docker إن لم يكن موجودًا.' },
      { t: 'هل هو مجاني؟', d: 'نعم. Dokyr مفتوح المصدر برخصة MIT. تدفع ثمن الخادم فقط.' },
      { t: 'هل يمكن لفريقي استخدامه؟', d: 'نعم. ادعُ أشخاصًا بدور مسؤول أو مشرف. يدخلون بكلمة مرور أو مفتاح مرور أو GitHub.' },
      { t: 'هل يعمل على أكثر من خادم؟', d: 'ليس بعد. يعمل Dokyr على خادم واحد عن قصد.' },
      { t: 'ماذا يحدث إذا توقفت اللوحة؟', d: 'تستمر تطبيقاتك في العمل. فهي حاويات Docker عادية خلف Caddy، وتستعيدها اللوحة عند تشغيلها.' },
    ],
  },
  install: {
    h2: 'ثبّت على خادم جديد',
    steps: [
      'شغّل هذا الأمر بصلاحية root على Ubuntu أو Debian، amd64 أو arm64، مع فتح المنفذين 80 و 443.',
      'يثبّت Docker عند الحاجة ويشغّل اللوحة.',
      'افتح رابط الإعداد الذي يطبعه وأنشئ حساب المالك.',
    ],
    update: 'حدّث لاحقًا من الإعدادات، أو عبر SSH بالأمر',
    copy: 'نسخ',
    copied: 'تم النسخ',
    source: 'المصدر على GitHub',
    releases: 'ملاحظات الإصدار',
  },
  footer: { license: 'Dokyr مفتوح المصدر برخصة MIT.', languages: 'اللغات' },
};

export const strings: Record<Locale, Strings> = { en, fr, es, ar };
