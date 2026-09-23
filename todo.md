# Constructor v1 — implementation checklist

Цель: привести Constructor к v1 по нормативным страницам
[`liapoldus.github.io/constructor`](../liapoldus.github.io/constructor/). Этот
лист — рабочая фиксация состояния, а не замена спецификации. Пункт отмечается
выполненным только после проверки реализации и соответствующего теста. Коммиты,
push и публикация не входят в критерий завершения без отдельного запроса.

## Подтверждённая основа

- [x] Constructor и `@liapoldus/react` остаются отдельными проектами и
  репозиториями.
- [x] React SDK source snapshot в scaffold и fixture синхронизирован с
  `react-lib/src/index.ts`.
- [x] Project scaffold создаёт versioned Site и локализованный content-файл по
  `liapoldus/content/<site-id>/<locale>.json`.
- [x] Content v1 задаёт `schemaVersion`, document `id` и instances с `id`,
  обязательными `pageId`, `component` и `fields`; неизвестные JSON-поля и
  повтор instance ID отклоняются.
- [x] Content write до repository mutation сверяет site/page/component/schema,
  проверяет поля и передаёт optimistic revision; unit tests покрывают отказ
  без записи и корректное сохранение revision.
- [x] Generated locale и preview используют полную структуру
  `pages → instances → fields`; SDK hook адресует значение по
  `(pageId, instanceId, fieldKey)`.
- [x] Обновлены project-format, content-assets, SDK и API docs. VitePress build
  проходит.
- [x] Базовые проверки текущего среза проходят: Go tests, TypeScript check,
  Constructor UI build/API test, fixture build, SDK build/typecheck/tests.
- [x] API отклоняет legacy/malformed content file paths до вызова repository;
  тест покрывает обход structured-content validator.
- [x] UI использует стабильные Site page IDs, а не slug display label; если
  документ содержит `{id,name}`, отображается имя, в content сохраняется ID.
- [x] Scaffold project manifest, Site document и development route используют
  общий `home` page ID; registry/integration test ловит несовпадение.
- [x] Отсутствующий content выбранной locale больше не подменяет демонстрационное
  содержимое; locale fallback не подменяет ETag целевого файла при сохранении.
- [x] Live browser smoke загрузил manifest, Site pages/instances/schema и routes;
  добавлены instances разных components/pages, Inspector редактировал draft,
  preview session запустилась и штатно остановилась; browser console чистая.
- [x] Editor draft получил dirty/saving/saved/conflict/error state и явную
  кнопку Save; Site/Locale/Project switch сохраняет dirty draft в памяти и
  возвращает его по ключу Project/Site/Locale. Активные Site/Locale с draft
  помечены в selector.
  Save сохраняет снимок с текущим ETag, оставляет более новые параллельные
  правки dirty и не теряет draft при 409/412. Build/Deploy не продолжаются при
  ошибке сохранения.
- [x] Выбранный component instance можно удалить после подтверждения; удаление
  остаётся draft до явного Save. API smoke на отдельной временной копии проекта
  подтвердил `If-Match`: успешная запись возвращает 200, stale revision — 409.
  Исходный fixture не изменён.
- [x] API bind address и Vite proxy target поддерживают явную настройку для
  изолированных локальных запусков; проверен browser smoke загрузки UI с
  отдельным API-портом.
- [x] Локальная editor history поддерживает Undo/Redo, восстанавливает
  selection вместе с content, объединяет последовательный ввод в одно поле в
  короткий шаг, ограничена 100 шагами и очищает redo-ветку при новой правке.
  Undo к сохранённому baseline снимает dirty; после Save baseline двигается на
  зафиксированный снимок. Есть toolbar controls и Cmd/Ctrl+Z, Shift+Z, Ctrl+Y;
  unit-тесты проверяют coalescing, undo/redo, selection и границу истории.
- [x] Validation diagnostics получили source coordinates `pageId`, `instanceId`,
  `fieldKey`; backend content validators прикладывают их к errors. Problems panel
  показывает code/severity/path/message, переход выделяет target instance/page
  и фокусирует field; diagnostics кэшируются отдельно по Project/Site/Locale и
  редактирование очищает только диагностику соответствующего поля. Domain,
  application и editor tests покрывают координаты и lifecycle.
- [x] `POST /api/v1/project/validate` без query проверяет все Sites и только их
  enabled locales; явный `siteId`/`locale` принимается только парой, скрытых
  `local-site`/`ru-RU` defaults нет. Integration test ловит пропущенную active
  locale и проверяет, что disabled locale не валидируется.
- [x] Frontend нормализует пустые history/RBAC collections (`null` → `[]`) на
  API boundary; дефект найден живым browser run.
- [x] Project registry не показывает workspace directories, чья basename не
  совпадает с manifest ID; предотвращает дублирующийся project ID и ошибочную
  активацию случайных временных worktrees. Зафиксировано infrastructure test.
- [x] Frontend API boundary отклоняет duplicate project IDs вместо рендера
  неоднозначных options; проверено на повторённом registry response. Живой
  просмотр обнаружил старый локальный API process на `127.0.0.1:8787`, который
  всё ещё возвращает несколько одинаковых IDs; UI теперь сообщает ошибку
  registry без React key warnings. Процесс не перезапускал и не останавливал.
