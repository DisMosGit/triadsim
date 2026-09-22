# 🗺 TriadSim Roadmap

> Каждая задача — **атомарная**: один осмысленный коммит, чёткое Definition of Done, минимум зависимостей от незавершённых задач.

---

## 📖 Как читать этот документ

- **Фазы** выполняются последовательно. Внутри фазы задачи можно брать в любом порядке, если не указано `depends on`.
- **Атомарность** = задача завершается за один рабочий подход (30 мин – 4 часа) и оставляет репозиторий в рабочем состоянии.
- **DoD** (Definition of Done) — обязательные критерии завершения.
- **Оценка** — грубая: `S` (< 1ч), `M` (1–3ч), `L` (3–8ч). `XL` — признак, что задачу надо дробить.
- **Метка 🧪** — задача сопровождается тестами.
- **Метка 📝** — задача сопровождается документацией (README, ADR, docstring).
- **Метка 🔌** — задача требует внешнего сервиса/инструмента (Docker, snmpwalk, snmptrapd, netopeer2-cli).

Статусы: `[ ]` — не начато, `[~]` — в работе, `[x]` — готово, `[-]` — отменено.

---

## 🎯 Целевое состояние (Definition of Done проекта)

- ✅ Один бинарник `simulator`, поднимающий SNMP v2c, NETCONF, RESTCONF, `/metrics`, опционально gNMI.
- ✅ Три домена: **radio (RRL)**, **l2**, **sync** — с общей моделью данных и EventBus.
- ✅ SNMP agent на `:1161`, traps на `:1162`, community `public`, vendor OID `1.3.6.1.4.1.99999.*`.
- ✅ NETCONF: SSH subsystem на `:830`, hello, framing, `get-config`, `edit-config`, `candidate`, `commit`, `discard-changes`, `confirmed-commit`, `create-subscription`, notifications.
- ✅ RESTCONF на `:8080`: URL-структура, JSON/XML media types, `GET`/`PUT`/`PATCH`/`POST`/`DELETE`.
- ✅ gNMI (opt.) на `:9339`: Get/Set/Subscribe minimal.
- ✅ Store: running/candidate/startup, JSON-персист `startup.json`.
- ✅ Router: `path ↔ model`, `OID ↔ path`, RPC dispatch.
- ✅ EventBus на Go channels, типы `AlarmRaised`, `AlarmCleared`, `ConfigChanged`, `StateTransition`.
- ✅ CLI (cobra): `start`, `alarm inject`, `dump`, `config validate`, `schema`, `version`.
- ✅ Главный кросс-доменный сценарий §3.6 из `desicion.md` проходит end-to-end.
- ✅ Тесты: unit + integration (testcontainers) + golden.
- ✅ `go vet ./...` и `go test ./...` зелёные.
- ✅ YANG-файлы в `yang/` как документация, встроены через `embed`.
- ✅ Документация `docs/protocols/*.md`, `docs/architecture.md`, `docs/store.md`, `docs/eventbus.md`, `docs/demo.md`.
- ✅ Лицензия MIT, Go 1.27+.

---

## Phase 0 — Skeleton 🦴

> **Цель:** пустой репозиторий превращается в проект, где всё собирается, тестируется и запускается.
> **Результат фазы:** `go run ./cmd/simulator start` поднимается, `go test ./...` зелёный.

### 0.1. Инициализация репозитория
- [x] `git init`, `.gitignore` (Go, IDE, `*.test`, `bin/`, `.env`, `startup.json`) · `S`
- [x] `LICENSE.md` (MIT) · `S` 📝
- [x] `README.md` (обзор + быстрый старт + один `curl` демо) · `S` 📝
- [x] `CONTRIBUTING.md` · `S` 📝
- [x] `CHANGELOG.md` (Keep a Changelog) · `S` 📝
- [x] `AGENTS.md` — единый файл для агентных IDE · `S` 📝
- [x] Первый коммит: `chore: initial skeleton` · `S`

**DoD:** репозиторий создан, `.md` файлы на месте, `AGENTS.md` ссылается на `ROADMAP.md`.

### 0.2. Go module + layout
- [x] `go mod init github.com/<user>/triadsim`, Go 1.27+ · `S` 📝
- [x] `cmd/simulator/main.go` — тонкий `main()`, делегирующий в `internal/cli` · `S`
- [x] `internal/` подпакеты: `model`, `store`, `router`, `event`, `radio`, `l2`, `sync`, `snmp`, `netconf`, `restconf`, `gnmi`, `cli`, `metrics` · `S`
- [x] Директории `configs/`, `docs/`, `docs/protocols/`, `docs/adr/`, `test/integration/`, `testdata/`, `yang/`, `scripts/` · `S`
- [x] `go build ./...` проходит · `S`
- [x] Коммит: `chore: go module + layout skeleton` · `S`

**DoD:** `go build ./...` и `go test ./...` проходят на пустом проекте.

### 0.3. Стек зависимостей
- [x] `go get github.com/gosnmp/gosnmp` · `S`
- [x] `go get golang.org/x/crypto/ssh` · `S`
- [x] `go get github.com/go-chi/chi/v5` · `S`
- [x] `go get github.com/spf13/cobra` · `S`
- [x] `go get github.com/prometheus/client_golang` · `S`
- [x] `go get github.com/stretchr/testify` · `S`
- [x] `go get github.com/testcontainers/testcontainers-go` (integration) · `S`
- [x] `go get gopkg.in/yaml.v3` · `S`
- [x] Коммит: `chore(deps): approved stack` · `S`

**DoD:** `go mod tidy` чист, все библиотеки зафиксированы.

### 0.4. Config (YAML)
- [x] `internal/config/config.go` — `Config` struct с тегами `yaml` · `M` 🧪
- [x] Поля: `SNMP.Port`, `SNMP.TrapPort`, `NETCONF.Port`, `RESTCONF.Port`, `Metrics.Port`, `GNMI.Enabled`, `GNMI.Port`, `Log.Level`, `Startup.File` · `S`
- [x] `configs/default.yaml` со всеми полями · `S` 📝
- [x] `Load(path string) (*Config, error)` + `Validate() error` · `M` 🧪
- [x] Тест: невалидный YAML → ошибка · `S` 🧪
- [x] Коммит: `feat(config): yaml config + validation` · `M` 🧪

**DoD:** `Load("configs/default.yaml")` возвращает валидный конфиг, тест на невалидные значения зелёный.

### 0.5. Логи (log/slog)
- [x] `internal/log/log.go` — обёртка над `slog` · `S` 🧪
- [x] JSON-вывод, уровень из `Config.Log.Level` · `S`
- [x] Хелперы `Info`, `Warn`, `Error`, `Debug` с контекстом · `S`
- [x] Коммит: `feat(log): slog setup` · `S` 📝

**DoD:** `log.Info("test")` пишет JSON в stderr.

