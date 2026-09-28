# cantcp-lib-go

[![CI](https://github.com/burn-lab-dev/cantcp-lib-go/actions/workflows/ci.yml/badge.svg)](https://github.com/burn-lab-dev/cantcp-lib-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/burn-lab-dev/cantcp-lib-go.svg)](https://pkg.go.dev/github.com/burn-lab-dev/cantcp-lib-go)
[![Go version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](go.mod)

Go-библиотека протокола **cantcp**: упаковка и распаковка фреймов Linux
SocketCAN, потоковая разметка с CRC-8, плюс модель разобранного CAN / CAN FD
фрейма.

> **Статус: WIP.** API не стабилизирован, протокол в разработке (`v0`).

Документация на английском: [README.md](README.md).

Разрабатывается в **[BURN-LAB](https://burn-lab.ru)** — разработка
встраиваемого ПО: Linux, драйверы, CAN и промышленная телеметрия.

## Что входит в пакет

Пакет гоняет только кадры CAN: потоковая разметка, разбор кадра и кодек.
Соединение — обычный TCP: handshake, подписки, keepalive, статистика и health
не входят в протокол. Сервер отдаёт их по своему API (например, HTTP), а
клиенты забирают по необходимости.

## Потоковая разметка

`Split` разбирает поток байт из пакетов:

```
[magic 2 байта][type 1 байт][can_frame 16 | canfd_frame 72][CRC-8 1 байт]
```

Байт типа пакета выбирает раскладку фрейма:

| Значение | Константа | Фрейм | Размер |
|---|---|---|---|
| `0x01` | `TypeClassic` | `struct can_frame` | 16 |
| `0x02` | `TypeFd` | `struct canfd_frame` | 72 |

Классический фрейм (`can_frame`):

| Смещение | Размер | Поле |
|---|---|---|
| 0 | 4 | `can_id` (little-endian) |
| 4 | 1 | `can_dlc` |
| 5 | 1 | `__pad` (обязан быть нулём) |
| 6 | 1 | `__res0` (обязан быть нулём) |
| 7 | 1 | `__res1` (обязан быть нулём) |
| 8 | 8 | `data` |

Фрейм CAN FD (`canfd_frame`):

| Смещение | Размер | Поле |
|---|---|---|
| 0 | 4 | `can_id` (little-endian) |
| 4 | 1 | `len` |
| 5 | 1 | `flags` (`BRS` 0x01, `ESI` 0x02; маркер `CANFD_FDF` 0x04 от ядра принимается и игнорируется) |
| 6 | 1 | `__res0` (обязан быть нулём) |
| 7 | 1 | `__res1` (обязан быть нулём) |
| 8 | 64 | `data` |

Reserved-байты участвуют в CRC: отправитель обязан писать туда нули. По
умолчанию CRC покрывает `magic+type+frame`, `WithCRCCoverFrameOnly` — только
сырой фрейм.

`Split` возвращает сырой фрейм токеном: 16 байт для классического CAN, 72
байта для CAN FD. Токен — окно в буфер `bufio.Scanner`, валиден только до
следующего вызова `Scan`; для хранения используйте `bytes.Clone`.

## Быстрый старт

Чтение декодером:

```go
d := cantcp.NewDecoder(conn,
	cantcp.WithLogger(slog.Default()),
	cantcp.WithLogLevel(cantcp.LevelTrace),
)
for {
	f, err := d.DecodeFrame()
	switch {
	case errors.Is(err, io.EOF): // участник закрыл поток
		return
	case errors.Is(err, net.ErrClosed): // наш shutdown закрыл соединение
		return
	case err != nil:
		log.Println("decode:", err) // ErrTruncated, ErrBadType, ...
		return
	}
	use(f)
}
log.Printf("skipped=%d dropped=%d", d.Stats().Skipped, d.Stats().Dropped)
```

Запись энкодером:

```go
e := cantcp.NewEncoder(conn)
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
if err := e.EncodeFrame(&f); err != nil {
	log.Println("encode:", err)
}
```

Низкоуровневые `Split` / `Encode` остаются для своих читателей и писателей;
см. справочник API ниже.

# Справочник API

API пакета состоит из трёх видов объектов и функций:

- `New` возвращает **парсер** (тип не экспортируется), настраивается
  функциями-настройками `With...` и используется через свои публичные методы;
- `NewDecoder` и `NewEncoder` возвращают **декодер** и **энкодер** (их типы
  тоже не экспортируются), используются через свои публичные методы;
- `Frame` — публичный объект разобранного фрейма; `Type`, `Stats`,
  `BadFramePolicy`, `Logger`, константы `Flag...` и sentinel-ошибки `Err...`
  дополняют поверхность;
- просто функции: `ValidateRaw`.

## `New` — парсер

```go
func New(opts ...option) *parser
```

Создаёт парсер; сам тип не экспортируется, поэтому парсер получают только
через `New` и настраивают только экспортированными функциями `With`.
Значения по умолчанию: magic `0xC3 0x3C`, полином CRC-8 `0x07` с покрытием
`magic+type+frame`, политика `BadFrameSkip`, логирование выключено на уровне
`slog.LevelInfo`.

```go
p := cantcp.New(cantcp.WithLogLevel(cantcp.LevelTrace))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
advance, _, _ := p.Split(pkt, true) // 20
```

### Настройки парсера

Функции-настройки ниже конфигурируют `New`; те же настройки применяются к
`NewDecoder` и `NewEncoder`, поэтому пакет, записанный с одним набором
настроек, всегда принимается читателем с тем же набором.

| Настройка | По умолчанию | Описание |
|---|---|---|
| `WithMagic(hi, lo byte)` | `0xC3 0x3C` | Двухбайтовый заголовок пакета |
| `WithCRCPoly(poly byte)` | `0x07` (SMBus) | Полином CRC-8, перестраивает таблицу |
| `WithCRCCoverFrameOnly()` | выкл. | CRC покрывает только сырой фрейм |
| `WithBadFramePolicy(policy)` | `BadFrameSkip` | Реакция на некорректный пакет |
| `WithLogger(l Logger)` | `nil` | Логгер; `nil` выключает логирование |
| `WithLogLevel(level slog.Level)` | `slog.LevelInfo` | Минимальный уровень для лога |

#### `WithMagic(hi, lo byte)`

```go
func WithMagic(hi, lo byte) option
```

Задаёт двухбайтовый заголовок пакета вместо значения по умолчанию
`0xC3 0x3C`. Magic всегда ровно два байта.

```go
p := cantcp.New(cantcp.WithMagic(0x11, 0x22))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
fmt.Printf("%x\n", pkt[:2]) // 1122
```

#### `WithCRCPoly(poly byte)`

```go
func WithCRCPoly(poly byte) option
```

Задаёт полином CRC-8 (по умолчанию `0x07`) и перестраивает таблицу парсера.

```go
p := cantcp.New(cantcp.WithCRCPoly(0x1D))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
advance, token, _ := p.Split(pkt, true) // 20, token == frame
```

#### `WithCRCCoverFrameOnly()`

```go
func WithCRCCoverFrameOnly() option
```

Заставляет CRC покрывать только сырой фрейм вместо `magic+type+frame` (по
умолчанию).

```go
p := cantcp.New(cantcp.WithCRCCoverFrameOnly())
frame := make([]byte, 72)
pkt, _ := p.Encode(nil, frame)
advance, token, _ := p.Split(pkt, true) // 76, token == frame
```

#### `WithBadFramePolicy(policy)`

```go
func WithBadFramePolicy(policy BadFramePolicy) option
```

Выбирает, как `Split` реагирует на неизвестный байт типа пакета или плохое
поле длины фрейма (`can_dlc > 8`, `canfd len > 64`); см. `BadFramePolicy`.

```go
p := cantcp.New(cantcp.WithBadFramePolicy(cantcp.BadFrameFail))
_, _, err := p.Split([]byte{0xC3, 0x3C, 0x7F}, false)
fmt.Println(err) // cantcp: unknown frame type
```

#### `WithLogger(l Logger)`

```go
func WithLogger(l Logger) option
```

Задаёт логгер. `nil` (значение по умолчанию) выключает логирование.
`*slog.Logger` реализует `Logger` напрямую.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()))
```

#### `WithLogLevel(level slog.Level)`

```go
func WithLogLevel(level slog.Level) option
```

Задаёт минимальный уровень для лога (по умолчанию `slog.LevelInfo`).
Используйте `LevelTrace`, чтобы трассировать каждый разобранный фрейм,
пропуск мусора, несовпадение CRC и отклонённый тип пакета.

> **Внимание:** `LevelTrace` пишет в лог полный hex сырого кадра, **включая
> данные (payload)** — телеметрию и команды с техники. Это чувствительные
> данные: трассировка предназначена только для отладки на изолированном
> стенде. Не включайте `LevelTrace` в проде, а логи с трассировкой считайте
> чувствительными.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()), cantcp.WithLogLevel(slog.LevelWarn))
// трассировка по каждому фрейму выключена, пишутся только предупреждения
```

### Методы парсера

#### `Split`

```go
func (p *parser) Split(data []byte, atEOF bool) (advance int, token []byte, err error)
```

`bufio.SplitFunc` для потоков классических пакетов и пакетов CAN FD.
Отбрасывает мусор, ресинхронизируется при несовпадении CRC и возвращает
сырой фрейм (16 или 72 байта по байту типа). `BadFrameSkip` считает и
отбрасывает неизвестный байт типа, `can_dlc > 8` или `canfd len > 64`;
`BadFrameFail` возвращает `ErrBadType`, `ErrBadDLC` или `ErrBadLen`. Пакет,
обрезанный на EOF, возвращает `ErrTruncated`.

```go
sc := bufio.NewScanner(conn)
sc.Split(p.Split)
for sc.Scan() {
	raw := bytes.Clone(sc.Bytes()) // 16 или 72 байта, независимая копия
	handleRaw(raw)
}
```

#### `Encode`

```go
func (p *parser) Encode(dst, frame []byte) ([]byte, error)
```

Дописывает в `dst` пакет с сырым фреймом. Длина фрейма выбирает тип: 16
байт — пакет `TypeClassic`, 72 байта — пакет `TypeFd`. Любая другая длина
возвращает `ErrFrameLen`; `can_dlc > 8` возвращает `ErrBadDLC`, `canfd
len > 64` — `ErrBadLen`. `dst` не меняется при ошибке.

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
raw, _ := f.MarshalBinary()
pkt, _ := p.Encode(nil, raw) // 76 байт
```

#### `Stats`, `ResetStats`

```go
func (p *parser) Stats() Stats
func (p *parser) ResetStats()
```

`Stats` возвращает копию счётчиков с последнего сброса, `ResetStats`
обнуляет их. Поля счётчиков — в разделе `Stats`.

```go
_, _, _ = p.Split([]byte{0x01, 0x02}, false)
fmt.Println("skipped:", p.Stats().Skipped) // skipped: 2
p.ResetStats()
fmt.Println("after reset:", p.Stats().Skipped) // after reset: 0
```

## `NewDecoder` — декодер

```go
func NewDecoder(r io.Reader, opts ...option) *decoder
```

Возвращает декодер, читающий пакеты из любого `io.Reader`. Возвращаемый тип
не экспортируется, как и парсер у `New`: декодер используется через
возвращённое значение и его публичные методы. Настройки — те же, что у
`New`. Декодер не безопасен для конкурентного использования: один декодер на
поток.

```go
d := cantcp.NewDecoder(conn, cantcp.WithLogger(slog.Default()))
```

### Методы декодера

#### `Decode`

```go
func (d *decoder) Decode() ([]byte, error)
```

Возвращает следующий сырой фрейм: 16 байт для классического CAN, 72 байта
для CAN FD. Слайс — независимая копия, остаётся валидным после следующего
вызова.

Конец потока возвращает `io.EOF`; пакет, обрезанный в середине, —
`ErrTruncated`; ошибки чтения возвращаются без изменений. Закрытый
`net.Conn` проявляется как `net.ErrClosed`, поэтому аккуратное завершение
можно отличить и от чистого закрытия (`io.EOF`), и от обрезанного пакета
(`ErrTruncated`).

```go
d := cantcp.NewDecoder(conn)
for {
	raw, err := d.Decode()
	switch {
	case errors.Is(err, io.EOF):
		return
	case err != nil:
		log.Println("decode:", err)
		return
	}
	handleRaw(raw)
}
```

#### `DecodeFrame`

```go
func (d *decoder) DecodeFrame() (Frame, error)
```

Возвращает следующий фрейм, разобранный в `Frame`. Это удобная обёртка над
`DecodeFrameInto`: используйте `DecodeFrameInto`, чтобы разбирать в
принадлежащий вызывающему `Frame` и не аллоцировать на каждый фрейм.

Разметчик остаётся толерантным (`canfd len` до 64), а модель `Frame` —
строгой, поэтому `canfd len`, который не кодируется 4-битным полем DLC,
возвращается как `ErrBadLen`, хотя разметчик его принял.

Возвращённый фрейм независим от декодера: он владеет своим сырым хранилищем,
поэтому `Data` остаётся валидным после следующего вызова.

```go
d := cantcp.NewDecoder(conn)
for {
	f, err := d.DecodeFrame()
	if err != nil {
		break
	}
	use(f)
}
```

#### `DecodeFrameInto`

```go
func (d *decoder) DecodeFrameInto(f *Frame) error
```

Разбирает следующий фрейм в `f`, переиспользуя его хранилище. Вызывающий,
передающий в цикле один и тот же `f`, не аллоцирует на каждый фрейм: сырой
фрейм копируется прямо в собственное raw-хранилище `f`, поэтому окно `Data`
указывает в `f` и остаётся валидным после следующего вызова.

Ошибки возвращаются те же, что у `Decode`. При ошибке `f` не меняется
(строгий `UnmarshalBinary` фиксирует результат только после всех проверок),
поэтому вызывающий может сохранить предыдущее содержимое и читать `f` только
при `nil`-ошибке.

Разбор так же строг, как `DecodeFrame`. `f` нельзя копировать, пока `Data`
используется: `Frame` хранит сырые данные в значении, а копия продолжает
указывать в оригинал.

```go
d := cantcp.NewDecoder(conn)
var f cantcp.Frame
for {
	if err := d.DecodeFrameInto(&f); err != nil {
		break
	}
	use(&f)
}
```

#### `Stats`, `ResetStats`

```go
func (d *decoder) Stats() Stats
func (d *decoder) ResetStats()
```

`Stats` возвращает копию счётчиков потока декодера, `ResetStats` обнуляет
их. Поля счётчиков — в разделе `Stats`.

```go
d := cantcp.NewDecoder(conn)
if _, err := d.DecodeFrame(); err != nil {
	log.Println("decode:", err)
	return
}
fmt.Println("skipped:", d.Stats().Skipped)
d.ResetStats()
fmt.Println("after reset:", d.Stats().Skipped)
```

## `NewEncoder` — энкодер

```go
func NewEncoder(w io.Writer, opts ...option) *encoder
```

Возвращает энкодер, пишущий пакеты в любой `io.Writer`. Возвращаемый тип не
экспортируется, как и парсер у `New`: энкодер используется через возвращённое
значение и его публичные методы. Настройки — те же, что у `New`. Энкодер не
безопасен для конкурентного использования: один энкодер на поток.

```go
e := cantcp.NewEncoder(conn)
```

### Методы энкодера

#### `Encode`

```go
func (e *encoder) Encode(frame []byte) error
```

Пишет пакет с сырым фреймом: 16 байт — пакет `TypeClassic`, 72 байта —
пакет `TypeFd`. Ошибки валидации `Encode` (`ErrFrameLen`, `ErrBadDLC`,
`ErrBadLen`) не трогают поток и оставляют буфер пригодным для повторного
использования.

Короткая запись возвращает `io.ErrShortWrite`; ошибка записи возвращается
без изменений и оставляет поток в неизвестном состоянии.

```go
e := cantcp.NewEncoder(conn)
raw := make([]byte, 16) // struct can_frame
if err := e.Encode(raw); err != nil {
	log.Println("encode:", err)
}
```

#### `EncodeFrame`

```go
func (e *encoder) EncodeFrame(f *Frame) error
```

Маршалит `f` через `Frame.MarshalBinary` и пишет пакет. Ошибки маршалинга
(`ErrBadType`, `ErrBadID`, `ErrBadFlags`, `ErrBadDLC`, `ErrBadLen`) не трогают
поток.

```go
e := cantcp.NewEncoder(conn)
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
if err := e.EncodeFrame(&f); err != nil {
	log.Println("encode:", err)
}
```

## `Frame` — объект разобранного фрейма

```go
type Frame struct {
	ID   uint32
	Type Type
	EFF  bool
	RTR  bool
	ERR  bool
	BRS  bool
	ESI  bool
	Data []byte
}
```

`Frame` — разобранный фрейм CAN или CAN FD, независимый от потоковой
разметки cantcp: `MarshalBinary` и `UnmarshalBinary` работают только с
сырыми раскладками Linux SocketCAN (`struct can_frame`, 16 байт, и
`struct canfd_frame`, 72 байта). Копирование значения `Frame` копирует поля,
но `Data` продолжает указывать в raw-хранилище исходного объекта: для
независимой копии — `GetRaw`.

Публичные поля:

| Поле | Тип | Значение |
|---|---|---|
| `ID` | `uint32` | Идентификатор: 11-битный стандартный, 29-битный с `EFF`; error-фрейм несёт 29-битную маску классов ошибок |
| `Type` | `Type` | `TypeClassic` или `TypeFd`; выбирает раскладку фрейма |
| `EFF` | `bool` | Расширенный (29-битный) идентификатор |
| `RTR` | `bool` | Запрос передачи (только классический CAN) |
| `ERR` | `bool` | Error-фрейм |
| `BRS` | `bool` | CAN FD bit rate switch |
| `ESI` | `bool` | CAN FD error state indicator |
| `Data` | `[]byte` | Данные (payload); `len(Data)` — поле длины фрейма (`can_dlc` / `len`) |

### `Frame.UnmarshalBinary`

```go
func (f *Frame) UnmarshalBinary(b []byte) error
```

Разбирает сырой фрейм: 16 байт как `can_frame`, 72 байта как `canfd_frame`,
любая другая длина возвращает `ErrFrameLen`. Строго: `ErrBadDLC`,
`ErrBadLen` (длина CAN FD, не кодируемая 4-битным DLC: допустимы только
`0..8, 12, 16, 20, 24, 32, 48, 64`), `ErrReserved` (ненулевые
pad/reserved-байты), `ErrBadFlags` (`FlagRTR` в CAN FD, `FlagEFF` или
`FlagRTR` вместе с `FlagERR`, неизвестные биты байта флагов CAN FD; маркер
`CANFD_FDF` 0x04 от ядра принимается и игнорируется),
`ErrBadID` (идентификатор не входит в разрядность режима адресации;
error-фрейм несёт в `ID` 29-битную маску классов ошибок вместо 11-битного
идентификатора). При ошибке фрейм не меняется.

```go
raw := make([]byte, 16)     // struct can_frame
raw[0], raw[1] = 0x23, 0x01 // can_id = 0x123 (little-endian)
raw[4] = 2                  // can_dlc
copy(raw[8:], []byte{0xDE, 0xAD})
var f cantcp.Frame
err := f.UnmarshalBinary(raw)
fmt.Println(f) // Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
```

### `Frame.MarshalBinary`

```go
func (f *Frame) MarshalBinary() ([]byte, error)
```

Собирает сырой фрейм (16 байт для `TypeClassic`, 72 байта для `TypeFd`),
проверяет `Type`, ID, флаги и `len(Data)`, сохраняет результат во внутреннем
сыром фрейме и возвращает копию. Ошибки: `ErrBadType`, `ErrBadID` (11 бит для
стандартного фрейма, 29 — с `FlagEFF` или в error-фрейме), `ErrBadFlags`
(`FlagERR` с `FlagEFF` или `FlagRTR`, `FlagBRS`/`FlagESI` в классическом
CAN, `FlagRTR` в CAN FD), `ErrBadDLC`, `ErrBadLen`.

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
raw, err := f.MarshalBinary()
fmt.Println(len(raw), err)     // 72 <nil>
fmt.Printf("%x\n", raw[:6])    // 230100000201
```

### `Frame.SetFlags`

```go
func (f *Frame) SetFlags(flags uint8) error
```

Заменяет все флаги. Собирайте набор побитовым OR из констант `Flag`;
флаги, недопустимые для текущего `Type`, или неизвестные биты возвращают
`ErrBadFlags`, `FlagERR` вместе с `FlagEFF` или `FlagRTR` возвращает
`ErrBadFlags` (error-фрейм не несёт режима адресации), неустановленный или
неизвестный `Type` возвращает `ErrBadType`.

```go
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x123}
err := f.SetFlags(cantcp.FlagEFF | cantcp.FlagRTR)
fmt.Println(f) // Frame{Type:CAN, ID:0x123, Flags:EFF|RTR, DLC:0, Data:}
```

### `Frame.GetRaw`

```go
func (f *Frame) GetRaw() []byte
```

Возвращает копию сохранённого сырого фрейма: 16 байт для `TypeClassic`,
72 байта для `TypeFd`; неустановленный или неизвестный `Type` возвращает
`nil`.

```go
var f cantcp.Frame
if err := f.UnmarshalBinary(raw72); err != nil {
	log.Fatal(err)
}
fmt.Println(len(f.GetRaw())) // 72
```

### `Frame.String`

Реализует `fmt.Stringer`:

```go
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x1ABCDE, EFF: true, Data: []byte{0xDE, 0xAD}}
fmt.Println(f) // Frame{Type:CAN, ID:0x1abcde, Flags:EFF, DLC:2, Data:dead}
```

## `Type`

```go
type Type uint8

const (
	TypeClassic Type = 0x01 // can_frame, 16 байт
	TypeFd      Type = 0x02 // canfd_frame, 72 байта
)
```

Выбирает раскладку фрейма. Те же значения используются как байт типа пакета
потоковой разметки.

`Type.String()` возвращает `"CAN"`, `"CAN FD"` или `"unknown"`:

```go
fmt.Println(cantcp.TypeClassic, cantcp.TypeFd, cantcp.Type(0)) // CAN CAN FD unknown
```

## Флаги

Константы `Flag` описывают `Frame`; собирайте их побитовым OR и передавайте в
`Frame.SetFlags`. `FlagEFF`, `FlagRTR` и `FlagERR` приходят из слова
идентификатора CAN; `FlagBRS` и `FlagESI` — из байта флагов CAN FD. Значения
внутренние для объекта: `MarshalBinary` и `UnmarshalBinary` отображают их в
сырые битовые позиции SocketCAN и обратно.

| Константа | Сырой бит | Значение |
|---|---|---|
| `FlagEFF` | `can_id` 0x80000000 | расширенный (29-битный) идентификатор |
| `FlagRTR` | `can_id` 0x40000000 | запрос передачи (только classic) |
| `FlagERR` | `can_id` 0x20000000 | error-фрейм |
| `FlagBRS` | `canfd flags` 0x01 | CAN FD bit rate switch |
| `FlagESI` | `canfd flags` 0x02 | CAN FD error state indicator |

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
err := f.SetFlags(cantcp.FlagEFF | cantcp.FlagBRS)
fmt.Println(f, err) // Frame{Type:CAN FD, ID:0x123, Flags:EFF|BRS, Len:2, Data:0102} <nil>
```

## `Stats`

Объект счётчиков, возвращаемый `Stats()` парсера или декодера. Мусорный
поток может гнать счётчики без предела: они насыщаются на `math.MaxInt`, а не
переполняются.

```go
type Stats struct {
	Skipped   int // байты, отброшенные при ресинхронизации
	Dropped   int // кандидаты, отклонённые CRC
	BadType   int // пакеты с неизвестным байтом типа
	BadDLC    int // классические фреймы с can_dlc > 8
	BadLen    int // фреймы CAN FD с canfd len > 64
	Truncated int // байты, потерянные на пакете, обрезанном на EOF
}
```

```go
_, _, _ = p.Split([]byte{0x01, 0x02}, false)
s := p.Stats()
fmt.Println(s.Skipped, s.Dropped) // 2 0
```

## `BadFramePolicy`

```go
type BadFramePolicy int

const (
	BadFrameSkip BadFramePolicy = iota // отбросить, посчитать и продолжить (по умолчанию)
	BadFrameFail                       // остановиться и вернуть ErrBadType/ErrBadDLC/ErrBadLen
)
```

Выбирает, как `Split` реагирует на структурно некорректный пакет;
устанавливается через `WithBadFramePolicy`.

```go
p := cantcp.New(cantcp.WithBadFramePolicy(cantcp.BadFrameSkip))
_, _, _ = p.Split([]byte{0xC3, 0x3C, 0x7F}, false)
fmt.Println(p.Stats().BadType) // 1
```

## `Logger`, `LevelTrace`

```go
type Logger interface {
	Log(ctx context.Context, level slog.Level, msg string, args ...any)
}

const LevelTrace = slog.Level(-8)
```

`Logger` — интерфейс из одного метода; `*slog.Logger` реализует его
напрямую, `nil` выключает логирование. `LevelTrace` (`slog.Level(-8)`)
включает трассировку по каждому пакету; он ниже `slog.LevelDebug`, поэтому
трассировку можно включать отдельно.

> **Внимание:** `LevelTrace` пишет в лог полный hex сырого кадра, **включая
> данные (payload)** — телеметрию и команды с техники. Это чувствительные
> данные: трассировка предназначена только для отладки на изолированном
> стенде. Не включайте `LevelTrace` в проде, а логи с трассировкой считайте
> чувствительными.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()), cantcp.WithLogLevel(cantcp.LevelTrace))
// трассируется каждый разобранный фрейм, пропущенный байт и несовпадение CRC
```

## `ValidateRaw`

```go
func ValidateRaw(b []byte) (Type, error)
```

Проверяет сырой фрейм без оболочки потока cantcp и сообщает его тип: 16 байт
— `can_frame` (`TypeClassic`), 72 байта — `canfd_frame` (`TypeFd`), любая
другая длина возвращает `ErrFrameLen`. Раскладка выбирается только по длине:
у сырого кадра SocketCAN нет отдельного признака CAN FD, тип фрейма
определяется сокетом, из которого он прочитан.

Правила те же, что у `Frame.UnmarshalBinary` (общая реализация):
`ErrBadID`, `ErrBadDLC`, `ErrBadLen`, `ErrBadFlags`, `ErrReserved`;
возвращается первая ошибка. При длине 16 или 72 байта тип сообщается даже
вместе с ошибкой, на `ErrFrameLen` он не установлен. Байты данных за полем
длины не проверяются. `ValidateRaw` не читает оболочку cantcp: magic, байт
типа пакета и CRC-8 принадлежат только `Split` и `Decode`.

```go
typ, err := cantcp.ValidateRaw(raw) // 16 или 72 байта
if err != nil {
	log.Println("invalid frame:", err) // ErrBadID, ErrBadFlags, ...
}
fmt.Println(typ) // CAN, CAN FD или unknown на ErrFrameLen
```

## Ошибки

Все ошибки — sentinel-значения, сравниваются через `errors.Is`; пакет не
использует `fmt`, тексты ошибок статичные.

| Ошибка | Значение |
|---|---|
| `ErrFrameLen` | длина фрейма не 16 и не 72 байта |
| `ErrBadDLC` | классический `can_dlc > 8` |
| `ErrBadLen` | неверная длина CAN FD (> 64 или не кодируется 4-битным DLC) |
| `ErrBadType` | неизвестный тип пакета/фрейма |
| `ErrBadFlags` | флаги недопустимы для типа фрейма |
| `ErrBadID` | идентификатор не входит в разрядность режима адресации |
| `ErrReserved` | ненулевые reserved-байты |
| `ErrTruncated` | пакет обрезан в конце потока |

```go
raw := make([]byte, 16)
raw[4] = 9 // can_dlc > 8
_, err := cantcp.ValidateRaw(raw)
fmt.Println(errors.Is(err, cantcp.ErrBadDLC)) // true
```

## Длины данных CAN FD

Длины данных CAN FD кодируются 4-битным DLC с дискретной шкалой, поэтому
существуют только следующие длины; любая другая отклоняется с `ErrBadLen`
(Linux `can_fd_len2dlc` тоже её отклоняет). `Split` и `Decode` остаются
толерантными (`len ≤ 64`) для сырого pass-through.

| DLC | 0..8 | 9 | 10 | 11 | 12 | 13 | 14 | 15 |
|---|---|---|---|---|---|---|---|---|
| байт | 0..8 | 12 | 16 | 20 | 24 | 32 | 48 | 64 |

## Безопасность

cantcp — транспорт, а не слой безопасности. Он рассчитан на работу **внутри
доверенного периметра** (закрытого сегмента сети). Аутентификация, шифрование
канала, защита целостности и защита от повторов — не задачи библиотеки: это
ответственность того, кто её применяет, — TLS/mTLS, VPN, сегментация сети и
аутентификация сторон на уровне приложения. CRC-8 — проверка разметки, не
криптографическая целостность; участник, способный писать в поток, может
подделать, изменить или повторить любой кадр. `LevelTrace` логирует сырой
кадр вместе с payload, включать его в проде нельзя.

Модель угроз, замечание о CPU-амплификации и чек-лист развёртывания — в
[SECURITY.ru.md](SECURITY.ru.md). Английская версия: [SECURITY.md](SECURITY.md).

Полный пример сервера и клиента TLS 1.3 с mutual TLS — загрузка
сертификатов, `tls.Config` для обеих сторон и кодек cantcp поверх соединения
— в [examples/tls](examples/tls/). Команды OpenSSL для генерации
сертификатов — в документации cantcp (`docs/TLS-KEYS.ru.md` в репозитории
[cantcp](https://github.com/burn-lab-dev/cantcp)).

## Примечания

- `Split` — самосинхронизирующийся: после сбойного кандидата ресинк может
  найти валидный фрейм даже внутри его data-области. Это осознанное свойство.
- `Encode` — сырой pass-through слой: не чистит паддинги и флаги, строгая
  модель — это `Frame`. Слои разделены намеренно.
- Копирование `Frame` value копирует поля, но `Data` продолжает указывать в
  raw-хранилище исходного объекта; для независимой копии — `GetRaw`.
- CRC-8 — проверка целостности разметки, не криптография: примерно 1 из 256
  случайных кандидатов её проходит.
- `LevelTrace` логирует сырой кадр вместе с данными (payload): это инструмент
  отладки на изолированном стенде, а не продакшн-уровень логирования; см.
  [SECURITY.ru.md](SECURITY.ru.md).
- Error-кадр не несёт режима адресации: `FlagERR` вместе с `FlagEFF` или
  `FlagRTR` отклоняется с `ErrBadFlags`, а в `ID` принимается 29-битная маска
  классов ошибок вместо 11-битного идентификатора. `FlagRTR` — только classic.
  ID вне разрядности — ошибка, а не молчаливое маскирование.
- `ValidateRaw` и `Frame.UnmarshalBinary` используют одно ядро валидации,
  поэтому принимают и отклоняют ровно одни и те же сырые кадры; `Split`
  остаётся толерантным и делает только проверки уровня потока.
- `DecodeFrame` аллоцирует `Frame` на каждый вызов (значение владеет своим
  raw-хранилищем). `DecodeFrameInto` переиспользует кадр вызывающего и не
  аллоцирует на кадр: применяйте его на горячем пути.
- `NewDecoder` и `NewEncoder` возвращают неэкспортируемые типы, как и `New`:
  они используются через конструктор и публичные методы.

## Зависимости

Только стандартная библиотека Go. Без внешних зависимостей.

## Лицензия

MIT — см. [LICENSE](LICENSE).
