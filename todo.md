# TODO — Liapoldus Constructor

Constructor — отдельный desktop-продукт и единственный Gateway UI. Архитектура
управления описана в [Gateway control plane](https://liapoldus.github.io/gateway/architecture/control-plane),
API — в [Group Releases](https://liapoldus.github.io/gateway/api/groups) и
[OpenAPI](https://liapoldus.github.io/gateway/api/openapi). Этот файл содержит
только незавершённые задачи Constructor.

## Workspace и редактор групп

- [ ] Заменить Gateway route/config editor на native Caddyfile editor; не
  создавать собственную DSL-модель listeners/routes/upstreams/policies.
- [ ] Реализовать системную страницу групп: system options, application groups,
  current/previous revisions, Caddy build identity и runtime drift.
- [ ] Реализовать Caddyfile diagnostics/adaptation через Gateway API и показать
  typed errors без скрытой трансформации исходного Caddyfile.
- [ ] Добавить frontend roots browser и включать их в групповой immutable
  revision; исключить локальные filesystem paths из API.
- [ ] Привязку plugin instance/capability показывать отдельно от Caddyfile;
  plugin settings не включать в group revision. UI показывает modes из plugin
  Manifest только как подсказку к Caddyfile binding; data-plane request остаётся
  прямым вызовом Caddy handler → plugin, Constructor не становится proxy.
- [ ] Для native Caddy Admin pass-through сделать явно обозначенный advanced
  raw JSON view с checkpoint предупреждением; не подключаться к Caddy Admin
  напрямую.
- [ ] При drift блокировать group publish и предлагать checkpoint restore или
  явный full-composition reconcile; показать preview и требуемый runtime digest.

## Group publish и операции

- [ ] Собирать immutable frontend output и отправлять один multipart запрос с
  metadata (idempotencyKey, expectedCurrentRevision), Caddyfile и optional
  single .tar.gz.
- [ ] Сопоставить frontend roots Caddyfile с archive paths frontends/<id>/...;
  показывать размер, digest, validation и archive limit errors до активации.
- [ ] Поддержать operation polling, стабильный retry с тем же key/digest,
  optimistic conflict и crash/reconnect recovery.
- [ ] Показать current/previous group revisions и выполнять rollback с
  expectedCurrentRevision; не изменять plugin settings при group rollback.
- [ ] Удалить site-oriented publish workflow, site.yaml editor и старый
  /api/sites/{slug}/publish контракт.
- [ ] Не отмечать deployment успешным, пока Gateway operation не завершилась
  succeeded и revision ID не сохранён.

## Секреты и подключение

- [ ] Поддержать web auth modes OIDC, local accounts/passwords + short-lived
  JWT и desktop single-user `none`; связывать OIDC user по `(iss, sub)` и
  запрещать не-provisioned subjects.
- [ ] Добавить WebAuthn/passkey challenge после OIDC и local password login,
  session/JWT в Secure/HttpOnly/SameSite cookie, rotating refresh, CSRF,
  strict Origin/Host, rate limits и безопасное MFA recovery.
- [ ] До реализации выпустить канонический versioned auth/security policy:
  access/refresh TTL, password hash параметры, login/recovery rate limits,
  CSRF/session rotation, WebAuthn recovery и retention audit defaults.
- [ ] Добавить role/environment-scoped Gateway permissions и проверять их в
  Constructor backend перед каждым действием; browser/UI visibility не является
  authorization boundary.
- [ ] Для каждой Gateway binding хранить отдельные Bearer token и web-backend
  mTLS credential references только в server-side secret storage; audit должен
  связывать пользователя, роль, environment, Gateway, operation и outcome.
- [ ] Реализовать desktop Go SSH bridge через внешний OpenSSH/bastion с
  short-lived SSH user certificates и port-forward-only policy к loopback
  Management API; внутри tunnel проверять TLS server identity и использовать
  Gateway token из OS credential store.
- [ ] Не требовать desktop client mTLS; не разрешать shell/SFTP/agent forwarding
  или произвольное port-forwarding; local auth `none` не должен отключать
  Gateway Bearer authorization.
- [ ] Разделить Constructor↔Gateway, desktop SSH, Gateway↔plugin и Caddy/ACME
  trust domains; не экспортировать plugin certificates/grants в UI.
- [ ] Проверять embedded/external Caddy variant и readiness в Gateway workspace;
  external Caddy — supervised child, а не независимый remote service.

## Блокер интеграции с Group Releases API

- [ ] Согласовать и реализовать модель связи Constructor delivery target с
  Gateway group: текущие `SiteID` и `EnvironmentID` не определяют однозначно
  `groupId` (group может содержать несколько frontend roots, а несколько
  групп активны одновременно).
- [ ] Добавить источник и редактор/хранилище native Caddyfile в Constructor.
  Текущая модель deployment содержит Site, Environment, Snapshot и Build, а
  build artifact — только каталог `dist`; Group Releases API требует
  обязательные `metadata` и UTF-8 `caddyfile`, плюс не более одного optional
  `.tar.gz` archive. Не отправлять пустой/синтетический Caddyfile и не выводить
  его из старых Site/Routes JSON без отдельного утверждённого контракта.
- [ ] Зафиксировать mapping готового build output в archive root
  `frontends/<id>/...`, включая идентичность root и соответствующий Caddyfile
  handler, а также семантику environment-specific groups.
- [ ] Определить границу локальной deployment history и Gateway
  `current`/`previous`: документированный Gateway rollback меняет предыдущую
  revision всей группы, тогда как Constructor сейчас адресует deployment по
  `(SiteID, EnvironmentID)`.
- [ ] До закрытия пунктов выше сохранить старый `/api/sites` client и flow:
  он по-прежнему соответствует работающему Gateway runtime в текущем workspace.
  На текущем состоянии миграция Constructor однозначно не реализуема без
  product-level решения; не удалять работающий integration и не заявлять
  соответствие Group Releases контракту.

## Остальные продуктовые работы

- [ ] Завершить project schema validation/migrations, immutable project
  revisions, deterministic build и preview isolation.
- [ ] Завершить plugin Admin declarative surfaces через фиксированные Gateway
  APIs; не исполнять plugin-provided code и не разрешать plugin URLs.
- [ ] Завершить typed API client по опубликованному OpenAPI и UI error states.
- [ ] Пройти unit/integration/browser/accessibility suite, race/static checks и
  package build. Constructor остаётся отдельным desktop product и не входит в
  Gateway server binary.
