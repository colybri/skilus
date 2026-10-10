# skilus

Gestor de skills (`SKILL.md`) para agentes de IA, en un único binario Go. Instala skills desde Git, URL o un directorio local en Claude Code, Codex, Cursor, GitHub Copilot y `.agents/skills`, y garantiza que lo instalado es exactamente lo que se revisó: cada skill se fija a un commit, se le calcula un hash de contenido y se inspecciona antes de instalarse.

> Estado: fase 2 en construcción. Hoy funcionan `skilus agents`, `skilus add` (desde un directorio local o un repositorio Git), `skilus list`, `skilus remove`, `skilus verify` y `skilus sync`.

## Instalación

Descarga el archivo de tu sistema desde [Releases](https://github.com/colybri/skilus/releases) (Linux, macOS y Windows, amd64 y arm64) y deja `skilus` en tu `PATH`. Necesitas `git` instalado para las fuentes Git.

Cada release publica `checksums.txt` firmado con [cosign](https://github.com/sigstore/cosign) sin claves (identidad OIDC del workflow) y un SBOM por archivo. Para verificar una descarga:

```sh
cosign verify-blob checksums.txt \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/colybri/skilus/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt
```

Para publicar una versión basta con empujar un tag `vX.Y.Z`: el workflow `release` crea la release como borrador para revisarla antes de hacerla pública.

## Uso

```sh
skilus agents          # agentes soportados, sus rutas y si están instalados
skilus agents --json

skilus add anthropics/skills --skill pdf     # GitHub, rama por defecto
skilus add github.com/o/r@v1.2.0            # un tag, rama o commit
skilus add git@gitlab.com:g/r.git           # cualquier URL de Git (https, ssh, file)
skilus add ./mis-skills                     # un directorio local: empieza por ./, ../ o /
skilus add ./mis-skills --skill review -y   # sin preguntar
skilus add ./mis-skills --agent claude-code --scope global

skilus list                                 # skills instaladas con commit, hash y agentes
skilus list --scope global --json
skilus remove review                        # la quita de los agentes, del lock y de skilus.yaml

skilus verify                               # comprueba que lo instalado coincide con el lock
skilus verify --json
skilus sync                                 # deja los agentes como dice el lock (como npm ci)
skilus sync --force                         # sobrescribe también lo cambiado a mano
```

Las fuentes Git se descargan con el `git` del sistema, así que valen tus credenciales, claves SSH y helpers; skilus no guarda tokens y rechaza URLs con contraseña. El repositorio se lee sin hacer checkout (no se ejecutan filtros ni hooks) y la skill se fija al commit exacto en `skilus.lock`.

`skilus add` lee una skill (con `SKILL.md` en la raíz) o un directorio con skills en `skills/<nombre>/` o `<nombre>/`. Antes de instalar inspecciona cada skill: bloquea symlinks que salen de la skill, ficheros o paquetes demasiado grandes y caracteres de control en la descripción; avisa de ficheros ejecutables, `curl … | sh`, texto invisible y secuencias de escape. Con `--strict` los avisos bloquean, y con `--yes` los ejecutables necesitan `--allow-scripts`.

El contenido se guarda en `~/.skilus/store/<sha256>`. En el proyecto se instala como copia (para poder versionarla) y en global como symlink al almacén. El resultado queda en `skilus.lock` (qué contenido exacto hay instalado y dónde) y la intención en `skilus.yaml`; en global, ambos viven en `~/.skilus/`.

`skilus verify` recalcula el hash de cada skill en cada agente y lo compara con el lock; si falta alguna o ha cambiado un fichero, dice cuál y sale con código 6. `skilus sync` instala lo que falta desde el almacén o, si no está, descargando cada skill por su commit, y falla si el hash no coincide con el del lock. No cambia `skilus.lock` ni `skilus.yaml` y no toca las skills modificadas a mano salvo con `--force`. Por defecto `sync` trabaja en el proyecto y `verify` en ambos ámbitos; los dos aceptan `--scope project|global|all`.

Códigos de salida: 0 bien, 1 error o cancelado, 2 uso inválido, 3 no encontrado, 4 ya instalado o conflicto, 5 rechazado por la inspección, 6 lo instalado no coincide con el lock.

## Desarrollo

Requisitos: Go 1.25 o superior, `git` y [golangci-lint](https://golangci-lint.run/) v2.

```sh
make test    # go test -race ./...
make lint    # incluye las reglas de capas con depguard
make build   # bin/skilus
```

La arquitectura es DDD hexagonal:

```text
cmd/skilus/        raíz de composición
internal/domain/   modelo: solo librería estándar, sin E/S
internal/app/      casos de uso y puertos
internal/adapter/  implementaciones de los puertos
internal/cli/      comandos Cobra
```

`test/gate/run.sh` contiene las puertas de las fases 1 y 2: instala 10 skills reales de tres repositorios públicos, fijadas a commits, y comprueba que el lock coincide con `test/gate/skilus.lock`; después borra los agentes y el almacén, comprueba que `sync` lo reconstruye y que `verify` detecta un fichero cambiado a mano. La CI la ejecuta en Linux, macOS y Windows. Si cambia el formato del lock, regenera la referencia con `GATE_UPDATE=1 test/gate/run.sh bin/skilus`.

Las reglas completas están en el plan del proyecto, sección "Reglas de arquitectura", y `.golangci.yml` las hace cumplir.

## Licencia

[PolyForm Noncommercial 1.0.0](LICENSE). El uso comercial requiere una licencia comercial: ver [COMMERCIAL.md](COMMERCIAL.md).
