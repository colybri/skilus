# skilus

Gestor de skills (`SKILL.md`) para agentes de IA, en un único binario Go. Instala skills desde Git, URL o un directorio local en 74 agentes (Claude Code, Codex, Cursor, GitHub Copilot, Gemini CLI, Windsurf, OpenCode, Cline…), y garantiza que lo instalado es exactamente lo que se revisó: cada skill se fija a un commit, se le calcula un hash de contenido y se inspecciona antes de instalarse.

> Estado: versión preliminar (0.x); el formato de `skilus.yaml` y `skilus.lock` aún puede cambiar. Funcionan `skilus agents`, `skilus add` (desde un directorio local, un repositorio Git o un ZIP o tar.gz), `skilus list`, `skilus remove`, `skilus edit`, `skilus inspect`, `skilus verify`, `skilus sync`, `skilus update`, `skilus profile`, `skilus search`, `skilus audit` y `skilus lang`.

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
skilus agents          # agentes detectados en tu equipo y sus rutas
skilus agents --all    # todos los soportados, con una columna que dice si están instalados
skilus agents --json

skilus add anthropics/skills --skill pdf     # GitHub, rama por defecto
skilus add github.com/o/r@v1.2.0            # un tag, rama o commit
skilus add git@gitlab.com:g/r.git           # cualquier URL de Git (https, ssh, file)
skilus add https://example.com/skills.zip   # un ZIP o tar.gz por https
skilus add ./mis-skills                     # un directorio local: empieza por ./, ../ o /
                                            # en una terminal, eliges las skills y los agentes
skilus add ./mis-skills --skill review -y   # sin preguntar
skilus add ./mis-skills --agent claude-code --scope global

skilus list                                 # skills instaladas con commit, hash y agentes
skilus list --scope global --json
skilus remove review                        # la quita de los agentes, del lock y de skilus.yaml,
                                            # con las dependencias que ya nadie necesita
skilus edit                                 # abre skilus.yaml en tu editor y solo lo guarda si es válido
skilus edit --scope global                  # el global, en ~/.skilus/skilus.yaml
skilus edit --path                          # solo imprime dónde está

skilus inspect anthropics/skills --skill pdf # lo que add revisaría, sin instalar nada
skilus inspect ./mis-skills --strict --json # para CI: código 5 si add lo rechazaría
skilus inspect ./mis-skills --full          # con las descripciones completas

skilus verify                               # comprueba que lo instalado y skilus.yaml coinciden con el lock
skilus verify --json
skilus sync                                 # deja los agentes como dice el lock (como npm ci)
skilus sync --force                         # sobrescribe también lo cambiado a mano

skilus update --check                       # qué ramas o tags apuntan a un commit nuevo, sin cambiar nada
skilus update                               # aplica skilus.yaml, muestra qué cambia, pregunta y actualiza
skilus update review --yes --allow-scripts

skilus profile create web --skill debug,ui --agent codex  # declara un perfil en skilus.yaml
skilus profile add web review               # añade skills (o --agent) a un perfil
skilus profile remove web ui --agent codex  # las quita
skilus profile delete web                   # borra el perfil; lo instalado no cambia
skilus profile list                         # perfiles de skilus.yaml; * marca el activo
skilus profile use web                      # despliega el perfil y retira lo que no le pertenece
skilus profile use web --dry-run            # solo enseña el plan

skilus search pdf                           # busca en skills.sh y en tus índices
skilus search pdf --owner anthropics --json

skilus audit                                # confianza, firmas e inspección de lo instalado
skilus audit --strict --json                # para CI: código 5 si hay avisos