### 0.6. EventBus
- [x] `internal/event/event.go` — `Event`, `EventType` · `S` 🧪
- [x] Типы: `AlarmRaised`, `AlarmCleared`, `ConfigChanged`, `StateTransition` · `S`
- [x] `internal/event/bus.go` — `Bus` на `chan Event` с буфером · `M` 🧪
- [x] Методы `Publish(e Event)`, `Subscribe() <-chan Event`, `Close()` · `M` 🧪
- [x] Тест: publish → subscriber получает событие · `M` 🧪
- [x] Коммит: `feat(event): eventbus on channels` · `M` 🧪

**DoD:** два подписчика получают одно событие; при переполнении буфера — non-blocking drop с логом.

### 0.7. Интерфейсы Store и Clock
- [x] `internal/store/store.go` — интерфейс `Store` (Get, Set, Delete, Diff, Commit, Rollback) · `M` 🧪
- [x] `internal/clock/clock.go` — интерфейс `Clock` (`Now`, `AfterFunc`) · `S` 🧪
- [x] Реализация `RealClock` · `S`
- [x] Тестовая реализация `FakeClock` в `internal/clock/fake.go` · `M` 🧪
- [x] Коммит: `feat(store,clock): interfaces` · `M` 🧪

**DoD:** тесты используют `FakeClock`, `time.Sleep` нигде не встречается.

### 0.8. Скрипты и Makefile
- [x] `scripts/check.sh` — `gofmt -l`, `go vet`, `go test ./...` · `S` 🔌
- [x] `scripts/demo.sh` — заглушка главного сценария · `S` 📝
- [x] `Makefile`: `build`, `test`, `lint`, `run`, `demo`, `clean` · `M` 📝
- [x] Коммит: `chore: scripts + Makefile` · `S` 📝

**DoD:** `make lint && make test` зелёные.

### 0.9. Документация фазы 0
- [x] `docs/architecture.md` — шаблон (модульный монолит, потоки данных) · `S` 📝
- [x] `docs/store.md` — шаблон (running/candidate/startup) · `S` 📝
- [x] `docs/eventbus.md` — типы событий, подписчики · `S` 📝
- [x] `docs/config.md` — YAML + startup JSON · `S` 📝
- [x] `docs/adr/0001-record-architecture-decisions.md` · `S` 📝
- [x] `docs/adr/template.md` · `S` 📝
- [x] Коммит: `docs: architecture, store, eventbus, config, adr` · `M` 📝

**✅ Phase 0 завершена, когда:** `go run ./cmd/simulator start` поднимается, логирует старт, `make lint && make test` зелёные.

> **Отклонения при реализации:**
> - `cmd/simulator/main.go` сразу делегирует в `internal/cli` (`root` + `start`), а не содержит пустой `main()`: без команды `start` не выполнялся бы DoD фазы. Остальные команды CLI остаются в 6.6.
> - `internal/tools/tools.go` (тег сборки `tools`) держит blank-импорты утверждённого стека, иначе `go mod tidy` удалил бы ещё не используемые библиотеки (gosnmp, x/crypto/ssh, chi, prometheus, testcontainers).
> - Коммиты 0.6/0.7 идут в обратном порядке относительно нумерации: `feat(store,clock): interfaces` перед `feat(event): eventbus on channels`, потому что `event.New` принимает `clock.Clock`.
> - `store.Store` адресует датастор явным параметром — `Get(ctx, ds, path)` вместо `Get(path)`. Это требование протоколов: NETCONF `get-config <source><candidate/>` и `copy-config` в `startup`, RESTCONF `?datastore=candidate`. Контракт зафиксирован в `docs/store.md`.
> - `docs/protocols/SNMP.md` §6.4/§7.2 приведены к плоской схеме конфига: community фиксирован `public`, списка trap-receivers нет.
> - Конфиг — плоско, по списку полей задачи 0.4; вложенная схема из ранней версии `docs/protocols/SNMP.md` не используется.

---

## Phase 1 — Model + Store + Router + SNMP v2c 📡

> **Цель:** SNMP-агент отдаёт `ifDescr` и vendor OID RSSI, состояние хранится в Store, Router маршрутизирует.
> **Результат фазы:** `snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2` возвращает `radio0`, `eth0`, `eth1`.

### 1.1. Модели домена radio
- [x] `internal/model/radio.go` — `RadioLink`, `ATPC`, `ACM`, `ModProfile` · `M` 🧪
- [x] Теги `path`, `xml`, `json`, `config:"false"` для read-only · `S`
- [x] `RadioLink.Validate() error` · `M` 🧪
- [x] Тесты на границы (`tx-power`, `atpc.min < max`) · `M` 🧪
- [x] Коммит: `feat(model): radio link` · `M` 🧪

**DoD:** `Validate()` покрыт тестами на граничные значения.

### 1.2. Модели домена l2
- [x] `internal/model/l2.go` — `VLAN`, `MACEntry`, `STPState`, `LLDPNeighbor`, `Interface` · `M` 🧪
- [x] `Validate()` на каждой · `M` 🧪
- [x] Коммит: `feat(model): l2` · `M` 🧪

### 1.3. Модели домена sync
- [x] `internal/model/sync.go` — `PTPClock`, `SyncEState`, `QL`, `ESMC` · `M` 🧪
- [x] `Validate()` на каждой · `M` 🧪
- [x] Коммит: `feat(model): sync` · `M` 🧪

### 1.4. Модель устройства и system-info
- [x] `internal/model/device.go` — `Device`, `SystemInfo` (device-id, uptime, interfaces) · `S` 🧪
- [x] `SystemInfo.Validate()` · `S` 🧪
- [x] Коммит: `feat(model): device + system-info` · `S` 🧪

### 1.5. Store: running/candidate/startup
- [x] `internal/store/memory.go` — `map[string]any` под мьютексом · `M` 🧪
- [x] `Get(path)`, `Set(path, val)`, `Delete(path)`, `List(prefix)` · `M` 🧪
- [x] `internal/store/diff.go` — diff candidate vs running · `M` 🧪
- [x] `internal/store/persist.go` — `Save/ Load` через `encoding/json` + `os.WriteFile` · `M` 🧪
- [x] `Commit` — валидация + применение + persist, `Rollback` — candidate = running · `M` 🧪
- [x] Тесты через `t.TempDir()` · `M` 🧪
- [x] Коммит: `feat(store): running/candidate/startup` · `L` 🧪

**DoD:** diff, commit, rollback, persist/load покрыты тестами.

