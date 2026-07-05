package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestMCPToolsUpdateTask(t *testing.T) {
	store, err := openStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject("tester", "Project", "", "", "active")
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateCard("tester", "Tester", project.ID, "Task", "Description", "todo", "medium", false, "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertUsers([]SyncedUser{
		{ID: 42, Email: "dev@example.com", Name: "Quân", IsActive: true},
	}, ""); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		PublicURL:          "https://task.zentechglobal.io",
		MCPToken:           "test-token",
		MCPActorID:         "mcp-test",
		MCPActorName:       "MCP Test",
		MCPDefaultAssignee: "Quân",
	}
	hub := NewEventHub()
	telegram := NewTelegramBot(cfg, store, hub)
	handler := mcpHandler(cfg, store, hub, telegram)

	list := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	result := list["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 10 {
		t.Fatalf("expected 10 MCP tools, got %d", len(tools))
	}

	projects := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_projects",
			"arguments": map[string]any{},
		},
	})
	assertMCPStructured(t, projects, "projects")

	tasks := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "list_project_tasks",
			"arguments": map[string]any{
				"project_id": project.ID,
			},
		},
	})
	assertMCPStructured(t, tasks, "tasks")

	taskDetail := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_task_detail",
			"arguments": map[string]any{
				"project_id": project.ID,
				"card_id":    fmt.Sprintf("%d", card.Number),
			},
		},
	})
	assertMCPStructured(t, taskDetail, "task")

	assignees := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_assignees",
			"arguments": map[string]any{},
		},
	})
	assertMCPStructured(t, assignees, "users")

	callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "update_task_status",
			"arguments": map[string]any{
				"project_id": project.ID,
				"card_id":    card.ID,
				"status":     "doing",
			},
		},
	})
	detail, err := store.GetCardDetail(project.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.Status != "doing" {
		t.Fatalf("expected status doing, got %q", detail.Card.Status)
	}

	callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "update_task_estimate",
			"arguments": map[string]any{
				"project_id":     project.ID,
				"card_id":        fmt.Sprintf("%d", card.Number),
				"estimate_hours": 4.5,
				"estimate_note":  "MCP estimate",
			},
		},
	})
	detail, err = store.GetCardDetail(project.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.EstimateHours != 4.5 || detail.Card.EstimateNote != "MCP estimate" {
		t.Fatalf("unexpected estimate: %.1f %q", detail.Card.EstimateHours, detail.Card.EstimateNote)
	}

	callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "add_task_comment",
			"arguments": map[string]any{
				"project_id": project.ID,
				"card_id":    card.ID,
				"body":       "MCP comment",
			},
		},
	})
	detail, err = store.GetCardDetail(project.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "MCP comment" {
		t.Fatalf("unexpected comments: %#v", detail.Comments)
	}

	callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "assign_task",
			"arguments": map[string]any{
				"project_id": project.ID,
				"card_id":    card.ID,
				"assignee":   "dev@example.com",
			},
		},
	})
	detail, err = store.GetCardDetail(project.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.AssigneeID != "42" || detail.Card.Assignee != "Quân" {
		t.Fatalf("unexpected assignee: %q %q", detail.Card.AssigneeID, detail.Card.Assignee)
	}

	created := callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "create_task",
			"arguments": map[string]any{
				"project_id":     project.ID,
				"title":          "Created from MCP",
				"description":    "Created in test",
				"priority":       "high",
				"cost_incurred":  true,
				"estimate_hours": 2.5,
				"estimate_note":  "MCP create estimate",
			},
		},
	})
	createdCard := mcpStructuredMap(t, created, "card")
	createdID, ok := createdCard["id"].(string)
	if !ok || createdID == "" {
		t.Fatalf("missing created card id: %#v", createdCard)
	}
	if createdCard["assignee_id"] != "42" || createdCard["assignee"] != "Quân" {
		t.Fatalf("created task should default assign Quân: %#v", createdCard)
	}
	if createdCard["cost_incurred"] != true {
		t.Fatalf("created task should keep cost_incurred flag: %#v", createdCard)
	}

	callMCPHandler(t, handler, "test-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "close_task",
			"arguments": map[string]any{
				"project_id": project.ID,
				"card_id":    createdID,
			},
		},
	})
	closedDetail, err := store.GetCardDetail(project.ID, createdID)
	if err != nil {
		t.Fatal(err)
	}
	if !closedDetail.Card.Closed || closedDetail.Card.Status != "done" || closedDetail.Card.CompletedAt == nil {
		t.Fatalf("close did not complete task: %#v", closedDetail.Card)
	}
	openCards, err := store.ListCards(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range openCards {
		if item.ID == createdID {
			t.Fatalf("closed task should be hidden from kanban list")
		}
	}
	stats := store.CompletedHoursStats("2026-07", mustParseReportMonth(t, "2026-07"))
	if stats.TotalTasks != 1 || stats.TotalHours != 2.5 {
		t.Fatalf("closed completed task should be in report, got tasks=%d hours=%.1f", stats.TotalTasks, stats.TotalHours)
	}
}