- [x] Последняя проверка с registry/API boundary изменениями: `go test
  -count=1 ./...`, `go test -race ./...`, `npx tsc --noEmit`, `npm test`,
  Constructor UI build, scaffold fixture build, SDK build/tests, VitePress docs
  build — успешны.
- [x] Active-project Site API (`GET /api/v1/project/sites`) перечисляет только
  canonical JSON Site documents через project repository port и cross-checks
  schema version, path ID, project ID и uniqueness. API integration + service
  tests добавлены.
- [x] Generic structured writes валидируют Site document и Component schema до
  repository mutation; invalid Site diagnostics типизированы как 422.
- [x] UI загружает список Site из активного project, выбирает существующий Site
  (или первый доступный), отображает страницы из его document и не создаёт
  deployment Site как побочный эффект редакторского запуска.
- [x] Route fixture теперь адресует `home/about` stable IDs, согласованные с
  Site content `pageId` и source filenames; Routes остаются валидны с manifest
  page IDs.
- [x] После этих изменений прошли `go test -count=1 ./...`, `go test -race
  ./...`, `npx tsc --noEmit`, frontend/API tests, Constructor + fixture builds и
  VitePress build.
- [x] Docs API inventory описывает `GET /api/v1/project/sites`, его project
  boundary и typed-validation failures; VitePress build проверен.
- [x] Project manifest получает strict decoder для unknown/trailing JSON,
  schemaVersion, ID/name/react entry и повторяющихся page/component IDs.
  Filesystem Manifest, registry List/Get и generic manifest writes используют
  общий domain validator.
- [x] Route fixture/test используют stable page IDs и проверяют source
  filenames от ID, не от React route path.
- [x] Удалена flat `Project.content` копия и hard-coded Demo project fallback:
  loaded projects теперь строятся только из валидного manifest, Site и
  ContentDocument; Instance fields имеют один canonical источник.
- [x] Удалены неиспользуемые frontend wrappers/types для старого delivery Site
  create и Git metadata fetch; эти панели ещё не реализованы и останутся в
  checklist до появления соответствующего UI.
- [x] Plugin integration boundary синхронизирован с новым protocol direction:
  Constructor остаётся REST, не импортирует `pluginprotocol` и не соединяется с
  plugin loopback; Admin UI использует только Gateway fixed internal REST API и
  canonical Admin UI contract.
- [x] Plugin protocol source проверен в соседнем repo: gRPC PluginService
  (`Manifest`, `ConfigSchema`, `ConfigApply`, `Shutdown`, generic `Call`, bidi
  `Stream`), standard health/reflection; Admin UI schema остаётся versioned
  JSON contract, не proto copy. На проверенном HEAD
  `pluginprotocol/e91646a` transport классифицирует gRPC `Canceled` и
  `DeadlineExceeded` как `context.Canceled` / `context.DeadlineExceeded` для
  dial, health, handshake, call и shutdown; protocol tests/client fixture
  подтверждают cancellation mapping. Constructor transport не копирует и
  остаётся на Gateway REST boundary.
- [ ] Gateway follow-up вне этого репозитория: plugin adapter пока сводит
  in-flight `context.Canceled` из protocol `CallJSON` к `ErrPluginUnavailable`
  (deadline сохраняет тип). Это подтверждено в текущем `core` source и
  `go test ./internal/infrastructure/plugins`; протащить caller cancellation
  через Gateway operation/API boundary. Реализация только в core с обязательными
  отдельными red-test и implementation commits.

## P0 — закончить основной редакторский сценарий

- [x] Загружать и сохранять content выбранных Site и Locale без
  hard-coded `local-site` / `ru-RU`; Site-specific helpers требуют явный контекст,
  project validation без контекста проходит все Sites/locales. Удалён также
  неиспользуемый startup snapshot prepare callback с локализованным default.
- [x] Live browser открыл существующий project после hard reload: показаны обе
  Site pages, все content instances, schemas и routes; сохранённое content
  восстановлено. Ошибка открытия попадает в Problems, hard-coded demo fallback
  удалён и покрыт API/frontend tests.
- [ ] Site API/UI: list для всех `liapoldus/sites/*.json` подключён к активному
  Project; Page creation теперь добавляет form в Explorer и revision-checked
  batch API для manifest/Site/development route/source; filesystem batch
  preflights revisions, blocks traversal/symlink escapes и поддерживает rollback.
  Rename и reorder в Explorer сохраняют только Site page labels/order через
  single-document If-Match update; stable page IDs остаются неизменными. Site
  rename и Create Site уже реализованы с validation/If-Match и atomic batch;
  остаются Site delete/editor и deployment target picker. Create Site клонирует
  pages/locales выбранного Site и создаёт пустые content documents через atomic
  manifest/source revision-checked batch; Explorer включает новую Site в список
  и переключает на неё. Application/HTTP tests проверяют initialized locales,
  stale revisions, collisions, no partial writes и `content.write` denial.
  Explorer умеет удалить page из
  выбранного Site после подтверждения; это сохраняет общий manifest page ID и
  TSX source, блокирует действие при draft instances, а backend — при оставшихся
  persisted content/route refs.
  Проверки этого среза: `go test -race ./...`, `go vet ./...`, TypeScript,
  20 frontend/API tests, Constructor Vite build и VitePress docs build проходят.
