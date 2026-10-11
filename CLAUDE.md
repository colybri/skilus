# CLAUDE.md

Guía para trabajar en skilus, el gestor de skills para agentes de IA escrito en Go. Lo que un usuario necesita saber está en el [README](README.md) y en la web (`docs/`). Aquí van las convenciones del código.

## Comandos

```sh
make test     # go test -race ./...
make lint     # golangci-lint v2: capas (depguard), errores traducibles (forbidigo), formato
make build    # bin/skilus
go test ./cmd/skilus -run 'TestScripts/edit$'      # un solo script e2e
$(go env GOPATH)/bin/goimports -local github.com/colybri/skilus -w <ficheros>
```

Antes de dar un cambio por terminado: `gofmt -l`, `make lint`, `make test` y `GOOS=windows go build ./...`. La CI ejecuta además `test/gate/run.sh bin/skilus` en Linux, macOS y Windows; necesita red y repositorios reales, así que en local no suele funcionar.

## Arquitectura

DDD hexagonal adaptado a Go idiomático. Las dependencias apuntan hacia dentro y `.golangci.yml` (depguard) lo hace cumplir:

- `internal/domain/`: modelo y objetos valor (`skill`, `source`, `lock`, `profile`, `agent`, `policy`). Solo la librería estándar, sin E/S. Los constructores validan.
- `internal/app/`: casos de uso (`XxxHandler` con un método por comando) y puertos en `ports.go`. Los puertos los declara quien los usa. Sin `os/exec`.
- `internal/adapter/`: implementaciones de los puertos (`yamlrepo`, `gitsrc`, `archivesrc`, `osfs`, `catalog`, `searchsrc`, `skillsrc`, `editor`). No dependen de la CLI ni de Cobra.
- `internal/cli/`: comandos Cobra. Solo habla con `app`; nunca importa adaptadores.
- `cmd/skilus/main.go`: raíz de composición, donde se conectan los adaptadores.

No hay bus de mensajes, contenedor de inyección ni variables globales. `context.Context` va siempre como primer argumento. En los tests de `app` se usan dobles en memoria, no mocks generados.

## Errores, códigos de salida e idiomas

- Todo error que pueda leer el usuario se crea con `domain.Errorf`, `domain.NewError` o `domain.Msg`, con el texto en español: ese texto es el id del mensaje. forbidigo rechaza `fmt.Errorf` y `errors.New` fuera de `internal/cli`, `cmd` y los tests.
- Se envuelven los centinelas de `internal/domain/errors.go` (`ErrInvalid`, `ErrNotFound`, `ErrAlreadyExists`, `ErrConflict`) o `app.ErrRejected` y `app.ErrDrift`. Solo `internal/cli/cli.go` los traduce a códigos: 2 inválido, 3 no encontrado, 4 conflicto, 5 rechazado, 6 discrepancia con el lock.
- Los textos de la CLI pasan por `t.T("…")` con un literal en español. Cada id nuevo se añade a los 12 catálogos de `internal/cli/i18n/locales/*.json` (todos menos `es`). `TestCatalogsTranslateEveryMessage` falla si falta uno, si sobra o si no coinciden los verbos, tabuladores o el salto de línea final. Si una traducción cambia el orden de los argumentos, usa verbos indexados (`%[2]q`).
- Al añadir entradas a los catálogos se conserva el orden del fichero y su formato (`json.dumps(..., ensure_ascii=False, indent=2)` y salto de línea final).

## Ficheros del usuario

- `skilus.yaml` (la intención) y `skilus.lock` (el resultado) están en la raíz del proyecto o, en ámbito global, en `~/.skilus/`. Ahí también están `config.yaml`, `agents.yaml` y `store/`.
- `yamlrepo` edita `skilus.yaml` sobre `yaml.Node` para conservar comentarios y orden, normaliza CRLF al leer y lo restaura al escribir. Las escrituras son atómicas (`writeAtomic`).
- No se guardan nunca tokens ni credenciales: Git se usa con el binario `git` del sistema y sus credenciales.

## Tests

- Unitarios junto al código (`*_test.go`); e2e con testscript en `cmd/skilus/testdata/script/*.txtar`. Los scripts comprueban los textos en español (`SKILUS_LANG=es`).
- `cmd/skilus/main_test.go` registra los comandos `skilus` y `fakeedit` (un editor falso para `skilus edit`). testscript no expande variables entre comillas simples y en Windows el editor se ejecuta sin shell, así que `fakeedit` expande `$WORK` por su cuenta.
- Los scripts que usan el directorio personal fijan `HOME` y, para Windows, `USERPROFILE`. Las rutas se comprueban con `[/\\]` para que valgan en los tres sistemas.
- Nunca se desactiva ni se salta un test para que la CI pase.

## Web de documentación (`docs/`)

Página estática con 13 idiomas: es, en, fr, de, pt, zh, ja, id, ar, ru, pl, ur y hi.

- `docs/content.js` tiene `order`, `nav`, los títulos (`T`, por posición en `order`) y las páginas en español e inglés. `docs/i18n/<idioma>.js` tiene las demás. Los bloques y los fragmentos compartidos están en `docs/i18n/shared.js`.
- Una página nueva va en `order` y `nav`, en los arrays de `T` de los 11 idiomas (en la misma posición), en las 13 páginas y en las `cards` de la guía de usuario.
- Los enlaces internos son `[texto](id-de-página)`. Dentro de celdas de `table` no se muestran, así que van en un párrafo.
- Las rutas de Windows en cadenas JS llevan la barra invertida escapada (`C:\\Users\\…`).
- Para revisarla, se sirve `docs/` con `python3 -m http.server` y se abre `#/<idioma>/<página>`.

## Estilo

- Comentarios y nombres en inglés; textos para el usuario, README y web en español (y traducidos).
- Sin compatibilidad hacia atrás que mantener: el formato de los ficheros puede cambiar si hace falta.
- La licencia es PolyForm Noncommercial 1.0.0; no se añaden dependencias con licencias incompatibles.
