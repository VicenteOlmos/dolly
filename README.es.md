# Dolly

[English](README.md)

CLI y TUI local-first para PostgreSQL: dump, restore y clone de bases de datos.

Después de instalar, el uso habitual es:

```bash
dolly tui
```

Abre el cockpit interactivo (conectar → schemas → dump/clone). Sin flags. Necesita una terminal real (TTY). La config vive en `config.jsonc` del directorio actual.

## Instalación

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | sh
```

Fijar versión:

```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_VERSION=0.1.0 sh
```

Ruta por defecto: `/usr/local/bin` (cambiar con `DOLLY_INSTALL_DIR`). Verifica checksums contra el `checksums.txt` del release.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Fijar versión:

```powershell
$env:DOLLY_VERSION="0.1.0"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
```

Ruta por defecto: `%LOCALAPPDATA%\Programs\dolly\bin` (se agrega al `PATH` de usuario).

### Desde código fuente

```bash
go install github.com/VicenteOlmos/dolly/cmd/dolly@latest
# o desde un checkout:
go build -buildvcs=false -o ./bin/dolly ./cmd/dolly
```

## Primeros minutos

1. Instalá (arriba).
2. En un directorio de proyecto, opcionalmente creá config:

   ```bash
   dolly config init
   ```

3. Arrancá la TUI:

   ```bash
   dolly tui
   ```

4. O usá la CLI con un DSN:

   ```bash
   export DB='postgres://user:pass@localhost:5432/mydb?sslmode=disable'
   dolly dump --dsn "$DB" --output ./dolly_dump
   dolly dump list --output ./dolly_dump
   dolly restore --dsn "$DB" --input ./dolly_dump/1 --on-conflict skip
   ```

## Comandos

| Comando | Para qué |
|---------|----------|
| `dolly tui` | Cockpit interactivo (conectar, dump, clone). |
| `dolly dump` | Exportar datos a directorios NDJSON numerados. |
| `dolly dump --percent N` | Dump parcial (raíces recientes + cierre FK; el tamaño puede superar N%). |
| `dolly dump list` | Listar historial local de dumps (sin conectar a la DB). |
| `dolly restore` | Cargar un dump en PostgreSQL. |
| `dolly clone` | Clonar con una estrategia (`schema-replay`, `template`, `logical-stream`, `physical-backup`). |
| `dolly config` | Crear/ver `config.jsonc` (`init`, `show`). |
| `dolly version` | Imprimir versión del build. |

`dolly <comando> --help` muestra los flags.

**Restore TUI vs CLI:** la TUI restaura desde el historial de dumps de Dolly. Para un directorio arbitrario usá `dolly restore --input <dir>`.

Si `pg_dump` está en el `PATH`, Dolly también captura `schema.sql` (sanitizado para restore entre versiones).

## Recetas comunes

### Dump local más chico

```bash
dolly dump --dsn "$DB" --output ./dolly_dump --percent 10 --max-rows-per-table 1000
```

`--percent` choca con `--seed-file` y `--slow-connection`.

### Restore masivo más rápido (avanzado)

El restore por defecto es una sola transacción. Para targets vacíos de confianza / cargas muy grandes:

```bash
dolly restore --dsn "$DB" --input ./dolly_dump/1 --no-transaction --yes
```

Si falla a mitad de camino puede quedar progreso parcial. Preferí el modo por defecto cuando necesitás rollback atómico.

### Estrategias de clone

| Estrategia | Cuándo | Sanitización |
|------------|--------|--------------|
| `schema-replay` | Clone cross-server / dev por defecto | Sí |
| `template` | Misma instancia Postgres, más rápido | No |
| `logical-stream` | Copia lógica grande entre servers | No |
| `physical-backup` | Copia del directorio del cluster | No |

## Configuración y conexiones guardadas

```bash
dolly config init   # escribe config.jsonc
```

Las conexiones guardadas vienen **apagadas**. Para activarlas en `config.jsonc`:

```jsonc
{
  "save_connections": true,
  "connections": {
    "scope": "xdg",   // o "project"
    "encrypt": true   // setear DOLLY_CONNECTIONS_KEY (32 bytes en base64 estándar)
  }
}
```

Después la CLI puede usar `--connection <nombre>` en lugar de `--dsn`.

Plantilla completa: `config.example.jsonc`.

## Modo agente / `--json`

`dump`, `restore`, `clone` y `version` aceptan `--json`:

- Exit 0 → JSON de éxito en **stdout**
- Exit 1 → `{"ok":false,"command":"...","error":"..."}` en **stderr**
- `clone --json` requiere `-ff` (no interactivo)
- `dump list --json` usa otra forma (array de historial)

```bash
result=$(dolly dump --dsn "$DB" --output ./out --json 2>err.json) || { cat err.json; exit 1; }
echo "$result"
```

## Seguridad

Tratá a Dolly como herramienta de admin de DB:

- `restore --replace` trunca tablas destino antes de insertar.
- `restore --no-transaction --yes` puede dejar estado parcial por tabla.
- La sanitización es por patrones y solo en dump / `schema-replay` — no es garantía de compliance.
- `template` y `physical-backup` copian datos sin sanitizar.

Más detalle: [seguridad](docs/security.md) · [physical backup](docs/physical-backup.md)

## Desarrollo

Postgres de desarrollo + round trip desde un checkout:

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
make test-integration   # necesita DOLLY_TEST_PG_DSN
```

Release: [docs/release.md](docs/release.md) · [CHANGELOG.md](CHANGELOG.md)

## Licencia

[MIT](LICENSE)