> **Отклонения при реализации 1.1–1.5:**
> - Store-контракт — уже замороженный интерфейс фазы 0 с явным датастором
>   (`Get(ctx, ds, path)`), а не эскиз этой задачи `Get(path)`. Значения — плоские листья
>   (`bool`, `int`, `uint32`, `float64`, `string`), не указатели на модели.
> - Добавлены вложенные модели, которые описывают `docs/protocols/*.md` и требуют фазы 4–6:
>   `LinkBudget`, `InterfaceCounters`, `VLANPort`, `STPPort`, `SyncEInterface`. Это сделано
>   сейчас, чтобы не проходить повторно «model + Validate + router + golden + YANG» позже.
> - Профили ACM адресуются индексом `uint8` 1–12, а не строками `acm-N`: так тривиальны
>   проверки `min <= max` и границ; отображаемое имя лежит в `ModProfile.Name`.
> - `rssi`, `fade-margin`, `capacity` — листья `radio-link`, а не контейнер
>   `radio-link/performance` (как в `.docs/plan.md` и §9.2 RESTCONF).
> - `SystemInfo` дополнен `name`/`description`/`contact`/`location`, потому что они
>   отображаются на `sysName`/`sysDescr`/`sysContact`/`sysLocation` в 1.8.
> - `startup.json` версионирован и хранит тип листа (`{"kind":"uint32","value":1500}`):
>   обычный JSON превратил бы `int`/`uint32` в `float64` после перезапуска.
> - Валидация store инжектируется (`Options.Validator`), т.к. `internal/store` не должен
>   импортировать `internal/model`; реальный валидатор появится в router (1.6).
> - `validate:"..."`-теги не используются: правила живут в `Validate()` (AGENTS.md).


### 1.6. Router: path ↔ model, OID ↔ path
- [x] `internal/router/path.go` — парсер `a/b[c=d]/e` · `M` 🧪
- [x] `internal/router/reflect.go` — навигация по `path`-тегам через `reflect` · `L` 🧪
- [x] `internal/router/oid.go` — таблица `OID → path` для стандартных MIB (`ifDescr`, `ifOperStatus`) · `M` 🧪
- [x] Vendor-таблица `1.3.6.1.4.1.99999.*` для RSSI, fade-margin, capacity, alarm-status · `M` 🧪
- [x] `Dispatch(op Op) Result` для RPC · `M` 🧪
- [x] Тесты: round-trip `path → model → path`, `OID → path → value` · `M` 🧪
- [x] Коммит: `feat(router): path + oid dispatch` · `L` 🧪

**DoD:** router находит `interfaces/interface[name=radio0]/radio-link/tx-power` по path и по OID.

### 1.7. Seed-модель по умолчанию
- [x] `internal/model/seed.go` — `DefaultDevice()` с `radio0`, `eth0`, `eth1` · `M` 🧪
- [x] Загрузка из `startup.json`, если есть · `M` 🧪
- [x] Коммит: `feat(model): default seed + startup load` · `M` 🧪

### 1.8. SNMP agent (v2c, get/walk)
- [x] `internal/snmp/agent.go` — `gosnmp.NewHandler()` · `M` 🔌 🧪
- [x] `internal/snmp/oid.go` — построение OID-дерева из router · `M` 🧪
- [x] Обработка `Get`, `GetNext`, `GetBulk` для community `public` · `M` 🧪
- [x] Порт `:1161` (не `:161`, чтобы не требовать root) · `S`
- [x] Запуск из `cmd/simulator` · `S`
- [x] Коммит: `feat(snmp): agent v2c get/walk` · `L` 🧪

**DoD:** `snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2` возвращает `radio0`, `eth0`, `eth1`; vendor OID RSSI отдаёт float.

### 1.9. Метрики (Prometheus)
- [x] `internal/metrics/metrics.go` — `simulator_uptime_seconds`, `simulator_snmp_requests_total` · `M` 🧪
- [x] HTTP endpoint `/metrics` на `:9090` · `S` 🔌
- [x] Коммит: `feat(metrics): prometheus endpoint` · `M` 🧪

### 1.10. Документация SNMP
- [x] `docs/protocols/SNMP.md` — OID tree, MIB-таблица, traps, примеры `snmpwalk`/`snmptrapd` · `M` 📝
- [x] Коммит: `docs(snmp): oid tree + examples` · `M` 📝

**✅ Phase 1 завершена, когда:** SNMP walk отдаёт `ifDescr` и vendor OID RSSI; `Validate()` покрыт тестами; `/metrics` работает.

> **Отклонения при реализации 1.6–1.10:**
> - У `gosnmp` нет серверной части: `gosnmp.NewHandler()` — это клиент. Агент — свой UDP-цикл
>   на `net.PacketConn`, декодирование через `(*gosnmp.GoSNMP).SnmpDecodePacket`, ответ —
>   `(*gosnmp.SnmpPacket).MarshalMsg`. `SnmpDecodePacket` добавляет ведущую точку к OID, поэтому
>   router/snmp нормализуют её при поиске.
> - Store расширен типами листьев `uint8`, `uint16`, `uint64` (1.5), иначе значения моделей
>   пришлось бы терять или приводить с риском. Формат `startup.json` остался версии 1.
> - Списки получили тег `key:"true"` на ключевом поле (`Interface.Name`, `ModProfile.ID`, …):
>   router адресует элемент как `interfaces/interface[name=radio0]`.
> - Router зависит от `model` и `store` (добавлено в правила зависимостей architecture.md).
> - Vendor-радио-объекты — скаляры `.0` для единственного радио-линка; `alarm-status`
>   (`…99999.1.1.8`) отложен до 6.2 (нет поля модели), `simSync*`/`simL2*` — до 5.6/4.11.
> - RSSI/fade-margin/tx-power отдаются как `OpaqueDouble` (RFC 5342): в SMIv2 нет float.
> - `start` получил тестовую подстановку адресов (`runtimeDeps`), чтобы юнит-тесты не занимали
>   порты 1161/9090.
> - Загрузка `startup.json` — это `store.LoadStartup` (1.5); `model/seed.go` только строит
>   `DefaultDevice`, а `router.Seed` пишет её в store.

---

## Phase 2 — NETCONF base 🔧

> **Цель:** SSH-сервер с NETCONF subsystem, hello, framing, `get-config`, `edit-config`, `candidate`, `commit`, `discard-changes`.
> **Результат фазы:** `ssh -p 1830 -s admin@localhost netconf` работает, golden-тесты проходят.

### 2.1. SSH-сервер
- [x] `internal/netconf/ssh.go` — `x/crypto/ssh` server · `M` 🧪
- [x] Любой логин/пароль принимается (см. `desicion.md` §1) · `S`
- [x] Subsystem `netconf` · `S` 🧪
- [x] Порт `:830` · `S` → `:1830` (см. отклонения)
- [x] Коммит: `feat(netconf): ssh subsystem` · `M` 🧪

### 2.2. Hello и capabilities
- [x] `internal/netconf/hello.go` — приём/отправка `<hello>` · `M` 🧪
- [x] Capabilities: `base:1.0`, `base:1.1`, `candidate`, `writable-running` (`confirmed-commit`, `notification` — в фазе 3, см. отклонения) · `S` 📝
- [x] Session-id (счётчик) · `S` 🧪
- [x] Коммит: `feat(netconf): hello + capabilities` · `M` 🧪

