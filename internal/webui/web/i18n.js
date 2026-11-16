// Client-side translations for the page's own static chrome (labels,
// buttons, headings, hints, confirm dialogs, status text, and the
// templates used to render the Scenery block's per-region rows).
//
// Deliberately NOT translated: the Log block (raw structured log lines
// from the Go backend — translating slog output would misrepresent what
// was actually logged), server-generated error strings broadcast over the
// WebSocket (e.g. "mount failed: ..." — real Go error text, not UI copy),
// and scenery region display names (sourced from GitHub release metadata,
// not something this page authors). Translating those would need a
// server-side i18n system of its own; this is a client-only UI-chrome
// translation layer, matching the scope of what was actually asked for.
//
// Each entry is a function(params) so every key — static or templated —
// is called the same way via t(key, params); static entries just ignore
// params.

const SUPPORTED_LANGS = ['en', 'es', 'ru', 'pt', 'el', 'ar', 'zh', 'fr', 'it', 'de'];

// RTL_LANGS drives the one html[dir] flip Arabic needs — applyStaticTranslations
// sets it on every render so load, switch, and reload all stay correct.
// Everything else here is plain translated text in an otherwise-LTR layout
// (labels, numbers, path strings); this is deliberately not a full RTL
// layout pass (mirrored icons/flex order/etc.), just correct text direction.
const RTL_LANGS = ['ar'];

