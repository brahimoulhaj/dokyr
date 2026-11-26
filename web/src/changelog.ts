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
    version: '0.42.0',
    date: '2026-11-26',
    changes: {
      en: [
        'Redirects on the Routing page: send www.example.com to example.com, or an old name to a new one, with the path kept.',
        'Volume backups in an app\'s Settings → Volumes: copy a named volume to S3 storage, restore it, delete it. On demand for now, not on a schedule.',
        'Invites can be sent by email, through the mail server of an email notification channel.',
        'The owner can hand the panel over to another member, after confirming their password.',
      ],
      fr: [
        'Redirections sur la page Routage : envoyez www.example.com vers example.com, ou un ancien nom vers un nouveau, chemin conservé.',
        'Sauvegardes de volumes dans Paramètres → Volumes d’une app : copiez un volume nommé vers un stockage S3, restaurez-le, supprimez-le. À la demande pour l’instant, sans planification.',
        'Les invitations peuvent être envoyées par e-mail, via le serveur de messagerie d’un canal de notification par e-mail.',
        'Le propriétaire peut céder le panneau à un autre membre, après confirmation de son mot de passe.',
      ],
      ar: [
        'إعادات التوجيه في صفحة التوجيه: أرسل www.example.com إلى example.com، أو اسمًا قديمًا إلى اسم جديد، مع الإبقاء على المسار.',
        'نسخ وحدات التخزين في الإعدادات ← وحدات التخزين للتطبيق: انسخ وحدة تخزين مسمّاة إلى تخزين S3، استعدها، احذفها. عند الطلب حاليًا، بلا جدولة.',
        'يمكن إرسال الدعوات بالبريد الإلكتروني عبر خادم بريد قناة إشعارات بالبريد.',
        'يستطيع المالك تسليم اللوحة إلى عضو آخر بعد تأكيد كلمة مروره.',
      ],
      es: [
        'Redirecciones en la página Enrutamiento: envía www.example.com a example.com, o un nombre antiguo a uno nuevo, con la ruta conservada.',
        'Copias de volúmenes en Ajustes → Volúmenes de una app: copia un volumen con nombre a un almacenamiento S3, restáuralo, elimínalo. Bajo demanda por ahora, sin programación.',
        'Las invitaciones pueden enviarse por correo, mediante el servidor de correo de un canal de notificación por correo.',
        'El propietario puede ceder el panel a otro miembro, tras confirmar su contraseña.',
      ],
    },
  },
  {
    version: '0.41.0',
    date: '2026-11-26',
    changes: {
      en: [
        'The Backup page lists the containers the panel no longer knows, which a restore of an older backup can leave running, and lets an admin remove them. Their volumes stay.',
        'Restoring the panel no longer removes an old backup: the copy taken before the restore is not counted against the number kept.',
      ],
      fr: [
        'La page Sauvegarde liste les conteneurs que le panneau ne connaît plus, qu’une restauration d’une sauvegarde ancienne peut laisser tourner, et permet à un administrateur de les supprimer. Leurs volumes restent.',
        'Restaurer le panneau ne supprime plus d’ancienne sauvegarde : la copie faite avant la restauration ne compte pas dans le nombre conservé.',
      ],
      ar: [
        'صفحة النسخ الاحتياطي تسرد الحاويات التي لم تعد اللوحة تعرفها، والتي قد تتركها استعادة نسخة قديمة قيد التشغيل، وتتيح للمسؤول حذفها. تبقى وحدات تخزينها.',
        'استعادة اللوحة لم تعد تحذف نسخة قديمة: النسخة المأخوذة قبل الاستعادة لا تُحتسب ضمن العدد المحتفظ به.',
      ],
      es: [
        'La página Copia de seguridad lista los contenedores que el panel ya no conoce, que restaurar una copia antigua puede dejar en marcha, y permite a un administrador eliminarlos. Sus volúmenes se conservan.',
        'Restaurar el panel ya no elimina una copia antigua: la copia hecha antes de restaurar no cuenta para el número conservado.',
      ],
    },
  },
  {
    version: '0.40.1',
    date: '2026-11-25',
    changes: {
      en: [
        'Restoring the panel now asks in a confirmation dialog, which says what is lost and what stays safe.',
        'Settings → Builds warns when more builds run at once than the CPU cap has cores: they share the cap, so each build gets slower.',
        'Attempts at a backup\'s passphrase are limited, like sign-in attempts.',
      ],
      fr: [
        'La restauration du panneau demande maintenant confirmation dans une boîte de dialogue, qui indique ce qui est perdu et ce qui reste à l’abri.',
        'Paramètres → Builds avertit quand plus de builds tournent en même temps que le plafond CPU n’a de cœurs : ils se partagent le plafond, chaque build ralentit.',
        'Les essais de phrase secrète d’une sauvegarde sont limités, comme les tentatives de connexion.',
      ],
      ar: [
        'استعادة اللوحة تطلب الآن التأكيد في مربع حوار يوضح ما سيُفقد وما يبقى آمنًا.',
        'الإعدادات ← عمليات البناء تحذّر عندما يتجاوز عدد عمليات البناء المتزامنة عدد أنوية حد المعالج: تتقاسم الحد فيصبح كل بناء أبطأ.',
        'محاولات إدخال عبارة مرور النسخة الاحتياطية محدودة، مثل محاولات تسجيل الدخول.',
      ],
      es: [
        'Restaurar el panel ahora pide confirmación en un diálogo que dice qué se pierde y qué queda a salvo.',
        'Ajustes → Builds avisa cuando hay más builds a la vez que núcleos en el límite de CPU: comparten el límite y cada build va más lento.',
        'Los intentos de frase de contraseña de una copia están limitados, como los de inicio de sesión.',
      ],
    },
  },
  {
    version: '0.40.0',
    date: '2026-11-25',
    changes: {
      en: [
        'Restore the panel from Settings → Backup: the owner picks a backup, enters its passphrase and confirms. The panel backs up its current state first, then restarts on the chosen backup.',
        'Settings → Builds sets how many builds run at once. The default is now two.',
      ],
      fr: [
        'Restaurez le panneau depuis Paramètres → Sauvegarde : le propriétaire choisit une sauvegarde, saisit sa phrase secrète et confirme. Le panneau sauvegarde d’abord son état actuel, puis redémarre sur la sauvegarde choisie.',
        'Paramètres → Builds règle le nombre de builds exécutés en même temps. La valeur par défaut est maintenant deux.',
      ],
      ar: [
        'استعد اللوحة من الإعدادات ← النسخ الاحتياطي: يختار المالك نسخة، يُدخل عبارة المرور الخاصة بها ثم يؤكد. تحفظ اللوحة حالتها الحالية أولًا، ثم تعيد التشغيل على النسخة المختارة.',
        'الإعدادات ← عمليات البناء تحدد عدد عمليات البناء التي تعمل في الوقت نفسه. القيمة الافتراضية الآن اثنتان.',
      ],
      es: [
        'Restaura el panel desde Ajustes → Copia de seguridad: el propietario elige una copia, escribe su frase de contraseña y confirma. El panel guarda primero su estado actual y luego se reinicia con la copia elegida.',
        'Ajustes → Builds define cuántos builds se ejecutan a la vez. El valor predeterminado ahora es dos.',
      ],
    },
  },
  {
    version: '0.39.0',
    date: '2026-11-24',
    changes: {
      en: [
        'Act on several apps at once: tick apps in a project\'s list, and a bar at the bottom deploys, restarts, stops, starts or deletes them together.',
        'Each of these asks before it runs and names the apps. Deleting several apps asks for your password, or a passkey if you have one.',
      ],
      fr: [
        'Agissez sur plusieurs applications à la fois : cochez-les dans la liste d’un projet, et une barre en bas les déploie, redémarre, arrête, démarre ou supprime ensemble.',
        'Chaque action demande confirmation et nomme les applications. Supprimer plusieurs applications demande votre mot de passe, ou une clé d’accès si vous en avez une.',
      ],
      ar: [
        'نفّذ إجراءً على عدة تطبيقات دفعة واحدة: حدّدها في قائمة المشروع، فيظهر شريط في الأسفل لنشرها أو إعادة تشغيلها أو إيقافها أو تشغيلها أو حذفها معًا.',
        'كل إجراء يطلب التأكيد ويذكر أسماء التطبيقات. حذف عدة تطبيقات يطلب كلمة المرور، أو مفتاح مرور إن كان لديك واحد.',
      ],
      es: [
        'Actúa sobre varias apps a la vez: márcalas en la lista de un proyecto y una barra inferior las despliega, reinicia, detiene, inicia o elimina juntas.',
        'Cada acción pide confirmación y nombra las apps. Eliminar varias apps pide tu contraseña, o una passkey si tienes una.',
      ],
    },
  },
  {
    version: '0.38.0',
    date: '2026-11-24',
    changes: {
      en: [
        'New template: SigNoz, for traces, metrics and logs over OpenTelemetry. It creates four apps: SigNoz, its collector, ClickHouse and its keeper. Deploy them in this order: keeper, ClickHouse, collector, then SigNoz.',
        'A template can now create several apps at once. Its card says how many, and the summary lists them before you create.',
      ],
      fr: [
        'Nouveau modèle : SigNoz, pour les traces, métriques et journaux via OpenTelemetry. Il crée quatre applications : SigNoz, son collector, ClickHouse et son keeper. Déployez-les dans cet ordre : keeper, ClickHouse, collector, puis SigNoz.',
        'Un modèle peut maintenant créer plusieurs applications à la fois. Sa carte indique combien, et le résumé les liste avant la création.',
      ],
      ar: [
        'قالب جديد: SigNoz للتتبعات والمقاييس والسجلات عبر OpenTelemetry. ينشئ أربعة تطبيقات: SigNoz و collector و ClickHouse و keeper. انشرها بهذا الترتيب: keeper ثم ClickHouse ثم collector ثم SigNoz.',
        'يمكن للقالب الآن إنشاء عدة تطبيقات دفعة واحدة. بطاقته تذكر عددها، والملخص يسردها قبل الإنشاء.',
      ],
      es: [
        'Nueva plantilla: SigNoz, para trazas, métricas y registros con OpenTelemetry. Crea cuatro apps: SigNoz, su collector, ClickHouse y su keeper. Despliégalas en este orden: keeper, ClickHouse, collector y luego SigNoz.',
        'Una plantilla ahora puede crear varias apps a la vez. Su tarjeta indica cuántas, y el resumen las lista antes de crear.',
      ],
    },
  },
  {
    version: '0.37.1',
    date: '2026-11-22',
    changes: {
      en: [
        'DNS status: a hostname that points at this server now says Verified. A panel installed with Docker could not see the server’s own address, so every hostname stayed Unconfirmed.',
        'New DNS status Cloudflare, for a hostname whose record goes through Cloudflare’s proxy.',
      ],
      fr: [
        'État DNS : un nom d’hôte qui pointe vers ce serveur indique maintenant Vérifié. Un panneau installé avec Docker ne voyait pas l’adresse du serveur, et tous les noms d’hôte restaient Non confirmé.',
        'Nouvel état DNS Cloudflare, pour un nom d’hôte dont l’enregistrement passe par le proxy de Cloudflare.',
      ],
      ar: [
        'حالة DNS: اسم المضيف الذي يشير إلى هذا الخادم يظهر الآن «تم التحقق». اللوحة المثبّتة عبر Docker لم تكن ترى عنوان الخادم نفسه، فبقيت كل الأسماء «غير مؤكد».',
        'حالة DNS جديدة «Cloudflare» لاسم المضيف الذي يمرّ سجله عبر وسيط Cloudflare.',
      ],
      es: [
        'Estado DNS: un nombre de host que apunta a este servidor ahora indica Verificado. Un panel instalado con Docker no veía la dirección del propio servidor, y todos los nombres de host quedaban Sin confirmar.',
        'Nuevo estado DNS Cloudflare, para un nombre de host cuyo registro pasa por el proxy de Cloudflare.',
      ],
    },
  },
  {
    version: '0.37.0',
    date: '2026-11-22',
    changes: {
      en: [
        'Fixed: a hostname that sits under a wildcard route (app.example.com beside *.example.com) got no certificate, so opening it failed with an SSL error. It gets its certificate as soon as the panel is updated.',
        'Routing is one compact list: a line per hostname, a line per path. An app’s Routing tab shows only its own routes, and projects have a Routing tab with the routes of all their apps.',
        'The Routing page has a filter by hostname, path, app or project once the list grows.',
      ],
      fr: [
        'Corrigé : un nom d’hôte placé sous une route générique (app.example.com à côté de *.example.com) n’obtenait pas de certificat, et son ouverture échouait avec une erreur SSL. Il reçoit son certificat dès la mise à jour du panneau.',
        'Le Routage est une seule liste compacte : une ligne par nom d’hôte, une ligne par chemin. L’onglet Routage d’une app ne montre que ses propres routes, et les projets ont un onglet Routage avec les routes de toutes leurs apps.',
        'La page Routage propose un filtre par nom d’hôte, chemin, app ou projet quand la liste s’allonge.',
      ],
      ar: [
        'إصلاح: اسم المضيف الواقع تحت مسار شامل (app.example.com بجانب ‎*.example.com) لم يكن يحصل على شهادة، فكان فتحه يفشل بخطأ SSL. يحصل على شهادته فور تحديث اللوحة.',
        '«التوجيه» قائمة واحدة مضغوطة: سطر لكل اسم مضيف وسطر لكل مسار. تبويب «التوجيه» في التطبيق يعرض مساراته فقط، وللمشاريع تبويب «التوجيه» بمسارات كل تطبيقاتها.',
        'في صفحة «التوجيه» تصفية حسب اسم المضيف أو المسار أو التطبيق أو المشروع عندما تطول القائمة.',
      ],
      es: [
        'Corregido: un nombre de host situado bajo una ruta comodín (app.example.com junto a *.example.com) no obtenía certificado, y abrirlo fallaba con un error de SSL. Recibe su certificado en cuanto se actualiza el panel.',
        'Enrutamiento es una sola lista compacta: una línea por nombre de host, una línea por ruta. La pestaña Enrutamiento de una app muestra solo sus propias rutas, y los proyectos tienen una pestaña Enrutamiento con las rutas de todas sus apps.',
        'La página Enrutamiento tiene un filtro por nombre de host, ruta, app o proyecto cuando la lista crece.',
      ],
    },
  },
  {
    version: '0.36.0',
    date: '2026-11-21',
    changes: {
      en: [
        'Domains become Routing: a route is an address (shop.example.com, or shop.example.com/api) and the app that answers on it. There is no domain to register or attach to a project first: type the hostname and pick the app.',
        'Each app has a Routing tab, the new-app form takes an optional address, and the Routing page in the menu shows every hostname with its DNS status and its paths. The Domains tab of projects is gone.',
        'Your routes are kept by the update. Domains that had no route are not carried over, and *.example.com now has its own HTTPS setting instead of following example.com.',
        'API: /api/routes and /api/hosts replace the /api/domains endpoints; scripts that call the old ones need an update.',
        'The host charts on the overview keep their last ten minutes across a restart or an update.',
      ],
      fr: [
        'Les domaines deviennent le Routage : une route est une adresse (shop.example.com, ou shop.example.com/api) et l’app qui y répond. Plus de domaine à enregistrer ni à rattacher à un projet : saisissez le nom d’hôte et choisissez l’app.',
        'Chaque app a un onglet Routage, le formulaire de nouvelle app accepte une adresse facultative, et la page Routage du menu montre chaque nom d’hôte avec son état DNS et ses chemins. L’onglet Domaines des projets disparaît.',
        'Vos routes sont conservées par la mise à jour. Les domaines sans route ne sont pas repris, et *.example.com a désormais son propre réglage HTTPS au lieu de suivre example.com.',
        'API : /api/routes et /api/hosts remplacent les points d’accès /api/domains ; les scripts qui appellent les anciens doivent être mis à jour.',
        'Les graphiques de l’hôte sur la vue d’ensemble gardent leurs dix dernières minutes après un redémarrage ou une mise à jour.',
      ],
      ar: [
        'النطاقات تصبح «التوجيه»: المسار عنوان (shop.example.com أو shop.example.com/api) والتطبيق الذي يجيب عليه. لم يعد هناك نطاق يُسجَّل أو يُربط بمشروع أولًا: اكتب اسم المضيف واختر التطبيق.',
        'لكل تطبيق تبويب «التوجيه»، ونموذج التطبيق الجديد يقبل عنوانًا اختياريًا، وصفحة «التوجيه» في القائمة تعرض كل اسم مضيف مع حالة DNS ومساراته. أُزيل تبويب النطاقات من المشاريع.',
        'يحتفظ التحديث بمساراتك. النطاقات التي لم يكن لها مسار لا تُنقل، و ‎*.example.com له الآن إعداد HTTPS خاص به بدل أن يتبع example.com.',
        'الواجهة البرمجية: ‎/api/routes و ‎/api/hosts تحلّان محل نقاط ‎/api/domains؛ السكربتات التي تستدعي القديمة تحتاج إلى تحديث.',
        'مخططات الخادم في النظرة العامة تحتفظ بآخر عشر دقائق بعد إعادة التشغيل أو التحديث.',
      ],
      es: [
        'Los dominios pasan a ser Enrutamiento: una ruta es una dirección (shop.example.com, o shop.example.com/api) y la app que responde en ella. Ya no hay un dominio que registrar ni asociar a un proyecto: escribe el nombre de host y elige la app.',
        'Cada app tiene una pestaña Enrutamiento, el formulario de nueva app admite una dirección opcional y la página Enrutamiento del menú muestra cada nombre de host con su estado DNS y sus rutas. La pestaña Dominios de los proyectos desaparece.',
        'La actualización conserva tus rutas. Los dominios sin ruta no se trasladan, y *.example.com tiene ahora su propio ajuste de HTTPS en lugar de seguir a example.com.',
        'API: /api/routes y /api/hosts sustituyen a los endpoints /api/domains; los scripts que llaman a los antiguos necesitan una actualización.',
        'Los gráficos del host en el resumen conservan sus últimos diez minutos tras un reinicio o una actualización.',
      ],
    },
  },
  {
    version: '0.35.0',
    date: '2026-11-20',
    changes: {
      en: [
        'Remote CLI: dokyr apps list, show, create and delete; dokyr deploy follows a deployment’s log live and fails when the deployment does; dokyr deployments shows what is in progress or an app’s history.',
        'Remote CLI: dokyr databases (list, show, create, start, stop, restart, delete) and dokyr registry list for the images of the built-in registry.',
        'These commands need no newer panel than v0.34.0: build the CLI again with make cli to get them.',
        'Maintenance: the OpenTelemetry modules used by the Docker client are updated (a reported weakness that the panel was not exposed to).',
      ],
      fr: [
        'CLI distante : dokyr apps list, show, create et delete ; dokyr deploy suit le journal d’un déploiement en direct et échoue si le déploiement échoue ; dokyr deployments montre ce qui est en cours ou l’historique d’une app.',
        'CLI distante : dokyr databases (list, show, create, start, stop, restart, delete) et dokyr registry list pour les images du registre intégré.',
        'Ces commandes ne demandent pas de panneau plus récent que v0.34.0 : recompilez la CLI avec make cli pour les obtenir.',
        'Maintenance : les modules OpenTelemetry utilisés par le client Docker sont mis à jour (une faiblesse signalée à laquelle le panneau n’était pas exposé).',
      ],
      ar: [
        'واجهة الأوامر عن بُعد: dokyr apps list و show و create و delete؛ و dokyr deploy يتابع سجل النشر مباشرة ويفشل إذا فشل النشر؛ و dokyr deployments يعرض ما يجري الآن أو سجل نشر تطبيق.',
        'واجهة الأوامر عن بُعد: dokyr databases (list, show, create, start, stop, restart, delete) و dokyr registry list لصور السجل المدمج.',
        'لا تحتاج هذه الأوامر إلى لوحة أحدث من v0.34.0: أعد بناء واجهة الأوامر بالأمر make cli للحصول عليها.',
        'صيانة: تحديث وحدات OpenTelemetry التي يستخدمها عميل Docker (ثغرة مُبلَّغ عنها لم تكن اللوحة معرّضة لها).',
      ],
      es: [
        'CLI remota: dokyr apps list, show, create y delete; dokyr deploy sigue el registro de un despliegue en directo y falla si el despliegue falla; dokyr deployments muestra lo que está en curso o el historial de una app.',
        'CLI remota: dokyr databases (list, show, create, start, stop, restart, delete) y dokyr registry list para las imágenes del registro integrado.',
        'Estos comandos no necesitan un panel más nuevo que v0.34.0: vuelve a compilar la CLI con make cli para tenerlos.',
        'Mantenimiento: se actualizan los módulos de OpenTelemetry que usa el cliente de Docker (una debilidad notificada a la que el panel no estaba expuesto).',
      ],
    },
  },
  {
    version: '0.34.0',
    date: '2026-11-20',
    changes: {
      en: [
        'Remote CLI: dokyr status (or whoami) shows which panel you are signed in to, as whom and with which role; dokyr logout revokes the CLI’s token on the panel and forgets it on your machine.',
        'dokyr projects create makes a project from the terminal.',
        'In a terminal the CLI now has colours, framed tables, a spinner while it waits for your approval, and asks for what is missing; in a script it stays plain text. Tab completion: dokyr completion.',
      ],
      fr: [
        'CLI distante : dokyr status (ou whoami) indique sur quel panneau vous êtes connecté, sous quel compte et avec quel rôle ; dokyr logout révoque le jeton de la CLI sur le panneau et l’oublie sur votre machine.',
        'dokyr projects create crée un projet depuis le terminal.',
        'Dans un terminal, la CLI a désormais des couleurs, des tableaux encadrés, un indicateur pendant l’attente de votre approbation, et demande ce qui manque ; dans un script, elle reste en texte brut. Complétion : dokyr completion.',
      ],
      ar: [
        'واجهة الأوامر عن بُعد: dokyr status (أو whoami) يعرض اللوحة التي سجّلت الدخول إليها، وبأي حساب وأي دور؛ و dokyr logout يُبطل رمز واجهة الأوامر في اللوحة وينساه على جهازك.',
        'dokyr projects create ينشئ مشروعًا من الطرفية.',
        'في الطرفية صار لواجهة الأوامر ألوان وجداول مؤطرة ومؤشر انتظار ريثما توافق، وتسأل عمّا ينقص؛ وفي السكربتات تبقى نصًا عاديًا. الإكمال التلقائي: dokyr completion.',
      ],
      es: [
        'CLI remota: dokyr status (o whoami) muestra en qué panel has iniciado sesión, con qué cuenta y con qué rol; dokyr logout revoca el token de la CLI en el panel y lo olvida en tu máquina.',
        'dokyr projects create crea un proyecto desde la terminal.',
        'En una terminal la CLI ahora tiene colores, tablas enmarcadas, un indicador mientras espera tu aprobación, y pregunta lo que falta; en un script sigue siendo texto plano. Autocompletado: dokyr completion.',
      ],
    },
  },
  {
    version: '0.33.0',
    date: '2026-11-20',
    changes: {
      en: [
        'A remote CLI, first step: dokyr login https://your-panel signs in from your own machine through the browser. You approve the device on the panel, and the CLI never sees your password.',
        'The CLI gets an API token named after the device, listed under API tokens and revocable there.',
        'dokyr projects list shows the projects of the panel in the terminal (--json for scripts). No released binaries yet: build it with make cli.',
      ],
      fr: [
        'Une CLI distante, première étape : dokyr login https://votre-panneau vous connecte depuis votre machine en passant par le navigateur. Vous approuvez l’appareil sur le panneau, et la CLI ne voit jamais votre mot de passe.',
        'La CLI reçoit un jeton d’API au nom de l’appareil, listé dans Jetons d’API et révocable à cet endroit.',
        'dokyr projects list affiche les projets du panneau dans le terminal (--json pour les scripts). Pas encore de binaires publiés : compilez-la avec make cli.',
      ],
      ar: [
        'واجهة أوامر عن بُعد، الخطوة الأولى: dokyr login https://your-panel يسجّل دخولك من جهازك عبر المتصفح. توافق على الجهاز في اللوحة، ولا ترى واجهة الأوامر كلمة مرورك أبدًا.',
        'تحصل واجهة الأوامر على رمز API باسم الجهاز، يظهر في صفحة رموز API ويمكن إبطاله منها.',
        'dokyr projects list يعرض مشاريع اللوحة في الطرفية (--json للسكربتات). لا توجد ملفات تنفيذية منشورة بعد: ابنِها بالأمر make cli.',
      ],
      es: [
        'Una CLI remota, primer paso: dokyr login https://tu-panel inicia sesión desde tu propia máquina a través del navegador. Apruebas el dispositivo en el panel, y la CLI nunca ve tu contraseña.',
        'La CLI recibe un token de API con el nombre del dispositivo, que aparece en Tokens de API y se puede revocar allí.',
        'dokyr projects list muestra los proyectos del panel en la terminal (--json para scripts). Aún no hay binarios publicados: compílala con make cli.',
      ],
    },
  },
  {
    version: '0.32.0',
    date: '2026-11-20',
    changes: {
      en: [
        'Security: a maintainer can no longer mount the panel’s own volumes, nor change or open a terminal in an app that mounts a host path (such as the Docker socket); an owner or an admin does.',
        'Apps can no longer forge network traffic on the network they share with the panel, and a hostname the panel used to have is not served by a wildcard route.',
        'An open host shell closes when its user stops being an admin, and resetting the owner password from the server also revokes API tokens.',
        'New guides: first deploy, backups and restore, recovery over SSH, and security.',
      ],
      fr: [
        'Sécurité : un mainteneur ne peut plus monter les volumes du panneau, ni modifier une app qui monte un chemin de l’hôte (comme le socket Docker) ou y ouvrir un terminal ; cela revient au propriétaire ou à un admin.',
        'Les apps ne peuvent plus falsifier le trafic du réseau qu’elles partagent avec le panneau, et un ancien nom d’hôte du panneau n’est plus servi par une route générique.',
        'Un shell de l’hôte ouvert se ferme quand son utilisateur n’est plus admin, et réinitialiser le mot de passe du propriétaire depuis le serveur révoque aussi les jetons d’API.',
        'Nouveaux guides : premier déploiement, sauvegardes et restauration, récupération par SSH, et sécurité.',
      ],
      ar: [
        'الأمان: لم يعد بإمكان المشرف (maintainer) ربط وحدات تخزين اللوحة نفسها، ولا تعديل تطبيق يربط مسارًا من الخادم (مثل مقبس Docker) أو فتح طرفيته؛ هذا للمالك أو للمسؤول.',
        'لم تعد التطبيقات قادرة على تزييف حركة الشبكة التي تتشاركها مع اللوحة، ولا يُخدَم اسم مضيف قديم للوحة عبر مسار عام (*).',
        'تُغلق صدفة الخادم المفتوحة حين لا يعود مستخدمها مسؤولاً، وإعادة تعيين كلمة مرور المالك من الخادم تُبطل رموز API أيضًا.',
        'أدلة جديدة: أول نشر، النسخ الاحتياطي والاستعادة، الاسترداد عبر SSH، والأمان.',
      ],
      es: [
        'Seguridad: un mantenedor ya no puede montar los volúmenes del propio panel, ni cambiar una app que monta una ruta del host (como el socket de Docker) o abrir su terminal; lo hace el propietario o un administrador.',
        'Las apps ya no pueden falsear el tráfico de la red que comparten con el panel, y un nombre de host que tuvo el panel no lo sirve una ruta comodín.',
        'Un shell del servidor abierto se cierra cuando su usuario deja de ser administrador, y restablecer la contraseña del propietario desde el servidor también revoca los tokens de API.',
        'Guías nuevas: primer despliegue, copias de seguridad y restauración, recuperación por SSH, y seguridad.',
      ],
    },
  },
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