- [x] Site page object `{id,name}` поддержан domain decoder, cross-file
  validation и editor: route/content identity использует stable ID, одинаковые
  display labels допустимы. Старые string-only Site pages читаются как
  `id == name`; scaffold и fixture создают объектную форму.
- [x] Проверить в браузере: выбрать страницу → добавить несколько instances
  разных components → переключать instances → менять поля → видеть именно эти
  значения в preview → сохранить с ETag → перезагрузить и получить те же данные.
- [x] Browser smoke обнаружил и исправил preview bridge: sandbox iframe сообщает
  opaque origin `null`, который SDK ранее отвергал; повторяемый ready/ack
  handshake устраняет гонку инициализации. Live browser подтвердил изменяемый
  preview, Save с ETag (HTTP 200) и reload с тем же значением; console errors нет.
- [x] Полная проверка текущего среза после bridge fix: SDK build/typecheck/9
  tests; Constructor typecheck/15 tests/Vite production build; `go test -count=1
  ./...`, `go test -race ./...`, `go vet ./...`; VitePress build; vendored SDK
  source совпадает со sibling source.
- [x] Выбор Component instance — отдельный selection state; удаление выбранного
  instance требует подтверждения, сохраняется optimistic revision check;
  `npx tsc --noEmit`, frontend/API test, Vite build и Go tests проходят.
- [x] Draft ведётся для полного content document и независимо кешируется по
  Project/Site/Locale; изменение выбранного instance не затирает sibling data.
  Unit tests проверяют multi-instance update и runtime-content mapping всех
  instance/page/fields; live browser preview→save→reload gate пройден.
- [x] Добавить локальные undo/redo content-draft и понятное состояние
  dirty/saved/conflict; история совместима с optimistic save revision.
- [x] Показывать validation diagnostics у соответствующего page/instance/field;
  сохранять их при переключении Site/Locale и очищать целевую диагностику при
  изменении поля.
- [x] Сверить route/page ID с Site document для кириллицы, пробелов и одинаковых
  display labels; page ID должен быть стабильным и не выводиться из label там,
  где уже существует явный ID. Проверены unicode display labels, повторные labels,
  stable ID cross-file validation и маршрут по page ID.
- [x] `ProjectService.Validate` теперь проверяет все Sites и их enabled locales;
  scaffold preview test и service tests используют project-wide semantics без
  legacy `local-site/ru-RU` default.
- [x] Scaffold smoke стал multi-page: добавляет `about` в manifest, Site,
  content и development route; проверяет source через preview, generated locale,
  routes и static bundle после immutable build.

## P1 — project format, schema и generator

- [ ] Описать/сверить JSON Schema для Project, Site, React routes, Content,
  Component, Theme, Assets, Infrastructure и Plugin documents; отвергать
  неизвестные ключи и несовместимые версии единообразно. Published structural
  schemas теперь есть для Project, Site, Routes, Content, Component, Assets и
  Theme; Infrastructure/Plugin schemas и единый runtime schema validator ещё
  отсутствуют.
- [ ] Runtime strict decoder для Project/Site/Component и route/content/asset
  validators есть; миграции и единая validation contract ещё отсутствуют.
  Published Project/Site/Routes/Content/Component/Assets/Theme schemas описывают
  structural contract; runtime schema integration и остальные document schemas
  остаются. Theme strict decoder, typed value and variant validation реализованы.
- [x] Все 8 published schemas проходят JSON Schema Draft 2020-12 meta-validation
  и принимают соответствующие project-fixture documents; docs `npm run build`
  проходит. Cross-file rules остаются отдельной application responsibility.
- [x] Убраны расхождения Project/Component reference schemas: фактические
  manifest fields и project ID contract, поддерживаемые runtime component
  field types и `nullable`; manifest Component IDs отклоняют path-unsafe формы.
  Domain tests, VitePress build и JSON parsing прошли.
- [x] Cross-file validation страниц: content→Site, route→union Site pages,
  Site page delete checks content/routes before mutation, manifest page removal
  checks Sites/routes; manifest component removal checks content references,
  every declared component requires a matching schema/source, and schema edits
  cannot invalidate saved instances. `/validate` scans every route document and
  enabled locale. Unit tests verify typed diagnostics and zero writes on rejected
  mutations.
- [x] Cross-file asset/theme validation и collection-scoped entity uniqueness.
  Asset v1 registry/reference/type/size/SHA-256,
  Site→Theme/Component token references и used-token deletion guard реализованы.
  Проверены и покрыты diagnostics uniqueness в собственных scope: Project pages
  и components (case-insensitive), Site pages/locales, Content instances within
  document, Component fields, route IDs/paths, asset IDs/paths и Theme IDs;
  глобальная уникальность между разными типами entity не вводилась, так как
  документация такого ограничения не задаёт.
- [x] Согласовать каноническую модель Site page `{id,name}` между документацией
  и runtime; manifest и route `page` хранят только stable IDs, а UI показывает
  display names. Совпадающие labels не влияют на route/content references.
- [x] Content validator: SDK и Go согласованы по field types, defaults,
  `nullable`, select `allowedValues` и форме asset/entity references. Неполный
  required localized draft можно сохранить; строгая проверка каждого enabled
  locale блокирует generation/snapshot и указывает field coordinates; domain и
  application tests покрывают обе политики, `go test ./...`, race и vet проходят.
