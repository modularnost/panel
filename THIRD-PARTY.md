# Third-party licenses

modularnost is Apache-2.0 (see [LICENSE](LICENSE)). This lists what it links
against and under which license.

Regenerate after changing dependencies:

```sh
scripts/licenses.sh
```

## Go modules compiled into the binary

30 modules: 16 Apache-2.0, 7 BSD-3-Clause, 6 MIT, 1 BSD-2-Clause. No copyleft,
nothing that restricts redistributing the binary or the image.

| Module | Version | License |
| --- | --- | --- |
| github.com/Microsoft/go-winio | v0.6.2 | MIT |
| github.com/cespare/xxhash/v2 | v2.3.0 | MIT |
| github.com/containerd/errdefs | v1.0.0 | Apache-2.0 |
| github.com/containerd/errdefs/pkg | v0.3.0 | Apache-2.0 |
| github.com/distribution/reference | v0.6.0 | Apache-2.0 |
| github.com/docker/docker | v28.3.2+incompatible | Apache-2.0 (ships a NOTICE, carried in [NOTICE](NOTICE)) |
| github.com/docker/go-connections | v0.5.0 | Apache-2.0 |
| github.com/docker/go-units | v0.5.0 | Apache-2.0 |
| github.com/dustin/go-humanize | v1.0.1 | MIT |
| github.com/felixge/httpsnoop | v1.1.0 | MIT |
| github.com/go-logr/logr | v1.4.4 | Apache-2.0 |
| github.com/go-logr/stdr | v1.2.2 | Apache-2.0 |
| github.com/gogo/protobuf | v1.3.2 | BSD-3-Clause |
| github.com/mattn/go-isatty | v0.0.24 | MIT |
| github.com/moby/docker-image-spec | v1.3.1 | Apache-2.0 |
| github.com/ncruces/go-strftime | v1.0.0 | MIT |
| github.com/opencontainers/go-digest | v1.0.0 | Apache-2.0 |
| github.com/opencontainers/image-spec | v1.1.1 | Apache-2.0 |
| github.com/pkg/errors | v0.9.1 | BSD-2-Clause |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748 | BSD-3-Clause |
| go.opentelemetry.io/auto/sdk | v1.2.1 | Apache-2.0 |
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | v0.70.0 | Apache-2.0 |
| go.opentelemetry.io/otel | v1.45.0 | Apache-2.0 |
| go.opentelemetry.io/otel/metric | v1.45.0 | Apache-2.0 |
| go.opentelemetry.io/otel/trace | v1.45.0 | Apache-2.0 |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause |
| modernc.org/libc | v1.74.4 | BSD-3-Clause |
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause |
| modernc.org/memory | v1.11.0 | BSD-3-Clause |
| modernc.org/sqlite | v1.57.0 | BSD-3-Clause |

Most of that list is pulled in by two direct dependencies: the Docker client SDK
and the pure-Go SQLite driver. The Go standard library covers the rest — HTTP,
templates, password hashing and crypto are all stdlib.

## Served to the browser

| | License |
| --- | --- |
| [htmx](https://htmx.org) 2.0.4, bundled at `internal/web/static/htmx.min.js` | Zero-Clause BSD — no attribution required, listed for completeness |

## Inside the container image

The published image is not just the binary:

| | License |
| --- | --- |
| Alpine Linux base and its packages | mostly MIT/BSD; musl is MIT |
| `docker-cli`, installed for `docker stack deploy` | Apache-2.0 |

Each carries its own license texts inside the image, at
`/usr/share/licenses` and in the apk database. This file does not reproduce
them: redistributing the image redistributes those files along with it.

## The examples

`examples/hello-module` and `examples/metrics-module` are part of this
repository and covered by its license. They pull in nothing beyond the Go
standard library.
