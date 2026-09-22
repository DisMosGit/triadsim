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
- [ ] `git init`, `.gitignore` (Go, IDE, `*.test`, `bin/`, `.env`, `startup.json`) · `S`
- [ ] `LICENSE.md` (MIT) · `S` 📝
- [ ] `README.md` (обзор + быстрый старт + один `curl` демо) · `S` 📝
- [ ] `CONTRIBUTING.md` · `S` 📝
- [ ] `CHANGELOG.md` (Keep a Changelog) · `S` 📝
- [ ] `AGENTS.md` — единый файл для агентных IDE · `S` 📝
- [ ] Первый коммит: `chore: initial skeleton` · `S`

**DoD:** репозиторий создан, `.md` файлы на месте, `AGENTS.md` ссылается на `ROADMAP.md`.

### 0.2. Go module + layout
- [ ] `go mod init github.com/<user>/triadsim`, Go 1.27+ · `S` 📝
- [ ] `cmd/simulator/main.go` с пустым `main()` · `S`
- [ ] `internal/` подпакеты: `model`, `store`, `router`, `event`, `radio`, `l2`, `sync`, `snmp`, `netconf`, `restconf`, `gnmi`, `cli`, `metrics` · `S`
- [ ] Директории `configs/`, `docs/`, `docs/protocols/`, `docs/adr/`, `test/integration/`, `testdata/`, `yang/`, `scripts/` · `S`
- [ ] `go build ./...` проходит · `S`
- [ ] Коммит: `chore: go module + layout skeleton` · `S`

**DoD:** `go build ./...` и `go test ./...` проходят на пустом проекте.

### 0.3. Стек зависимостей
- [ ] `go get github.com/gosnmp/gosnmp` · `S`
- [ ] `go get golang.org/x/crypto/ssh` · `S`
- [ ] `go get github.com/go-chi/chi/v5` · `S`
- [ ] `go get github.com/spf13/cobra` · `S`
- [ ] `go get github.com/prometheus/client_golang` · `S`
- [ ] `go get github.com/stretchr/testify` · `S`
- [ ] `go get github.com/testcontainers/testcontainers-go` (integration) · `S`
- [ ] `go get gopkg.in/yaml.v3` · `S`
- [ ] Коммит: `chore(deps): approved stack` · `S`

**DoD:** `go mod tidy` чист, все библиотеки зафиксированы.

### 0.4. Config (YAML)
- [ ] `internal/config/config.go` — `Config` struct с тегами `yaml` · `M` 🧪
- [ ] Поля: `SNMP.Port`, `SNMP.TrapPort`, `NETCONF.Port`, `RESTCONF.Port`, `Metrics.Port`, `GNMI.Enabled`, `GNMI.Port`, `Log.Level`, `Startup.File` · `S`
- [ ] `configs/default.yaml` со всеми полями · `S` 📝
- [ ] `Load(path string) (*Config, error)` + `Validate() error` · `M` 🧪
- [ ] Тест: невалидный YAML → ошибка · `S` 🧪
- [ ] Коммит: `feat(config): yaml config + validation` · `M` 🧪

**DoD:** `Load("configs/default.yaml")` возвращает валидный конфиг, тест на невалидные значения зелёный.

### 0.5. Логи (log/slog)
- [ ] `internal/log/log.go` — обёртка над `slog` · `S` 🧪
- [ ] JSON-вывод, уровень из `Config.Log.Level` · `S`
- [ ] Хелперы `Info`, `Warn`, `Error`, `Debug` с контекстом · `S`
- [ ] Коммит: `feat(log): slog setup` · `S` 📝

**DoD:** `log.Info("test")` пишет JSON в stderr.

