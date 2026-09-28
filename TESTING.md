# Testing

**English** | [Русский](TESTING.ru.md)

Developed by **[BURN-LAB](https://burn-lab.ru)** — embedded software
development: Linux, drivers, CAN and industrial telemetry.

## Unit tests

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

The module directive is `go 1.24.0`; the CI checks **1.24.x, 1.25.x and
1.26.x**. Run a specific toolchain locally with `GOTOOLCHAIN`:

```sh
GOTOOLCHAIN=go1.25.11 go test -race ./...
GOTOOLCHAIN=local go test ./...        # strictly the local 1.24 toolchain
```

## Fuzzing

The decoders and the parser carry fuzz targets; they run in the nightly CI
(2 minutes each) and can be run locally:

```sh
go test -run='^$' -fuzz='^FuzzSplit$' -fuzztime=30s
go test -run='^$' -fuzz='^FuzzValidateRaw$' -fuzztime=30s
go test -run='^$' -fuzz='^FuzzDecoder$' -fuzztime=30s
```

Targets: `FuzzSplit`, `FuzzEncodeSplit`, `FuzzFrameUnmarshal`,
`FuzzValidateRaw`, `FuzzDecoder`, `FuzzDecodeFrameInto`.

## Test vectors and the canon

The protocol canon lives in
[cantcp-spec](https://github.com/burn-lab-dev/cantcp-spec); this repository
keeps a synced copy in `testdata/vectors.json` with `testdata/vectors.sha256`
and checks it in CI:

```sh
scripts/sync_vectors.sh --check        # against the sibling checkout
scripts/sync_vectors.sh --check --url https://raw.githubusercontent.com/burn-lab-dev/cantcp-spec/main/vectors.json
(cd testdata && sha256sum -c vectors.sha256)
```

## End-to-end

The library itself does not open sockets; the end-to-end scenarios (a
`cantcpd` daemon on a virtual CAN bus, the Go and Python clients) live in the
[cantcp](https://github.com/burn-lab-dev/cantcp) repository:
`scripts/vcan-smoke.sh` and [its TESTING.md](https://github.com/burn-lab-dev/cantcp/blob/main/TESTING.md).

## CI

| Job | What it runs |
|---|---|
| `test` | gofmt, vet, `go test -race` on 1.24.x, 1.25.x, 1.26.x |
| `canon` | the vectors copy against `cantcp-spec` + the local checksum |
| `fuzz` | the six targets, nightly, 2 minutes each |
