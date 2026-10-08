# skilus

Gestor de skills (`SKILL.md`) para agentes de IA, en un único binario Go. Instala skills desde Git, URL o un directorio local en Claude Code, Codex, Cursor, GitHub Copilot y `.agents/skills`, y garantiza que lo instalado es exactamente lo que se revisó: cada skill se fija a un commit, se le calcula un hash de contenido y se inspecciona antes de instalarse.

> Estado: fase 1 en construcción. Hoy funciona `skilus agents`.

## Uso

```sh
skilus agents          # agentes soportados, sus rutas y si están instalados
skilus agents --json
```

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

Las reglas completas están en el plan del proyecto, sección "Reglas de arquitectura", y `.golangci.yml` las hace cumplir.

## Licencia

[PolyForm Noncommercial 1.0.0](LICENSE). El uso comercial requiere una licencia comercial: ver [COMMERCIAL.md](COMMERCIAL.md).