const I18N = {
  en: {
    'status.connecting': () => 'connecting…',
    'status.connected': () => 'connected',
    'status.disconnected': () => 'disconnected — retrying…',
    'status.stopped': () => 'stopped',
    'status.running': (p) => `running (${p.n} mount${p.n === 1 ? '' : 's'})`,
    'status.saved': () => 'Saved',
    'status.error': (p) => `Error: ${p.msg}`,
    'status.cache_cleaned': (p) => `Cache cleaned: ${p.n} file(s), ${p.mb} MB freed`,

    'btn.run': () => 'Run',
    'btn.stop': () => 'Stop',
    'btn.shutdown': () => 'Shutdown',
    'btn.restart': () => 'Restart',

    'setup.heading': () => 'Setup',
    'setup.xplane_label': () => 'X-Plane install dir',
    'setup.xplane_hint': () => 'The X-Plane root folder itself — the one that directly contains <code>Custom Scenery</code>, not that folder. Example: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Scenery install dir',
    'setup.scenery_hint': () => 'A dedicated folder for autoOr-simg’s own packages — commonly <code>&lt;X-Plane path&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg installs/uninstalls everything under this folder, so give it one of its own rather than reusing a folder with other content.',
    'setup.cache_label': () => 'Image cache dir',
    'setup.cache_hint': () => 'Decoded/compressed tile cache, read on every texture request — pick a fast local drive (SSD) with room to grow toward the cache size limit below. Default: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Temp download dir',
    'setup.download_hint': () => 'Scratch space for in-progress scenery downloads — needs roughly 2x the download size free temporarily (checked before each install starts). Default: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Browse…',
    'setup.detect_xplane': () => 'Find X-Plane…',
    'setup.detect_xplane_none': () => 'No X-Plane install found automatically — use Browse… instead.',
    'setup.detect_xplane_found': (p) => `Found: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Found ${p.n} X-Plane installs — pick one.`,
    'setup.detect_xplane_error': (p) => `Couldn't search for X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Cache size (GB)',
    'setup.clean_cache': () => 'Clean Cache',
    'setup.auto_open_label': () => 'Open browser automatically on startup',
    'setup.save': () => 'Save',
    'setup.save_tooltip': () => 'It saves all configurations',

    'zoom.heading': () => 'Zoom & quality',
    'zoom.maptype_label': () => 'Map type override',
    'zoom.maptype_default': () => '(provider default)',
    'zoom.minzoom_label': () => 'Min zoom',
    'zoom.minzoom_hint': () => '(floor, not a ceiling — never raises quality above what the provider serves)',
    'zoom.maxzoom_label': () => 'Max zoom',
    'zoom.maxzoom_hint': () => '(ceiling — caps the sharpest imagery ever fetched, to save disk space and bandwidth. Dense areas — cities, airports — natively want higher zoom than open countryside, so a low cap hits them far harder: capping 3 levels below what an area wants means 8x fewer pixels there, not a mild blur. 18 is a practical floor for airports/cities to still look sharp; "No cap" leaves this limit disabled.)',
    'zoom.nocap': () => 'No cap',
    'zoom.maxzoom_warning': () => '⚠ Below 18, dense areas (cities, airports) will look noticeably blocky — this caps them far harder than open countryside at the same setting.',
    'zoom.maxwait_label': () => 'Max wait (seconds per chunk fetch)',
    'zoom.maxbuilds_label': () => 'Max concurrent tile builds',
    'zoom.maxbuilds_hint': () => '(memory budget, not network — takes effect on next restart)',
    'zoom.fetchthreads_label': () => 'Chunk fetch threads',
    'zoom.fetchthreads_hint': () => "(one shared pool every tile's chunk fetches compete through fairly, process-wide; takes effect on next restart)",
    'zoom.maxopentiles_label': () => 'Max open tiles',
    'zoom.maxopentiles_hint': () => '(memory budget — tiles opened past this serve a cheap placeholder instead of a full build)',
    'zoom.perfmode_label': () => 'Performance mode',
    'zoom.perfmode_hint': () => '(how early to trade sharpness for speed when many tiles want full detail at once — e.g. a dense city or a wide view; takes effect immediately)',
    'zoom.perfmode_quality': () => 'Quality',
    'zoom.perfmode_balanced': () => 'Balanced',
    'zoom.perfmode_speed': () => 'Speed',
    'zoom.coarsefallback_label': () => 'Disable blurry fallback for slow/missing tiles',
    'zoom.coarsefallback_hint': () => '(when off, a slow or failed chunk is shown as a blurry stand-in from a coarser cached zoom level instead of a flat color patch — looks like soft-focus over land, can look washed-out/dirty over water; takes effect immediately)',

    'stats.heading': () => 'Stats',
    'stats.cached_tiles': () => 'Cached tiles',
    'stats.open_tiles': () => 'Open tiles',
    'stats.process_memory': () => 'Process memory',
    'stats.active_mounts': () => 'Active mounts',

    'scenery.heading': () => 'Scenery',
    'scenery.refresh': () => 'Refresh',
    'scenery.install': () => 'Install',
    'scenery.update': () => 'Update',
    'scenery.working': () => 'Working…',
    'scenery.installed': (p) => `installed ${p.ver}`,
    'scenery.update_available': (p) => `update available: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `error: ${p.msg}`,
    'scenery.package_n_of': (p) => `package ${p.index}/${p.total}`,
    'scenery.verify': () => 'Verify',
    'scenery.mount': () => 'Mount',
    'scenery.unmount': () => 'Unmount',
    'scenery.mounted': () => 'Mounted',
    'scenery.unmounted': () => 'Not mounted',
    'scenery.verifying': () => 'Verifying…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'Log',

    'confirm.yes': () => 'Yes',
    'confirm.cancel': () => 'Cancel',
    'confirm.stop_text': () => 'Stop all mounts? X-Plane will lose its textures immediately.',
    'confirm.shutdown_text': () => 'Shut down autoOr-simg completely? This stops mounts and exits the program.',
    'confirm.restart_text': () => 'Restart autoOr-simg now? This stops mounts, then starts a fresh instance immediately.',

    'dirbrowser.up': () => '↑ Up',
    'dirbrowser.select': () => 'Select this folder',
    'dirbrowser.cancel': () => 'Cancel',

    'theme.light': () => 'Light',
    'theme.dark': () => 'Dark',
    'attribution.text': () => 'autoOr-simg is an unofficial Go reimplementation of <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">kubilus1’s AutoOrtho</a> — our thanks to the original project and its author.',

    'guide.tooltip': () => 'Usage Guide',
    'guide.title': () => 'Usage Guide',
    'guide.content': () => `
      <h3>1. Point autoOr-simg at X-Plane</h3>
      <p>In the <strong>Setup</strong> block:</p>
      <ul>
        <li><strong>X-Plane install dir</strong> — the folder that directly contains <code>Custom Scenery</code>, e.g. <code>.../Steam/steamapps/common/X-Plane 12</code>.</li>
        <li><strong>Scenery install dir</strong> — a dedicated folder for autoOr-simg's own packages (commonly <code>&lt;X-Plane path&gt;/Custom Scenery/z_autoortho</code>). Don't point this at a folder with anything else in it.</li>
        <li>Click <strong>Save</strong>.</li>
      </ul>

      <h3>2. Download scenery</h3>
      <p>In the <strong>Scenery</strong> panel, click <strong>Install</strong> on whichever regions you actually fly over — each is a separate download, so there's no need to grab all of them.</p>

      <h3>3. Mount it</h3>
      <p>Every installed region gets its own <strong>Mount</strong>/<strong>Unmount</strong> toggle in the Scenery panel (or use the global <strong>Run</strong> button to mount everything at once). The first time a region is installed, autoOr-simg automatically creates its folder under <code>Custom Scenery</code> and adds the right line to <code>scenery_packs.ini</code> — no manual file editing needed.</p>
      <p class="hint">X-Plane only re-reads <code>scenery_packs.ini</code> on launch, so if X-Plane was already running before you installed a region, restart X-Plane once to pick it up.</p>

      <h3>4. Fly</h3>
      <p>Load an airport in a region you downloaded. The first time through a dense area, tiles build progressively as X-Plane requests them — that's normal, not a problem to fix.</p>

      <h3>5. Tune quality vs. performance</h3>
      <p>In the <strong>Zoom & quality</strong> block: <strong>Performance mode</strong> trades sharpness for smoothness when a lot of tiles want full detail at once (dense cities, wide views) — try <em>Speed</em> if a big airport stutters. <strong>Max zoom</strong> caps the sharpest imagery ever fetched to save disk space and bandwidth; keep it at 18 or above over cities and airports, or they'll look noticeably blocky.</p>

      <h3>6. Manage the cache</h3>
      <p>In the <strong>Setup</strong> block, set a <strong>Cache size</strong> that fits your disk, and use <strong>Clean Cache</strong> whenever you want to reclaim space. The <strong>Stats</strong> block shows how many tiles are cached and open, and how much memory autoOr-simg is using, in real time.</p>

      <h3>7. Make it yours</h3>
      <p>Open <strong>Settings</strong> (the gear icon) to pick a language and theme, show a background photo behind the panels, or dress up Natalia in a different outfit. None of this affects how scenery loads — it's just for you.</p>
    `,

    'settings.tooltip': () => 'Settings',
    'settings.heading': () => 'Settings',
    'settings.close': () => 'Close',
    'settings.language_label': () => 'Language',
    'settings.theme_label': () => 'Theme',
    'settings.demo_mode_label': () => 'Demonstration mode',
    'settings.demo_mode_hint': () => 'Redacts your username in the X-Plane/scenery/cache/download path fields and in the Log block — for screenshots and screen recordings. Only changes what is displayed, not the real saved values. Only affects this browser tab, not saved config.',
    'settings.demo_mode_enable': () => 'Enable demonstration mode',
    'settings.background_label': () => 'Background',
    'settings.background_hint': () => 'Pick a photo to show faintly behind the page — panels stay solid, so everything stays readable.',
    'settings.choose_background': () => 'Choose image…',
    'settings.remove_background': () => 'Remove background',
    'settings.opacity_label': () => 'Opacity',
    'settings.background_too_large': () => "Couldn't save that image (too large for browser storage) — try a smaller photo.",
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'A little lady in the corner who watches your cursor. Click her for a tip.',
    'settings.mascot_outfit': () => 'Outfit',
    'mascot.outfit_jetfighter': () => 'Jetfighter',
    'mascot.outfit_acrobatic': () => 'Acrobatic',
    'mascot.outfit_airliner': () => 'Airliner',
    'mascot.outfit_agricultural': () => 'Agricultural',
  },

  es: {
    'status.connecting': () => 'conectando…',
    'status.connected': () => 'conectado',
    'status.disconnected': () => 'desconectado — reintentando…',
    'status.stopped': () => 'detenido',
    'status.running': (p) => `en ejecución (${p.n} montaje${p.n === 1 ? '' : 's'})`,
    'status.saved': () => 'Guardado',
    'status.error': (p) => `Error: ${p.msg}`,
    'status.cache_cleaned': (p) => `Caché limpiada: ${p.n} archivo(s), ${p.mb} MB liberados`,

    'btn.run': () => 'Iniciar',
    'btn.stop': () => 'Detener',
    'btn.shutdown': () => 'Apagar',
    'btn.restart': () => 'Reiniciar',

    'setup.heading': () => 'Configuración',
    'setup.xplane_label': () => 'Directorio de instalación de X-Plane',
    'setup.xplane_hint': () => 'La carpeta raíz de X-Plane — la que contiene directamente <code>Custom Scenery</code>, no esa carpeta en sí. Ejemplo: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Directorio de instalación de escenarios',
    'setup.scenery_hint': () => 'Una carpeta dedicada para los paquetes propios de autoOr-simg — normalmente <code>&lt;ruta de X-Plane&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg instala y desinstala todo lo que hay en esta carpeta, así que usa una propia en lugar de reutilizar una con otro contenido.',
    'setup.cache_label': () => 'Directorio de caché de imágenes',
    'setup.cache_hint': () => 'Caché de teselas decodificadas/comprimidas, leída en cada solicitud de textura — elige una unidad local rápida (SSD) con espacio para crecer hasta el límite indicado abajo. Por defecto: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Directorio de descargas temporales',
    'setup.download_hint': () => 'Espacio temporal para descargas de escenarios en curso — necesita aproximadamente el doble del tamaño de la descarga, libre temporalmente (se comprueba antes de cada instalación). Por defecto: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Examinar…',
    'setup.detect_xplane': () => 'Buscar X-Plane…',
    'setup.detect_xplane_none': () => 'No se encontró ninguna instalación de X-Plane automáticamente — usa Examinar… en su lugar.',
    'setup.detect_xplane_found': (p) => `Encontrado: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Se encontraron ${p.n} instalaciones de X-Plane — elige una.`,
    'setup.detect_xplane_error': (p) => `No se pudo buscar X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Tamaño de caché (GB)',
    'setup.clean_cache': () => 'Limpiar caché',
    'setup.auto_open_label': () => 'Abrir el navegador automáticamente al iniciar',
    'setup.save': () => 'Guardar',
    'setup.save_tooltip': () => 'Guarda toda la configuración',

    'zoom.heading': () => 'Zoom y calidad',
    'zoom.maptype_label': () => 'Proveedor de imágenes (anular)',
    'zoom.maptype_default': () => '(por defecto del proveedor)',
    'zoom.minzoom_label': () => 'Zoom mínimo',
    'zoom.minzoom_hint': () => '(un piso, no un techo — nunca sube la calidad por encima de lo que ofrece el proveedor)',
    'zoom.maxzoom_label': () => 'Zoom máximo',
    'zoom.maxzoom_hint': () => '(un techo — limita la nitidez máxima descargada, para ahorrar espacio en disco y ancho de banda. Las zonas densas — ciudades, aeropuertos — piden de forma natural más zoom que el campo abierto, así que un límite bajo las afecta mucho más: limitar 3 niveles por debajo de lo que una zona pide significa 8 veces menos píxeles ahí, no un simple desenfoque. 18 es un mínimo práctico para que aeropuertos/ciudades se vean nítidos; "Sin límite" mantiene el comportamiento actual.)',
    'zoom.nocap': () => 'Sin límite',
    'zoom.maxzoom_warning': () => '⚠ Por debajo de 18, las zonas densas (ciudades, aeropuertos) se verán notablemente bloqueadas — esto las afecta mucho más que al campo abierto con el mismo ajuste.',
    'zoom.maxwait_label': () => 'Espera máxima (segundos por fragmento)',
    'zoom.maxbuilds_label': () => 'Construcciones de teselas simultáneas máx.',
    'zoom.maxbuilds_hint': () => '(presupuesto de memoria, no de red — aplica tras el próximo reinicio)',
    'zoom.fetchthreads_label': () => 'Hilos de descarga de fragmentos',
    'zoom.fetchthreads_hint': () => '(un único grupo compartido por el que compiten equitativamente las descargas de todas las teselas; aplica tras el próximo reinicio)',
    'zoom.maxopentiles_label': () => 'Teselas abiertas máx.',
    'zoom.maxopentiles_hint': () => '(presupuesto de memoria — las teselas abiertas por encima de este límite muestran un marcador simple en vez de construirse por completo)',
    'zoom.perfmode_label': () => 'Modo de rendimiento',
    'zoom.perfmode_hint': () => '(qué tan pronto se sacrifica nitidez por velocidad cuando muchas teselas quieren detalle completo a la vez — p. ej. una ciudad densa o una vista amplia; se aplica de inmediato)',
    'zoom.perfmode_quality': () => 'Calidad',
    'zoom.perfmode_balanced': () => 'Equilibrado',
    'zoom.perfmode_speed': () => 'Velocidad',
    'zoom.coarsefallback_label': () => 'Desactivar el sustituto borroso para teselas lentas/faltantes',
    'zoom.coarsefallback_hint': () => '(si está desactivado, un trozo lento o fallido se muestra como un parche borroso de un nivel de zoom más bajo en caché en vez de un color plano — se ve como un suave desenfoque sobre tierra, puede verse sucio/deslavado sobre agua; se aplica de inmediato)',

    'stats.heading': () => 'Estadísticas',
    'stats.cached_tiles': () => 'Teselas en caché',
    'stats.open_tiles': () => 'Teselas abiertas',
    'stats.process_memory': () => 'Memoria del proceso',
    'stats.active_mounts': () => 'Montajes activos',

    'scenery.heading': () => 'Escenarios',
    'scenery.refresh': () => 'Actualizar lista',
    'scenery.install': () => 'Instalar',
    'scenery.update': () => 'Actualizar',
    'scenery.working': () => 'Trabajando…',
    'scenery.installed': (p) => `instalado ${p.ver}`,
    'scenery.update_available': (p) => `actualización disponible: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `error: ${p.msg}`,
    'scenery.package_n_of': (p) => `paquete ${p.index}/${p.total}`,
    'scenery.mount': () => 'Montar',
    'scenery.unmount': () => 'Desmontar',
    'scenery.mounted': () => 'Montado',
    'scenery.unmounted': () => 'No montado',
    'scenery.verify': () => 'Verificar',
    'scenery.verifying': () => 'Verificando…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'Registro',

    'confirm.yes': () => 'Sí',
    'confirm.cancel': () => 'Cancelar',
    'confirm.stop_text': () => '¿Detener todos los montajes? X-Plane perderá sus texturas de inmediato.',
    'confirm.shutdown_text': () => '¿Apagar autoOr-simg por completo? Esto detiene los montajes y cierra el programa.',
    'confirm.restart_text': () => '¿Reiniciar autoOr-simg ahora? Esto detiene los montajes y luego inicia una instancia nueva de inmediato.',

    'dirbrowser.up': () => '↑ Subir',
    'dirbrowser.select': () => 'Seleccionar esta carpeta',
    'dirbrowser.cancel': () => 'Cancelar',

    'theme.light': () => 'Claro',
    'theme.dark': () => 'Oscuro',
    'attribution.text': () => 'autoOr-simg es una reimplementación no oficial en Go de <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho, de kubilus1</a> — gracias al proyecto original y a su autor.',

    'settings.tooltip': () => 'Configuración',
    'settings.heading': () => 'Configuración',
    'settings.close': () => 'Cerrar',
    'settings.language_label': () => 'Idioma',
    'settings.theme_label': () => 'Tema',
    'settings.demo_mode_label': () => 'Modo demostración',
    'settings.demo_mode_hint': () => 'Oculta tu nombre de usuario en los campos de ruta de X-Plane/escenario/caché/descargas y en el bloque de Log — para capturas y grabaciones de pantalla. Solo cambia lo que se muestra, no los valores reales guardados. Solo afecta esta pestaña del navegador, no se guarda en la configuración.',
    'settings.demo_mode_enable': () => 'Activar modo demostración',
    'settings.background_label': () => 'Fondo',
    'settings.background_hint': () => 'Elige una foto para mostrarla tenuemente detrás de la página — los paneles siguen siendo sólidos, así que todo sigue siendo legible.',
    'settings.choose_background': () => 'Elegir imagen…',
    'settings.remove_background': () => 'Quitar fondo',
    'settings.opacity_label': () => 'Opacidad',
    'settings.background_too_large': () => 'No se pudo guardar esa imagen (demasiado grande para el almacenamiento del navegador) — prueba con una foto más pequeña.',
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'Una pequeña dama en la esquina que sigue tu cursor con la mirada. Haz clic en ella para un consejo.',
    'settings.mascot_outfit': () => 'Vestuario',
    'mascot.outfit_jetfighter': () => 'Caza',
    'mascot.outfit_acrobatic': () => 'Acrobático',
    'mascot.outfit_airliner': () => 'Aerolínea',
    'mascot.outfit_agricultural': () => 'Agrícola',
  },

  ru: {
    'status.connecting': () => 'подключение…',
    'status.connected': () => 'подключено',
    'status.disconnected': () => 'соединение потеряно — повтор…',
    'status.stopped': () => 'остановлено',
    'status.running': (p) => `запущено (${p.n} ${ruMountWord(p.n)})`,
    'status.saved': () => 'Сохранено',
    'status.error': (p) => `Ошибка: ${p.msg}`,
    'status.cache_cleaned': (p) => `Кэш очищен: файлов — ${p.n}, освобождено ${p.mb} МБ`,

    'btn.run': () => 'Запустить',
    'btn.stop': () => 'Остановить',
    'btn.shutdown': () => 'Выключить',
    'btn.restart': () => 'Перезапустить',

    'setup.heading': () => 'Настройка',
    'setup.xplane_label': () => 'Папка установки X-Plane',
    'setup.xplane_hint': () => 'Корневая папка X-Plane — та, что содержит <code>Custom Scenery</code> напрямую, а не сама эта папка. Пример: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Папка установки сцен',
    'setup.scenery_hint': () => 'Отдельная папка для собственных пакетов autoOr-simg — обычно <code>&lt;путь к X-Plane&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg устанавливает и удаляет всё содержимое этой папки, поэтому выделите под неё отдельную папку, а не используйте уже занятую другим содержимым.',
    'setup.cache_label': () => 'Папка кэша изображений',
    'setup.cache_hint': () => 'Кэш декодированных/сжатых тайлов, читается при каждом запросе текстуры — выберите быстрый локальный диск (SSD) с запасом места до предела, указанного ниже. По умолчанию: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Папка временных загрузок',
    'setup.download_hint': () => 'Временное место для загружаемых сцен — нужно примерно вдвое больше свободного места, чем размер загрузки (проверяется перед каждой установкой). По умолчанию: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Обзор…',
    'setup.detect_xplane': () => 'Найти X-Plane…',
    'setup.detect_xplane_none': () => 'Не удалось автоматически найти установку X-Plane — используйте «Обзор…».',
    'setup.detect_xplane_found': (p) => `Найдено: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Найдено ${p.n} ${ruInstallWord(p.n)} X-Plane — выберите одну.`,
    'setup.detect_xplane_error': (p) => `Не удалось найти X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Размер кэша (ГБ)',
    'setup.clean_cache': () => 'Очистить кэш',
    'setup.auto_open_label': () => 'Открывать браузер автоматически при запуске',
    'setup.save': () => 'Сохранить',
    'setup.save_tooltip': () => 'Сохраняет все настройки',

    'zoom.heading': () => 'Масштаб и качество',
    'zoom.maptype_label': () => 'Переопределение провайдера снимков',
    'zoom.maptype_default': () => '(по умолчанию провайдера)',
    'zoom.minzoom_label': () => 'Минимальный масштаб',
    'zoom.minzoom_hint': () => '(нижний предел, а не верхний — никогда не повышает качество выше того, что даёт провайдер)',
    'zoom.maxzoom_label': () => 'Максимальный масштаб',
    'zoom.maxzoom_hint': () => '(верхний предел — ограничивает максимальную чёткость загружаемых снимков, экономя место на диске и трафик. Плотные зоны — города, аэропорты — изначально требуют более высокий масштаб, чем открытая местность, поэтому низкий предел бьёт по ним гораздо сильнее: ограничение на 3 уровня ниже нужного означает в 8 раз меньше пикселей там, а не лёгкое размытие. 18 — практический минимум, чтобы аэропорты/города оставались чёткими; «Без ограничения» сохраняет текущее поведение.)',
    'zoom.nocap': () => 'Без ограничения',
    'zoom.maxzoom_warning': () => '⚠ Ниже 18 плотные зоны (города, аэропорты) будут заметно блочными — это бьёт по ним намного сильнее, чем по открытой местности при том же значении.',
    'zoom.maxwait_label': () => 'Макс. ожидание (секунд на фрагмент)',
    'zoom.maxbuilds_label': () => 'Макс. одновременных построений тайлов',
    'zoom.maxbuilds_hint': () => '(бюджет памяти, не сети — вступит в силу после перезапуска)',
    'zoom.fetchthreads_label': () => 'Потоков загрузки фрагментов',
    'zoom.fetchthreads_hint': () => '(единый общий пул, за который честно конкурируют загрузки всех тайлов; вступит в силу после перезапуска)',
    'zoom.maxopentiles_label': () => 'Макс. открытых тайлов',
    'zoom.maxopentiles_hint': () => '(бюджет памяти — тайлы, открытые сверх этого предела, получают дешёвую заглушку вместо полной сборки)',
    'zoom.perfmode_label': () => 'Режим производительности',
    'zoom.perfmode_hint': () => '(насколько рано жертвовать чёткостью ради скорости, когда много тайлов одновременно хотят полную детализацию — например, плотный город или широкий обзор; применяется немедленно)',
    'zoom.perfmode_quality': () => 'Качество',
    'zoom.perfmode_balanced': () => 'Баланс',
    'zoom.perfmode_speed': () => 'Скорость',
    'zoom.coarsefallback_label': () => 'Отключить размытую заглушку для медленных/отсутствующих тайлов',
    'zoom.coarsefallback_hint': () => '(если выключено, медленный или неудавшийся фрагмент показывается как размытая заглушка из более грубого кэшированного уровня зума вместо сплошного цвета — выглядит как мягкий фокус над сушей, может выглядеть грязно над водой; применяется немедленно)',

    'stats.heading': () => 'Статистика',
    'stats.cached_tiles': () => 'Тайлов в кэше',
    'stats.open_tiles': () => 'Открытых тайлов',
    'stats.process_memory': () => 'Память процесса',
    'stats.active_mounts': () => 'Активных подключений',

    'scenery.heading': () => 'Сцены',
    'scenery.refresh': () => 'Обновить',
    'scenery.install': () => 'Установить',
    'scenery.update': () => 'Обновить',
    'scenery.working': () => 'Выполняется…',
    'scenery.installed': (p) => `установлено ${p.ver}`,
    'scenery.update_available': (p) => `доступно обновление: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} МБ`,
    'scenery.error': (p) => `ошибка: ${p.msg}`,
    'scenery.package_n_of': (p) => `пакет ${p.index}/${p.total}`,
    'scenery.verify': () => 'Проверить',
    'scenery.mount': () => 'Смонтировать',
    'scenery.unmount': () => 'Размонтировать',
    'scenery.mounted': () => 'Смонтировано',
    'scenery.unmounted': () => 'Не смонтировано',
    'scenery.verifying': () => 'Проверка…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'Журнал',

    'confirm.yes': () => 'Да',
    'confirm.cancel': () => 'Отмена',
    'confirm.stop_text': () => 'Остановить все подключения? X-Plane немедленно потеряет текстуры.',
    'confirm.shutdown_text': () => 'Полностью выключить autoOr-simg? Это остановит все подключения и завершит программу.',
    'confirm.restart_text': () => 'Перезапустить autoOr-simg сейчас? Это остановит подключения, а затем немедленно запустит новый экземпляр.',

    'dirbrowser.up': () => '↑ Вверх',
    'dirbrowser.select': () => 'Выбрать эту папку',
    'dirbrowser.cancel': () => 'Отмена',

    'theme.light': () => 'Светлая',
    'theme.dark': () => 'Тёмная',
    'attribution.text': () => 'autoOr-simg — неофициальная Go-реализация <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho авторства kubilus1</a> — спасибо оригинальному проекту и его автору.',

    'settings.tooltip': () => 'Настройки',
    'settings.heading': () => 'Настройки',
    'settings.close': () => 'Закрыть',
    'settings.language_label': () => 'Язык',
    'settings.theme_label': () => 'Тема',
    'settings.demo_mode_label': () => 'Демонстрационный режим',
    'settings.demo_mode_hint': () => 'Скрывает ваше имя пользователя в полях пути X-Plane/сценарии/кэш/загрузки и в блоке журнала — для скриншотов и записи экрана. Меняет только отображение, а не реальные сохранённые значения. Влияет только на эту вкладку браузера, не сохраняется в конфигурации.',
    'settings.demo_mode_enable': () => 'Включить демонстрационный режим',
    'settings.background_label': () => 'Фон',
    'settings.background_hint': () => 'Выберите фото, которое будет слабо видно позади страницы — панели остаются сплошными, так что всё по-прежнему читается.',
    'settings.choose_background': () => 'Выбрать изображение…',
    'settings.remove_background': () => 'Убрать фон',
    'settings.opacity_label': () => 'Непрозрачность',
    'settings.background_too_large': () => 'Не удалось сохранить это изображение (слишком большое для хранилища браузера) — попробуйте фото поменьше.',
    'settings.mascot_label': () => 'Наталья',
    'settings.mascot_hint': () => 'Маленькая леди в углу, которая следит глазами за курсором. Нажмите на неё, чтобы получить совет.',
    'settings.mascot_outfit': () => 'Костюм',
    'mascot.outfit_jetfighter': () => 'Истребитель',
    'mascot.outfit_acrobatic': () => 'Пилотажник',
    'mascot.outfit_airliner': () => 'Авиалайнер',
    'mascot.outfit_agricultural': () => 'Агропилот',
  },

  pt: {
    'status.connecting': () => 'conectando…',
    'status.connected': () => 'conectado',
    'status.disconnected': () => 'desconectado — tentando novamente…',
    'status.stopped': () => 'parado',
    'status.running': (p) => `em execução (${p.n} ${p.n === 1 ? 'montagem' : 'montagens'})`,
    'status.saved': () => 'Salvo',
    'status.error': (p) => `Erro: ${p.msg}`,
    'status.cache_cleaned': (p) => `Cache limpo: ${p.n} arquivo(s), ${p.mb} MB liberados`,

    'btn.run': () => 'Iniciar',
    'btn.stop': () => 'Parar',
    'btn.shutdown': () => 'Desligar',
    'btn.restart': () => 'Reiniciar',

    'setup.heading': () => 'Configuração',
    'setup.xplane_label': () => 'Diretório de instalação do X-Plane',
    'setup.xplane_hint': () => 'A pasta raiz do X-Plane — a que contém diretamente <code>Custom Scenery</code>, não essa pasta em si. Exemplo: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Diretório de instalação de cenários',
    'setup.scenery_hint': () => 'Uma pasta dedicada para os pacotes próprios do autoOr-simg — geralmente <code>&lt;caminho do X-Plane&gt;/Custom Scenery/z_autoortho</code>. O autoOr-simg instala e desinstala tudo dentro dessa pasta, então use uma pasta própria em vez de reaproveitar uma com outro conteúdo.',
    'setup.cache_label': () => 'Diretório de cache de imagens',
    'setup.cache_hint': () => 'Cache de blocos decodificados/comprimidos, lido a cada solicitação de textura — escolha um disco local rápido (SSD) com espaço para crescer até o limite indicado abaixo. Padrão: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Diretório de downloads temporários',
    'setup.download_hint': () => 'Espaço temporário para downloads de cenários em andamento — precisa de aproximadamente 2x o tamanho do download livre temporariamente (verificado antes de cada instalação). Padrão: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Procurar…',
    'setup.detect_xplane': () => 'Localizar X-Plane…',
    'setup.detect_xplane_none': () => 'Nenhuma instalação do X-Plane encontrada automaticamente — use Procurar… em vez disso.',
    'setup.detect_xplane_found': (p) => `Encontrado: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Encontradas ${p.n} instalações do X-Plane — escolha uma.`,
    'setup.detect_xplane_error': (p) => `Não foi possível procurar o X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Tamanho do cache (GB)',
    'setup.clean_cache': () => 'Limpar cache',
    'setup.auto_open_label': () => 'Abrir o navegador automaticamente ao iniciar',
    'setup.save': () => 'Salvar',
    'setup.save_tooltip': () => 'Salva todas as configurações',

    'zoom.heading': () => 'Zoom e qualidade',
    'zoom.maptype_label': () => 'Substituir provedor de imagens',
    'zoom.maptype_default': () => '(padrão do provedor)',
    'zoom.minzoom_label': () => 'Zoom mínimo',
    'zoom.minzoom_hint': () => '(um piso, não um teto — nunca aumenta a qualidade além do que o provedor oferece)',
    'zoom.maxzoom_label': () => 'Zoom máximo',
    'zoom.maxzoom_hint': () => '(um teto — limita a nitidez máxima baixada, para economizar espaço em disco e banda. Áreas densas — cidades, aeroportos — naturalmente pedem mais zoom que o campo aberto, então um limite baixo as atinge muito mais: limitar 3 níveis abaixo do que uma área pede significa 8x menos pixels ali, não um simples desfoque. 18 é um piso prático para aeroportos/cidades continuarem nítidos; "Sem limite" mantém o comportamento atual.)',
    'zoom.nocap': () => 'Sem limite',
    'zoom.maxzoom_warning': () => '⚠ Abaixo de 18, áreas densas (cidades, aeroportos) ficarão visivelmente quadriculadas — isso as afeta muito mais do que o campo aberto com o mesmo ajuste.',
    'zoom.maxwait_label': () => 'Espera máxima (segundos por fragmento)',
    'zoom.maxbuilds_label': () => 'Máx. de construções de blocos simultâneas',
    'zoom.maxbuilds_hint': () => '(orçamento de memória, não de rede — aplica-se após o próximo reinício)',
    'zoom.fetchthreads_label': () => 'Threads de busca de fragmentos',
    'zoom.fetchthreads_hint': () => '(um único grupo compartilhado pelo qual as buscas de todos os blocos competem igualmente; aplica-se após o próximo reinício)',
    'zoom.maxopentiles_label': () => 'Máx. de blocos abertos',
    'zoom.maxopentiles_hint': () => '(orçamento de memória — blocos abertos além deste limite recebem um placeholder simples em vez de uma construção completa)',
    'zoom.perfmode_label': () => 'Modo de desempenho',
    'zoom.perfmode_hint': () => '(quão cedo trocar nitidez por velocidade quando muitos blocos querem detalhe total ao mesmo tempo — ex.: uma cidade densa ou uma vista ampla; aplicado imediatamente)',
    'zoom.perfmode_quality': () => 'Qualidade',
    'zoom.perfmode_balanced': () => 'Equilibrado',
    'zoom.perfmode_speed': () => 'Velocidade',
    'zoom.coarsefallback_label': () => 'Desativar o substituto desfocado para blocos lentos/ausentes',
    'zoom.coarsefallback_hint': () => '(quando desativado, um fragmento lento ou com falha é mostrado como um substituto desfocado de um nível de zoom mais baixo em cache, em vez de um patch de cor sólida — parece um suave desfoque sobre terra, pode parecer sujo/desbotado sobre água; aplicado imediatamente)',

    'stats.heading': () => 'Estatísticas',
    'stats.cached_tiles': () => 'Blocos em cache',
    'stats.open_tiles': () => 'Blocos abertos',
    'stats.process_memory': () => 'Memória do processo',
    'stats.active_mounts': () => 'Montagens ativas',

    'scenery.heading': () => 'Cenários',
    'scenery.refresh': () => 'Atualizar',
    'scenery.install': () => 'Instalar',
    'scenery.update': () => 'Atualizar',
    'scenery.working': () => 'Processando…',
    'scenery.installed': (p) => `instalado ${p.ver}`,
    'scenery.update_available': (p) => `atualização disponível: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `erro: ${p.msg}`,
    'scenery.package_n_of': (p) => `pacote ${p.index}/${p.total}`,
    'scenery.mount': () => 'Montar',
    'scenery.unmount': () => 'Desmontar',
    'scenery.mounted': () => 'Montado',
    'scenery.unmounted': () => 'Não montado',
    'scenery.verify': () => 'Verificar',
    'scenery.verifying': () => 'Verificando…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'Registro',

    'confirm.yes': () => 'Sim',
    'confirm.cancel': () => 'Cancelar',
    'confirm.stop_text': () => 'Parar todas as montagens? O X-Plane perderá as texturas imediatamente.',
    'confirm.shutdown_text': () => 'Desligar o autoOr-simg completamente? Isso para as montagens e encerra o programa.',
    'confirm.restart_text': () => 'Reiniciar o autoOr-simg agora? Isso para as montagens e inicia uma nova instância imediatamente.',

    'dirbrowser.up': () => '↑ Subir',
    'dirbrowser.select': () => 'Selecionar esta pasta',
    'dirbrowser.cancel': () => 'Cancelar',

    'theme.light': () => 'Claro',
    'theme.dark': () => 'Escuro',
    'attribution.text': () => 'autoOr-simg é uma reimplementação não oficial em Go do <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho, de kubilus1</a> — nosso agradecimento ao projeto original e ao seu autor.',

    'settings.tooltip': () => 'Configurações',
    'settings.heading': () => 'Configurações',
    'settings.close': () => 'Fechar',
    'settings.language_label': () => 'Idioma',
    'settings.theme_label': () => 'Tema',
    'settings.demo_mode_label': () => 'Modo de demonstração',
    'settings.demo_mode_hint': () => 'Oculta seu nome de usuário nos campos de caminho do X-Plane/cenário/cache/downloads e no bloco de Log — para capturas e gravações de tela. Altera apenas o que é exibido, não os valores reais salvos. Afeta apenas esta aba do navegador, não é salvo na configuração.',
    'settings.demo_mode_enable': () => 'Ativar modo de demonstração',
    'settings.background_label': () => 'Plano de fundo',
    'settings.background_hint': () => 'Escolha uma foto para aparecer de forma sutil atrás da página — os painéis continuam sólidos, então tudo continua legível.',
    'settings.choose_background': () => 'Escolher imagem…',
    'settings.remove_background': () => 'Remover plano de fundo',
    'settings.opacity_label': () => 'Opacidade',
    'settings.background_too_large': () => 'Não foi possível salvar essa imagem (grande demais para o armazenamento do navegador) — tente uma foto menor.',
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'Uma pequena dama no canto que acompanha seu cursor com o olhar. Clique nela para uma dica.',
    'settings.mascot_outfit': () => 'Roupa',
    'mascot.outfit_jetfighter': () => 'Caça',
    'mascot.outfit_acrobatic': () => 'Acrobático',
    'mascot.outfit_airliner': () => 'Comercial',
    'mascot.outfit_agricultural': () => 'Agrícola',
  },

  el: {
    'status.connecting': () => 'σύνδεση…',
    'status.connected': () => 'συνδέθηκε',
    'status.disconnected': () => 'αποσυνδέθηκε — νέα προσπάθεια…',
    'status.stopped': () => 'διακόπηκε',
    'status.running': (p) => `σε λειτουργία (${p.n} ${p.n === 1 ? 'προσάρτηση' : 'προσαρτήσεις'})`,
    'status.saved': () => 'Αποθηκεύτηκε',
    'status.error': (p) => `Σφάλμα: ${p.msg}`,
    'status.cache_cleaned': (p) => `Η προσωρινή μνήμη καθαρίστηκε: ${p.n} αρχείο(α), ${p.mb} MB ελευθερώθηκαν`,

    'btn.run': () => 'Εκτέλεση',
    'btn.stop': () => 'Διακοπή',
    'btn.shutdown': () => 'Τερματισμός',
    'btn.restart': () => 'Επανεκκίνηση',

    'setup.heading': () => 'Ρύθμιση',
    'setup.xplane_label': () => 'Φάκελος εγκατάστασης X-Plane',
    'setup.xplane_hint': () => 'Ο ριζικός φάκελος του X-Plane — αυτός που περιέχει απευθείας τον φάκελο <code>Custom Scenery</code>, όχι αυτόν τον φάκελο. Παράδειγμα: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Φάκελος εγκατάστασης σκηνικών',
    'setup.scenery_hint': () => 'Ένας αποκλειστικός φάκελος για τα δικά πακέτα του autoOr-simg — συνήθως <code>&lt;διαδρομή X-Plane&gt;/Custom Scenery/z_autoortho</code>. Το autoOr-simg εγκαθιστά/απεγκαθιστά τα πάντα μέσα σε αυτόν τον φάκελο, οπότε δώστε του έναν δικό του αντί να επαναχρησιμοποιήσετε έναν φάκελο με άλλο περιεχόμενο.',
    'setup.cache_label': () => 'Φάκελος προσωρινής μνήμης εικόνων',
    'setup.cache_hint': () => 'Προσωρινή μνήμη αποκωδικοποιημένων/συμπιεσμένων πλακιδίων, που διαβάζεται σε κάθε αίτημα υφής — επιλέξτε έναν γρήγορο τοπικό δίσκο (SSD) με χώρο να μεγαλώσει έως το παρακάτω όριο. Προεπιλογή: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Προσωρινός φάκελος λήψεων',
    'setup.download_hint': () => 'Προσωρινός χώρος για λήψεις σκηνικών σε εξέλιξη — χρειάζεται περίπου 2x το μέγεθος της λήψης ελεύθερο προσωρινά (ελέγχεται πριν από κάθε εγκατάσταση). Προεπιλογή: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Περιήγηση…',
    'setup.detect_xplane': () => 'Εύρεση X-Plane…',
    'setup.detect_xplane_none': () => 'Δεν βρέθηκε αυτόματα εγκατάσταση X-Plane — χρησιμοποιήστε την Περιήγηση… αντ’ αυτού.',
    'setup.detect_xplane_found': (p) => `Βρέθηκε: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Βρέθηκαν ${p.n} εγκαταστάσεις X-Plane — επιλέξτε μία.`,
    'setup.detect_xplane_error': (p) => `Δεν ήταν δυνατή η αναζήτηση για X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Μέγεθος προσωρινής μνήμης (GB)',
    'setup.clean_cache': () => 'Εκκαθάριση προσωρινής μνήμης',
    'setup.auto_open_label': () => 'Αυτόματο άνοιγμα του προγράμματος περιήγησης κατά την εκκίνηση',
    'setup.save': () => 'Αποθήκευση',
    'setup.save_tooltip': () => 'Αποθηκεύει όλες τις ρυθμίσεις',

    'zoom.heading': () => 'Ζουμ & ποιότητα',
    'zoom.maptype_label': () => 'Παράκαμψη τύπου χάρτη',
    'zoom.maptype_default': () => '(προεπιλογή παρόχου)',
    'zoom.minzoom_label': () => 'Ελάχιστο ζουμ',
    'zoom.minzoom_hint': () => '(κατώτατο όριο, όχι ανώτατο — ποτέ δεν αυξάνει την ποιότητα πάνω από ό,τι προσφέρει ο πάροχος)',
    'zoom.maxzoom_label': () => 'Μέγιστο ζουμ',
    'zoom.maxzoom_hint': () => '(ανώτατο όριο — περιορίζει την πιο ευκρινή εικόνα που λαμβάνεται ποτέ, για εξοικονόμηση χώρου δίσκου και εύρους ζώνης. Οι πυκνές περιοχές — πόλεις, αεροδρόμια — ζητούν εγγενώς μεγαλύτερο ζουμ από την ανοιχτή ύπαιθρο, οπότε ένα χαμηλό όριο τις επηρεάζει πολύ περισσότερο: περιορισμός 3 επιπέδων κάτω από ό,τι θέλει μια περιοχή σημαίνει 8 φορές λιγότερα pixel εκεί, όχι απλό θόλωμα. Το 18 είναι ένα πρακτικό κατώτατο όριο ώστε αεροδρόμια/πόλεις να παραμένουν ευκρινή· η επιλογή "Χωρίς όριο" αφήνει αυτό το όριο απενεργοποιημένο.)',
    'zoom.nocap': () => 'Χωρίς όριο',
    'zoom.maxzoom_warning': () => '⚠ Κάτω από το 18, οι πυκνές περιοχές (πόλεις, αεροδρόμια) θα φαίνονται αισθητά τετραγωνισμένες — αυτό τις επηρεάζει πολύ περισσότερο από την ανοιχτή ύπαιθρο με την ίδια ρύθμιση.',
    'zoom.maxwait_label': () => 'Μέγιστη αναμονή (δευτερόλεπτα ανά λήψη τμήματος)',
    'zoom.maxbuilds_label': () => 'Μέγιστες ταυτόχρονες κατασκευές πλακιδίων',
    'zoom.maxbuilds_hint': () => '(προϋπολογισμός μνήμης, όχι δικτύου — ισχύει από την επόμενη επανεκκίνηση)',
    'zoom.fetchthreads_label': () => 'Νήματα λήψης τμημάτων',
    'zoom.fetchthreads_hint': () => '(μία κοινόχρηστη δεξαμενή για την οποία ανταγωνίζονται δίκαια οι λήψεις τμημάτων όλων των πλακιδίων, σε όλη τη διεργασία· ισχύει από την επόμενη επανεκκίνηση)',
    'zoom.maxopentiles_label': () => 'Μέγιστα ανοιχτά πλακίδια',
    'zoom.maxopentiles_hint': () => '(προϋπολογισμός μνήμης — τα πλακίδια που ανοίγουν πέρα από αυτό το όριο εξυπηρετούνται με ένα απλό υποκατάστατο αντί για πλήρη κατασκευή)',
    'zoom.perfmode_label': () => 'Λειτουργία απόδοσης',
    'zoom.perfmode_hint': () => '(πόσο νωρίς θυσιάζεται η ευκρίνεια για ταχύτητα όταν πολλά πλακίδια θέλουν πλήρη λεπτομέρεια ταυτόχρονα — π.χ. μια πυκνή πόλη ή μια ευρεία θέα· ισχύει άμεσα)',
    'zoom.perfmode_quality': () => 'Ποιότητα',
    'zoom.perfmode_balanced': () => 'Ισορροπημένο',
    'zoom.perfmode_speed': () => 'Ταχύτητα',
    'zoom.coarsefallback_label': () => 'Απενεργοποίηση θολού υποκατάστατου για αργά/λείποντα πλακίδια',
    'zoom.coarsefallback_hint': () => '(όταν είναι απενεργοποιημένο, ένα αργό ή αποτυχημένο τμήμα εμφανίζεται ως θολό υποκατάστατο από ένα πιο χονδρικό αποθηκευμένο επίπεδο ζουμ αντί για ένα συμπαγές χρωματιστό μπάλωμα — μοιάζει με απαλό θόλωμα πάνω από ξηρά, μπορεί να φαίνεται θαμπό/βρόμικο πάνω από νερό· ισχύει άμεσα)',

    'stats.heading': () => 'Στατιστικά',
    'stats.cached_tiles': () => 'Πλακίδια στην προσωρινή μνήμη',
    'stats.open_tiles': () => 'Ανοιχτά πλακίδια',
    'stats.process_memory': () => 'Μνήμη διεργασίας',
    'stats.active_mounts': () => 'Ενεργές προσαρτήσεις',

    'scenery.heading': () => 'Σκηνικά',
    'scenery.refresh': () => 'Ανανέωση',
    'scenery.install': () => 'Εγκατάσταση',
    'scenery.update': () => 'Ενημέρωση',
    'scenery.working': () => 'Επεξεργασία…',
    'scenery.installed': (p) => `εγκατεστημένο ${p.ver}`,
    'scenery.update_available': (p) => `διαθέσιμη ενημέρωση: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `σφάλμα: ${p.msg}`,
    'scenery.package_n_of': (p) => `πακέτο ${p.index}/${p.total}`,
    'scenery.verify': () => 'Επαλήθευση',
    'scenery.mount': () => 'Προσάρτηση',
    'scenery.unmount': () => 'Αποπροσάρτηση',
    'scenery.mounted': () => 'Προσαρτημένο',
    'scenery.unmounted': () => 'Μη προσαρτημένο',
    'scenery.verifying': () => 'Επαλήθευση σε εξέλιξη…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'Αρχείο καταγραφής',

    'confirm.yes': () => 'Ναι',
    'confirm.cancel': () => 'Ακύρωση',
    'confirm.stop_text': () => 'Διακοπή όλων των προσαρτήσεων; Το X-Plane θα χάσει τις υφές του αμέσως.',
    'confirm.shutdown_text': () => 'Πλήρης τερματισμός του autoOr-simg; Αυτό σταματά τις προσαρτήσεις και κλείνει το πρόγραμμα.',
    'confirm.restart_text': () => 'Επανεκκίνηση του autoOr-simg τώρα; Αυτό σταματά τις προσαρτήσεις και ξεκινά αμέσως μια νέα διεργασία.',

    'dirbrowser.up': () => '↑ Πάνω',
    'dirbrowser.select': () => 'Επιλογή αυτού του φακέλου',
    'dirbrowser.cancel': () => 'Ακύρωση',

    'theme.light': () => 'Φωτεινό',
    'theme.dark': () => 'Σκοτεινό',
    'attribution.text': () => 'Το autoOr-simg είναι μια ανεπίσημη υλοποίηση σε Go του <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho του kubilus1</a> — ευχαριστούμε το αρχικό έργο και τον δημιουργό του.',

    'settings.tooltip': () => 'Ρυθμίσεις',
    'settings.heading': () => 'Ρυθμίσεις',
    'settings.close': () => 'Κλείσιμο',
    'settings.language_label': () => 'Γλώσσα',
    'settings.theme_label': () => 'Θέμα',
    'settings.demo_mode_label': () => 'Λειτουργία επίδειξης',
    'settings.demo_mode_hint': () => 'Αποκρύπτει το όνομα χρήστη σας στα πεδία διαδρομής X-Plane/σκηνικών/προσωρινής μνήμης/λήψεων και στο τμήμα καταγραφής — για στιγμιότυπα και εγγραφές οθόνης. Αλλάζει μόνο ό,τι εμφανίζεται, όχι τις πραγματικές αποθηκευμένες τιμές. Επηρεάζει μόνο αυτήν την καρτέλα του προγράμματος περιήγησης, δεν αποθηκεύεται στις ρυθμίσεις.',
    'settings.demo_mode_enable': () => 'Ενεργοποίηση λειτουργίας επίδειξης',
    'settings.background_label': () => 'Φόντο',
    'settings.background_hint': () => 'Επιλέξτε μια φωτογραφία να εμφανίζεται αχνά πίσω από τη σελίδα — τα πλαίσια παραμένουν συμπαγή, οπότε όλα παραμένουν ευανάγνωστα.',
    'settings.choose_background': () => 'Επιλογή εικόνας…',
    'settings.remove_background': () => 'Αφαίρεση φόντου',
    'settings.opacity_label': () => 'Αδιαφάνεια',
    'settings.background_too_large': () => 'Δεν ήταν δυνατή η αποθήκευση αυτής της εικόνας (πολύ μεγάλη για τον χώρο αποθήκευσης του προγράμματος περιήγησης) — δοκιμάστε μια μικρότερη φωτογραφία.',
    'settings.mascot_label': () => 'Νατάλια',
    'settings.mascot_hint': () => 'Μια μικρή κυρία στη γωνία που παρακολουθεί τον κέρσορά σας. Κάντε κλικ πάνω της για μια συμβουλή.',
    'settings.mascot_outfit': () => 'Στολή',
    'mascot.outfit_jetfighter': () => 'Μαχητικό',
    'mascot.outfit_acrobatic': () => 'Ακροβατικό',
    'mascot.outfit_airliner': () => 'Επιβατικό',
    'mascot.outfit_agricultural': () => 'Αγροτικό',
  },

  ar: {
    'status.connecting': () => 'جارٍ الاتصال…',
    'status.connected': () => 'متصل',
    'status.disconnected': () => 'انقطع الاتصال — إعادة المحاولة…',
    'status.stopped': () => 'متوقف',
    'status.running': (p) => `قيد التشغيل (${p.n} ${p.n === 1 ? 'تثبيت' : 'تثبيتات'})`,
    'status.saved': () => 'تم الحفظ',
    'status.error': (p) => `خطأ: ${p.msg}`,
    'status.cache_cleaned': (p) => `تم تنظيف ذاكرة التخزين المؤقت: ${p.n} ملف(ات)، تم تحرير ${p.mb} ميجابايت`,

    'btn.run': () => 'تشغيل',
    'btn.stop': () => 'إيقاف',
    'btn.shutdown': () => 'إغلاق',
    'btn.restart': () => 'إعادة التشغيل',

    'setup.heading': () => 'الإعداد',
    'setup.xplane_label': () => 'مجلد تثبيت X-Plane',
    'setup.xplane_hint': () => 'المجلد الجذر لـ X-Plane نفسه — الذي يحتوي مباشرة على <code>Custom Scenery</code>، وليس ذلك المجلد. مثال: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'مجلد تثبيت المشاهد',
    'setup.scenery_hint': () => 'مجلد مخصص لحزم autoOr-simg الخاصة — عادةً <code>&lt;مسار X-Plane&gt;/Custom Scenery/z_autoortho</code>. يقوم autoOr-simg بتثبيت/إلغاء تثبيت كل شيء داخل هذا المجلد، لذا اجعله مجلدًا خاصًا به بدلاً من إعادة استخدام مجلد يحتوي على محتوى آخر.',
    'setup.cache_label': () => 'مجلد ذاكرة التخزين المؤقت للصور',
    'setup.cache_hint': () => 'ذاكرة تخزين مؤقت للبلاطات المفكوكة الترميز/المضغوطة، تُقرأ مع كل طلب نسيج — اختر قرصًا محليًا سريعًا (SSD) مع مساحة للنمو حتى حد الحجم أدناه. الافتراضي: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'مجلد التنزيلات المؤقتة',
    'setup.download_hint': () => 'مساحة مؤقتة لتنزيلات المشاهد الجارية — تحتاج تقريبًا ضعف حجم التنزيل كمساحة حرة مؤقتًا (يُتحقق منها قبل بدء كل تثبيت). الافتراضي: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'استعراض…',
    'setup.detect_xplane': () => 'البحث عن X-Plane…',
    'setup.detect_xplane_none': () => 'لم يتم العثور على تثبيت X-Plane تلقائيًا — استخدم استعراض… بدلاً من ذلك.',
    'setup.detect_xplane_found': (p) => `تم العثور عليه: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `تم العثور على ${p.n} من تثبيتات X-Plane — اختر واحدًا.`,
    'setup.detect_xplane_error': (p) => `تعذر البحث عن X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'حجم ذاكرة التخزين المؤقت (جيجابايت)',
    'setup.clean_cache': () => 'تنظيف ذاكرة التخزين المؤقت',
    'setup.auto_open_label': () => 'فتح المتصفح تلقائيًا عند بدء التشغيل',
    'setup.save': () => 'حفظ',
    'setup.save_tooltip': () => 'يحفظ جميع الإعدادات',

    'zoom.heading': () => 'التكبير والجودة',
    'zoom.maptype_label': () => 'تجاوز نوع الخريطة',
    'zoom.maptype_default': () => '(افتراضي المزوّد)',
    'zoom.minzoom_label': () => 'الحد الأدنى للتكبير',
    'zoom.minzoom_hint': () => '(حد أدنى، وليس حدًا أقصى — لا يرفع الجودة أبدًا فوق ما يقدمه المزوّد)',
    'zoom.maxzoom_label': () => 'الحد الأقصى للتكبير',
    'zoom.maxzoom_hint': () => '(حد أقصى — يحدّ من أوضح صورة يتم جلبها على الإطلاق، لتوفير مساحة القرص وعرض النطاق. المناطق الكثيفة — المدن والمطارات — تحتاج بطبيعتها إلى تكبير أعلى من الريف المفتوح، لذا فإن الحد المنخفض يؤثر عليها بشكل أكبر بكثير: تحديد 3 مستويات أقل مما تحتاجه منطقة ما يعني 8 أضعاف عدد بكسلات أقل هناك، وليس مجرد ضبابية خفيفة. يعد 18 حدًا أدنى عمليًا لتبقى المطارات/المدن واضحة؛ "بلا حد" يحافظ على السلوك الحالي.)',
    'zoom.nocap': () => 'بلا حد',
    'zoom.maxzoom_warning': () => '⚠ أقل من 18، ستبدو المناطق الكثيفة (المدن، المطارات) مربعة بشكل ملحوظ — هذا يؤثر عليها أكثر بكثير من الريف المفتوح بنفس الإعداد.',
    'zoom.maxwait_label': () => 'أقصى انتظار (ثوانٍ لكل جلب جزء)',
    'zoom.maxbuilds_label': () => 'أقصى عدد من إنشاءات البلاطات المتزامنة',
    'zoom.maxbuilds_hint': () => '(ميزانية ذاكرة، وليست شبكة — يسري عند إعادة التشغيل التالية)',
    'zoom.fetchthreads_label': () => 'خيوط جلب الأجزاء',
    'zoom.fetchthreads_hint': () => '(مجمع مشترك واحد تتنافس من خلاله عمليات جلب أجزاء جميع البلاطات بإنصاف، على مستوى العملية بأكملها؛ يسري عند إعادة التشغيل التالية)',
    'zoom.maxopentiles_label': () => 'أقصى عدد للبلاطات المفتوحة',
    'zoom.maxopentiles_hint': () => '(ميزانية ذاكرة — البلاطات التي تُفتح بعد هذا الحد تُقدَّم كبديل رخيص بدلاً من إنشاء كامل)',
    'zoom.perfmode_label': () => 'وضع الأداء',
    'zoom.perfmode_hint': () => '(متى يبدأ التضحية بالوضوح من أجل السرعة عندما تريد بلاطات كثيرة التفاصيل الكاملة في آن واحد — مثل مدينة كثيفة أو منظر واسع؛ يسري فورًا)',
    'zoom.perfmode_quality': () => 'الجودة',
    'zoom.perfmode_balanced': () => 'متوازن',
    'zoom.perfmode_speed': () => 'السرعة',
    'zoom.coarsefallback_label': () => 'تعطيل البديل الضبابي للبلاطات البطيئة/المفقودة',
    'zoom.coarsefallback_hint': () => '(عند التعطيل، يُعرض الجزء البطيء أو الفاشل كبديل ضبابي من مستوى تكبير أقل دقة مخزّن مؤقتًا بدلاً من لطخة لون مسطح — يبدو كتمويه ناعم فوق اليابسة، وقد يبدو باهتًا/قذرًا فوق الماء؛ يسري فورًا)',

    'stats.heading': () => 'الإحصائيات',
    'stats.cached_tiles': () => 'البلاطات المخزّنة مؤقتًا',
    'stats.open_tiles': () => 'البلاطات المفتوحة',
    'stats.process_memory': () => 'ذاكرة العملية',
    'stats.active_mounts': () => 'التثبيتات النشطة',

    'scenery.heading': () => 'المشاهد',
    'scenery.refresh': () => 'تحديث',
    'scenery.install': () => 'تثبيت',
    'scenery.update': () => 'تحديث',
    'scenery.working': () => 'جارٍ العمل…',
    'scenery.installed': (p) => `تم التثبيت ${p.ver}`,
    'scenery.update_available': (p) => `يتوفر تحديث: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} ميجابايت`,
    'scenery.error': (p) => `خطأ: ${p.msg}`,
    'scenery.package_n_of': (p) => `الحزمة ${p.index}/${p.total}`,
    'scenery.verify': () => 'تحقق',
    'scenery.mount': () => 'ربط',
    'scenery.unmount': () => 'فك الربط',
    'scenery.mounted': () => 'مرتبط',
    'scenery.unmounted': () => 'غير مرتبط',
    'scenery.verifying': () => 'جارٍ التحقق…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,

    'log.heading': () => 'السجل',

    'confirm.yes': () => 'نعم',
    'confirm.cancel': () => 'إلغاء',
    'confirm.stop_text': () => 'إيقاف جميع التثبيتات؟ سيفقد X-Plane نسيجه على الفور.',
    'confirm.shutdown_text': () => 'إغلاق autoOr-simg تمامًا؟ هذا سيوقف التثبيتات وينهي البرنامج.',
    'confirm.restart_text': () => 'إعادة تشغيل autoOr-simg الآن؟ هذا سيوقف التثبيتات ثم يبدأ نسخة جديدة فورًا.',

    'dirbrowser.up': () => '↑ أعلى',
    'dirbrowser.select': () => 'اختيار هذا المجلد',
    'dirbrowser.cancel': () => 'إلغاء',

    'theme.light': () => 'فاتح',
    'theme.dark': () => 'داكن',
    'attribution.text': () => 'autoOr-simg هو تنفيذ غير رسمي بلغة Go لـ <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho الخاص بـ kubilus1</a> — شكرنا للمشروع الأصلي ومؤلفه.',

    'settings.tooltip': () => 'الإعدادات',
    'settings.heading': () => 'الإعدادات',
    'settings.close': () => 'إغلاق',
    'settings.language_label': () => 'اللغة',
    'settings.theme_label': () => 'السمة',
    'settings.demo_mode_label': () => 'وضع العرض التوضيحي',
    'settings.demo_mode_hint': () => 'يخفي اسم المستخدم الخاص بك في حقول مسارات X-Plane/المشاهد/ذاكرة التخزين المؤقت/التنزيلات وفي كتلة السجل — لقطات الشاشة وتسجيلاتها. يغيّر فقط ما يُعرض، وليس القيم الفعلية المحفوظة. يؤثر فقط على علامة تبويب المتصفح هذه، ولا يُحفظ في الإعدادات.',
    'settings.demo_mode_enable': () => 'تفعيل وضع العرض التوضيحي',
    'settings.background_label': () => 'الخلفية',
    'settings.background_hint': () => 'اختر صورة لعرضها بخفوت خلف الصفحة — تبقى اللوحات صلبة، لذا يظل كل شيء قابلاً للقراءة.',
    'settings.choose_background': () => 'اختيار صورة…',
    'settings.remove_background': () => 'إزالة الخلفية',
    'settings.opacity_label': () => 'الشفافية',
    'settings.background_too_large': () => 'تعذر حفظ هذه الصورة (كبيرة جدًا لتخزين المتصفح) — جرّب صورة أصغر.',
    'settings.mascot_label': () => 'ناتاليا',
    'settings.mascot_hint': () => 'سيدة صغيرة في الزاوية تراقب مؤشرك. اضغط عليها للحصول على نصيحة.',
    'settings.mascot_outfit': () => 'الزي',
    'mascot.outfit_jetfighter': () => 'مقاتلة',
    'mascot.outfit_acrobatic': () => 'استعراضية',
    'mascot.outfit_airliner': () => 'طائرة ركاب',
    'mascot.outfit_agricultural': () => 'طائرة زراعية',
  },

  zh: {
    'status.connecting': () => '连接中…',
    'status.connected': () => '已连接',
    'status.disconnected': () => '连接已断开 — 正在重试…',
    'status.stopped': () => '已停止',
    'status.running': (p) => `运行中（${p.n} 个挂载点）`,
    'status.saved': () => '已保存',
    'status.error': (p) => `错误：${p.msg}`,
    'status.cache_cleaned': (p) => `缓存已清理：${p.n} 个文件，释放 ${p.mb} MB`,

    'btn.run': () => '运行',
    'btn.stop': () => '停止',
    'btn.shutdown': () => '关闭',
    'btn.restart': () => '重启',

    'setup.heading': () => '设置',
    'setup.xplane_label': () => 'X-Plane 安装目录',
    'setup.xplane_hint': () => 'X-Plane 的根目录本身 — 即直接包含 <code>Custom Scenery</code> 的那个目录，而不是该文件夹本身。示例：<code>.../Steam/steamapps/common/X-Plane 12</code>。',
    'setup.scenery_label': () => '场景安装目录',
    'setup.scenery_hint': () => '专用于 autoOr-simg 自身包的文件夹 — 通常是 <code>&lt;X-Plane 路径&gt;/Custom Scenery/z_autoortho</code>。autoOr-simg 会在此文件夹下安装/卸载所有内容，因此请为其单独指定一个文件夹，而不要与其他内容共用。',
    'setup.cache_label': () => '图像缓存目录',
    'setup.cache_hint': () => '已解码/压缩的瓦片缓存，每次纹理请求都会读取 — 选择一个有增长空间（直到下方缓存大小上限）的快速本地磁盘（SSD）。默认：<code>~/.autoortho-data/cache</code>。',
    'setup.download_label': () => '临时下载目录',
    'setup.download_hint': () => '正在进行的场景下载的临时空间 — 大约需要下载大小两倍的临时可用空间（每次安装开始前都会检查）。默认：<code>~/.autoortho-data/downloads</code>。',
    'setup.browse': () => '浏览…',
    'setup.detect_xplane': () => '查找 X-Plane…',
    'setup.detect_xplane_none': () => '未能自动找到 X-Plane 安装 — 请改用"浏览…"。',
    'setup.detect_xplane_found': (p) => `已找到：${p.path}`,
    'setup.detect_xplane_multiple': (p) => `找到 ${p.n} 个 X-Plane 安装 — 请选择一个。`,
    'setup.detect_xplane_error': (p) => `无法搜索 X-Plane：${p.error}`,
    'setup.cache_size_label': () => '缓存大小（GB）',
    'setup.clean_cache': () => '清理缓存',
    'setup.auto_open_label': () => '启动时自动打开浏览器',
    'setup.save': () => '保存',
    'setup.save_tooltip': () => '保存所有配置',

    'zoom.heading': () => '缩放与质量',
    'zoom.maptype_label': () => '覆盖地图类型',
    'zoom.maptype_default': () => '（提供方默认）',
    'zoom.minzoom_label': () => '最小缩放级别',
    'zoom.minzoom_hint': () => '（下限，而非上限 — 永远不会将质量提升到提供方所能提供的水平之上）',
    'zoom.maxzoom_label': () => '最大缩放级别',
    'zoom.maxzoom_hint': () => '（上限 — 限制获取的最高清晰度，以节省磁盘空间和带宽。密集区域（城市、机场）本身就比开阔乡村需要更高的缩放级别，所以较低的上限对它们影响更大：将上限设为比某区域实际需要的低 3 级，意味着那里的像素会减少 8 倍，而不只是轻微模糊。18 是机场/城市仍能保持清晰的一个实用下限；"无上限"保持当前行为。）',
    'zoom.nocap': () => '无上限',
    'zoom.maxzoom_warning': () => '⚠ 低于 18 时，密集区域（城市、机场）会明显呈现方块状 — 在相同设置下，这对它们的影响远大于开阔乡村。',
    'zoom.maxwait_label': () => '最大等待时间（每个区块获取的秒数）',
    'zoom.maxbuilds_label': () => '最大并发瓦片构建数',
    'zoom.maxbuilds_hint': () => '（内存预算，而非网络 — 下次重启后生效）',
    'zoom.fetchthreads_label': () => '区块获取线程数',
    'zoom.fetchthreads_hint': () => '（所有瓦片的区块获取请求公平共享的一个线程池，面向整个进程；下次重启后生效）',
    'zoom.maxopentiles_label': () => '最大打开瓦片数',
    'zoom.maxopentiles_hint': () => '（内存预算 — 超过此限制打开的瓦片将显示为廉价占位符，而不是完整构建）',
    'zoom.perfmode_label': () => '性能模式',
    'zoom.perfmode_hint': () => '（当许多瓦片同时需要完整细节时 — 例如密集城市或宽视野 — 多早开始牺牲清晰度以换取速度；立即生效）',
    'zoom.perfmode_quality': () => '质量优先',
    'zoom.perfmode_balanced': () => '均衡',
    'zoom.perfmode_speed': () => '速度优先',
    'zoom.coarsefallback_label': () => '禁用慢速/缺失瓦片的模糊替代效果',
    'zoom.coarsefallback_hint': () => '（关闭后，缓慢或失败的区块将显示为纯色色块，而不是来自较粗缩放级别缓存的模糊替代图像 — 该替代效果在陆地上看起来像柔焦，但在水面上可能显得发灰/脏；立即生效）',

    'stats.heading': () => '统计信息',
    'stats.cached_tiles': () => '已缓存瓦片',
    'stats.open_tiles': () => '已打开瓦片',
    'stats.process_memory': () => '进程内存',
    'stats.active_mounts': () => '活动挂载点',

    'scenery.heading': () => '场景',
    'scenery.refresh': () => '刷新',
    'scenery.install': () => '安装',
    'scenery.update': () => '更新',
    'scenery.working': () => '处理中…',
    'scenery.installed': (p) => `已安装 ${p.ver}`,
    'scenery.update_available': (p) => `有可用更新：${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `错误：${p.msg}`,
    'scenery.package_n_of': (p) => `第 ${p.index}/${p.total} 个包`,
    'scenery.verify': () => '校验',
    'scenery.mount': () => '挂载',
    'scenery.unmount': () => '卸载',
    'scenery.mounted': () => '已挂载',
    'scenery.unmounted': () => '未挂载',
    'scenery.verifying': () => '校验中…',
    'scenery.verify_ok': (p) => `✓ ${p.name}：${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}：${p.message}`,

    'log.heading': () => '日志',

    'confirm.yes': () => '是',
    'confirm.cancel': () => '取消',
    'confirm.stop_text': () => '停止所有挂载点？X-Plane 将立即失去其纹理。',
    'confirm.shutdown_text': () => '完全关闭 autoOr-simg？这将停止所有挂载点并退出程序。',
    'confirm.restart_text': () => '立即重启 autoOr-simg？这将停止所有挂载点，然后立即启动一个全新的实例。',

    'dirbrowser.up': () => '↑ 上一级',
    'dirbrowser.select': () => '选择此文件夹',
    'dirbrowser.cancel': () => '取消',

    'theme.light': () => '浅色',
    'theme.dark': () => '深色',
    'attribution.text': () => 'autoOr-simg 是 <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">kubilus1 的 AutoOrtho</a> 的非官方 Go 语言实现 — 感谢原项目及其作者。',

    'settings.tooltip': () => '设置',
    'settings.heading': () => '设置',
    'settings.close': () => '关闭',
    'settings.language_label': () => '语言',
    'settings.theme_label': () => '主题',
    'settings.demo_mode_label': () => '演示模式',
    'settings.demo_mode_hint': () => '在 X-Plane/场景/缓存/下载路径字段以及日志区域中隐藏你的用户名 — 便于截图和录屏。只改变显示内容，不改变实际保存的值。仅影响当前浏览器标签页，不会保存到配置中。',
    'settings.demo_mode_enable': () => '启用演示模式',
    'settings.background_label': () => '背景',
    'settings.background_hint': () => '选择一张图片，淡淡地显示在页面背后 — 各面板仍保持不透明，因此一切内容都保持可读。',
    'settings.choose_background': () => '选择图片…',
    'settings.remove_background': () => '移除背景',
    'settings.opacity_label': () => '不透明度',
    'settings.background_too_large': () => '无法保存该图片（对浏览器存储来说太大） — 请尝试更小的图片。',
    'settings.mascot_label': () => '娜塔莉亚',
    'settings.mascot_hint': () => '角落里的一个小姑娘，会盯着你的光标看。点击她可以获得小提示。',
    'settings.mascot_outfit': () => '装扮',
    'mascot.outfit_jetfighter': () => '战斗机',
    'mascot.outfit_acrobatic': () => '特技机',
    'mascot.outfit_airliner': () => '客机',
    'mascot.outfit_agricultural': () => '农业机',
  },

  fr: {
    'status.connecting': () => 'connexion…',
    'status.connected': () => 'connecté',
    'status.disconnected': () => 'déconnecté — nouvelle tentative…',
    'status.stopped': () => 'arrêté',
    'status.running': (p) => `en cours d’exécution (${p.n} ${p.n === 1 ? 'montage' : 'montages'})`,
    'status.saved': () => 'Enregistré',
    'status.error': (p) => `Erreur : ${p.msg}`,
    'status.cache_cleaned': (p) => `Cache nettoyé : ${p.n} fichier(s), ${p.mb} Mo libérés`,

    'btn.run': () => 'Démarrer',
    'btn.stop': () => 'Arrêter',
    'btn.shutdown': () => 'Éteindre',
    'btn.restart': () => 'Redémarrer',

    'setup.heading': () => 'Configuration',
    'setup.xplane_label': () => 'Dossier d’installation de X-Plane',
    'setup.xplane_hint': () => 'Le dossier racine de X-Plane lui-même — celui qui contient directement <code>Custom Scenery</code>, pas ce dossier-là. Exemple : <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Dossier d’installation des scènes',
    'setup.scenery_hint': () => 'Un dossier dédié aux propres paquets d’autoOr-simg — généralement <code>&lt;chemin X-Plane&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg installe/désinstalle tout dans ce dossier, donnez-lui donc le sien plutôt que de réutiliser un dossier contenant autre chose.',
    'setup.cache_label': () => 'Dossier de cache d’images',
    'setup.cache_hint': () => 'Cache des tuiles décodées/compressées, lu à chaque requête de texture — choisissez un disque local rapide (SSD) avec de la place pour grandir jusqu’à la limite de taille ci-dessous. Par défaut : <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Dossier de téléchargements temporaires',
    'setup.download_hint': () => 'Espace temporaire pour les téléchargements de scènes en cours — nécessite environ 2x la taille du téléchargement en espace libre temporaire (vérifié avant chaque installation). Par défaut : <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Parcourir…',
    'setup.detect_xplane': () => 'Rechercher X-Plane…',
    'setup.detect_xplane_none': () => 'Aucune installation de X-Plane trouvée automatiquement — utilisez Parcourir… à la place.',
    'setup.detect_xplane_found': (p) => `Trouvé : ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `${p.n} installations de X-Plane trouvées — choisissez-en une.`,
    'setup.detect_xplane_error': (p) => `Impossible de rechercher X-Plane : ${p.error}`,
    'setup.cache_size_label': () => 'Taille du cache (Go)',
    'setup.clean_cache': () => 'Nettoyer le cache',
    'setup.auto_open_label': () => 'Ouvrir le navigateur automatiquement au démarrage',
    'setup.save': () => 'Enregistrer',
    'setup.save_tooltip': () => 'Enregistre tous les paramètres',

    'zoom.heading': () => 'Zoom et qualité',
    'zoom.maptype_label': () => 'Forcer le type de carte',
    'zoom.maptype_default': () => '(par défaut du fournisseur)',
    'zoom.minzoom_label': () => 'Zoom minimum',
    'zoom.minzoom_hint': () => '(un plancher, pas un plafond — n’augmente jamais la qualité au-delà de ce que le fournisseur propose)',
    'zoom.maxzoom_label': () => 'Zoom maximum',
    'zoom.maxzoom_hint': () => '(un plafond — limite la netteté maximale jamais téléchargée, pour économiser l’espace disque et la bande passante. Les zones denses — villes, aéroports — demandent naturellement plus de zoom que la campagne ouverte, donc une limite basse les affecte bien plus fortement : limiter 3 niveaux en dessous de ce qu’une zone demande signifie 8 fois moins de pixels là-bas, pas un simple flou léger. 18 est un plancher pratique pour que les aéroports/villes restent nets ; "Pas de limite" laisse cette limite désactivée.)',
    'zoom.nocap': () => 'Pas de limite',
    'zoom.maxzoom_warning': () => '⚠ En dessous de 18, les zones denses (villes, aéroports) paraîtront nettement pixelisées — cela les affecte bien plus que la campagne ouverte avec le même réglage.',
    'zoom.maxwait_label': () => 'Attente maximale (secondes par récupération de fragment)',
    'zoom.maxbuilds_label': () => 'Constructions de tuiles simultanées max',
    'zoom.maxbuilds_hint': () => '(budget mémoire, pas réseau — prend effet au prochain redémarrage)',
    'zoom.fetchthreads_label': () => 'Threads de récupération des fragments',
    'zoom.fetchthreads_hint': () => '(un seul pool partagé pour lequel les récupérations de fragments de toutes les tuiles se disputent équitablement, à l’échelle du processus ; prend effet au prochain redémarrage)',
    'zoom.maxopentiles_label': () => 'Tuiles ouvertes max',
    'zoom.maxopentiles_hint': () => '(budget mémoire — les tuiles ouvertes au-delà de cette limite reçoivent un substitut bon marché au lieu d’une construction complète)',
    'zoom.perfmode_label': () => 'Mode de performance',
    'zoom.perfmode_hint': () => '(à quel point sacrifier tôt la netteté pour la vitesse quand beaucoup de tuiles veulent le détail complet en même temps — ex. une ville dense ou une vue large ; prend effet immédiatement)',
    'zoom.perfmode_quality': () => 'Qualité',
    'zoom.perfmode_balanced': () => 'Équilibré',
    'zoom.perfmode_speed': () => 'Vitesse',
    'zoom.coarsefallback_label': () => 'Désactiver le substitut flou pour les tuiles lentes/manquantes',
    'zoom.coarsefallback_hint': () => '(si désactivé, un fragment lent ou en échec s’affiche comme une pastille de couleur unie au lieu d’un substitut flou provenant d’un niveau de zoom plus grossier en cache — ce substitut ressemble à un flou artistique sur terre, mais peut paraître sale/délavé sur l’eau ; prend effet immédiatement)',

    'stats.heading': () => 'Statistiques',
    'stats.cached_tiles': () => 'Tuiles en cache',
    'stats.open_tiles': () => 'Tuiles ouvertes',
    'stats.process_memory': () => 'Mémoire du processus',
    'stats.active_mounts': () => 'Montages actifs',

    'scenery.heading': () => 'Scènes',
    'scenery.refresh': () => 'Actualiser',
    'scenery.install': () => 'Installer',
    'scenery.update': () => 'Mettre à jour',
    'scenery.working': () => 'Traitement…',
    'scenery.installed': (p) => `installé ${p.ver}`,
    'scenery.update_available': (p) => `mise à jour disponible : ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} Mo`,
    'scenery.error': (p) => `erreur : ${p.msg}`,
    'scenery.package_n_of': (p) => `paquet ${p.index}/${p.total}`,
    'scenery.verify': () => 'Vérifier',
    'scenery.verifying': () => 'Vérification…',
    'scenery.verify_ok': (p) => `✓ ${p.name} : ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name} : ${p.message}`,
    'scenery.mount': () => 'Monter',
    'scenery.unmount': () => 'Démonter',
    'scenery.mounted': () => 'Monté',
    'scenery.unmounted': () => 'Non monté',

    'log.heading': () => 'Journal',

    'confirm.yes': () => 'Oui',
    'confirm.cancel': () => 'Annuler',
    'confirm.stop_text': () => 'Arrêter tous les montages ? X-Plane perdra ses textures immédiatement.',
    'confirm.shutdown_text': () => 'Éteindre complètement autoOr-simg ? Cela arrête les montages et quitte le programme.',
    'confirm.restart_text': () => 'Redémarrer autoOr-simg maintenant ? Cela arrête les montages puis démarre immédiatement une nouvelle instance.',

    'dirbrowser.up': () => '↑ Parent',
    'dirbrowser.select': () => 'Sélectionner ce dossier',
    'dirbrowser.cancel': () => 'Annuler',

    'theme.light': () => 'Clair',
    'theme.dark': () => 'Sombre',
    'attribution.text': () => 'autoOr-simg est une réimplémentation non officielle en Go de <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho de kubilus1</a> — merci au projet original et à son auteur.',

    'settings.tooltip': () => 'Paramètres',
    'settings.heading': () => 'Paramètres',
    'settings.close': () => 'Fermer',
    'settings.language_label': () => 'Langue',
    'settings.theme_label': () => 'Thème',
    'settings.demo_mode_label': () => 'Mode démonstration',
    'settings.demo_mode_hint': () => 'Masque votre nom d’utilisateur dans les champs de chemin X-Plane/scènes/cache/téléchargements et dans le bloc Journal — pour les captures et enregistrements d’écran. Ne change que ce qui est affiché, pas les valeurs réellement enregistrées. N’affecte que cet onglet du navigateur, pas la configuration enregistrée.',
    'settings.demo_mode_enable': () => 'Activer le mode démonstration',
    'settings.background_label': () => 'Arrière-plan',
    'settings.background_hint': () => 'Choisissez une photo à afficher discrètement derrière la page — les panneaux restent opaques, donc tout reste lisible.',
    'settings.choose_background': () => 'Choisir une image…',
    'settings.remove_background': () => 'Retirer l’arrière-plan',
    'settings.opacity_label': () => 'Opacité',
    'settings.background_too_large': () => 'Impossible d’enregistrer cette image (trop grande pour le stockage du navigateur) — essayez une photo plus petite.',
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'Une petite demoiselle dans le coin qui suit votre curseur du regard. Cliquez sur elle pour une astuce.',
    'settings.mascot_outfit': () => 'Tenue',
    'mascot.outfit_jetfighter': () => 'Chasseur',
    'mascot.outfit_acrobatic': () => 'Voltige',
    'mascot.outfit_airliner': () => 'Avion de ligne',
    'mascot.outfit_agricultural': () => 'Agricole',
  },

  it: {
    'status.connecting': () => 'connessione…',
    'status.connected': () => 'connesso',
    'status.disconnected': () => 'disconnesso — nuovo tentativo…',
    'status.stopped': () => 'fermato',
    'status.running': (p) => `in esecuzione (${p.n} ${p.n === 1 ? 'montaggio' : 'montaggi'})`,
    'status.saved': () => 'Salvato',
    'status.error': (p) => `Errore: ${p.msg}`,
    'status.cache_cleaned': (p) => `Cache ripulita: ${p.n} file, ${p.mb} MB liberati`,

    'btn.run': () => 'Avvia',
    'btn.stop': () => 'Ferma',
    'btn.shutdown': () => 'Spegni',
    'btn.restart': () => 'Riavvia',

    'setup.heading': () => 'Impostazione',
    'setup.xplane_label': () => 'Cartella di installazione di X-Plane',
    'setup.xplane_hint': () => 'La cartella radice di X-Plane stessa — quella che contiene direttamente <code>Custom Scenery</code>, non quella cartella. Esempio: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Cartella di installazione degli scenari',
    'setup.scenery_hint': () => 'Una cartella dedicata ai pacchetti propri di autoOr-simg — solitamente <code>&lt;percorso X-Plane&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg installa/disinstalla tutto all’interno di questa cartella, quindi assegnale una propria invece di riutilizzare una cartella con altro contenuto.',
    'setup.cache_label': () => 'Cartella cache immagini',
    'setup.cache_hint': () => 'Cache dei tasselli decodificati/compressi, letta a ogni richiesta di texture — scegli un disco locale veloce (SSD) con spazio per crescere fino al limite di dimensione sottostante. Predefinito: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Cartella download temporanei',
    'setup.download_hint': () => 'Spazio temporaneo per i download di scenari in corso — richiede circa il doppio della dimensione del download come spazio libero temporaneo (controllato prima di ogni installazione). Predefinito: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Sfoglia…',
    'setup.detect_xplane': () => 'Trova X-Plane…',
    'setup.detect_xplane_none': () => 'Nessuna installazione di X-Plane trovata automaticamente — usa Sfoglia… invece.',
    'setup.detect_xplane_found': (p) => `Trovato: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `Trovate ${p.n} installazioni di X-Plane — scegline una.`,
    'setup.detect_xplane_error': (p) => `Impossibile cercare X-Plane: ${p.error}`,
    'setup.cache_size_label': () => 'Dimensione cache (GB)',
    'setup.clean_cache': () => 'Pulisci cache',
    'setup.auto_open_label': () => 'Apri il browser automaticamente all’avvio',
    'setup.save': () => 'Salva',
    'setup.save_tooltip': () => 'Salva tutte le configurazioni',

    'zoom.heading': () => 'Zoom e qualità',
    'zoom.maptype_label': () => 'Sovrascrivi tipo di mappa',
    'zoom.maptype_default': () => '(predefinito del fornitore)',
    'zoom.minzoom_label': () => 'Zoom minimo',
    'zoom.minzoom_hint': () => '(un minimo, non un massimo — non aumenta mai la qualità oltre quanto offerto dal fornitore)',
    'zoom.maxzoom_label': () => 'Zoom massimo',
    'zoom.maxzoom_hint': () => '(un massimo — limita la nitidezza massima mai scaricata, per risparmiare spazio su disco e banda. Le aree dense — città, aeroporti — richiedono naturalmente uno zoom maggiore rispetto alla campagna aperta, quindi un limite basso le colpisce molto di più: limitare 3 livelli sotto quanto un’area richiede significa 8 volte meno pixel lì, non una semplice sfocatura. 18 è un minimo pratico perché aeroporti/città restino nitidi; "Nessun limite" lascia questo limite disattivato.)',
    'zoom.nocap': () => 'Nessun limite',
    'zoom.maxzoom_warning': () => '⚠ Sotto 18, le aree dense (città, aeroporti) appariranno visibilmente a blocchi — questo le colpisce molto più della campagna aperta con la stessa impostazione.',
    'zoom.maxwait_label': () => 'Attesa massima (secondi per recupero frammento)',
    'zoom.maxbuilds_label': () => 'Costruzioni di tasselli simultanee massime',
    'zoom.maxbuilds_hint': () => '(budget di memoria, non di rete — ha effetto al prossimo riavvio)',
    'zoom.fetchthreads_label': () => 'Thread di recupero frammenti',
    'zoom.fetchthreads_hint': () => '(un unico pool condiviso per cui i recuperi di frammenti di tutti i tasselli competono equamente, a livello di processo; ha effetto al prossimo riavvio)',
    'zoom.maxopentiles_label': () => 'Tasselli aperti massimi',
    'zoom.maxopentiles_hint': () => '(budget di memoria — i tasselli aperti oltre questo limite ricevono un sostituto economico invece di una costruzione completa)',
    'zoom.perfmode_label': () => 'Modalità prestazioni',
    'zoom.perfmode_hint': () => '(quanto presto sacrificare la nitidezza per la velocità quando molti tasselli vogliono il dettaglio completo contemporaneamente — es. una città densa o una vista ampia; ha effetto immediatamente)',
    'zoom.perfmode_quality': () => 'Qualità',
    'zoom.perfmode_balanced': () => 'Bilanciato',
    'zoom.perfmode_speed': () => 'Velocità',
    'zoom.coarsefallback_label': () => 'Disattiva il sostituto sfocato per i tasselli lenti/mancanti',
    'zoom.coarsefallback_hint': () => '(se disattivato, un frammento lento o fallito viene mostrato come una macchia di colore pieno invece di un sostituto sfocato da un livello di zoom più grezzo in cache — questo sostituto sembra una sfocatura morbida sulla terra, ma può apparire sporco/sbiadito sull’acqua; ha effetto immediatamente)',

    'stats.heading': () => 'Statistiche',
    'stats.cached_tiles': () => 'Tasselli in cache',
    'stats.open_tiles': () => 'Tasselli aperti',
    'stats.process_memory': () => 'Memoria del processo',
    'stats.active_mounts': () => 'Montaggi attivi',

    'scenery.heading': () => 'Scenari',
    'scenery.refresh': () => 'Aggiorna',
    'scenery.install': () => 'Installa',
    'scenery.update': () => 'Aggiorna',
    'scenery.working': () => 'Elaborazione…',
    'scenery.installed': (p) => `installato ${p.ver}`,
    'scenery.update_available': (p) => `aggiornamento disponibile: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `errore: ${p.msg}`,
    'scenery.package_n_of': (p) => `pacchetto ${p.index}/${p.total}`,
    'scenery.verify': () => 'Verifica',
    'scenery.verifying': () => 'Verifica in corso…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,
    'scenery.mount': () => 'Monta',
    'scenery.unmount': () => 'Smonta',
    'scenery.mounted': () => 'Montato',
    'scenery.unmounted': () => 'Non montato',

    'log.heading': () => 'Registro',

    'confirm.yes': () => 'Sì',
    'confirm.cancel': () => 'Annulla',
    'confirm.stop_text': () => 'Fermare tutti i montaggi? X-Plane perderà immediatamente le sue texture.',
    'confirm.shutdown_text': () => 'Spegnere completamente autoOr-simg? Questo ferma i montaggi ed esce dal programma.',
    'confirm.restart_text': () => 'Riavviare autoOr-simg ora? Questo ferma i montaggi e poi avvia subito una nuova istanza.',

    'dirbrowser.up': () => '↑ Su',
    'dirbrowser.select': () => 'Seleziona questa cartella',
    'dirbrowser.cancel': () => 'Annulla',

    'theme.light': () => 'Chiaro',
    'theme.dark': () => 'Scuro',
    'attribution.text': () => 'autoOr-simg è una reimplementazione non ufficiale in Go di <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">AutoOrtho di kubilus1</a> — i nostri ringraziamenti al progetto originale e al suo autore.',

    'settings.tooltip': () => 'Impostazioni',
    'settings.heading': () => 'Impostazioni',
    'settings.close': () => 'Chiudi',
    'settings.language_label': () => 'Lingua',
    'settings.theme_label': () => 'Tema',
    'settings.demo_mode_label': () => 'Modalità dimostrazione',
    'settings.demo_mode_hint': () => 'Nasconde il tuo nome utente nei campi dei percorsi X-Plane/scenari/cache/download e nel blocco Registro — per screenshot e registrazioni schermo. Cambia solo ciò che viene mostrato, non i valori realmente salvati. Influisce solo su questa scheda del browser, non viene salvato nella configurazione.',
    'settings.demo_mode_enable': () => 'Attiva modalità dimostrazione',
    'settings.background_label': () => 'Sfondo',
    'settings.background_hint': () => 'Scegli una foto da mostrare tenuemente dietro la pagina — i pannelli restano solidi, quindi tutto resta leggibile.',
    'settings.choose_background': () => 'Scegli immagine…',
    'settings.remove_background': () => 'Rimuovi sfondo',
    'settings.opacity_label': () => 'Opacità',
    'settings.background_too_large': () => 'Impossibile salvare questa immagine (troppo grande per l’archiviazione del browser) — prova una foto più piccola.',
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'Una piccola signorina nell’angolo che segue il tuo cursore con lo sguardo. Cliccaci sopra per un consiglio.',
    'settings.mascot_outfit': () => 'Abito',
    'mascot.outfit_jetfighter': () => 'Caccia',
    'mascot.outfit_acrobatic': () => 'Acrobatico',
    'mascot.outfit_airliner': () => 'Aereo di linea',
    'mascot.outfit_agricultural': () => 'Agricolo',
  },

  de: {
    'status.connecting': () => 'Verbindung wird hergestellt…',
    'status.connected': () => 'verbunden',
    'status.disconnected': () => 'Verbindung getrennt — erneuter Versuch…',
    'status.stopped': () => 'gestoppt',
    'status.running': (p) => `in Betrieb (${p.n} ${p.n === 1 ? 'Einhängung' : 'Einhängungen'})`,
    'status.saved': () => 'Gespeichert',
    'status.error': (p) => `Fehler: ${p.msg}`,
    'status.cache_cleaned': (p) => `Cache geleert: ${p.n} Datei(en), ${p.mb} MB freigegeben`,

    'btn.run': () => 'Starten',
    'btn.stop': () => 'Stoppen',
    'btn.shutdown': () => 'Beenden',
    'btn.restart': () => 'Neustart',

    'setup.heading': () => 'Einrichtung',
    'setup.xplane_label': () => 'X-Plane-Installationsordner',
    'setup.xplane_hint': () => 'Der X-Plane-Stammordner selbst — der, der direkt <code>Custom Scenery</code> enthält, nicht dieser Ordner. Beispiel: <code>.../Steam/steamapps/common/X-Plane 12</code>.',
    'setup.scenery_label': () => 'Szenerie-Installationsordner',
    'setup.scenery_hint': () => 'Ein eigener Ordner für autoOr-simgs eigene Pakete — meist <code>&lt;X-Plane-Pfad&gt;/Custom Scenery/z_autoortho</code>. autoOr-simg installiert/deinstalliert alles in diesem Ordner, also gib ihm einen eigenen statt einen Ordner mit anderem Inhalt wiederzuverwenden.',
    'setup.cache_label': () => 'Bildcache-Ordner',
    'setup.cache_hint': () => 'Cache für dekodierte/komprimierte Kacheln, bei jeder Textur-Anfrage gelesen — wähle eine schnelle lokale Festplatte (SSD) mit Platz zum Wachsen bis zur unten angegebenen Größengrenze. Standard: <code>~/.autoortho-data/cache</code>.',
    'setup.download_label': () => 'Temporärer Download-Ordner',
    'setup.download_hint': () => 'Temporärer Speicherplatz für laufende Szenerie-Downloads — benötigt etwa die doppelte Downloadgröße als temporär freien Speicher (vor jeder Installation geprüft). Standard: <code>~/.autoortho-data/downloads</code>.',
    'setup.browse': () => 'Durchsuchen…',
    'setup.detect_xplane': () => 'X-Plane suchen…',
    'setup.detect_xplane_none': () => 'Keine X-Plane-Installation automatisch gefunden — verwende stattdessen Durchsuchen….',
    'setup.detect_xplane_found': (p) => `Gefunden: ${p.path}`,
    'setup.detect_xplane_multiple': (p) => `${p.n} X-Plane-Installationen gefunden — wähle eine aus.`,
    'setup.detect_xplane_error': (p) => `X-Plane konnte nicht gesucht werden: ${p.error}`,
    'setup.cache_size_label': () => 'Cache-Größe (GB)',
    'setup.clean_cache': () => 'Cache leeren',
    'setup.auto_open_label': () => 'Browser beim Start automatisch öffnen',
    'setup.save': () => 'Speichern',
    'setup.save_tooltip': () => 'Speichert alle Einstellungen',

    'zoom.heading': () => 'Zoom & Qualität',
    'zoom.maptype_label': () => 'Kartentyp überschreiben',
    'zoom.maptype_default': () => '(Anbieter-Standard)',
    'zoom.minzoom_label': () => 'Mindest-Zoom',
    'zoom.minzoom_hint': () => '(eine Untergrenze, keine Obergrenze — erhöht die Qualität nie über das hinaus, was der Anbieter liefert)',
    'zoom.maxzoom_label': () => 'Maximal-Zoom',
    'zoom.maxzoom_hint': () => '(eine Obergrenze — begrenzt die schärfsten jemals abgerufenen Bilder, um Speicherplatz und Bandbreite zu sparen. Dichte Gebiete — Städte, Flughäfen — benötigen von Natur aus mehr Zoom als offenes Land, daher trifft eine niedrige Grenze sie viel stärker: eine Begrenzung 3 Stufen unter dem, was ein Gebiet braucht, bedeutet dort 8-mal weniger Pixel, keine bloße leichte Unschärfe. 18 ist eine praktische Untergrenze, damit Flughäfen/Städte scharf bleiben; "Keine Begrenzung" lässt dieses Limit deaktiviert.)',
    'zoom.nocap': () => 'Keine Begrenzung',
    'zoom.maxzoom_warning': () => '⚠ Unter 18 wirken dichte Gebiete (Städte, Flughäfen) deutlich blockartig — das betrifft sie viel stärker als offenes Land bei derselben Einstellung.',
    'zoom.maxwait_label': () => 'Maximale Wartezeit (Sekunden pro Fragmentabruf)',
    'zoom.maxbuilds_label': () => 'Maximale gleichzeitige Kachelerstellungen',
    'zoom.maxbuilds_hint': () => '(Speicherbudget, kein Netzwerk — wirkt erst nach dem nächsten Neustart)',
    'zoom.fetchthreads_label': () => 'Threads für Fragmentabruf',
    'zoom.fetchthreads_hint': () => '(ein einziger gemeinsamer Pool, um den sich die Fragmentabrufe aller Kacheln prozessweit fair teilen; wirkt erst nach dem nächsten Neustart)',
    'zoom.maxopentiles_label': () => 'Maximal offene Kacheln',
    'zoom.maxopentiles_hint': () => '(Speicherbudget — Kacheln, die über dieses Limit hinaus geöffnet werden, erhalten einen billigen Platzhalter statt eines vollständigen Aufbaus)',
    'zoom.perfmode_label': () => 'Leistungsmodus',
    'zoom.perfmode_hint': () => '(wie früh Schärfe zugunsten von Geschwindigkeit geopfert wird, wenn viele Kacheln gleichzeitig volle Details wollen — z. B. eine dichte Stadt oder eine weite Aussicht; wirkt sofort)',
    'zoom.perfmode_quality': () => 'Qualität',
    'zoom.perfmode_balanced': () => 'Ausgewogen',
    'zoom.perfmode_speed': () => 'Geschwindigkeit',
    'zoom.coarsefallback_label': () => 'Unscharfen Ersatz für langsame/fehlende Kacheln deaktivieren',
    'zoom.coarsefallback_hint': () => '(wenn deaktiviert, wird ein langsames oder fehlgeschlagenes Fragment als einfarbiger Fleck statt als unscharfer Ersatz aus einer gröberen zwischengespeicherten Zoomstufe angezeigt — dieser Ersatz wirkt über Land wie ein sanfter Weichzeichner, kann über Wasser aber schmutzig/verwaschen aussehen; wirkt sofort)',

    'stats.heading': () => 'Statistiken',
    'stats.cached_tiles': () => 'Zwischengespeicherte Kacheln',
    'stats.open_tiles': () => 'Offene Kacheln',
    'stats.process_memory': () => 'Prozessspeicher',
    'stats.active_mounts': () => 'Aktive Einhängungen',

    'scenery.heading': () => 'Szenerie',
    'scenery.refresh': () => 'Aktualisieren',
    'scenery.install': () => 'Installieren',
    'scenery.update': () => 'Aktualisieren',
    'scenery.working': () => 'Verarbeitung…',
    'scenery.installed': (p) => `installiert ${p.ver}`,
    'scenery.update_available': (p) => `Update verfügbar: ${p.local} → ${p.latest}`,
    'scenery.size': (p) => `${p.size} MB`,
    'scenery.error': (p) => `Fehler: ${p.msg}`,
    'scenery.package_n_of': (p) => `Paket ${p.index}/${p.total}`,
    'scenery.verify': () => 'Prüfen',
    'scenery.verifying': () => 'Wird geprüft…',
    'scenery.verify_ok': (p) => `✓ ${p.name}: ${p.message}`,
    'scenery.verify_failed': (p) => `⚠ ${p.name}: ${p.message}`,
    'scenery.mount': () => 'Einhängen',
    'scenery.unmount': () => 'Aushängen',
    'scenery.mounted': () => 'Eingehängt',
    'scenery.unmounted': () => 'Nicht eingehängt',

    'log.heading': () => 'Protokoll',

    'confirm.yes': () => 'Ja',
    'confirm.cancel': () => 'Abbrechen',
    'confirm.stop_text': () => 'Alle Einhängungen stoppen? X-Plane verliert sofort seine Texturen.',
    'confirm.shutdown_text': () => 'autoOr-simg vollständig beenden? Dies stoppt die Einhängungen und beendet das Programm.',
    'confirm.restart_text': () => 'autoOr-simg jetzt neu starten? Dies stoppt die Einhängungen und startet sofort eine neue Instanz.',

    'dirbrowser.up': () => '↑ Nach oben',
    'dirbrowser.select': () => 'Diesen Ordner auswählen',
    'dirbrowser.cancel': () => 'Abbrechen',

    'theme.light': () => 'Hell',
    'theme.dark': () => 'Dunkel',
    'attribution.text': () => 'autoOr-simg ist eine inoffizielle Go-Neuimplementierung von <a href="https://github.com/kubilus1/autoortho" target="_blank" rel="noopener">kubilus1s AutoOrtho</a> — unser Dank gilt dem ursprünglichen Projekt und seinem Autor.',

    'settings.tooltip': () => 'Einstellungen',
    'settings.heading': () => 'Einstellungen',
    'settings.close': () => 'Schließen',
    'settings.language_label': () => 'Sprache',
    'settings.theme_label': () => 'Design',
    'settings.demo_mode_label': () => 'Vorführmodus',
    'settings.demo_mode_hint': () => 'Verbirgt deinen Benutzernamen in den Pfadfeldern für X-Plane/Szenerie/Cache/Downloads sowie im Protokollblock — für Screenshots und Bildschirmaufnahmen. Ändert nur die Anzeige, nicht die tatsächlich gespeicherten Werte. Betrifft nur diesen Browser-Tab, wird nicht in der Konfiguration gespeichert.',
    'settings.demo_mode_enable': () => 'Vorführmodus aktivieren',
    'settings.background_label': () => 'Hintergrund',
    'settings.background_hint': () => 'Wähle ein Foto, das dezent hinter der Seite angezeigt wird — die Panels bleiben deckend, sodass alles lesbar bleibt.',
    'settings.choose_background': () => 'Bild wählen…',
    'settings.remove_background': () => 'Hintergrund entfernen',
    'settings.opacity_label': () => 'Deckkraft',
    'settings.background_too_large': () => 'Dieses Bild konnte nicht gespeichert werden (zu groß für den Browser-Speicher) — versuche ein kleineres Foto.',
    'settings.mascot_label': () => 'Natalia',
    'settings.mascot_hint': () => 'Ein kleines Mädchen in der Ecke, das deinem Cursor mit den Augen folgt. Klick sie an für einen Tipp.',
    'settings.mascot_outfit': () => 'Outfit',
    'mascot.outfit_jetfighter': () => 'Jäger',
    'mascot.outfit_acrobatic': () => 'Kunstflieger',
    'mascot.outfit_airliner': () => 'Linienflugzeug',
    'mascot.outfit_agricultural': () => 'Agrarflieger',
  },
};

// ruMountWord picks the right plural form of "подключение" (connection,
// used here for "mount") — Russian needs three forms (1, 2-4, 5+ with the
// usual 11-14 exception), not the two forms English/Spanish get by with.
function ruPluralWord(n, one, few, many) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return few;
  return many;
}

function ruMountWord(n) {
  return ruPluralWord(n, 'подключение', 'подключения', 'подключений');
}

function ruInstallWord(n) {
  return ruPluralWord(n, 'установка', 'установки', 'установок');
}

const LANG_STORAGE_KEY = 'autoortho_lang';

function detectDefaultLang() {
  const nav = ((navigator.language || 'en').split('-')[0] || 'en').toLowerCase();
  return SUPPORTED_LANGS.includes(nav) ? nav : 'en';
}

function loadStoredLang() {
  try {
    const stored = localStorage.getItem(LANG_STORAGE_KEY);
    if (stored && SUPPORTED_LANGS.includes(stored)) return stored;
  } catch (e) {
    // localStorage unavailable (private browsing, blocked site data) —
    // fall through to browser-language detection for this load.
  }
  return detectDefaultLang();
}

let currentLang = loadStoredLang();

function t(key, params) {
  const dict = I18N[currentLang] || I18N.en;
  const fn = dict[key] || I18N.en[key];
  if (!fn) return key;
  return fn(params || {});
}

// setLanguage persists the choice and re-renders everything — both the
// static data-i18n/data-i18n-html chrome and (via the caller's onChange)
// any dynamically-built content (region rows, status text) that embeds
// translated strings.
function setLanguage(lang, onChange) {
  currentLang = SUPPORTED_LANGS.includes(lang) ? lang : 'en';
  try {
    localStorage.setItem(LANG_STORAGE_KEY, currentLang);
  } catch (e) {
    // Non-fatal — the choice just won't survive a reload this session.
  }
  applyStaticTranslations();
  if (typeof onChange === 'function') onChange();
}

// applyStaticTranslations walks every element carrying data-i18n (plain
// text content) or data-i18n-html (the four Setup-block hints, which
// embed <code> tags around literal paths that must survive translation
// unescaped) and fills in the current language's string.
function applyStaticTranslations() {
  document.documentElement.dir = RTL_LANGS.includes(currentLang) ? 'rtl' : 'ltr';
  document.querySelectorAll('[data-i18n]').forEach((el) => {
    el.textContent = t(el.dataset.i18n);
  });
  document.querySelectorAll('[data-i18n-html]').forEach((el) => {
    el.innerHTML = t(el.dataset.i18nHtml);
  });
  document.querySelectorAll('[data-i18n-title]').forEach((el) => {
    el.title = t(el.dataset.i18nTitle);
  });
  document.querySelectorAll('.lang-btn').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.lang === currentLang);
  });
  document.querySelectorAll('.theme-btn').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.theme === currentTheme);
  });
}

// --- Light/dark theme -------------------------------------------------
// Same detect-then-persist-the-explicit-choice pattern as the language
// switcher above: honor the OS/browser's prefers-color-scheme on first
// load, then remember whatever the user actually clicks.

const THEME_STORAGE_KEY = 'autoortho_theme';

function detectDefaultTheme() {
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches
    ? 'light'
    : 'dark';
}

function loadStoredTheme() {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark') return stored;
  } catch (e) {
    // localStorage unavailable — fall through to OS-preference detection.
  }
  return detectDefaultTheme();
}

let currentTheme = loadStoredTheme();

// applyTheme sets data-theme on <html> — style.css's light-mode variable
// overrides are scoped to :root[data-theme="light"], mirroring exactly how
// Claude Artifacts' own theme convention works, just hand-applied here
// instead of host-driven.
function applyTheme() {
  document.documentElement.dataset.theme = currentTheme;
  document.querySelectorAll('.theme-btn').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.theme === currentTheme);
  });
}

function setTheme(theme) {
  currentTheme = theme === 'light' ? 'light' : 'dark';
  try {
    localStorage.setItem(THEME_STORAGE_KEY, currentTheme);
  } catch (e) {
    // Non-fatal — the choice just won't survive a reload this session.
  }
  applyTheme();
}