- [ ] Nested item constraints для `object`/`array` остаются вне v1: текущая
  schema намеренно не поддерживает nested schemas, что зафиксировано в
  `constructor/components.md`.
- [x] Реализовать locale fallback `locale → language → default` при validation
  и generation; target locale всё равно обязан быть включён в Site, артефакт
  записывается под target locale, а source revision не используется для записи.
- [x] Санитизировать rich-text только на generation boundary через HTML5 parser:
  allowlist formatting tags, удаление active-content subtrees и непредусмотренных
  attributes, scheme-check ссылок и bounds input/tree size; malicious/nested
  fixtures подтверждают сохранение canonical source без mutation.
- [ ] Генерировать deterministic artifacts для всех включённых locales,
  routes, theme CSS и asset manifest; не терять instances и не зависеть от map
  iteration order. Реализованы all-locale content, routes и проверенный asset
  manifest и typed CSS tokens выбранной Site Theme; артефакты сортируются и
  одной revision-checked batch-операцией публикуются при создании snapshot.
- [ ] Генерация должна выполняться на immutable revision/worktree, атомарно
  публиковать artifacts и возвращать typed diagnostics/build metadata.
  Snapshot теперь закрепляет входную рабочую revision, генерирует артефакты в
  отдельном detached Git worktree и хранит final revision; active checkout не
  меняется, временные worktree/base refs очищаются. Проверено инфраструктурным
  Git test, HTTP success/failure smoke и Node/Vite build smoke: правка generated
  content после snapshot не попадает в pinned build. Typed diagnostics
  возвращаются. Snapshot сохраняет project ID и приватный repository locator;
  worker строит из закреплённого проекта даже после переключения активного
  workspace. Locator требует неизменного пути проекта до Build; registry-based
  relocation и отдельные артефактные metadata ещё не реализованы.
- [ ] Поддержать explicit schema migrations `vN → vN+1` с dry-run, diff,
  diagnostics и Git boundary; не выполнять implicit legacy migration.
- [x] Дополнить SDK API/типы и runtime contract тестами `component`, `primitive`,
  schema-driven props, content/assets/images, reactive/computed/state/action,
  route/navigation, theme/locale, preview isolation и generated artifacts;
  production bundle не обращается к Constructor API/fetch, `npm pack --dry-run`
  содержит runtime JS/declarations, package metadata и MIT license.
- [x] Проверить production SDK/runtime на отсутствие Constructor API, fetch и
  случайных dev dependencies; typecheck покрывает exported props/types, package
  metadata указывает React peer dependency, bundle содержит только SDK runtime.
- [x] Cross-file validation slice прошёл `go test -count=1 ./...`,
  `go test -race ./...`, `go vet ./...`; нормативная project-format страница
  прошла VitePress build.

## P1 — Site editing и визуальный workspace

- [ ] AppShell: навигация по режимам, command palette, shortcuts, panels,
  responsive canvas и accessibility/keyboard focus. Command palette доступна
  по Cmd/Ctrl+K, фильтрует Save/Build/Deploy/Undo/Redo и поддерживает Escape,
  стрелки и Enter; теперь Tab удерживает фокус внутри модального диалога, а при
  закрытии фокус возвращается на открывший элемент. Playwright smoke подтвердил
  `Shift+Tab` на последний доступный command, `Tab` обратно в поиск и Escape с
  восстановлением фокуса на trigger. Тот же UI smoke выявил старый внешний API
  на 127.0.0.1, возвращающий duplicate project ID/404; его не останавливал и не
  менял. Остальная shell-навигация и полный accessibility audit остаются.
- [ ] Explorer: Sites → Pages → component instances; create/rename/reorder и
  удаление page из Site с confirmation и cross-file reference safety реализованы;
  остаются Site CRUD и полноценное управление несколькими Site.
- [ ] Canvas: настоящий React route/page renderer с selection overlay,
  responsive breakpoints и безопасным изолированным preview; не подменять
  page-builder static mockup. Изолированный настоящий Vite/React preview уже
  работает, добавлены Desktop (1440), Tablet (768) и Mobile (390) viewport
  controls. Browser smoke на 1200px и 390px viewport подтвердил отсутствие
  horizontal overflow, правильное clamping width и работающий preview без
  console errors. Добавлен SDK `PreviewInstance(instanceId)` с layout-neutral
  DOM-bounds outline, draft bridge доставляет stable selected instance ID;
  scaffold и fixture размечают content instance. Остальные project-authored
  pages должны размечать instances явно; automatic arbitrary-JSX instrumentation
  и breakpoint-specific rendering/assertions ещё не реализованы.
- [ ] Inspector: controls по типам schema, defaults, assets, localization,
  validation, permissions и optimistic revisions.
- [ ] Source-owned primitives на Radix/shadcn pattern; design tokens отдельно
  от domain-модели и CSS framework.
- [ ] Navigation canvas с node/edge моделью, actions, link validation и
  reference diagnostics.
- [ ] Problems panel: сортировка severity, переход к source/entity, stale
  diagnostic lifecycle и build logs. Сортировка реализована: error → warning →
  info → неизвестная severity с сохранением исходного порядка внутри уровня;
  чистый helper покрыт unit tests. Полный build-log viewer ещё отсутствует.

## P1 — Assets, Theme и Localization

