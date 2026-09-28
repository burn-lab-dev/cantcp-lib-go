# Тестирование

[English](TESTING.md) | **Русский**

Разрабатывается **[BURN-LAB](https://burn-lab.ru)** — встраиваемый Linux:
драйверы, CAN и промышленная телеметрия.

## Юнит-тесты

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

Директива модуля — `go 1.24.0`; CI проверяет **1.24.x, 1.25.x и 1.26.x**.
Локально конкретный тулчейн задаётся `GOTOOLCHAIN`:

```sh
GOTOOLCHAIN=go1.25.11 go test -race ./...
GOTOOLCHAIN=local go test ./...        # строго локальный тулчейн 1.24
```

## Fuzzing

На декодерах и парсере есть fuzz-цели; они идут в ночном CI (по 2 минуты) и
запускаются локально:

```sh
go test -run='^$' -fuzz='^FuzzSplit$' -fuzztime=30s
go test -run='^$' -fuzz='^FuzzValidateRaw$' -fuzztime=30s
go test -run='^$' -fuzz='^FuzzDecoder$' -fuzztime=30s
```

Цели: `FuzzSplit`, `FuzzEncodeSplit`, `FuzzFrameUnmarshal`,
`FuzzValidateRaw`, `FuzzDecoder`, `FuzzDecodeFrameInto`.

## Тестовые векторы и канон

Канон протокола живёт в
[cantcp-spec](https://github.com/burn-lab-dev/cantcp-spec); этот репозиторий
держит синхронизированную копию в `testdata/vectors.json` с
`testdata/vectors.sha256` и сверяет её в CI:

```sh
scripts/sync_vectors.sh --check        # против соседнего чекаута
scripts/sync_vectors.sh --check --url https://raw.githubusercontent.com/burn-lab-dev/cantcp-spec/main/vectors.json
(cd testdata && sha256sum -c vectors.sha256)
```

## End-to-end

Библиотека сама сокеты не открывает; сквозные сценарии (демон `cantcpd` на
виртуальной шине CAN, Go- и Python-клиенты) живут в репозитории
[cantcp](https://github.com/burn-lab-dev/cantcp): `scripts/vcan-smoke.sh` и
[его TESTING.md](https://github.com/burn-lab-dev/cantcp/blob/main/TESTING.ru.md).

## CI

| Джоба | Что запускает |
|---|---|
| `test` | gofmt, vet, `go test -race` на 1.24.x, 1.25.x, 1.26.x |
| `canon` | копию векторов против `cantcp-spec` + локальную контрольную сумму |
| `fuzz` | шесть целей, ночью, по 2 минуты |
