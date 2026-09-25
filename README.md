# Liapoldus Constructor

Constructor — отдельный продукт для создания и управления сайтами. Интерфейс
работает на React, backend на Go; Gateway и plugin transport не встраиваются в
Constructor. Официальные Gateway-контракты и текущий план реализации находятся
в [документации Liapoldus](https://liapoldus.github.io/).

## Локальный запуск

```sh
npm install
npm run dev
CONSTRUCTOR_PROJECT_ROOT=./project-fixture npm run api
```

Для API проекта нужен существующий Git commit: Constructor не создаёт revision
для незакоммиченного состояния. Для локального fixture его можно создать так:

```sh
git -C ./project-fixture init
git -C ./project-fixture add .
git -C ./project-fixture -c user.name=local -c user.email=local@example.invalid commit -m "chore: initialize project"
```

## Текущие возможности

- Go API запускает проектный preview на loopback-порту; iframe изолирован от
  редактора Constructor.
- Управление проектами, проверка и генерация локализованного контента, чтение и
  редактирование файлов с проверкой revision.
- React UI хранит черновики в Zustand. Новые проекты включают версионированную
  копию исходников `@liapoldus/react` и его MIT license.
- Операционные snapshots и builds сохраняются в SQLite. Путь к базе задаётся
  через `CONSTRUCTOR_DB`; PostgreSQL-адаптера в текущем backend нет.
- При подключении Gateway Constructor использует только его Management REST
  API. Gateway credentials не передаются renderer-у и хранятся через Go backend
  в OS credential store.

## Gateway publishing

Целевой контракт публикации — immutable Caddyfile group release через
`POST /api/groups/{id}/releases`; Constructor не передаёт Gateway локальные
filesystem paths. Исходный Caddyfile можно редактировать локально, но его
привязка к Gateway group ещё не завершена. До закрытия соответствующего плана
не считать group publishing доступным end-to-end.

## Проверки

```sh
go test ./...
npm test
npm run build
go vet ./...
go build ./...
```