### 0.6. EventBus
- [ ] `internal/event/event.go` — `Event`, `EventType` · `S` 🧪
- [ ] Типы: `AlarmRaised`, `AlarmCleared`, `ConfigChanged`, `StateTransition` · `S`
- [ ] `internal/event/bus.go` — `Bus` на `chan Event` с буфером · `M` 🧪
- [ ] Методы `Publish(e Event)`, `Subscribe() <-chan Event`, `Close()` · `M` 🧪
- [ ] Тест: publish → subscriber получает событие · `M` 🧪
- [ ] Коммит: `feat(event): eventbus on channels` · `M` 🧪

**DoD:** два подписчика получают одно событие; при переполнении буфера — non-blocking drop с логом.

### 0.7. Интерфейсы Store и Clock
- [ ] `internal/store/store.go` — интерфейс `Store` (Get, Set, Delete, Diff, Commit, Rollback) · `M` 🧪
- [ ] `internal/clock/clock.go` — интерфейс `Clock` (`Now`, `AfterFunc`) · `S` 🧪
- [ ] Реализация `RealClock` · `S`
- [ ] Тестовая реализация `FakeClock` в `internal/clock/fake.go` · `M` 🧪
- [ ] Коммит: `feat(store,clock): interfaces` · `M` 🧪

**DoD:** тесты используют `FakeClock`, `time.Sleep` нигде не встречается.

### 0.8. Скрипты и Makefile
- [ ] `scripts/check.sh` — `gofmt -l`, `go vet`, `go test ./...` · `S` 🔌
- [ ] `scripts/demo.sh` — заглушка главного сценария · `S` 📝
- [ ] `Makefile`: `build`, `test`, `lint`, `run`, `demo`, `clean` · `M` 📝
- [ ] Коммит: `chore: scripts + Makefile` · `S` 📝

**DoD:** `make lint && make test` зелёные.

### 0.9. Документация фазы 0
- [ ] `docs/architecture.md` — шаблон (модульный монолит, потоки данных) · `S` 📝
- [ ] `docs/store.md` — шаблон (running/candidate/startup) · `S` 📝
- [ ] `docs/eventbus.md` — типы событий, подписчики · `S` 📝
- [ ] `docs/config.md` — YAML + startup JSON · `S` 📝
- [ ] `docs/adr/0001-record-architecture-decisions.md` · `S` 📝
- [ ] `docs/adr/template.md` · `S` 📝
- [ ] Коммит: `docs: architecture, store, eventbus, config, adr` · `M` 📝

**✅ Phase 0 завершена, когда:** `go run ./cmd/simulator start` поднимается, логирует старт, `make lint && make test` зелёные.

---

## Phase 1 — Model + Store + Router + SNMP v2c 📡

> **Цель:** SNMP-агент отдаёт `ifDescr` и vendor OID RSSI, состояние хранится в Store, Router маршрутизирует.
> **Результат фазы:** `snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2` возвращает `radio0`, `eth0`, `eth1`.

### 1.1. Модели домена radio
- [ ] `internal/model/radio.go` — `RadioLink`, `ATPC`, `ACM`, `ModProfile` · `M` 🧪
- [ ] Теги `path`, `xml`, `json`, `config:"false"` для read-only · `S`
- [ ] `RadioLink.Validate() error` · `M` 🧪
- [ ] Тесты на границы (`tx-power`, `atpc.min < max`) · `M` 🧪
- [ ] Коммит: `feat(model): radio link` · `M` 🧪

**DoD:** `Validate()` покрыт тестами на граничные значения.

### 1.2. Модели домена l2
- [ ] `internal/model/l2.go` — `VLAN`, `MACEntry`, `STPState`, `LLDPNeighbor`, `Interface` · `M` 🧪
- [ ] `Validate()` на каждой · `M` 🧪
- [ ] Коммит: `feat(model): l2` · `M` 🧪

### 1.3. Модели домена sync
- [ ] `internal/model/sync.go` — `PTPClock`, `SyncEState`, `QL`, `ESMC` · `M` 🧪
- [ ] `Validate()` на каждой · `M` 🧪
- [ ] Коммит: `feat(model): sync` · `M` 🧪