skilus lang                                 # idioma en uso, de dónde sale y los disponibles
skilus lang set en                          # cambia el idioma de skilus para tu usuario
skilus lang set auto                        # vuelve a seguir al sistema
```

Las fuentes Git se descargan con el `git` del sistema, así que valen tus credenciales, claves SSH y helpers; skilus no guarda tokens y rechaza URLs con contraseña. El repositorio se lee sin hacer checkout (no se ejecutan filtros ni hooks) y la skill se fija al commit exacto en `skilus.lock`.

Los archivos ZIP y tar.gz (también `.tgz`) se descargan solo por https y se descomprimen en memoria, sin escribir nada en disco: se rechazan rutas absolutas, con `..` o con `\`, y entradas que no son ficheros, directorios o symlinks, y hay límites de descarga (50 MB), de tamaño descomprimido y de número de entradas. Si todo cuelga de un único directorio, como en los archivos que genera GitHub, se quita. No llevan `@ref` ni commit: el lock fija su contenido por el hash, y una URL con `?` se rechaza porque suele llevar un token.

`skilus add` lee una skill (con `SKILL.md` en la raíz) o un directorio con skills en `skills/<nombre>/`, `skills/.curated/<nombre>/` (como openai/skills) o `<nombre>/`. Antes de instalar inspecciona cada skill: bloquea symlinks que salen de la skill, ficheros o paquetes demasiado grandes y caracteres de control en la descripción; avisa de ficheros ejecutables, `curl … | sh` y accesos a credenciales (`~/.ssh`, `.aws/credentials`…) en cualquier fichero de texto, y de texto invisible y secuencias de escape en el Markdown. Con `--strict` los avisos bloquean, y con `--yes` los ejecutables necesitan `--allow-scripts`.

En una terminal, el plan se ajusta a su ancho, recorta cada descripción a dos líneas (`--full` las muestra enteras) y usa colores: ✓ en verde si no hay hallazgos, ! en amarillo para los avisos y ✗ en rojo para los bloqueos, con un resumen final antes de preguntar. `--no-color` o la variable `NO_COLOR` quitan los colores, y en una tubería o con `--json` nunca se usan.

Para limitar de dónde se instala, añade `trust:` a `skilus.yaml` (del proyecto o el global en `~/.skilus/`):

```yaml
trust:
  - github.com/anthropics          # la organización entera
  - github.com/obra/superpowers    # un repositorio
```

Con la lista, cualquier otra fuente recibe un aviso `untrusted-source`, que con `--strict` impide instalarla. Sin lista no se comprueba nada, y los directorios locales nunca.

El contenido se guarda en `~/.skilus/store/<sha256>`. En el proyecto se instala como copia (para poder versionarla) y en global como symlink al almacén. El resultado queda en `skilus.lock` (qué contenido exacto hay instalado y dónde) y la intención en `skilus.yaml`; en global, ambos viven en `~/.skilus/`.

El `skilus.yaml` del proyecto está en el directorio donde ejecutas skilus y el global en `~/.skilus/skilus.yaml`: `/home/<usuario>/.skilus/skilus.yaml` en Linux, `/Users/<usuario>/.skilus/skilus.yaml` en macOS y `C:\Users\<usuario>\.skilus\skilus.yaml` en Windows. `skilus edit` lo abre en el editor de `SKILUS_EDITOR`, `VISUAL`, `EDITOR` o el `core.editor` de git (si no hay ninguno, `notepad` en Windows y `nano` o `vi` en el resto; uno gráfico tiene que esperar a que cierres el fichero, como `code --wait`) y, si no existe, empieza con una plantilla comentada. Al cerrar el editor lo comprueba como `verify` y solo lo guarda si es válido; una clave desconocida en el primer nivel, como `skils:`, también es un error. Si hay errores los enumera y pregunta si volver a editarlo; si no, descarta los cambios y sale con código 2. Editarlo no instala nada: después, `skilus verify` dice qué falta por aplicar.

`skilus verify` recalcula el hash de cada skill en cada agente y lo compara con el lock; si falta alguna o ha cambiado un fichero, dice cuál y sale con código 6. También comprueba `skilus.yaml`: que se puede leer, que cada skill de `skills:` está instalada (salvo las que no pertenecen al perfil activo), que cada skill instalada, salvo las dependencias, está en `skills:` y que los perfiles solo usan skills declaradas; si el fichero tiene errores sale con código 2 y, si no coincide con el lock, con código 6, proponiendo el comando que lo arregla. `skilus sync` instala lo que falta desde el almacén o, si no está, descargando cada skill por su commit, y falla si el hash no coincide con el del lock. No cambia `skilus.lock` ni `skilus.yaml` y no toca las skills modificadas a mano salvo con `--force`. `skilus update --check` consulta con `git ls-remote`, sin descargar contenido, a qué commit apunta ahora la rama o el tag de cada skill; las fijadas a un commit no se comprueban. `skilus update` descarga esa versión, enseña los ficheros añadidos, borrados y modificados y la inspección de la versión nueva (los ejecutables que ya tenía no vuelven a preguntarse) y, tras confirmar, reemplaza las copias instaladas y actualiza el lock; `skilus.yaml` no cambia porque guarda la rama o el tag, no el commit. `skilus update` también aplica lo que hayas editado a mano en `skilus.yaml`: instala en los agentes detectados las skills que añadas a `skills:`, retira las que quites (con las dependencias que ya nadie necesite) y descarga del nuevo origen o ref las que cambies; con un perfil activo no instala las que el perfil deja fuera. Hasta entonces, `skilus verify` avisa de la diferencia y `skilus update --check` la muestra.

Un perfil agrupa skills ya declaradas en `skills:` de `skilus.yaml` y, si quieres, los agentes donde van:

```yaml
profiles:
  backend:
    skills: [systematic-debugging, mcp-builder]
    agents: [claude-code]
  web:
    skills: [systematic-debugging, web-design-guidelines]   # sin agents: los detectados