### 2.3. Framing (EOM и chunked)
- [x] `internal/netconf/framing.go` — `]]>]]>` delimiter + chunked framing (`\n#<hex-len>\n...`) · `L` 🧪
- [x] Детект режима после hello (+ sniff первого байта клиентского hello) · `M` 🧪
- [x] Тесты на оба режима · `M` 🧪
- [x] Коммит: `feat(netconf): framing eom + chunked` · `L` 🧪

### 2.4. XML RPC-парсинг
- [x] `internal/netconf/rpc.go` — типы `<rpc>`, `<rpc-reply>`, `<rpc-error>` через `encoding/xml` · `M` 🧪
- [x] Dispatcher по имени операции · `M` 🧪
- [x] Коммит: `feat(netconf): rpc parsing + dispatch` · `M` 🧪

### 2.5. get-config
- [x] `internal/netconf/ops/getconfig.go` · `M` 🧪
- [x] Subtree-фильтр (упрощённый) · `M` 🧪
- [x] XPath-фильтр — заглушка (`operation-not-supported`) · `S`
- [x] Коммит: `feat(netconf): get-config` · `M` 🧪

### 2.6. edit-config
- [x] `internal/netconf/ops/editconfig.go` · `M` 🧪
- [x] Operations: `merge`, `replace`, `create`, `delete`, `remove` · `M` 🧪
- [x] Запись в candidate (и в running по capability `writable-running`) · `M` 🧪
- [x] Ошибка `invalid-value` при провале `Validate()` · `M` 🧪
- [x] Коммит: `feat(netconf): edit-config` · `L` 🧪

### 2.7. candidate / commit / discard-changes
- [x] `internal/netconf/ops/commit.go` · `M` 🧪
- [x] `internal/netconf/ops/discard.go` · `S` 🧪
- [x] `commit` валидирует и применяет candidate → running, persist в `startup.json` · `M` 🧪
- [x] Публикация `ConfigChanged` в EventBus · `S` 🧪
- [x] Коммит: `feat(netconf): commit + discard-changes` · `M` 🧪

### 2.8. Golden-тесты NETCONF
- [x] `testdata/netconf/edit-config.xml` + `.golden.xml` · `M` 🧪
- [x] `testdata/netconf/get-config.xml` + `.golden.xml` · `M` 🧪
- [x] Харнесс с флагом `-update` · `M` 🧪
- [x] Коммит: `test(netconf): golden tests` · `M` 🧪

### 2.9. Документация NETCONF
- [x] `docs/protocols/NETCONF.md` — SSH, hello, framing, операции, XML-примеры, коды ошибок · `L` 📝
- [x] Коммит: `docs(netconf): base operations` · `M` 📝

**✅ Phase 2 завершена, когда:** `ssh -s netconf` работает; edit-config → commit → get-config round-trip; golden-тесты зелёные.

> **Отклонения при реализации Phase 2:**
> - **Порт `:1830` вместо `:830`.** 830 — привилегированный порт: bind требует root или
>   `CAP_NET_BIND_SERVICE`, а README и `docs/config.md` обещают, что все порты непривилегированные.
>   Отклонение повторяет решение SNMP (`1161` вместо `161`). Меняется в `configs/default.yaml`.
> - **Команда подключения: `ssh -p 1830 -s admin@localhost netconf`.** В OpenSSH `-s` — флаг без
>   аргумента, имя subsystem передаётся как remote command; форма из DoD
>   (`ssh -p 830 -s netconf admin@localhost`) трактует `netconf` как хост. Исправлено в
>   `docs/protocols/NETCONF.md` и README.
> - **Capabilities только реализованные:** `base:1.0`, `base:1.1`, `candidate`,
>   `writable-running`. `confirmed-commit:1.1` и `notification:1.0` добавляются в фазе 3 вместе с
>   реализацией, иначе сервер объявлял бы то, чего не умеет.
> - **Данные в XML с корнем в модели.** `<config>` и `<data>` содержат top-level узлы модели
>   (`system-info`, `interfaces`), list keys — дочерние листья; примеры из `.docs/plan.md` §4.2 и
>   `.docs/desicion.md` §3.3 с корневым `<radio-link>` заменены на RFC 6241-совместимые.
> - **`get-config` отдаёт только configuration data** (`config:"false"`-листья не возвращаются).
>   Отдельного state-датасторa и операции `<get>` в этой фазе нет: state остаётся в SNMP.
> - **`edit-config` валидирует proposed snapshot целиком** и только потом пишет, поэтому
>   отклонённая правка не оставляет мусора в candidate; subtree `replace`/`delete` никогда не
>   удаляют read-only листья.
> - **Пакет `ops` внутри `netconf`**, как в роадмапе, но тип `Error` и дерево `Element` живут в
>   `ops`: иначе `netconf` (рендер `<rpc-error>`) и `ops` импортировали бы друг друга.
> - **Дополнительный коммит `feat(router): schema navigation`** — `Children`/`Node` нужны
>   XML-кодеку, чтобы отличать контейнеры, списки (с именем ключа) и листья с их типом. Схема
>   строится по типам модели и не зависит от инстансов в датасторе. Пригодится RESTCONF в фазе 4.
> - **Проводка в `start` (2.1) идёт вместе с коммитом 2.3**, когда сессия реально завершает hello
>   и согласует framing; порядок коммитов 2.1–2.3 отличается от нумерации (как в фазе 0).
> - **`go test ./... -update` не работает** для пакетов без флага: golden-файлы пересобираются
>   командой `go test ./internal/netconf -update`.
> - **Нет `lock`/`unlock`, `kill-session`, `copy-config`, `delete-config`, `validate`, `<get>`** —
>   вне scope фазы; неизвестные операции дают `operation-not-supported`. Candidate общий для всех
>   сессий, при конкуренции побеждает последняя запись.

---

## Phase 3 — NETCONF advanced: confirmed-commit + notifications 🔔

> **Цель:** `confirmed-commit` с таймаутом и rollback, `create-subscription` + notifications.
> **Результат фазы:** confirmed-commit откатывается по таймауту; подписка получает notification при событии.

### 3.1. confirmed-commit
- [x] `internal/netconf/ops/confirmed_commit.go` · `M` 🧪
- [x] `<confirm-timeout>` (по умолчанию 600с) · `S`
- [x] `time.AfterFunc` через `clock.Clock` · `M` 🧪
- [x] `commit` без `<confirmed/>` в течение таймаута отменяет rollback · `M` 🧪
- [x] Публикация `ConfigChanged` после подтверждения · `S`
- [x] Тесты через `FakeClock` · `M` 🧪
- [x] Коммит: `feat(netconf): confirmed-commit with rollback` · `L` 🧪

