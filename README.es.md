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
| Trabajar de forma interactiva | `dolly tui` — conecte, inspeccione esquemas, cree volcados y clone desde una terminal real. |
| Automatizar un flujo de trabajo | `dolly dump`, `dolly restore` y `dolly clone` — use un DSN o una conexión guardada. |

`dolly tui` no tiene flags, requiere una TTY y lee `config.jsonc` desde el directorio actual.

## Instalación

Los instaladores descargan el recurso correspondiente de GitHub Release y lo verifican con el `checksums.txt` de esa versión antes de instalarlo.

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Fijar una versión:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.1.1 sh
```

Ruta de instalación predeterminada: `/usr/local/bin`. Defina `DOLLY_INSTALL_DIR` para instalar en otra ubicación.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Fijar una versión:

```powershell
$env:DOLLY_VERSION="0.1.1"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Ruta de instalación predeterminada: `%LOCALAPPDATA%\Programs\dolly\bin`; el instalador la agrega al `PATH` del usuario.

### Fijación de versión y política de soporte

| Tema | Detalle |
|---|---|
| Última versión | Los instaladores usan por defecto la última [GitHub Release](https://github.com/VicenteOlmos/dolly/releases). |
| Fijar una versión | Defina `DOLLY_VERSION` (por ejemplo `0.1.1`) en el comando de instalación anterior. |
| Etiquetas SemVer | Las etiquetas de versión siguen `vX.Y.Z`. Solo la **última versión** recibe correcciones de seguridad. |
| Activos inmutables | Las etiquetas y los archivos de versión no se sobrescriben; use una nueva etiqueta de parche para correcciones. |
| Sumas de verificación | Cada versión incluye `checksums.txt`; los instaladores verifican los archivos antes de instalar. |

### Desde el código fuente

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# or from a checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

## Primer flujo de trabajo

Cree una configuración local opcional y elija la ruta interactiva o automatizable.

```bash
dolly config init
dolly tui
```

Para un flujo de trabajo con la CLI, proporcione un DSN de PostgreSQL:

```bash
export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
dolly dump --dsn "$DB" --output ./dolly_dump
dolly dump list --output ./dolly_dump
dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
```

## Funcionalidades de Dolly

| Comando | Propósito |
|---|---|
| `dolly tui` | Interfaz interactiva para conectarse, crear volcados y clonar. |
| `dolly dump` | Exporta datos a directorios de volcado NDJSON numerados. |
| `dolly dump --percent N` | Volcado parcial: raíces recientes más cierre de claves foráneas; la salida puede superar el `N%`. |
| `dolly dump list` | Enumera el historial local de volcados sin conectarse a una base de datos. |
| `dolly restore` | Carga un volcado de Dolly en PostgreSQL. |
| `dolly clone` | Clona con `schema-replay`, `template`, `logical-stream` o `physical-backup`. |
| `dolly config` | Crea o inspecciona `config.jsonc` con `init` y `show`. |
| `dolly version` | Muestra la versión de compilación. |

Ejecute `dolly <command> --help` para consultar los flags específicos de cada comando.

**La restauración mediante TUI y CLI es diferente:** la TUI restaura desde el historial de volcados de Dolly. Para restaurar un directorio arbitrario, use `dolly restore --input <dir>`.

Cuando `pg_dump` está en el `PATH`, Dolly captura `schema.sql` y lo sanitiza para permitir restauraciones compatibles entre versiones. Restore nunca ejecuta ese SQL a menos que pase explícitamente `--trust-schema-sql` para artefactos revisados.

La reproducción de esquema de confianza se ejecuta fuera de la transacción de restore, así que confirme ambas condiciones explícitamente:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --trust-schema-sql --no-transaction --yes
```

## Flujos de trabajo y límites habituales

### Volcado local más pequeño

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` es incompatible con `--seed-file` y `--slow-connection`. El cierre de claves foráneas puede hacer que un volcado parcial supere el porcentaje solicitado.

### Restauración masiva más rápida — avanzado

La restauración predeterminada se ejecuta en una sola transacción. Para destinos vacíos de confianza o cargas muy grandes:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

Este modo puede dejar avances parciales si falla durante el proceso. Prefiera el modo predeterminado cuando necesite una reversión atómica.

### Estrategias de clonación

| Estrategia | Cuándo usarla | Sanitización |
|---|---|---|
| `schema-replay` | Clonación predeterminada entre servidores o para desarrollo | Compatible |
| `template` | Misma instancia de PostgreSQL; más rápida | No |
| `logical-stream` | Copia lógica grande entre servidores | No |
| `physical-backup` | Copia del directorio de todo el clúster | No |

`physical-backup` usa `pg_basebackup`, requiere privilegios de replicación y copia todo el directorio de datos del clúster en lugar de una sola base de datos. Lea [copia de seguridad física](docs/physical-backup.md) antes de usarla.

## Seguridad

Trate a Dolly como una herramienta de administración de bases de datos:

- `restore --replace` trunca las tablas de destino antes de insertar.
- `restore --no-transaction --yes` puede dejar un estado parcial en las tablas.
- La sanitización se basa en patrones y solo se aplica a `dump` y `schema-replay`; no garantiza el cumplimiento normativo.
- `template`, `logical-stream` y `physical-backup` copian datos de filas sin sanitizar.

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