- [x] Asset metadata v1 model в `liapoldus/assets.json`, local binary references
  под `public/assets/`, typed content-reference checks и size/SHA-256 integrity
  до structured write; domain/application tests покрывают valid/dangling refs
  и rejection без repository mutation.
- [x] Проверки после asset v1 slice: `go test -count=1 ./...`, `go test -race
  ./...`, `go vet ./...` и VitePress `npm run build` успешны.
- [x] Asset registry UI/API: добавлен `GET /api/v1/project/assets`, который
  валидирует metadata и integrity binary, и Inspector выбирает registered image,
  icon/file references; Image preview и alt редактируются в content draft.
  API/domain/HTTP tests покрывают canonical response и checksum mismatch.
  Изолированный Playwright smoke подтвердил переключение asset, обновление
  preview и dirty draft; console без ошибок. Search/categories, reuse и загрузка
  из Inspector теперь подключены.
- [x] Asset upload API/storage lifecycle: raw image bytes до 25 MiB; MIME и
  декодируемые dimensions определяются по содержимому (PNG/JPEG/WebP/GIF), SVG
  проходит XML allowlist sanitizer; Constructor генерирует content-derived ID и
  безопасный path, deduplicates SHA-256, а binary + registry публикует атомарным
  batch. UI принимает разрешённые типы, добавляет новый Asset в каталог и
  выбирает его в draft. Invalid/oversized content, SVG active/external payloads,
  duplicate bytes и permission boundary покрыты тестами.
- [x] Image pipeline: PNG/JPEG/WebP получают metadata и WebP variants 320/640/
  1280/1920 px (без upscale), quality 82, без crop, с сохранением aspect ratio
  и оригиналом как fallback; GIF/SVG остаются исходными. Width/height, variant
  metadata, size и SHA-256 проверяются, binaries+registry пишутся одним batch,
  generated runtime manifest содержит `srcset` entries. Upload, registry,
  generator и GIF/original fallback tests покрывают producer lifecycle.
- [x] Asset picker search/categories, reuse workflow and SVG/icon sanitization;
  Inspector теперь выполняет case-insensitive search по ID/path/MIME/type и
  фильтрует категории по зарегистрированному asset type; helper покрыт unit
  tests. Reuse через повторный выбор одного stable asset ID и upload checksum
  deduplication работают. SVG sanitizer strip-allowlist не сохраняет tags,
  атрибуты и внешние references вне безопасного набора. User-defined category/tag
  model ожидает отдельного product decision.
- [x] Theme schema/token validation и CSS generation: categories colors,
  typography, spacing, radii, shadows, breakpoints, animations; strict value
  validators, light/dark overrides, deterministic token ordering, system dark
  preference, Site `themeId` picker/selection, theme list API and reference checks.
- [x] Theme Editor создаёт Theme starter document, редактирует typed base tokens
  и light/dark overrides, Site выбирает тему из validated list; save использует
  ETag/If-Match, поэтому stale token edits не теряются.
- [x] Component `themeTokens` и cross-file validation/used-token deletion guard
  объявления token references в Component schema; arbitrary source-code CSS не
  сканируется и не блокирует удаление.
- [x] Locale editor/fallback и enabled-locale snapshot rules.
  File-per-Site/Locale принят как каноническая модель; semantics согласована:
  `localized:true` читает exact → language → default, `localized:false` читает
  только shared default, а instance/page/component structure едина между
  locale-документами. Generator и validation собирают resolved view, отклоняют
  несовпадающую структуру и имеют regression tests для shared/localized
  значений и precedence. Inspector показывает field scope; отдельный `Default
  values` editing scope редактирует fallback, не меняя enabled locales.
  `/api/v1/project/content` отдаёт resolved view и revisions exact/default;
  atomic split-write разносит shared/localized values, синхронизирует instance
  structure между существующими locale-документами и отвергает stale revisions
  без partial writes. Draft save допускает незаполненные required localized
  values, validation/build остаются строгими. Field-aware three-way merge
  объединяет disjoint edits и оставляет overlapping conflicts без записи.
  Editor показывает fallback provenance. Site UI теперь может добавлять/отключать locale с If-Match; отключение не
  удаляет locale file/draft, а last-locale removal запрещён. `site-management.md`
  описывает поведение. Проверки текущей реализации: `go test -count=1 -p 1
  ./...`, `go test -race -p 1 ./internal/application ./internal/presentation`,
  `go vet ./...`, 42 frontend/API tests, TypeScript check, Constructor Vite
  production build и VitePress docs build прошли. Tests покрывают resolved
  fallback, общий/default editing scope, локальные override, atomic split-write,
  propagation структуры, stale-revision rollback и field-aware merge.

## P1 — API, persistence и безопасность

- [ ] Сопоставить каждый endpoint из `constructor/api.md` с handler, typed
  request/response, auth/grant, idempotency, concurrency, audit и failure tests.
  Snapshot→Build и Build→Deployment path endpoints теперь подключены и используют
  те же application services/auth guards; rollback path требует exact target
  confirmation. Все документированные mutating endpoints теперь входят в
  permission matrix test; неизвестные mutation routes закрыты fail-closed.
  Проверка с авторизатором, запрещающим grant, подтверждает HTTP 403 до dispatch;
  read-only `/validate` POST остаётся доступен, а unsupported method у известного
  endpoint сохраняет 405 для авторизованного caller.
  Остаётся полный endpoint inventory/typed contract/idempotency/concurrency/error/audit
  coverage, включая Gateway binding и Plugin Admin surface.