**DoD:** при отсутствии подтверждения candidate откатывается к running.

### 3.2. create-subscription
- [x] `internal/netconf/notif/subscription.go` · `M` 🧪
- [x] Stream `sim-events` · `S`
- [x] Привязка подписки к EventBus · `M` 🧪
- [x] Replay (`<replayStartTime>`) — заглушка · `S`
- [x] Коммит: `feat(netconf): create-subscription` · `M` 🧪

### 3.3. Notification dispatcher
- [x] `internal/netconf/notif/dispatcher.go` · `M` 🧪
- [x] Сериализация `Event` → `<notification>` XML · `M` 🧪
- [x] Отправка всем активным подпискам · `M` 🧪
- [x] Framing через chunked, если hello 1.1 · `S` 🧪
- [x] Коммит: `feat(netconf): notification dispatch` · `M` 🧪

### 3.4. Интеграция с EventBus
- [x] Подписка NETCONF-диспетчера на EventBus при старте · `S` 🧪
- [x] Тест: `Publish` в EventBus → notification уходит в сессию · `M` 🧪
- [x] Коммит: `feat(netconf): eventbus integration` · `M` 🧪

### 3.5. Документация notifications
- [x] Дополнить `docs/protocols/NETCONF.md` разделом про confirmed-commit и notifications · `M` 📝
- [x] Коммит: `docs(netconf): confirmed-commit + notifications` · `S` 📝

**✅ Phase 3 завершена, когда:** confirmed-commit откатывается по таймауту; подписка получает `<notification>`.

> **Отклонения при реализации Phase 3:**
> - **`:confirmed-commit:1.0`, а не `1.1`.** Реализованы `<confirmed/>` и `<confirm-timeout>`
>   (RFC 4741 §8.4); `<persist>`, `<persist-id>` и операция `<cancel-commit>` из RFC 6241 §8.4
>   не реализованы и отвечают `operation-not-supported`. Сервер объявляет только то, что умеет,
>   поэтому capability — версии 1.0.
> - **Один confirmed commit на сервер.** Candidate общий для всех сессий, поэтому повторный
>   confirmed commit из другой сессии — `access-denied`; follow-up confirmed commit из той же
>   сессии применяет свои изменения, сохраняет цель отката первого commit'а и лишь
>   перезапускает таймер. Подтверждающим считается любой успешный `<commit/>` без
>   `<confirmed/>`, из любой сессии (RFC 4741 §8.4.1 не ограничивает сессию подтверждения).
> - **Откат — это новый примитив store.** Появились `Store.Snapshot`/`Store.Restore`:
>   снимок running берётся до confirmed commit, откат восстанавливает running и startup, но
>   **не** candidate — незакоммиченная конфигурация остаётся видимой и её можно закоммитить
>   заново. Формат `startup.json` не изменился (версия 1).
> - **Откат при завершении сессии.** По RFC 4741 §8.4.1 confirmed commit откатывается, если
>   сессия, которая его выдала, завершилась до подтверждения — включая `close-session` и
>   shutdown; поэтому `Server.Close` не сбрасывает таймер, а `Serve` дожидается завершения
>   сессий.
> - **`create-subscription`: один stream без replay.** Stream `sim-events`; отсутствие
>   `<stream>` выбирает его, другое имя — `invalid-value`; `<filter>`, `<startTime>` и
>   `<stopTime>` (replay) отвечают `operation-not-supported` — так же, как XPath-заглушка фазы 2.
> - **Подписка на сессию, а не на RPC.** Вторая `create-subscription` в той же сессии заменяет
>   предыдущую подписку; подписка снимается при завершении сессии (RFC 5277 §2.3), отдельного
>   unsubscribe нет. Диспетчер держит одну подписку на шину на весь сервер и по буферизованному
>   каналу на сессию; переполнение — drop с логом, как у `event.Bus`.
> - **Формат notification.** `<notification>` в namespace RFC 5277, `<eventTime>` в RFC 3339
>   UTC, payload `<event xmlns="urn:sim:sim-events">` с `type`/`resource`/`severity`/`message` —
>   тот же модуль `sim-events`, что у RESTCONF SSE (фаза 4). Framing — тот же, что у сессии
>   (`base:1.1` → chunked); запись защищена мьютексом messageWriter, чтобы reply и notification
>   не перемешивались.
> - **Проводка.** `Deps` получил `SessionID` и `Confirmed`, `netconf.Options` — `Clock`
>   (в тестах `FakeClock`); диспетчер создаётся в `New` и запускается в `Serve`; при `bus == nil`
>   `create-subscription` отвечает `operation-not-supported`.
> - **Golden-транскрипты.** Хелло в `testdata/netconf/*.golden.xml` изменился (две новые
>   capability) — файлы перегенерированы через `go test ./internal/netconf -update`; добавлен
>   `testdata/netconf/confirmed-commit.xml`.

---

## Phase 4 — RESTCONF + L2 switching 🌐

> **Цель:** RESTCONF на chi, домен L2 (VLAN, QinQ, MAC-table, STP/RSTP simplified, LLDP, counters).
> **Результат фазы:** VLAN через RESTCONF; MAC-table через SNMP; STP state виден через RESTCONF.

### 4.1. RESTCONF HTTP-каркас
- [x] `internal/restconf/server.go` — `chi.Router` · `M` 🧪
- [x] Порт `:8080`, медиатипы `application/yang-data+json`, `+xml` · `S`
- [x] `GET`, `PUT`, `PATCH`, `POST`, `DELETE` · `M` 🧪
- [x] Коммит: `feat(restconf): chi skeleton` · `M` 🧪

### 4.2. URL-структура и маршрутизация
- [x] `internal/restconf/path.go` — парсинг `/restconf/data/<module>:<path>` · `M` 🧪
- [x] Маппинг на `router.path` · `M` 🧪
- [x] Коммит: `feat(restconf): url routing` · `M` 🧪

### 4.3. GET / PUT / PATCH / DELETE
- [x] `internal/restconf/ops/get.go` · `M` 🧪
- [x] `internal/restconf/ops/put.go` — replace + validate + apply · `M` 🧪
- [x] `internal/restconf/ops/patch.go` — merge + validate + apply · `M` 🧪
- [x] `internal/restconf/ops/delete.go` · `S` 🧪
- [x] Коды ошибок: 400, 404, 409, 415, 422 · `M` 🧪
- [x] Коммит: `feat(restconf): CRUD operations` · `L` 🧪

### 4.4. Media types
- [x] JSON codec через `encoding/json` · `S` 🧪
- [x] XML codec через `encoding/xml` · `M` 🧪
- [x] Согласование `Accept`/`Content-Type` · `S` 🧪
- [x] Коммит: `feat(restconf): json + xml codecs` · `M` 🧪