### 1.4. Модель устройства и system-info
- [ ] `internal/model/device.go` — `Device`, `SystemInfo` (device-id, uptime, interfaces) · `S` 🧪
- [ ] `SystemInfo.Validate()` · `S` 🧪
- [ ] Коммит: `feat(model): device + system-info` · `S` 🧪

### 1.5. Store: running/candidate/startup
- [ ] `internal/store/memory.go` — `map[string]any` под мьютексом · `M` 🧪
- [ ] `Get(path)`, `Set(path, val)`, `Delete(path)`, `List(prefix)` · `M` 🧪
- [ ] `internal/store/diff.go` — diff candidate vs running · `M` 🧪
- [ ] `internal/store/persist.go` — `Save/ Load` через `encoding/json` + `os.WriteFile` · `M` 🧪
- [ ] `Commit` — валидация + применение + persist, `Rollback` — candidate = running · `M` 🧪
- [ ] Тесты через `t.TempDir()` · `M` 🧪
- [ ] Коммит: `feat(store): running/candidate/startup` · `L` 🧪

**DoD:** diff, commit, rollback, persist/load покрыты тестами.

### 1.6. Router: path ↔ model, OID ↔ path
- [ ] `internal/router/path.go` — парсер `a/b[c=d]/e` · `M` 🧪
- [ ] `internal/router/reflect.go` — навигация по `path`-тегам через `reflect` · `L` 🧪
- [ ] `internal/router/oid.go` — таблица `OID → path` для стандартных MIB (`ifDescr`, `ifOperStatus`) · `M` 🧪
- [ ] Vendor-таблица `1.3.6.1.4.1.99999.*` для RSSI, fade-margin, capacity, alarm-status · `M` 🧪
- [ ] `Dispatch(op Op) Result` для RPC · `M` 🧪
- [ ] Тесты: round-trip `path → model → path`, `OID → path → value` · `M` 🧪
- [ ] Коммит: `feat(router): path + oid dispatch` · `L` 🧪

**DoD:** router находит `interfaces/interface[name=radio0]/radio-link/tx-power` по path и по OID.

### 1.7. Seed-модель по умолчанию
- [ ] `internal/model/seed.go` — `DefaultDevice()` с `radio0`, `eth0`, `eth1` · `M` 🧪
- [ ] Загрузка из `startup.json`, если есть · `M` 🧪
- [ ] Коммит: `feat(model): default seed + startup load` · `M` 🧪

### 1.8. SNMP agent (v2c, get/walk)
- [ ] `internal/snmp/agent.go` — `gosnmp.NewHandler()` · `M` 🔌 🧪
- [ ] `internal/snmp/oid.go` — построение OID-дерева из router · `M` 🧪
- [ ] Обработка `Get`, `GetNext`, `GetBulk` для community `public` · `M` 🧪
- [ ] Порт `:1161` (не `:161`, чтобы не требовать root) · `S`
- [ ] Запуск из `cmd/simulator` · `S`
- [ ] Коммит: `feat(snmp): agent v2c get/walk` · `L` 🧪

**DoD:** `snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2` возвращает `radio0`, `eth0`, `eth1`; vendor OID RSSI отдаёт float.

### 1.9. Метрики (Prometheus)
- [ ] `internal/metrics/metrics.go` — `simulator_uptime_seconds`, `simulator_snmp_requests_total` · `M` 🧪
- [ ] HTTP endpoint `/metrics` на `:9090` · `S` 🔌
- [ ] Коммит: `feat(metrics): prometheus endpoint` · `M` 🧪

### 1.10. Документация SNMP
- [ ] `docs/protocols/SNMP.md` — OID tree, MIB-таблица, traps, примеры `snmpwalk`/`snmptrapd` · `M` 📝
- [ ] Коммит: `docs(snmp): oid tree + examples` · `M` 📝