- [ ] Стабильные machine error codes/RFC 9457 problem responses; никаких raw
  internal errors/stack traces или секретов в HTTP response. RFC problem layer
  и stable codes для HTTP errors реализованы, request IDs выдаются на success
  и error responses; snapshot generation также сериализует structured/content
  diagnostics как `project_validation_failed` с исходными diagnostic codes.
  Остаётся полный endpoint-by-endpoint error catalog audit.
- [x] `application/problem+json` responses используют RFC 9457 core fields,
  stable `code`, `requestId`, `instance`; validation diagnostics/revision
  conflict идут расширениями, 5xx details redacted. Все method-not-allowed
  ответы переведены на problem contract; tests покрывают 400/405/500 и success
  request ID и неизменность canonical file body. Frontend API boundary
  преобразует problem details и diagnostics в user-facing сообщения;
  typecheck/API tests/production build прошли.
- [x] HTTP request body limits: operational JSON — 4 KiB, structured project
  file/route/merge writes — 10 MiB; превышение возвращает `413
  payload_too_large`; typed JSON rejects unknown keys and bodies must contain
  exactly one value. Проверки включают точную границу, превышение лимита,
  неизвестные поля и несколько JSON values.
- [x] File conflict response carries expected/current revisions and current /
  candidate blobs; editor retains the baseline as merge base and keeps draft
  data. Three-way merge is read-only, returns the exact current ETag, and the
  editor retries only through explicit `If-Match` write. Tests cover disjoint
  edits without data loss plus overlapping edits with no file mutation; UI
  exposes the safe merge action and keeps drafts on another conflict.
- [ ] Project open/create/clone/worktree и repository boundary: path traversal,
  symlink, limits, atomicity, cancellation, process environment allowlist.
  Project-ID-scoped GET/HEAD/PUT now target the selected registry root without
  activation, reuse active-project mutation serialization, enforce structured
  validation and optimistic revisions, and reject lexical traversal/symlink
  path components. The file adapter also denies `.git`, `.env*` and
  `node_modules`; HTTP tests prove inactive-project access does not switch the
  active root and stale writes do not replace current data. `POST
  /api/v1/projects/{id}/validate` validates an inactive project through its
  isolated root. Project scaffold and initial commit run in hidden staging and
  publish with one atomic no-replace directory rename; failure/cancellation
  removes staging, and registry discovery rejects symlinked project
  roots/metadata/manifests. Clone publication now uses the same atomic
  descriptor-relative no-replace rename, so incomplete trees are never visible
  and existing targets cannot be replaced. Worktree publication now uses the
  same atomic no-replace directory rename followed by `git worktree repair`;
  cancellation during repair triggers bounded background repair/cleanup.
  Tests prove existing target contents survive, cancellation returns promptly,
  and real Git can repair/remove a worktree moved before its metadata update.
  A fsynced, versioned recovery record is written before `git worktree add`,
  updated before publication, and cleared only after repair/cleanup. Startup
  scans and validates those records before serving HTTP, repairs and removes an
  interrupted worktree, and fails closed on mismatched or unsafe paths. A
  restart test verifies target removal, Git registration cleanup and journal
  deletion; a negative test proves a tampered out-of-workspace path is not
  touched. On
  macOS/Linux, project-file reads, single writes, listings and batch
  staging/publication now traverse anchored
  directory descriptors with `O_NOFOLLOW`; final symlinks and non-regular
  leaves are rejected, and batch rollback uses descriptor-relative rename and
  unlink. This removes the prior check-then-use race in the project-file
  adapter. `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`,
  Constructor `npm test`/typecheck/production build and docs `npm run build` all
  pass with this implementation. The filesystem/repository manager and Linux
  `openat`/no-replace rename sources cross-compile successfully on this host.
  Full Linux app/runtime tests remain open because its CGO-disabled cross
  compile omits SQLite driver types. Git clone/worktree use context-aware process-group
  cancellation and cleanup, target/source containment with symlink checks,
  staging, full commit IDs, HTTPS/SSH/local-source policy, redacted subprocess
  failures, and an explicit environment allowlist. Project-create and
  registry-symlink tests pass. Tests exercise local clone, detached worktree,
  failed-operation cleanup, traversal/symlink rejection,
  cancellation of a child process, and environment isolation. The allowlist
  forwards PATH, HOME, and SSH agent variables only; user global Git config is
  retained for credential helpers while system config/hooks and `ext::` are
  disabled. Project initialization uses the same child-process environment
  boundary and cancellation cleanup.
- [ ] SQLite-compatible operational storage migrations, transaction boundaries,
  recovery и concurrency; operational state не смешивать с Git source of truth.
  Delivery-store schema creation is now tracked in five ordered SQLite
  migrations, each applying its DDL/data seed and history row in one transaction;
  old snapshots/builds/deployments retain their values while columns are added.
  Reopen/idempotency, unsupported-future-version and failed-index migration
  rollback tests pass; the slice passes `go test -count=1 ./...`,
  `go test -race ./...` and `go vet ./...`. Remaining storage work includes
  broader migration fixtures, durable job/recovery coverage and concurrency
  audit across every operational record family.
