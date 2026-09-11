# hello

The sample service of the [BACK lab](https://github.com/hvpaiva/back): a status page that shows which version is running, in which environment, and on which pod.

It's styled as a hang tag: yellow in dev, green in prod. The big line is the version, and the barcode is drawn from it, so a new version looks different at a glance. The page reloads by itself when a new version answers, which makes a rollout visible without touching the browser.

## How it ships

1. A push to `main` runs the tests, builds the image and publishes it as `ghcr.io/hvpaiva/back-hello:sha-<commit>`.
2. The same workflow commits that tag to `apps/hello/dev/values.yaml` in [back-gitops](https://github.com/hvpaiva/back-gitops).
3. Argo CD sees the commit and rolls out the new version in the `hello-dev` namespace, at http://hello.dev.localhost.

CI never touches the cluster. Promotion to prod is a pull request in back-gitops.

## Run it locally

```sh
mise install     # Go and just, pinned in mise.toml
just run         # http://localhost:8080, as the grey "local" tag
just run dev     # preview the dev tag (or prod)
just test
```

## Interface

| | |
|---|---|
| `GET /` | The status page |
| `GET /api/info` | The same facts as JSON |
| `GET /healthz` | `ok`, for the liveness and readiness probes |

It listens on `PORT` (8080 by default) and reads `APP_ENV`, plus `POD_NAME`, `POD_NAMESPACE` and `NODE_NAME`, which the platform's chart fills in from the Kubernetes downward API.

The font is Barlow Condensed, under the SIL Open Font License (`web/fonts/OFL.txt`).