**✅ Phase 1 завершена, когда:** SNMP walk отдаёт `ifDescr` и vendor OID RSSI; `Validate()` покрыт тестами; `/metrics` работает.

---

## Phase 2 — NETCONF base 🔧

> **Цель:** SSH-сервер с NETCONF subsystem, hello, framing, `get-config`, `edit-config`, `candidate`, `commit`, `discard-changes`.
> **Результат фазы:** `ssh -p 830 -s netconf admin@localhost` работает, golden-тесты проходят.

### 2.1. SSH-сервер
- [ ] `internal/netconf/ssh.go` — `x/crypto/ssh` server · `M` 🧪
- [ ] Любой логин/пароль принимается (см. `desicion.md` §1) · `S`
- [ ] Subsystem `netconf` · `S` 🧪
- [ ] Порт `:830` · `S`
- [ ] Коммит: `feat(netconf): ssh subsystem` · `M` 🧪

### 2.2. Hello и capabilities
- [ ] `internal/netconf/hello.go` — приём/отправка `<hello>` · `M` 🧪
- [ ] Capabilities: `base:1.0`, `base:1.1`, `candidate`, `confirmed-commit`, `notification`, `writable-running` · `S` 📝
- [ ] Session-id (счётчик) · `S` 🧪
- [ ] Коммит: `feat(netconf): hello + capabilities` · `M` 🧪

### 2.3. Framing (EOM и chunked)
- [ ] `internal/netconf/framing.go` — `\n##\n` delimiter + chunked framing (`\n#<len>\n...`) · `L` 🧪
- [ ] Детект режима после hello · `M` 🧪
- [ ] Тесты на оба режима · `M` 🧪
- [ ] Коммит: `feat(netconf): framing eom + chunked` · `L` 🧪

### 2.4. XML RPC-парсинг
- [ ] `internal/netconf/rpc.go` — типы `<rpc>`, `<rpc-reply>`, `<rpc-error>` через `encoding/xml` · `M` 🧪
- [ ] Dispatcher по имени операции · `M` 🧪
- [ ] Коммит: `feat(netconf): rpc parsing + dispatch` · `M` 🧪

### 2.5. get-config
- [ ] `internal/netconf/ops/getconfig.go` · `M` 🧪
- [ ] Subtree-фильтр (упрощённый) · `M` 🧪
- [ ] XPath-фильтр — заглушка · `S`
- [ ] Коммит: `feat(netconf): get-config` · `M` 🧪

### 2.6. edit-config
- [ ] `internal/netconf/ops/editconfig.go` · `M` 🧪
- [ ] Operations: `merge`, `replace`, `create`, `delete`, `remove` · `M` 🧪
- [ ] Запись в candidate · `M` 🧪
- [ ] Ошибка `invalid-value` при провале `Validate()` · `M` 🧪
- [ ] Коммит: `feat(netconf): edit-config` · `L` 🧪

### 2.7. candidate / commit / discard-changes
- [ ] `internal/netconf/ops/commit.go` · `M` 🧪
- [ ] `internal/netconf/ops/discard.go` · `S` 🧪
- [ ] `commit` валидирует и применяет candidate → running, persist в `startup.json` · `M` 🧪
- [ ] Публикация `ConfigChanged` в EventBus · `S` 🧪
- [ ] Коммит: `feat(netconf): commit + discard-changes` · `M` 🧪

### 2.8. Golden-тесты NETCONF
- [ ] `testdata/netconf/edit-config.xml` + `.golden.xml` · `M` 🧪
- [ ] `testdata/netconf/get-config.xml` + `.golden.xml` · `M` 🧪
- [ ] Харнесс с флагом `-update` · `M` 🧪
- [ ] Коммит: `test(netconf): golden tests` · `M` 🧪

### 2.9. Документация NETCONF
- [ ] `docs/protocols/NETCONF.md` — SSH, hello, framing, операции, XML-примеры, коды ошибок · `L` 📝
- [ ] Коммит: `docs(netconf): base operations` · `M` 📝