- [ ] Session/auth/RBAC/grants для каждой mutation; immutable system admin
  invariant и отрицательные authorization tests. Grant middleware now defaults
  every unclassified mutation to `admin.roles`; documented write routes are
  covered by a permission-matrix test, explicit read-only validation POSTs remain
  grant-free, and a negative HTTP test verifies denial before route dispatch.
  Full session lifecycle, durable users/roles store and per-resource ownership
  authorization still require audit/implementation.
- [ ] Secrets хранить только как `secret://` references; redact logs, preview,
  subprocess env и API diagnostics.
- [ ] Browser origin/CSRF/CORS policy, Content Security Policy и sandbox
  integration tests для loopback preview. API now rejects non-loopback or
  opaque `Origin` on every request and non-loopback `Host` at the HTTP server;
  bind configuration accepts only literal loopback IPs. Tests cover reads,
  writes, DNS-rebinding hostnames, invalid ports and the supported localhost,
  IPv4 and IPv6 forms. `go test -count=1 ./...`, `go test -race ./...`,
  `go vet ./...` and VitePress `npm run build` pass. Remaining: CSP policy and
  live browser-origin/preview security smoke across supported shells.

## P1 — Git, Snapshot, Build, Deploy и rollback

- [x] Git panel читает status, HEAD-to-working-tree diff, branches и history
  только активного project; commit доступен только явной кнопкой с заданным
  сообщением и блокируется при несохранённом Inspector draft. Исправлены два
  backend дефекта: for-each-ref использует настоящий tab delimiter, diff
  включает staged и unstaged изменения. UI/API tests и filesystem test покрывают
  контракты; browser smoke на созданном временном Git project подтвердил branch,
  clean state и изменённый content после Save. Local branch create/checkout
  теперь реализованы отдельно ниже.
- [ ] Git integration: remote binding/auth, pull/push и
  полный conflict workflow; credential handling не должен попадать в logs.
  Реализованные локальные status/diff/commit/branches/history и snapshot/worktree
  операции переведены на общий Git runner: allowlisted child environment,
  сохранение пользовательского `HOME`/global config, process-group cancellation,
  2–5 минут timeout и отсутствие raw Git stderr в errors. Fake-Git test
  подтверждает удаление произвольного secret env, caller `GIT_INDEX_FILE` и
  override terminal/system-config flags, а также redaction stderr при ошибке.
  Полный Go suite, race и vet прошли. Local create/checkout endpoints требуют
  чистый worktree и возвращают stable problem codes; UI также блокирует их при
  editor drafts и перечитывает project после branch switch. Remote operations,
  conflict workflow и request-context propagation остаются.
- [x] Snapshot привязан к pinned Git commit + project/Site/locale identity;
  tests проверяют locale isolation и repeatability для одной revision/scope.
  Capture сверяет hash дерева двумя чтениями через временный index и возвращает
  typed `409 snapshot_source_changed`, если files меняются во время операции;
  Git regression test детерминированно моделирует гонку clean-filter mutation
  и проверяет, что snapshot ref не остался. Constructor-managed writes
  сериализованы до capture; immutable final revision собирается в detached
  worktree. SQLite migration и active-workspace switch covered by tests.
- [x] Рабочий tree materialization уже создаёт отдельный snapshot commit без
  изменения index/HEAD; snapshot commit теперь закрепляется под
  `refs/constructor/snapshots/<commit>`, чтобы SQLite-ссылки не стали dangling
  после Git GC. Тест проверяет ref и доступность commit после `git gc --prune=now`.
- [ ] Node/Vite worker собирает immutable Git revision, использует allowlisted
  env и возвращает path + deterministic SHA-256 artifact checksum; checksum
  сохраняется в SQLite с backward-compatible migration. Зависимости и build
  разделяют 15-минутный deadline; cancellation завершает process tree, raw logs
  не попадают в HTTP error. Env allowlist теперь покрыта тестом на удаление
  Gateway/Git токенов, Constructor DB path и NODE_OPTIONS. Остаются CPU/memory
  isolation, network policy и typed logs.
- [ ] Build retry/idempotency policy, retention/cleanup artifacts и полноценные
  cross-platform process-tree tests для macOS/Linux/Windows.
- [x] Deployment flow требует authorization, ready matching Snapshot/Build и
  точного подтверждения Site/Environment. Эти guards реализованы; Gateway
  получает artifact `source` с idempotency key, ожидание operation success перед
  promotion, и SQLite CAS-транзакция сохраняет только один active deployment на
  `(site, environment)`. SQLite partial unique index резервирует один pending/
  applying operation на target; failed ID можно безопасно повторить, conflict
  на другом ID не затирает существующую операцию. Gateway core теперь атомарно
  сравнивает `expectedCurrentRevision` внутри registry lock для publish и rollback,
  отдаёт release revision в operation result и проверяет stale/concurrent calls
  integration tests. Constructor читает paginated Gateway `/api/sites`, требует
  подтверждение непустого внешнего baseline при первом deploy, сверяет следующие
  операции с active revision, передаёт CAS expected revision в publish/rollback
  и сохраняет operation result revision в SQLite. Отдельно сохраняются исходные
  CAS precondition и action; startup recovery повторяет незавершённую операцию с
  тем же idempotency key, не освобождая target до подтверждения Gateway.
  Неопределённый сетевой исход возвращает `202` с состоянием `applying`, а не
  ложный failure. Tests cover pending/applying publish recovery, rollback replay,
  target reservation and SQLite reopen persistence. Gateway validation прошёл
  `go test -count=1 ./...`,
  `go test -race ./...`, `go vet ./...` и `make check` (155 TS tests +
  architecture lint). Constructor Go suite, race tests, vet, 21 UI/API tests,
  TypeScript, production build and docs build проходят.
  Локальный process-restart recovery реализован. Остаются durable operation
  records в самом Gateway для восстановления после Gateway restart/потери его
  idempotency cache, диагностический recovery UI и reconciliation workflow для
  подтверждённого out-of-band Gateway drift.
