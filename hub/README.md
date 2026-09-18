# hub

The community hub service, `b4hub`: its own Go module (`github.com/daniellavrushin/b4hub`,
`replace ../src`), never part of the router binary. `cmd/b4hub` is the CLI (`serve`, `mirror`,
`build`, `moderate`, `keygen`, `version`), `internal/` the service, `ui/` the moderation
console, `Dockerfile` the image (build context is the repository root).

Operator documentation, the deployment bundle (Docker Compose, systemd unit, nginx vhost) and
the releases live in the [b4hub repository](https://github.com/DanielLavrushin/b4hub). The hub
is versioned on its own; `CHANGELOG.md` here is the source of its release notes, and the
release workflow over there builds a chosen commit of this repository.

Local development, from the repository root:

```sh
make hub-test                          # go test ./... in hub/
make hub-run                           # console + binary, serves on 0.0.0.0:7100 with hub/data
make hub-build HUB_VERSION=1.0.0       # binary into out/b4hub; HUB_VERSION defaults to dev
make hub-deploy HUB_VERSION=1.0.1      # cross-compile, install and restart on HUB_DEPLOY_HOST from .env
```

`make hub-run` needs a key once (`make hub-keygen`) and `B4HUB_ADMIN_PASSWORD` in the
environment for the console.