func assertMCPStructured(t *testing.T, response map[string]any, key string) {
	t.Helper()
	_ = mcpStructuredMap(t, response, key)
}

func mcpStructuredMap(t *testing.T, response map[string]any, key string) map[string]any {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing MCP result: %#v", response)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structuredContent: %#v", result)
	}
	value, ok := structured[key]
	if !ok {
		t.Fatalf("missing structuredContent key %q: %#v", key, structured)
	}
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return structured
}

func mustParseReportMonth(t *testing.T, month string) time.Time {
	t.Helper()
	parsed, err := parseReportMonth(month)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestMCPRejectsUnauthorized(t *testing.T) {
	cfg := Config{PublicURL: "https://task.zentechglobal.io", MCPToken: "test-token"}
	handler := mcpHandler(cfg, &Store{}, NewEventHub(), NewTelegramBot(cfg, &Store{}, NewEventHub()))
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestTelegramNotificationsBatchPerTask(t *testing.T) {
	store, err := openStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject("tester", "Project", "", "", "active")
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateCard("tester", "Tester", project.ID, "Task", "", "todo", "medium", false, "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	bot := NewTelegramBot(Config{TelegramBotToken: "test-token", PublicURL: "https://task.zentechglobal.io"}, store, NewEventHub())

	bot.NotifyTaskCreated(card, "tester", "Tester")
	bot.NotifyTaskUpdated(card, "tester", "Tester", "đổi estimate")

	key := card.ProjectID + ":" + card.ID
	bot.notifyMu.Lock()
	item := bot.notifyQueue[key]
	if item == nil {
		t.Fatal("missing queued notification")
	}
	if len(item.lines) != 2 {
		t.Fatalf("expected 2 queued lines, got %d", len(item.lines))
	}
	version := item.version
	item.timer.Stop()
	bot.notifyMu.Unlock()

	bot.flushTaskNotification(key, version)

	bot.notifyMu.Lock()
	defer bot.notifyMu.Unlock()
	if _, ok := bot.notifyQueue[key]; ok {
		t.Fatal("expected notification queue to be flushed")
	}
}

func TestTaskEvidenceLinkDetection(t *testing.T) {
	if !taskHasEvidenceLink(Card{Description: "Ảnh lỗi: https://work.zentechglobal.io/storage/error.png"}) {
		t.Fatal("expected https evidence link to be detected")
	}
	if taskHasEvidenceLink(Card{Title: "Kiểm tra báo cáo thiếu số", Description: "User báo thiếu dữ liệu nhưng chưa gửi ảnh"}) {
		t.Fatal("did not expect evidence link without http/https URL")
	}
}

func callMCPHandler(t *testing.T, handler http.HandlerFunc, token string, payload map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", res.Code, res.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["error"] != nil {
		t.Fatalf("unexpected JSON-RPC error: %#v", decoded["error"])
	}
	return decoded
}
