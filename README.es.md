<p align="center">
  <img src="./assets/readme/hero.svg" width="720" alt="Dos ovejas Dolly adorables e idénticas, cada una abrazando una gran base de datos marcada DB bajo el nombre Dolly, con un arco brillante de duplicación entre ellas.">
</p>

<h1 align="center">Dolly</h1>

<p align="center">
  CLI y TUI local-first para PostgreSQL que permiten crear volcados, restaurar y clonar bases de datos.<br>
  <a href="README.md">English</a>
</p>

Elija su ruta:

| Si desea… | Comience aquí |
|---|---|
| Clonar su base de datos primero | [Inicio rápido: clonar](#inicio-rápido-clonar) — instale, agregue un `.env`, ejecute `dolly clone`. |
| Trabajar de forma interactiva | `dolly tui` — conecte, inspeccione esquemas, cree volcados y clone desde una terminal real. |
| Automatizar volcado o restauración | `dolly dump`, `dolly restore` y `dolly clone` — use un DSN o una conexión guardada. |

`dolly tui` no tiene flags, requiere una TTY y lee `config.jsonc` desde el directorio actual. La pantalla de volcado incluye la fila de modo sin transacción; los campos de conexión permiten alternar channel binding y los perfiles guardados conservan esa elección junto con sslrootcert, sslcert y sslkey, y una ruta de certificado cambiada es la que se usa para conectar; la estrategia y on-conflict del clon siguen la configuración actual hasta que el formulario de clon las sobrescribe; los clones template y physical-backup no arrancan si la sanitización está activada. En la pantalla de volcado, la sección **Mode** permite editar el tamaño de fragmento, archivos de listas de tablas, los reintentos en conexión lenta y un interruptor **Sanitize**; la restauración desde **History** puede fijar la política de conflicto de filas, replace y **workers** en paralelo. El formulario **Clone** puede fijar replace y on-conflict para la fase de restauración, sobrescribir el **directorio destino** del backup físico y el **directorio de volcado** del clon, y activar **skip-create** (physical-backup exige un directorio destino). En la pantalla de conexión, **SSLMODE** recorre modos habituales con Espacio cuando ese campo tiene el foco.

## Instalación

Los instaladores descargan el recurso correspondiente de GitHub Release y lo verifican con el `checksums.txt` de esa versión antes de instalarlo.

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Fijar una versión:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.3.5 sh
```

Ruta de instalación predeterminada: `/usr/local/bin`. Defina `DOLLY_INSTALL_DIR` para instalar en otra ubicación.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Fijar una versión:

```powershell
$env:DOLLY_VERSION="0.3.5"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Ruta de instalación predeterminada: `%LOCALAPPDATA%\Programs\dolly\bin`; el instalador la agrega al `PATH` del usuario.

### Fijación de versión y política de soporte

| Tema | Detalle |
|---|---|
| Última versión | Los instaladores usan por defecto la última [GitHub Release](https://github.com/VicenteOlmos/dolly/releases). |
| Fijar una versión | Defina `DOLLY_VERSION` (por ejemplo `0.3.5`) en el comando de instalación anterior. |
| Etiquetas SemVer | Las etiquetas de versión siguen `vX.Y.Z`. Solo la **última versión** recibe correcciones de seguridad. |
| Activos inmutables | Las etiquetas y los archivos de versión no se sobrescriben; use una nueva etiqueta de parche para correcciones. |
| Sumas de verificación | Cada versión incluye `checksums.txt`; los instaladores verifican los archivos antes de instalar. |

### Desde el código fuente

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# or from a checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

<!-- readme:quick-start-clone -->
## Inicio rápido: clonar

1. **Instale Dolly** con los pasos de [Instalación](#instalación) anteriores.
2. En el directorio de su proyecto, cree un archivo `.env` con una conexión compatible. Use `DB_URL` o las variables discretas `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER` y `DB_PASSWORD`:

```bash
DB_URL='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
# o variables discretas:
# DB_HOST=localhost
# DB_PORT=5432
# DB_NAME=mydb
# DB_USER=user
# DB_PASSWORD=pass
```

3. Ejecute:

```bash
dolly clone
```

Dolly descubre `.env` en el **directorio de trabajo actual** al resolver la base de datos origen. En Unix, si el archivo tiene permisos amplios (legible por grupo u otros), Dolly emite una advertencia y continúa **sin cambiar** bytes, modo, propietario ni marcas de tiempo de ese archivo.

Para clonación automatizada con valores predeterminados de configuración:

```bash
dolly clone -ff
```

Opcional: `dolly config init` escribe `config.jsonc` para URL de destino, nombres de clon, estrategias y otros valores predeterminados.

<!-- readme:security:dotenv-advisory -->
Se recomiendan permisos solo para el propietario (por ejemplo `chmod 600 .env`) en archivos con secretos. Dolly no exige ni modifica permisos en archivos `.env` externos que descubre.
<!-- /readme:security:dotenv-advisory -->

## Más flujos de trabajo

Cree una configuración local opcional y elija la ruta interactiva o automatizable.

```bash
dolly config init
dolly tui
```

Para volcado y restauración sin clonar, proporcione un DSN de PostgreSQL:

```bash
export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
dolly dump --dsn "$DB" --output ./dolly_dump
dolly dump --dsn "$DB" --schemas app,public --exclude-schema staging --output ./dolly_dump
dolly dump list --output ./dolly_dump
dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
```

<!-- situation-guidance:start -->

Dolly no inspecciona el tamaño de la base de datos ni las condiciones de red, ni ajusta los modos automáticamente. Considere los tamaños y las velocidades como cualitativos: el hardware, la forma del esquema, el ancho de fila y la latencia afectan los resultados.

**¿No está seguro de qué elegir?** Use `dolly tui` para opciones guiadas. En la CLI, omitir flags de optimización mantiene los valores predeterminados seguros y seriales (`workers=1`, restore transaccional).

| Situación | Recomendación | Motivo | Límite |
|---|---|---|---|
| <!-- situation:safe-default --> Dudas / camino más seguro | `dolly dump --dsn "$DB" --output ./dolly_dump` → `dolly restore --dsn "$TARGET_DB" --input ./dolly_dump/1` | Un worker por defecto; restore transaccional y atómico | Más lento que modos paralelos en bases grandes |
| <!-- situation:small-database --> Base pequeña, copia directa | `dolly dump --dsn "$DB" --output ./dolly_dump` | Volcado completo con pocos flags | Se vuelve lento al crecer los datos |
| <!-- situation:large-stable --> Tablas muy grandes donde la reanudabilidad importa más que la velocidad | `dolly dump ... --chunk-table public.large_table --workers 1` | Los planes con PK o clave única apta usan fragmentos reanudables | Las tablas sin clave segura reanudan con `ctid`; VACUUM o actualizaciones pueden omitir o duplicar filas |
| <!-- situation:large-unreliable --> Datos grandes, enlace lento o inestable | `dolly dump ... --slow-connection --workers 1` | Las claves seguras obtienen puntos de control; las tablas sin clave segura reanudan con `ctid` | La reanudación con `ctid` puede omitir o duplicar filas tras VACUUM o actualizaciones; modo no transaccional e incompatible con subconjunto y volcado paralelo |
| <!-- situation:maximum-dump-speed --> Base grande, conexión estable, máximo rendimiento de volcado | `dolly dump ... --workers "$WORKERS"` | Snapshot consistente compartido entre workers de tabla | Elija entre 1 y 16 según pruebas; requiere `max_open_conns >= workers+1`; excluye slow/chunk/subconjunto/`--no-transaction` |
| <!-- situation:maximum-restore-speed --> Máximo rendimiento de restore | **AVANZADO — NO ATÓMICO** `dolly restore ... --workers "$WORKERS" --no-transaction --yes --ack-partial-state` | Restauración paralela de tablas tras reconocer riesgo de estado parcial | Sin reversión atómica; `on-conflict` debe ser `error`; no usar `--replace`, `--trust-schema-sql`, skip ni upsert |
| <!-- situation:representative-sample --> Muestra para desarrollo/pruebas, no copia completa | `dolly dump ... --percent "$PERCENT" --max-rows-per-table "$ROW_CAP"` | Raíces recientes más cierre de claves foráneas | No es representación estadística; el cierre puede superar el porcentaje |
| <!-- situation:same-instance-clone --> Clonación más rápida en la misma instancia | `dolly clone --strategy template` | Copia por plantilla en un solo servidor PostgreSQL | Sin conexiones activas en el origen; se niega si la sanitización está activada |
| <!-- situation:cross-server-large-clone --> Copia grande entre servidores de una sola base | `dolly clone --strategy logical-stream` | Flujo lógico para copias remotas grandes | Redacta si la sanitización está activada; no es copia física del clúster |

`$WORKERS`, `$PERCENT` y `$ROW_CAP` son valores elegidos por el operador; Dolly no los establece automáticamente.

En `--chunk-table` y `--slow-connection`, Dolly elige por tabla la PK existente, luego una clave B-tree `UNIQUE NOT NULL` simple o compuesta apta. Si no existe una clave segura, reanuda con `ctid` y advierte que VACUUM o actualizaciones pueden omitir o duplicar filas. `--require-safe-key` rechaza ese plan con `ctid`. La clave de config `dump.require_safe_key` (por defecto `false`) activa el mismo rechazo; el flag de CLI prevalece cuando se indica. La reanudación exige la misma estrategia y huella de clave; los cambios fallan de forma cerrada y preservan los artefactos interrumpidos.

Consulte `dolly dump --help`, `dolly restore --help` y `dolly clone --help` para conocer los flags. Más detalle en [Flujos de trabajo y límites habituales](#flujos-de-trabajo-y-límites-habituales) y [Estrategias de clonación](#estrategias-de-clonación).

<!-- situation-guidance:end -->

## Funcionalidades de Dolly

| Comando | Propósito |
|---|---|
| `dolly tui` | Interfaz interactiva para conectarse, crear volcados y clonar. |
| `dolly dump` | Exporta datos a directorios de volcado NDJSON numerados. Alcance de esquemas: `--schemas` (separados por comas) anula los del perfil guardado, luego `dump.schemas` en config y, si no hay ninguno, `public`. `--exclude-schema` (o `dump.exclude_schemas`) quita esquemas tras resolver los incluidos. Se niega si el alcance efectivo queda vacío. Los metadatos del volcado registran las versiones de PostgreSQL y de dolly cuando están disponibles. |
| `dolly dump --percent N` | Volcado parcial: raíces recientes más cierre de claves foráneas; la salida puede superar el `N%`. |
| `dolly dump list` | Enumera el historial local de volcados sin conectarse a una base de datos. |
| `dolly restore` | Carga un volcado de Dolly en PostgreSQL. `--schemas` anula los esquemas del perfil guardado. Las columnas identity `ALWAYS` usan `INSERT ... OVERRIDING SYSTEM VALUE` en la ruta fila a fila; COPY mantiene esas columnas en la lista explícita. |
| `dolly clone` | Clona con `schema-replay`, `template`, `logical-stream` o `physical-backup`. Los flags `--replace`, `--on-conflict`, `--skip-create` y `--dump-dir` anulan las claves `clone.*` correspondientes en esa ejecución. |
| `dolly config` | Crea o inspecciona `config.jsonc` con `init` y `show`. |
| `dolly version` | Muestra la versión de compilación. |

Ejecute `dolly <command> --help` para consultar los flags específicos de cada comando.

**Restauración mediante TUI y CLI:** la sección de historial de la TUI restaura el volcado seleccionado, o un directorio que escribas ahí (`p` para editar la ruta). `dolly restore --input <dir>` sigue siendo la vía para scripts.

**Modo de volcado en la TUI:** la sección Mode fija conexión lenta, `--require-safe-key`, workers, porcentaje, archivo de semillas, tablas en fragmentos, límites de subconjunto (`--max-depth`, `--max-tables`, `--max-rows`, `--max-rows-per-table`, `--max-in-list-size`) e include/exclude para la siguiente ejecución. Los mismos flags siguen en `dolly dump`.

Configure `dump.history_path` en `config.jsonc` para cambiar la ubicación de `.dolly/dump-history.json` (TUI y CLI usan la misma ruta).

Las tablas padre particionadas no se exportan: un `SELECT` del padre devuelve todas las hijas, así que Dolly vuelca y clona solo las particiones hoja. Nombra esas particiones en `--include-table`; incluir el padre falla con sus hojas directas o indica que no hay hojas en el alcance cuando el padre no tiene ninguna, y excluir el padre también excluye cada hoja anidada. Los volcados completos registran los padres omitidos en la procedencia de `metadata.json` y registran `no_transaction` cuando se usa `--no-transaction`. Las secuencias identity de padres particionados también se capturan. Las columnas `GENERATED ALWAYS` quedan en los metadatos y se omiten al restaurar y en `logical-stream` para que el destino las calcule. Las columnas identity sí se copian.

Cuando `pg_dump` está en el `PATH`, Dolly captura `schema.sql` y lo sanitiza para permitir restauraciones compatibles entre versiones, incluyendo `CREATE SCHEMA IF NOT EXISTS` para que `--trust-schema-sql` pueda reproducirse en una base nueva que ya tiene `public`. Si falta `pg_dump`, Dolly escribe el mismo archivo desde la reproducción del catálogo (sin propietarios ni ACL) y avisa en stderr. Restore nunca ejecuta ese SQL a menos que pase explícitamente `--trust-schema-sql` para artefactos revisados.

La reproducción de esquema de confianza se ejecuta fuera de la transacción de restore, así que confirme ambas condiciones explícitamente:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --trust-schema-sql --no-transaction --yes
```

## Flujos de trabajo y límites habituales

### Volcado local más pequeño

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` es incompatible con `--seed-file` y `--slow-connection`. El cierre de claves foráneas puede hacer que un volcado parcial supere el porcentaje solicitado. En un archivo de semillas, `"table"` puede ser `schema.table`; un nombre sin esquema debe coincidir con exactamente una tabla en el alcance del volcado.

### Restauración masiva más rápida — avanzado

La restauración predeterminada se ejecuta en una sola transacción. Con política de conflicto `error` y un DSN, Dolly carga cada tabla con COPY en esa misma transacción y actualiza las secuencias antes del commit. La restauración de secuencias aplica el incremento, el mínimo, el máximo, la caché, el ciclo y el tipo de datos (smallint, integer o bigint) capturados antes de setval cuando la definición de destino difiere. Los dumps escritos sin esos campos solo ejecutan setval. Una secuencia de destino que ya avanzó no se reduce, y los límites que excluirían su valor actual se dejan sin cambio. Skip y upsert siguen en INSERT y requieren una clave primaria o una clave única persistida en la tabla, con `OVERRIDING SYSTEM VALUE` cuando los metadatos marcan identity `ALWAYS`; upsert no asigna columnas identity `GENERATED ALWAYS` ni columnas generadas en `ON CONFLICT DO UPDATE SET`. `--no-transaction` usa una conexión COPY aparte y confirma por tabla.

Para destinos vacíos de confianza o cargas muy grandes:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

Este modo puede dejar avances parciales si falla durante el proceso. Prefiera el modo predeterminado cuando necesite una reversión atómica.

La restauración paralela (`--workers` mayor que 1) exige `--ack-partial-state` y escribe `.dolly-restore-partial-state.json` hasta el éxito completo. El manifiesto registra host, puerto, base de datos y una huella del conjunto de tablas del volcado; la restauración falla de forma segura si el destino o el conjunto de tablas no coincide con el manifiesto. La TUI exige activar el reconocimiento de riesgo de estado parcial en la pantalla de historial antes de un restore paralelo cuando los workers son mayores que 1 (no se guarda en la configuración).

### Estrategias de clonación

<!-- readme:fidelity:schema-replay -->
La estrategia predeterminada `schema-replay` recrea definiciones de esquema y objetos (incluidas definiciones de disparadores y vistas materializadas), restaura datos de tablas regulares y el estado de secuencias, y refresca las vistas materializadas después de cargar los datos. El contenido de vistas materializadas no se clona como copia aparte; se refresca desde las tablas restauradas. Los disparadores de usuario se desactivan mientras se cargan las filas; los disparadores clonados pueden ejecutarse después de reactivarlos. Los propietarios y las ACL se omiten salvo que pases `--with-privileges` (el destino ya debe tener esos roles). Los roles y tablespaces de ámbito de clúster no se crean. `template` y `physical-backup` se niegan a ejecutarse cuando la sanitización está activada. `logical-stream` redacta columnas sensibles en ese caso. Si `pg_dump` no está en el PATH, schema-replay reproduce el catálogo (incluye funciones, vistas, disparadores, reglas, agregados normales y ordered-set, operadores definidos por el usuario, restricciones de exclusión y publicaciones lógicas de tablas en alcance; los agregados hypothetical y las clases de operadores requieren `pg_dump`). Las tablas particionadas se crean con `PARTITION BY` y solo se copian las particiones hoja. Las columnas generadas se declaran en la tabla y se omiten en la carga de datos. La reproducción del catálogo también emite columnas de identidad, intercalaciones distintas de la predeterminada, tablas `UNLOGGED` e índices que existen solo en una partición hoja. El formulario de clonación puede pasar `--with-privileges`. La reproducción del catálogo también restaura comprobaciones CHECK de dominios, estadísticas extendidas, comentarios en funciones e índices, y privilegios de tablas, esquemas, columnas, secuencias y rutinas más `ALTER DEFAULT PRIVILEGES` (incluido PUBLIC y `WITH GRANT OPTION`) cuando `--with-privileges` está activo. También reproduce restricciones UNIQUE diferibles, identidad de réplica distinta de la predeterminada, almacenamiento por columna y comentarios en restricciones y dominios. También reproduce intercalaciones definidas por el usuario, compresión por columna y el parámetro fillfactor de tablas, y comentarios en tipos enumerados y compuestos. También reproduce tipos de datos de secuencia distintos de bigint, COLLATE en atributos compuestos, comentarios en intercalaciones y políticas, y privilegios USAGE en tipos enumerados, de dominio y compuestos independientes cuando `--with-privileges` está activo, incluido REVOKE USAGE FROM PUBLIC cuando el origen quitó ese valor predeterminado. También reproduce el esquema (incluido public) y la versión de las extensiones, comentarios en disparadores y reglas, opciones de vistas (security_barrier, security_invoker y check_option), el fillfactor de vistas materializadas y tipos de rango con su función canónica, subtype diff y nombre de multirango.
<!-- /readme:fidelity:schema-replay -->

| Estrategia | Cuándo usarla | Sanitización |
|---|---|---|
| `schema-replay` | Clonación predeterminada entre servidores o para desarrollo | Compatible |
| `template` | Misma instancia de PostgreSQL; más rápida | Se niega si está activada |
| `logical-stream` | Copia lógica grande entre servidores | Redacta si está activada |
| `physical-backup` | Copia del directorio de todo el clúster | Se niega si está activada |

`physical-backup` usa `pg_basebackup`, requiere privilegios de replicación y copia todo el directorio de datos del clúster en lugar de una sola base de datos. Lea [copia de seguridad física](docs/physical-backup.md) antes de usarla.

## Seguridad

Trate a Dolly como una herramienta de administración de bases de datos:

- `restore --replace` trunca las tablas de destino antes de insertar.
- `restore --no-transaction --yes` puede dejar un estado parcial en las tablas.
- La sanitización se basa en patrones y se aplica a `dump`, `schema-replay` y `logical-stream`; no garantiza el cumplimiento normativo.
- `template` y `physical-backup` se niegan a ejecutarse cuando la sanitización está activada. Con la sanitización desactivada copian datos de filas sin sanitizar. `logical-stream` redacta filas cuando la sanitización está activada.

Antes de usar datos de producción o similares a producción, utilice un rol con privilegios mínimos, mantenga los DSN y los volcados fuera de Git, confirme que los destinos de operaciones destructivas sean descartables, valide manualmente la sanitización y ensaye en un entorno de preproducción. Consulte [seguridad](docs/security.md) y [copia de seguridad física](docs/physical-backup.md).

## Configuración y automatización

`dolly config init` escribe `config.jsonc`. Consulte [config.example.jsonc](config.example.jsonc) para ver la plantilla completa.

Las conexiones guardadas están desactivadas de forma predeterminada. Actívelas explícitamente:

```jsonc
{
  "save_connections": true,
  "connections": {
    "scope": "xdg",   // or "project"
    "encrypt": true    // set DOLLY_CONNECTIONS_KEY (32-byte standard base64)
  }
}
```

Después, los comandos de la CLI pueden usar `--connection <name>` en lugar de `--dsn`. Los almacenes con alcance de proyecto son prácticos, pero es más fácil incluirlos en un commit por accidente; los almacenes cifrados requieren `DOLLY_CONNECTIONS_KEY`, y perder esa clave impide acceder a los perfiles cifrados.

`dump`, `restore`, `clone` y `version` aceptan `--json`:

- Exit 0: JSON de éxito en **stdout**.
- Exit 1: `{"ok":false,"command":"...","error":"..."}` en **stderr**.
- `clone --json` requiere `-ff` para uso no interactivo.
- `dump list --json` devuelve un array de registros del historial en lugar del sobre del comando.

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json) || { cat err.json; exit 1; }
echo "$result"
```

## Desarrollo

Requiere Go 1.26.3+ (coincidir con `go.mod`) y herramientas cliente de PostgreSQL 16 en el `PATH` para captura de esquema y estrategias de clonación.

Ejecute un recorrido completo con PostgreSQL local desde un checkout del código fuente:

```bash
docker compose up -d
export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
./bin/dolly dump --dsn "$DOLLY_TEST_PG_DSN" --output ./dolly_dump
./bin/dolly restore --dsn "$DOLLY_TEST_PG_DSN" --input ./dolly_dump/1 --on-conflict skip
```

```bash
go test ./...
go vet ./...
make preflight
make test-integration   # needs DOLLY_TEST_PG_DSN
```

Notas de la versión: [docs/release.md](docs/release.md) · [CHANGELOG.md](CHANGELOG.md)

Reporte problemas de seguridad en privado mediante [reporte privado de vulnerabilidades de GitHub](https://github.com/VicenteOlmos/dolly/security/advisories/new) — consulte [SECURITY.md](SECURITY.md).

## Licencia

[MIT](LICENSE)