**✅ Phase 2 завершена, когда:** `ssh -s netconf` работает; edit-config → commit → get-config round-trip; golden-тесты зелёные.

---

## Phase 3 — NETCONF advanced: confirmed-commit + notifications 🔔

> **Цель:** `confirmed-commit` с таймаутом и rollback, `create-subscription` + notifications.
> **Результат фазы:** confirmed-commit откатывается по таймауту; подписка получает notification при событии.

### 3.1. confirmed-commit
- [ ] `internal/netconf/ops/confirmed_commit.go` · `M` 🧪
- [ ] `<confirm-timeout>` (по умолчанию 600с) · `S`
- [ ] `time.AfterFunc` через `clock.Clock` · `M` 🧪
- [ ] `commit` без `<confirmed/>` в течение таймаута отменяет rollback · `M` 🧪
- [ ] Публикация `ConfigChanged` после подтверждения · `S`
- [ ] Тесты через `FakeClock` · `M` 🧪
- [ ] Коммит: `feat(netconf): confirmed-commit with rollback` · `L` 🧪

**DoD:** при отсутствии подтверждения candidate откатывается к running.

### 3.2. create-subscription
- [ ] `internal/netconf/notif/subscription.go` · `M` 🧪
- [ ] Stream `sim-events` · `S`
- [ ] Привязка подписки к EventBus · `M` 🧪
- [ ] Replay (`<replayStartTime>`) — заглушка · `S`
- [ ] Коммит: `feat(netconf): create-subscription` · `M` 🧪

### 3.3. Notification dispatcher
- [ ] `internal/netconf/notif/dispatcher.go` · `M` 🧪
- [ ] Сериализация `Event` → `<notification>` XML · `M` 🧪
- [ ] Отправка всем активным подпискам · `M` 🧪
- [ ] Framing через chunked, если hello 1.1 · `S` 🧪
- [ ] Коммит: `feat(netconf): notification dispatch` · `M` 🧪

### 3.4. Интеграция с EventBus
- [ ] Подписка NETCONF-диспетчера на EventBus при старте · `S` 🧪
- [ ] Тест: `Publish` в EventBus → notification уходит в сессию · `M` 🧪
- [ ] Коммит: `feat(netconf): eventbus integration` · `M` 🧪

### 3.5. Документация notifications
- [ ] Дополнить `docs/protocols/NETCONF.md` разделом про confirmed-commit и notifications · `M` 📝
- [ ] Коммит: `docs(netconf): confirmed-commit + notifications` · `S` 📝

**✅ Phase 3 завершена, когда:** confirmed-commit откатывается по таймауту; подписка получает `<notification>`.

---

## Phase 4 — RESTCONF + L2 switching 🌐

> **Цель:** RESTCONF на chi, домен L2 (VLAN, QinQ, MAC-table, STP/RSTP simplified, LLDP, counters).
> **Результат фазы:** VLAN через RESTCONF; MAC-table через SNMP; STP state виден через RESTCONF.

### 4.1. RESTCONF HTTP-каркас
- [ ] `internal/restconf/server.go` — `chi.Router` · `M` 🧪
- [ ] Порт `:8080`, медиатипы `application/yang-data+json`, `+xml` · `S`
- [ ] `GET`, `PUT`, `PATCH`, `POST`, `DELETE` · `M` 🧪
- [ ] Коммит: `feat(restconf): chi skeleton` · `M` 🧪

### 4.2. URL-структура и маршрутизация
- [ ] `internal/restconf/path.go` — парсинг `/restconf/data/<module>:<path>` · `M` 🧪
- [ ] Маппинг на `router.path` · `M` 🧪
- [ ] Коммит: `feat(restconf): url routing` · `M` 🧪