```

No hace falta editar el YAML a mano: `skilus profile create web --skill systematic-debugging,web-design-guidelines` declara el perfil (`--agent` fija sus agentes), `skilus profile add` y `skilus profile remove` añaden o quitan skills y, con `--agent`, agentes, y `skilus profile delete` lo borra. Estos comandos solo editan `profiles:`, conservan los comentarios y el orden del fichero y no instalan ni retiran nada; las skills tienen que estar ya en `skills:` (añádelas antes con `skilus add`) y los agentes, en el catálogo.

`skilus profile use web` instala las skills del perfil que faltan, desde el origen que registra `skills:` y con la misma inspección que `add` (`allow: [scripts]` en esa entrada cuenta como `--allow-scripts`); mueve a los agentes del perfil las que ya estaban, reutilizando el contenido fijado en el lock; y retira de los agentes y del lock las que el perfil no incluye. Primero instala y solo después retira, y `skilus.yaml` no cambia, así que volver a `backend` reinstala exactamente lo mismo. El perfil activo no se guarda: es aquel cuyas skills coinciden con las del lock.

En una terminal, `skilus add` sin `--skill` te deja marcar qué skills del origen instalar y, sin `--agent`, en qué agentes de los instalados en tu equipo; los que comparten carpeta (en un proyecto, Codex, Cursor, Gemini CLI y otros usan `.agents/skills`) salen como una sola opción y reciben una sola copia. Las flechas mueven, la barra espaciadora marca, → marca todo, ← nada, al escribir se filtra y Esc cancela. Con `-y`, o si la entrada o la salida no son una terminal, no pregunta y usa todas las skills y todos los agentes detectados. `skilus agents` lista los agentes detectados en tu equipo, con sus rutas, y `skilus agents --all` los 74 que conoce; la tabla completa está también en la web.

Para añadir un agente que skilus no trae, o cambiar las rutas de uno, crea `~/.skilus/agents.yaml` con el formato del catálogo incluido (`internal/adapter/catalog/agents.yaml`): un id que ya existe reemplaza a ese agente y uno nuevo se añade al final.

```yaml
version: 1
agents:
  - id: acme
    name: Acme Agent
    project_dir: .acme/skills
    global_dir: ~/.acme/skills
    detect: ~/.acme
```

Solo se lee el de tu directorio personal: un repositorio clonado no puede decidir dónde escribe skilus fuera del proyecto.

`skilus search` consulta [skills.sh](https://skills.sh) y los índices curados que declara `indexes:` en `skilus.yaml` (del proyecto y el global). Un índice es un fichero YAML, por https o `file://`, que un equipo mantiene con las skills que recomienda:

```yaml
version: 1
skills:
  - name: pdf
    source: anthropics/skills@v1.0.0
    description: Lee y crea PDF
```

Los resultados de los índices salen primero, y con una lista `trust:` cada uno dice si su origen es de confianza. Si un origen no responde se avisa y se muestran los demás; `--no-registry` deja solo los índices y `SKILUS_REGISTRY_URL` apunta a otro servidor compatible (solo https). `search` no instala nada: muestra el `skilus inspect` con el que revisar el resultado antes del `add`.