### 4.5. L2: VLAN + QinQ
- [x] `internal/l2/vlan.go` — CRUD VLAN, QinQ outer/inner · `M` 🧪
- [x] Обработка members (tagged/untagged) на интерфейсах · `M` 🧪
- [x] Валидация: VLAN id 1–4094, QinQ outer ≠ inner · `S` 🧪
- [x] Коммит: `feat(l2): vlan + qinq` · `M` 🧪

### 4.6. L2: MAC table
- [x] `internal/l2/mac.go` — learning, aging (через `clock.Clock`) · `M` 🧪
- [x] VLAN-aware таблица · `M` 🧪
- [x] Публикация MAC-таблицы через SNMP `dot1dTpFdbPort` · `M` 🧪
- [x] Коммит: `feat(l2): mac table` · `M` 🧪

### 4.7. L2: STP/RSTP simplified
- [x] `internal/l2/stp.go` — state machine (Disabled, Blocking, Listening, Learning, Forwarding) · `M` 🧪
- [x] Root election (упрощённая) · `M` 🧪
- [x] Публикация состояния в EventBus · `S` 🧪
- [x] Тесты табличных переходов · `M` 🧪
- [x] Коммит: `feat(l2): stp/rstp state machine` · `L` 🧪

### 4.8. L2: LLDP
- [x] `internal/l2/lldp.go` — статические соседи из конфига · `M` 🧪
- [x] Периодические TLV (упрощённо) через `clock.Clock` · `S` 🧪
- [x] Коммит: `feat(l2): lldp` · `M` 🧪

### 4.9. L2: interface counters
- [x] `internal/l2/counters.go` — rx/tx bytes, packets, errors, drops · `M` 🧪
- [x] Инкремент при симуляции трафика · `M` 🧪
- [x] Публикация через SNMP `ifTable` · `M` 🧪
- [x] Коммит: `feat(l2): counters` · `M` 🧪

### 4.10. Broadcast storm simulation
- [x] `internal/l2/storm.go` — API-эндпоинт инжекции · `M` 🧪
- [x] Rate-limit → alarm · `M` 🧪
- [x] Коммит: `feat(l2): broadcast storm simulation` · `M` 🧪

### 4.11. SNMP-привязка L2
- [x] Заполнить `internal/router/oid.go` MIB-II (ifTable) и BRIDGE-MIB · `M` 🧪
- [x] Тест: `snmpwalk ...1.3.6.1.2.1.17.4.3.1.2` возвращает MAC-port · `M` 🧪
- [x] Коммит: `feat(snmp): l2 mib bindings` · `M` 🧪

### 4.12. Документация L2 + RESTCONF
- [x] `docs/protocols/L2.md` — 802.1Q, QinQ, MAC, STP/RSTP simplified, LLDP, counters, MIB-соответствие · `L` 📝
- [x] `docs/protocols/RESTCONF.md` — URL, media types, коды ошибок, примеры `curl` · `M` 📝
- [x] Коммит: `docs: l2 + restconf` · `M` 📝