### 4.3. GET / PUT / PATCH / DELETE
- [ ] `internal/restconf/ops/get.go` · `M` 🧪
- [ ] `internal/restconf/ops/put.go` — replace + validate + apply · `M` 🧪
- [ ] `internal/restconf/ops/patch.go` — merge + validate + apply · `M` 🧪
- [ ] `internal/restconf/ops/delete.go` · `S` 🧪
- [ ] Коды ошибок: 400, 404, 409, 415, 422 · `M` 🧪
- [ ] Коммит: `feat(restconf): CRUD operations` · `L` 🧪

### 4.4. Media types
- [ ] JSON codec через `encoding/json` · `S` 🧪
- [ ] XML codec через `encoding/xml` · `M` 🧪
- [ ] Согласование `Accept`/`Content-Type` · `S` 🧪
- [ ] Коммит: `feat(restconf): json + xml codecs` · `M` 🧪

### 4.5. L2: VLAN + QinQ
- [ ] `internal/l2/vlan.go` — CRUD VLAN, QinQ outer/inner · `M` 🧪
- [ ] Обработка members (tagged/untagged) на интерфейсах · `M` 🧪
- [ ] Валидация: VLAN id 1–4094, QinQ outer ≠ inner · `S` 🧪
- [ ] Коммит: `feat(l2): vlan + qinq` · `M` 🧪

### 4.6. L2: MAC table
- [ ] `internal/l2/mac.go` — learning, aging (через `clock.Clock`) · `M` 🧪
- [ ] VLAN-aware таблица · `M` 🧪
- [ ] Публикация MAC-таблицы через SNMP `dot1dTpFdbPort` · `M` 🧪
- [ ] Коммит: `feat(l2): mac table` · `M` 🧪

### 4.7. L2: STP/RSTP simplified
- [ ] `internal/l2/stp.go` — state machine (Disabled, Blocking, Listening, Learning, Forwarding) · `M` 🧪
- [ ] Root election (упрощённая) · `M` 🧪
- [ ] Публикация состояния в EventBus · `S` 🧪
- [ ] Тесты табличных переходов · `M` 🧪
- [ ] Коммит: `feat(l2): stp/rstp state machine` · `L` 🧪

### 4.8. L2: LLDP
- [ ] `internal/l2/lldp.go` — статические соседи из конфига · `M` 🧪
- [ ] Периодические TLV (упрощённо) через `clock.Clock` · `S` 🧪
- [ ] Коммит: `feat(l2): lldp` · `M` 🧪

### 4.9. L2: interface counters
- [ ] `internal/l2/counters.go` — rx/tx bytes, packets, errors, drops · `M` 🧪
- [ ] Инкремент при симуляции трафика · `M` 🧪
- [ ] Публикация через SNMP `ifTable` · `M` 🧪
- [ ] Коммит: `feat(l2): counters` · `M` 🧪

### 4.10. Broadcast storm simulation
- [ ] `internal/l2/storm.go` — API-эндпоинт инжекции · `M` 🧪
- [ ] Rate-limit → alarm · `M` 🧪
- [ ] Коммит: `feat(l2): broadcast storm simulation` · `M` 🧪

### 4.11. SNMP-привязка L2
- [ ] Заполнить `internal/router/oid.go` MIB-II (ifTable) и BRIDGE-MIB · `M` 🧪
- [ ] Тест: `snmpwalk ...1.3.6.1.2.1.17.4.3.1.2` возвращает MAC-port · `M` 🧪
- [ ] Коммит: `feat(snmp): l2 mib bindings` · `M` 🧪

### 4.12. Документация L2 + RESTCONF
- [ ] `docs/protocols/L2.md` — 802.1Q, QinQ, MAC, STP/RSTP simplified, LLDP, counters, MIB-соответствие · `L` 📝
- [ ] `docs/protocols/RESTCONF.md` — URL, media types, коды ошибок, примеры `curl` · `M` 📝
- [ ] Коммит: `docs: l2 + restconf` · `M` 📝

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