`skilus audit` revisa cada skill de `skilus.lock` con las reglas de hoy:
- Si su origen está en `trust:`.
- Si el commit fijado está firmado (GPG, SSH o x509, que incluye gitsign de Sigstore) y si la firma se verifica. Primero se intenta con `git verify-commit`, es decir, con tus claves, tus `allowedSignersFile` y tu `gpg.x509.program`. Si eso no basta y el repositorio está en GitHub, se pregunta a GitHub, que conoce las claves que registran sus usuarios. Solo se descarga el objeto del commit, y `GITHUB_TOKEN`, si existe, se usa únicamente para subir el límite de la API.
- Además vuelve a inspeccionar el contenido del almacén, por si las heurísticas han cambiado desde que se instaló. Los ejecutables que ya aceptaste no se repiten.

Un commit sin firma, o un ZIP o tar.gz, que nunca la llevan, da un aviso `unsigned-commit`. Con `--strict` los avisos hacen fallar la auditoría con código 5.

Una skill puede necesitar otras. Lo declara en el frontmatter de su `SKILL.md`, bajo `metadata`, que el formato de Agent Skills reserva para datos propios de cada herramienta y que los agentes ignoran:

```yaml
---
name: report
description: Genera informes en PDF y Word
metadata:
  requires: pdf anthropics/skills#docx   # o una lista YAML
---
```

Cada entrada es un nombre, que se busca en el mismo origen que la skill, u `origen#nombre` para otro origen (no un directorio local). `skilus add` instala las que falten en los mismos agentes, con la misma inspección y comprobación de `trust:`, y las enseña en el plan. En `skilus.lock` cada skill guarda qué necesita y las instaladas así quedan marcadas como dependencia; en `skilus.yaml` solo se anota lo que pediste. Una necesidad se da por cumplida si ya hay instalada una skill con ese nombre, venga de donde venga. `skilus remove` no quita una skill que otra necesita salvo que quites ambas, y al quitar la última que necesitaba una dependencia, la quita también. `skilus update` instala lo que pida la versión nueva y `skilus profile use` despliega las dependencias de las skills del perfil.

skilus escribe en el idioma del sistema si es uno de los 13 de la documentación (español, inglés, francés, alemán, portugués, chino, japonés, indonesio, árabe, ruso, polaco, urdu e hindi). Lo lee de `LANGUAGE`, `LC_ALL`, `LC_MESSAGES` y `LANG`, en ese orden, y en Windows del idioma del usuario; si ninguno es uno de esos, usa el inglés. `skilus lang set <idioma>` lo fija en `~/.skilus/config.yaml` para tu usuario y `skilus lang set auto` lo quita. La variable `SKILUS_LANG` manda sobre todo lo demás, útil en CI. Se traducen los mensajes, las preguntas, la ayuda y los errores, incluidos los de la línea de comandos y los detalles de los hallazgos; no cambian las claves y los valores de estado de `--json`, los códigos de los hallazgos ni el texto que viene de git, del sistema operativo o de una biblioteca. En el código, todo error que pueda leer el usuario se crea con `domain.Errorf` o `domain.NewError` y el texto en español, que es el identificador del mensaje: el linter rechaza `fmt.Errorf` y `errors.New` fuera de la CLI y una prueba comprueba que los 12 catálogos traducen cada mensaje.

Por defecto `sync` trabaja en el proyecto y `verify` en ambos ámbitos; los dos aceptan `--scope project|global|all`.

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

`test/gate/run.sh` contiene las puertas de las fases 1 a 4: instala 12 skills reales de cuatro repositorios públicos (uno de ellos, openai/skills, con las skills en `skills/.curated/`), fijadas a commits, y comprueba que el lock coincide con `test/gate/skilus.lock`; después borra los agentes y el almacén, comprueba que `sync` lo reconstruye y que `verify` detecta un fichero cambiado a mano; por último, en un repositorio que solo tiene `skilus.yaml` con un perfil backend y otro web, despliega backend y pasa a web con un solo `skilus profile use web`; y comprueba que `search` encuentra una skill real en skills.sh, que `audit` verifica las firmas de las 12 skills y que falla con una skill sin firma y fuera de `trust:`. La CI la ejecuta en Linux, macOS y Windows. Si cambia el formato del lock, regenera la referencia con `GATE_UPDATE=1 test/gate/run.sh bin/skilus`.

Las reglas completas están en el plan del proyecto, sección "Reglas de arquitectura", y `.golangci.yml` las hace cumplir.

## Licencia

[PolyForm Noncommercial 1.0.0](LICENSE). El uso comercial requiere una licencia comercial: ver [COMMERCIAL.md](COMMERCIAL.md).
