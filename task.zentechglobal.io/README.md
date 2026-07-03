# Zentech Tasks

Task app for `task.zentechglobal.io`.

## Stack

- Backend: Go, OAuth client for `https://id.zentechglobal.io`
- Frontend: Vue 3 + Vite
- Login: redirects to `id.zentechglobal.io` with `client_id=task`

## Local Development

Backend:

```bash
cd backend
TASK_COOKIE_SECURE=false \
TASK_ID_ISSUER=http://localhost:8080 \
TASK_ID_REDIRECT_URI=http://localhost:5173/auth/callback \
go run .
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

For local OAuth, add this redirect URI to the identity server site:

```text
http://localhost:5173/auth/callback
```

## Production

Identity site/client:

```text
slug: task
name: Zentech Tasks
redirect_uris: https://task.zentechglobal.io/auth/callback
```

Run:

```bash
./scripts/deploy.sh --check
./scripts/deploy.sh --dry-run
./scripts/deploy.sh --yes
```

Current server layout:

- Nginx serves frontend files from `/var/www/task.zentechglobal.io`.
- Nginx proxies `/api/`, `/auth/`, and `/healthz` to `127.0.0.1:18082`.
- Backend runs as `task-zentechglobal.service`.
- Backend binary is installed at `/opt/task-zentechglobal/bin/task-server`.
- Runtime data is stored at `/opt/task-zentechglobal/data/tasks.json`.

`./scripts/deploy.sh --yes` builds the Vue frontend, builds a Linux AMD64 Go binary,
uploads both to the `zentech` SSH host, backs up the previous backend binary,
restarts the service, and verifies `https://task.zentechglobal.io/healthz`.

Telegram task notifications are batched per task for 10 seconds before sending.

## MCP

The backend exposes an HTTP MCP endpoint at `/api/mcp` when `TASK_MCP_TOKEN` is
configured. Use `Authorization: Bearer <TASK_MCP_TOKEN>` or `X-MCP-Token`.
MCP actions default to actor `Quân`, and `create_task` defaults the assignee to
`TASK_MCP_DEFAULT_ASSIGNEE` when no assignee is provided.
When `TASK_OPENAI_API_KEY` is configured, newly created tasks are reviewed by AI.
If a task needs verification/debugging but has no `http://` or `https://`
evidence link in the title, description, or estimate note, the backend adds a
comment asking for evidence.

Available tools:

- `list_projects`: list all projects.
- `list_project_tasks`: list open tasks for a project.
- `get_task_detail`: get a task with comments, attachments, and history.
- `list_assignees`: list active users that can be assigned to tasks.
- `create_task`: create a task in a project.
- `update_task_status`: set `todo`, `doing`, `review`, or `done`.
- `update_task_estimate`: set `estimate_hours` and optional `estimate_note`.
- `add_task_comment`: add a task comment.
- `assign_task`: assign a task to a user ID, email, or name.
- `close_task`: close a task, mark it done, and keep it in completed-hours reports.

Example:

```bash
curl https://task.zentechglobal.io/api/mcp \
  -H "Authorization: Bearer $TASK_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```