- [x] Gateway deploy/rollback выполняется через Gateway Admin API с expected
  active revision; Constructor хранит returned release revision и блокирует stale
  CAS. Constructor не реализует Gateway runtime/config semantics. Остаётся
  acceptance с настоящим Gateway deployment.
- [x] Rollback зовёт Gateway до локального promote и записывает новый Deployment
  с `action=rollback`; ошибки не меняют прежний active. Добавлены application и
  Gateway contract tests. Constructor передаёт expected Gateway revision и
  сохраняет результат; пакетные Go tests, race tests, TypeScript, 21 UI/API test,
  production UI build и VitePress docs build проходят. Остаётся end-to-end
  acceptance с настоящим Gateway release registry.

## P1 — Gateway и plugins

- [ ] Gateway workspace/binding state, drift, validation и apply через Gateway
  API; не принимать секреты или конкурентную конфигурацию без digest.
- [x] Gateway core plugin adapter переведён на `pluginprotocol v1.1.0` через
  локальный sibling-module replace; проверены `core/go.mod`, gRPC adapter imports
  и `go test ./internal/infrastructure/plugins`. Release ещё не опубликован,
  остаточные acceptance gates ведутся в `core/TODO.md`. Constructor остаётся
  REST-only и не добавляет direct transport или fallback.
- [ ] Plugin Admin UI: consume fixed Gateway Surface/query/action REST contract,
  validate Surface, authorize every operation, redact data, support digest
  invalidation/danger confirmations and schema-driven renderer; no plugin code
  execution or direct gRPC/loopback client in Constructor. Добавлена строгая
  pure validator для protocol `version`/`requiredCapabilities` Surface и
  Gateway digest envelope; server-side allowlist bridge поддерживает только
  `/api/plugins`, per-instance surface, query, action и page health. Browser не
  получает `GATEWAY_TOKEN`; digest/idempotency/one-time confirmation headers
  проходят с fixed routes. Typed upstream statuses сохраняются; stale Surface,
  invalid body/ID и non-JSON payload покрыты тестами. Добавлен Surface browser
  panel: показывает только healthy/ready instances, читает declared pages,
  загружает schema-declared dynamic options, отправляет только typed data query
  и action payload, показывает unavailable reason без fallback к произвольному
  input. Gateway в текущем core всё ещё не отдаёт rich Surface и не реализует
  digest/confirmation guarantees, поэтому реальный Gateway acceptance остаётся.
- [ ] Plugin UI schema renderer и generated plugin surfaces с contract tests;
  не исполнять plugin code в Constructor process. Validator принимает только
  объявленные field/section/action kinds и блокирует URL/executable properties;
  panel и typed `inputSchema`/row binding/dynamic-select `optionsSource` helpers
  добавлены, включая option/data/action API tests. Полный renderer остальных
  field/projection kinds и реальные generated fixtures остаются. Canonical
  `pluginprotocol` schema/fixture ещё требует синхронизации с documented action
  input/row binding/options-source extension: repo имеет staged пользовательские
  изменения к grant transport, поэтому его `.proto`/contracts не менялись.

## P1 — Wails boundary и запуск

- [ ] Единый React bundle для web и desktop; typed bridge для filesystem/Git,
  без прямых FS вызовов из UI. Frontend API теперь проходит через injectable
  `ConstructorBridge.request` с browser fallback; bridge dispatch покрыт тестом.
  Wails host, typed filesystem/Git capabilities и desktop lifecycle smoke ещё
  не реализованы.
- [ ] Smoke-test Wails-compatible API adapter, lifecycle/shutdown и desktop
  origin policy; без отдельной бизнес-логики desktop.
- [ ] Проверить dev запуск, production build/serve и graceful shutdown API,
  preview worker и SQLite.

## Release gate v1

- [ ] Requirement-by-requirement audit всех страниц `constructor/*.md` и
  соответствующих endpoints/artifacts. Первый проход нашёл и исправил устаревшее
  утверждение в `sdk-v1.md` про отсутствующий Asset variant producer и добавил
  ссылку на канонический `themeTokens` contract; остальные страницы требуют
  сверки с endpoints/runtime, релизный audit пока открыт.
- [ ] Все unit, integration, contract, security-negative и smoke/e2e tests
  проходят на поддерживаемых ОС.
- [ ] `go test -race ./...`, lint/static analysis, architecture/import checks,
  frontend typecheck/tests/build, SDK package tests и VitePress build проходят.
- [ ] Ручной UI audit основных workflows и accessibility; сохранённые evidence
  подтверждают поведение, не только успешную компиляцию.
- [ ] Обновить этот файл: незакрытых обязательных v1 пунктов не осталось.
