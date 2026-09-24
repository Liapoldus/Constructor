# Liapoldus Constructor

Local-first React Constructor foundation. The web shell currently demonstrates
the editor and isolated project-preview flow; `cmd/constructor-api` is the
versioned Go API boundary and binds to loopback only.

```sh
npm install
npm run dev
CONSTRUCTOR_PROJECT_ROOT=./project-fixture npm run api

# In a real project, initialize its Git history before creating a snapshot.
# The API never invents a revision for an uncommitted project.
git -C ./project-fixture init
git -C ./project-fixture add .
git -C ./project-fixture -c user.name=local -c user.email=local@example.invalid commit -m "chore: initialize project"
```

For parallel local instances, the API bind address and Vite's API proxy can be
overridden with `CONSTRUCTOR_API_ADDR` and `CONSTRUCTOR_API_PROXY`.

The project preview is started explicitly from the Canvas and runs the active
project's `npm run dev` server on a loopback port. The project's `package.json`
must define `scripts.dev`; the iframe has scripts enabled but no same-origin
access to the Constructor editor.

Newly scaffolded projects vendor the exact `@liapoldus/react` source and MIT
license under `src/vendor/liapoldus/react/`. This keeps the project revision
self-contained while the SDK remains an independently versioned repository;
the scaffold sync test checks that local snapshot against the sibling checkout
when it is available.

The API exposes `/api/v1/health`, `/api/v1/project`, `POST
/api/v1/project/validate` and revision-checked
`/api/v1/project/file?path=...`. The Go implementation follows the required
`domain → application → infrastructure/presentation` boundaries. The UI keeps
drafts in an isolated Zustand store and is designed to move to generated project
artifacts without coupling the domain model to the UI kit.

`GET /api/v1/projects` lists projects under `CONSTRUCTOR_WORKSPACE_ROOT`,
`GET /api/v1/projects/{id}` opens a manifest record, and `POST /api/v1/projects`
creates a new manifest-backed project directory there.
`POST /api/v1/projects/{id}/activate` atomically changes the shared active
project context used by filesystem, Git and build services without restarting
the API process.
The active project remains selected by `CONSTRUCTOR_PROJECT_ROOT`; switching
the active root is intentionally a host/session concern until the multi-project
session API is introduced.

`POST /api/v1/project/generate?siteId=<site-id>&locale=<locale>` validates the
selected Site/locale and materializes content into
`src/generated/content/<locale>.json`. Project validation without context checks
all Sites and their enabled locales; Site-specific generation never assumes a
default Site or locale.

Plugin IPC is owned by Gateway, not Constructor. The updated
[`pluginprotocol`](https://github.com/Liapoldus/pluginprotocol) contract uses
gRPC/HTTP2 over plugin loopback for `Manifest`, config, `Call`, and `Stream`;
Constructor stays REST-only and consumes declarative plugin Admin UI only
through Gateway's fixed REST API. The current sibling `core` checkout has
migrated its adapter to `pluginprotocol v1.1.0` through a local module replace;
that release is not published yet, and remaining Gateway acceptance work is
tracked in `core/TODO.md`. Constructor must not add a direct plugin connection or
transport fallback. The protocol Go client now preserves gRPC cancellation and
deadline statuses as `context.Canceled` / `context.DeadlineExceeded`. The
current sibling Gateway adapter preserves deadlines, but its in-flight
`CallJSON` path still translates explicit cancellation to
`ErrPluginUnavailable`; fixing that belongs in Gateway's plugin adapter.
Constructor stays REST-only and must not duplicate plugin transport or
cancellation policy.

Operational snapshots and builds are persisted in SQLite. Set `CONSTRUCTOR_DB`
to choose the database file; PostgreSQL is a deployment-level adapter option.

При подключении Gateway Constructor использует только фиксированный
Management REST API. Service key сохраняется в OS credential store через Go
backend; React renderer не хранит credential в browser storage и не получает
универсальный proxy к Gateway. Plugin Admin доступен только по заранее
описанным namespaced routes.

Собранный frontend публикуется как часть Caddyfile group revision через один
`POST /api/groups/{id}/releases`: metadata с idempotency/CAS, Caddyfile и один
необязательный `.tar.gz` с immutable frontend roots. Constructor не отправляет
filesystem source path и не использует старый site-oriented publish API.
Без настроенного Gateway Constructor явно показывает недоступность и никогда
не объявляет publish/rollback успешным без terminal Gateway operation.

В активном проекте Constructor доступен отдельный редактор исходного текста
`Caddyfile` в корне проекта. Он сохраняет введённый UTF-8 текст без локального
парсинга или адаптации, использует revision `ETag`/`If-Match` и показывает
diagnostics только если они вернулись из API. Этот источник пока не связан с
Gateway group, системной группой или Group Publish; соответствие
`SiteID`/`EnvironmentID` и `groupId` не предполагается. Named-project GET
требует permission `content.read`, сохранение — `content.write`. В исходнике
должны использоваться внешние secret references; редактор не является
исключением из политики хранения секретов и не выполняет локальное сканирование.