> **Отклонения при реализации Phase 4:**
> - **`creatable:"true"` — новый тег модели.** Списки, которые наполняются в рантайме
>   (`vlans/vlan`, `ports/port`, `mac-table/entry`, `lldp/neighbors/neighbor`), помечены новым
>   тегом; `router.selectElement` синтезирует элемент для такого списка, поэтому PUT/PATCH/POST
>   и SNMP SET могут адресовать запись, которой нет в boot-шаблоне. Закрытые списки (interfaces,
>   stp/state/ports/port) по-прежнему отвечают `unknown-element`.
> - **Гидратация снимка — клон шаблона.** `Router.deviceFromValues` начинает с deep-copy
>   boot-шаблона (порядок инстансов, значения листьев, которых нет в сторе), но **вырезает**
>   элементы списков, которых нет в датасторе (`pruneLists`), поэтому удаление VLAN/MAC/LLDP
>   видно домену. Полностью пустой датастор считается «ещё не засеянным» и сохраняет шаблон —
>   это нужно валидации свежего candidate. Следствие: PUT-replace может не заметить пропущенный
>   обязательный лист, если шаблон его подставляет (существовавшая и раньше дыра).
> - **`Router.Snapshot`** отдаёт доменам гидратированный `*model.Device`; `internal/l2` читает
>   состояние через него, пишет конфигурацию в running (`Router.Set`), а изученное/измеренное —
>   через `Router.SetState` (running + candidate, потому что `Store.Commit` копирует candidate).
>   Конфигурация, записанная по RESTCONF, в candidate не попадает — это осознанный компромисс,
>   к которому стоит вернуться в Phase 6.
> - **STP: пять фаз, три состояния у RSTP.** Машина проходит классические
>   disabled → blocking → listening → learning → forwarding; модель принимает оба словаря
>   (`model.STPPortStates`), а для `rstp` первые три фазы пишутся как `discarding`. Forward delay
>   (по умолчанию 15 с) планируется на инжектированных часах и применяется периодическим
>   тиком. Выборы корня упрощены: сравнение (priority, address), в модели хранятся только
>   root-id и root-cost, BPDU по проводу не кодируются.
> - **Кольцо событий.** Домен публикует только `StateTransition` (STP) и
>   `AlarmRaised`/`AlarmCleared` (broadcast storm). Событий `VLANCreated`/`MACLearned` нет —
>   документация L2 приведена в соответствие.
> - **`internal/datatree`** — общий движок чтения/правки дерева для NETCONF и RESTCONF
>   (RFC 6241 error-tag'и, edit-семантика). NETCONF-голдены перегенерированы
>   (`go test ./internal/netconf -update`): добавлены message-id 4–6 (создание VLAN 200 и
>   фильтрованный get-config), старые 4–6 сдвинуты в 7–9.
> - **RESTCONF.** `/restconf/operations` и `/restconf/streams` отвечают 501, YANG Patch, depth,
>   fields, filter и yang-library не реализованы; авторизации и TLS нет. `POST` в
>   `/api/simulate/l2-storm` — вне `/restconf` и вне YANG-модели.
> - **SNMP.** Добавлен тип `Counter64` (ifXTable-счётчики) и табличные scope'ы
>   `scopeSTPPort`/`scopeMAC`/`scopeVLAN`; SET теперь резолвит путь из найденного binding'а, а не
>   из шаблона, поэтому запись в выросшую таблицу тоже возможна. Один модельный лист может
>   отображаться в несколько MIB-объектов (ifAdminStatus/ifOperStatus) — в `byPath` остаётся
>   отображение с декодером.
> - **CLI.** Команд `simulator l2 ...` нет: домен управляется через RESTCONF/NETCONF/SNMP,
>   а `start` поднимает `l2.Manager.Run` вместе с менеджмент-плоскостями.

**✅ Phase 4 завершена, когда:** VLAN через RESTCONF; MAC-table через SNMP; STP state виден через RESTCONF.

---

## Phase 5 — Sync: PTP, SyncE, ESMC/SSM ⏱️

> **Цель:** домен синхронизации — PTP state machine, SyncE, ESMC/SSM, holdover, quality levels.
> **Результат фазы:** PTP master + holdover переход; переходы в EventBus.

### 5.1. PTP state machine
- [ ] `internal/sync/ptp.go` — `State` (`freerun`, `master`, `holdover`) · `M` 🧪
- [ ] `Handle(event Event) State` · `M` 🧪
- [ ] Таймер holdover через `clock.Clock` · `M` 🧪
- [ ] Публикация `StateTransition` в EventBus · `S` 🧪
- [ ] Табличные тесты переходов · `M` 🧪
- [ ] Коммит: `feat(sync): ptp state machine` · `L` 🧪

### 5.2. SyncE
- [ ] `internal/sync/synce.go` — `Enabled`, `SelectedSource`, `Port` state · `M` 🧪
- [ ] Выбор источника (приоритет, QL) · `M` 🧪
- [ ] Коммит: `feat(sync): synce` · `M` 🧪

### 5.3. ESMC / SSM
- [ ] `internal/sync/esmc.go` — ESMC-сообщения (упрощённо) · `M` 🧪
- [ ] QL: `QL-PRC`, `QL-SSU-A`, `QL-SSU-B`, `QL-SEC`, `QL-DNU` · `S` 🧪
- [ ] Маппинг QL → приоритет · `S` 🧪
- [ ] Коммит: `feat(sync): esmc + ssm` · `M` 🧪

### 5.4. Holdover и quality
- [ ] Время удержания при потере источника · `M` 🧪
- [ ] Публикация `AlarmRaised` при истечении holdover · `S` 🧪
- [ ] Коммит: `feat(sync): holdover + quality` · `M` 🧪

### 5.5. Jitter/offset (симуляция)
- [ ] Генерация значений offset и jitter по источнику · `M` 🧪
- [ ] Публикация в модель `PTPClock` · `S` 🧪
- [ ] Коммит: `feat(sync): jitter + offset simulation` · `M` 🧪

### 5.6. RESTCONF/SNMP привязка sync
- [ ] RESTCONF `sim-sync:ptp/clock` (GET/PATCH) · `M` 🧪
- [ ] Vendor OID для sync state · `S` 🧪
- [ ] Коммит: `feat(sync): restconf + snmp bindings` · `M` 🧪

### 5.7. Документация sync
- [ ] `docs/protocols/PTP.md` — IEEE 1588, state machine, domain, master/holdover/freerun, упрощения · `L` 📝
- [ ] `docs/protocols/SYNCE.md` — ESMC/SSM, QL, связь с PTP, упрощения · `M` 📝
- [ ] Коммит: `docs: ptp + synce` · `M` 📝

**✅ Phase 5 завершена, когда:** PTP master + holdover переход; переходы в EventBus; RESTCONF отдаёт state.

---

## Phase 6 — Аварии, traps, notifications, метрики, CLI `alarm inject` 🚨

> **Цель:** главный кросс-доменный сценарий §3.6 из `desicion.md` проходит end-to-end.
> **Результат фазы:** одна `curl` команда порождает trap + notification + метрику + переход state machine.

### 6.1. API симуляции
- [ ] `internal/restconf/sim/` — `POST /api/simulate/radio-failure` · `M` 🧪
- [ ] `POST /api/simulate/sync-loss` · `S` 🧪
- [ ] `POST /api/simulate/l2-storm` · `S` 🧪
- [ ] Коммит: `feat(restconf): simulation API` · `M` 🧪

### 6.2. Radio: link budget и аварии
- [ ] `internal/radio/linkbudget.go` — расчёт RSSI, fade margin, capacity · `M` 🧪
- [ ] `internal/radio/alarms.go` — `radioLinkDown`, `radioLinkDegraded` · `M` 🧪
- [ ] `internal/radio/atpc.go` — шаг ATPC (шаг 1 дБ, границы min/max) · `M` 🧪
- [ ] `internal/radio/acm.go` — выбор профиля модуляции по SNR · `M` 🧪
- [ ] Публикация `AlarmRaised`/`AlarmCleared` в EventBus · `S` 🧪
- [ ] Коммит: `feat(radio): link budget + alarms + atpc + acm` · `L` 🧪

### 6.3. SNMP traps
- [ ] `internal/snmp/trap.go` — отправка trap на `:1162` · `M` 🧪
- [ ] Vendor OID traps: `1.3.6.1.4.1.99999.0.1 radioLinkDown`, `.0.2 radioLinkUp` · `S`
- [ ] Varbinds с именем линка, RSSI, fade margin · `S` 🧪
- [ ] Подписка на EventBus `AlarmRaised`/`AlarmCleared` · `M` 🧪
- [ ] Коммит: `feat(snmp): traps` · `M` 🧪

### 6.4. Prometheus метрики аварий
- [ ] `simulator_alarms_total{type,severity}` · `S` 🧪
- [ ] `simulator_ptp_state_transitions_total{from,to}` · `S` 🧪
- [ ] `simulator_config_changes_total` · `S` 🧪
- [ ] Коммит: `feat(metrics): alarm + state counters` · `M` 🧪

### 6.5. Кросс-доменная связка radio → sync
- [ ] При `radioLinkDown` → PTP уходит в `holdover` · `M` 🧪
- [ ] При восстановлении линка → PTP возвращается в `master` · `M` 🧪
- [ ] Интеграционный тест полного сценария §3.6 · `L` 🧪
- [ ] Коммит: `feat(sync): cross-domain holdover on radio failure` · `L` 🧪

### 6.6. CLI (cobra)
- [ ] `internal/cli/root.go` — корневая команда · `S` 🧪
- [ ] `start` — запуск симулятора · `S` 🧪
- [ ] `alarm inject --type radioLinkDown --link radio0` · `M` 🧪
- [ ] `dump --format json` · `M` 🧪
- [ ] `config validate --file configs/default.yaml` · `S` 🧪
- [ ] `version` · `S` 🧪
- [ ] Коммит: `feat(cli): cobra commands` · `L` 🧪

### 6.7. Integration-тесты с testcontainers
- [ ] `test/integration/netconf_test.go` — round-trip NETCONF · `L` 🔌 🧪
- [ ] `test/integration/snmp_test.go` — walk + trap receiver · `L` 🔌 🧪
- [ ] `test/integration/crossdomain_test.go` — §3.6 · `L` 🔌 🧪
- [ ] Коммит: `test(integration): netconf + snmp + crossdomain` · `L` 🧪

### 6.8. Документация аварий
- [ ] `docs/protocols/RADIO-RRL.md` — link budget, RSSI, ATPC, ACM, профили модуляции, fade margin, alarms · `L` 📝
- [ ] `docs/demo.md` — главный кросс-доменный сценарий §3.6 пошагово · `M` 📝
- [ ] `docs/metrics.md` — Prometheus-метрики · `S` 📝
- [ ] `docs/cli.md` — команды cobra · `M` 📝
- [ ] Коммит: `docs: radio, demo, metrics, cli` · `L` 📝

**✅ Phase 6 завершена, когда:** главный кросс-доменный сценарий проходит end-to-end; `/metrics` отдаёт `simulator_alarms_total`.

---

## Phase 7 — gNMI (опционально) + YANG + polish 🎛️

> **Цель:** тонкий слой gNMI поверх router; YANG-файлы в репозитории; README, demo-скрипты, релиз v0.1.0.
> **Результат фазы:** gNMI Get/Set работают; все `docs/protocols/*.md` на месте; `scripts/demo.sh` воспроизводит §3.6.

### 7.1. gNMI сервис
- [ ] `internal/gnmi/server.go` — gRPC сервер на `:9339` · `L` 🔌 🧪
- [ ] `Capabilities`, `Get`, `Set` (replace/update/delete) · `L` 🧪
- [ ] `Subscribe` (ONCE + STREAM ON_CHANGE) minimal · `L` 🧪
- [ ] TLS-off (per `desicion.md` §1) · `S`
- [ ] Запуск по флагу `GNMI.Enabled` · `S` 🧪
- [ ] Коммит: `feat(gnmi): server + get/set/subscribe` · `L` 🧪

### 7.2. YANG-файлы как документация
- [ ] `yang/sim-device.yang` · `M` 📝
- [ ] `yang/sim-radio-link.yang` · `M` 📝
- [ ] `yang/sim-l2-switching.yang` · `M` 📝
- [ ] `yang/sim-sync.yang` · `M` 📝
- [ ] `internal/model/embed.go` — `//go:embed ../../yang/*.yang` · `S` 🧪
- [ ] CLI `schema --yang` дампит встроенные YANG · `M` 🧪
- [ ] Коммит: `feat(yang): embedded schema + schema command` · `L` 📝

### 7.3. gNMI + YANG документация
- [ ] `docs/protocols/gNMI.md` — Get/Set/Subscribe, paths, порты, TLS-off, примеры · `M` 📝
- [ ] Коммит: `docs(gnmi): service + examples` · `M` 📝

### 7.4. Demo-скрипт
- [ ] `scripts/demo.sh` — воспроизводит §3.6 одной командой · `M` 🔌 📝
- [ ] `scripts/check.sh` — обновить (gofmt, vet, tests, integration) · `S` 🔌
- [ ] Коммит: `chore: demo + check scripts` · `M` 📝

### 7.5. README polish
- [ ] README с диаграммой (ASCII или Mermaid), быстрым стартом, одним `curl` демо · `L` 📝
- [ ] Пример `snmptrapd` вывода · `S` 📝
- [ ] Пример `ssh -s netconf` сессии · `M` 📝
- [ ] Коммит: `docs(readme): full walkthrough` · `M` 📝

### 7.6. ADR
- [ ] `docs/adr/0002-model-vs-yang.md` — почему модели, а не YANG runtime · `M` 📝
- [ ] `docs/adr/0003-modular-monolith.md` — почему один бинарник, а не микросервисы · `M` 📝
- [ ] `docs/adr/0004-gosnmp-ssh-xml.md` — почему этот стек · `M` 📝
- [ ] Коммит: `docs(adr): model, monolith, stack` · `M` 📝

### 7.7. Финальная вычитка
- [ ] Пройтись по `TODO` в коде · `M`
- [ ] Убрать мёртвый код, отладочные логи уровня Debug · `S`
- [ ] `go vet ./... && go test ./... && go test -tags=integration ./...` — зелёные · `S` 🔌
- [ ] Обновить `CHANGELOG.md` релизом `v0.1.0` · `S` 📝
- [ ] Тег `v0.1.0` · `S`
- [ ] Коммит: `chore: release v0.1.0` · `S`

**✅ Phase 7 завершена, когда:** gNMI Get/Set работают; все `docs/protocols/*.md` на месте; `scripts/demo.sh` воспроизводит §3.6; `v0.1.0` тегирован.

---

## 📊 Сводка по фазам

| Фаза | Тема | Задач (примерно) | Оценка |
|------|------|------------------|--------|
| 0 | Skeleton | 30 | 1–2 дня |
| 1 | Model + Store + Router + SNMP | 35 | 4–5 дней |
| 2 | NETCONF base | 25 | 3–4 дня |
| 3 | NETCONF advanced | 15 | 2 дня |
| 4 | RESTCONF + L2 | 40 | 5–6 дней |
| 5 | Sync (PTP, SyncE, ESMC) | 20 | 3 дня |
| 6 | Аварии, traps, метрики, CLI | 30 | 4–5 дней |
| 7 | gNMI + YANG + polish | 25 | 3–4 дня |
| **Всего** | | **~220** | **~25–31 день** |

---

## 🎯 Milestones (для GitHub Milestones)

| Milestone | Фазы | Что демонстрирует |
|-----------|------|-------------------|
| **M0 — Foundation** | 0 | Go module, config, slog, EventBus, Store/Clock интерфейсы |
| **M1 — SNMP** | 1 | Model + Store + Router + SNMP v2c walk |
| **M2 — NETCONF** | 2 | SSH subsystem, framing, edit-config/get-config/commit/discard |
| **M2.5 — NETCONF+** | 3 | confirmed-commit + notifications + create-subscription |
| **M3 — RESTCONF + L2** | 4 | VLAN, QinQ, MAC-table, STP/RSTP simplified, LLDP, counters |
| **M3.5 — Sync** | 5 | PTP state machine, SyncE, ESMC/SSM, holdover |
| **M4 — Cross-domain** | 6 | Главный сценарий §3.6: trap + notification + метрика + holdover |
| **M5 — gNMI + YANG** | 7 | gNMI Get/Set/Subscribe, YANG-файлы, demo-скрипт |
| **M6 — v0.1.0** | 7 | README, ADR, релиз |

---

## 📌 Правила работы с роадмапом

1. **Атомарность важнее скорости.** Если задача кажется `XL` — дели на подзадачи прямо в этом файле.
2. **Каждая задача — коммит.** Имя = тип + scope + subject (см. `CONTRIBUTING.md`).
3. **DoD обязателен.** Не закрывай задачу, если DoD не выполнен полностью.
4. **Метки 🧪, 📝, 🔌 — не опциональны.** Тесты, документация и инфра идут вместе с кодом.
5. **Фазы последовательны, задачи внутри фазы — параллельны.** Зависимости указаны явно.
6. **Обновляй этот файл по ходу.** Новая задача — в нужную фазу, не в бэклог.
7. **Отменённые задачи** помечай `[-]` с комментарием «почему».
8. **Прогресс виден в GitHub Projects.** Board: `Backlog` / `In Progress` / `Review` / `Done`.
