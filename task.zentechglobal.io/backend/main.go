package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Addr             string
	Issuer           string
	ClientID         string
	RedirectURI      string
	CookieSecure     bool
	StorePath        string
	UploadPath       string
	TelegramBotToken string
	TelegramChatIDs  []int64
	PublicURL        string
	OpenAIAPIKey     string
	OpenAIModel      string
}

type Claims struct {
	Subject   string   `json:"sub"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Site      string   `json:"site"`
	Roles     []string `json:"roles"`
	Admin     bool     `json:"admin"`
	ExpiresAt int64    `json:"exp"`
}

type AuthSession struct {
	Claims      Claims
	AccessToken string
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Project struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	EstimateContext string    `json:"estimate_context,omitempty"`
	Status          string    `json:"status"`
	TelegramCode    string    `json:"telegram_code"`
	TelegramChatID  int64     `json:"telegram_chat_id,omitempty"`
	TelegramTopicID int64     `json:"telegram_topic_id,omitempty"`
	TelegramChat    string    `json:"telegram_chat,omitempty"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Card struct {
	ID            string     `json:"id"`
	Number        int64      `json:"number"`
	ProjectID     string     `json:"project_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	AssigneeID    string     `json:"assignee_id"`
	Assignee      string     `json:"assignee"`
	DueDate       string     `json:"due_date"`
	EstimateHours float64    `json:"estimate_hours"`
	EstimateNote  string     `json:"estimate_note"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Closed        bool       `json:"closed"`
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CardDetail struct {
	Card        Card           `json:"card"`
	Comments    []Comment      `json:"comments"`
	Attachments []Attachment   `json:"attachments"`
	History     []HistoryEvent `json:"history"`
}

type CompletedHoursStats struct {
	Month      string                     `json:"month"`
	StartedAt  time.Time                  `json:"started_at"`
	TotalHours float64                    `json:"total_hours"`
	TotalTasks int                        `json:"total_tasks"`
	Employees  []CompletedHoursByEmployee `json:"employees"`
}

type CompletedHoursByEmployee struct {
	AssigneeID string  `json:"assignee_id"`
	Assignee   string  `json:"assignee"`
	Hours      float64 `json:"hours"`
	TaskCount  int     `json:"task_count"`
	Tasks      []Card  `json:"tasks"`
}

type Comment struct {
	ID        string    `json:"id"`
	CardID    string    `json:"card_id"`
	AuthorID  string    `json:"author_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Attachment struct {
	ID          string    `json:"id"`
	CardID      string    `json:"card_id"`
	UploaderID  string    `json:"uploader_id"`
	Uploader    string    `json:"uploader"`
	Filename    string    `json:"filename"`
	StoredName  string    `json:"stored_name,omitempty"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
	URL         string    `json:"url"`
}

type HistoryEvent struct {
	ID        string    `json:"id"`
	CardID    string    `json:"card_id"`
	ActorID   string    `json:"actor_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type SyncedUser struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	IsAdmin     bool       `json:"is_admin"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	SyncedAt    time.Time  `json:"synced_at"`
}

type Store struct {
	mu             sync.Mutex
	path           string
	Tasks          map[string][]Task         `json:"tasks"`
	Projects       map[string]Project        `json:"projects"`
	Cards          map[string][]Card         `json:"cards"`
	Comments       map[string][]Comment      `json:"comments"`
	Attachments    map[string][]Attachment   `json:"attachments"`
	History        map[string][]HistoryEvent `json:"history"`
	TelegramChats  map[string]TelegramChat   `json:"telegram_chats"`
	Users          map[string]SyncedUser     `json:"users"`
	UserSyncCursor string                    `json:"user_sync_cursor,omitempty"`
	NextCardNumber int64                     `json:"next_card_number,omitempty"`
}

type TelegramChat struct {
	ID              int64     `json:"id"`
	MessageThreadID int64     `json:"message_thread_id,omitempty"`
	Title           string    `json:"title"`
	Username        string    `json:"username"`
	ProjectID       string    `json:"project_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type TelegramTarget struct {
	ChatID          int64
	MessageThreadID int64
}

type EventHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{clients: map[chan string]struct{}{}}
}

func (h *EventHub) Subscribe() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *EventHub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
}

func (h *EventHub) Broadcast(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
		}
	}
}

type TelegramBot struct {
	token     string
	chats     []int64
	publicURL string
	store     *Store
	hub       *EventHub
	client    *http.Client
}

type EstimateResult struct {
	Hours float64 `json:"hours"`
	Note  string  `json:"note"`
}

type telegramUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []telegramUpdate `json:"result"`
}

type telegramUpdate struct {
	UpdateID int64           `json:"update_id"`
	Message  telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID       int64        `json:"message_id"`
	MessageThreadID int64        `json:"message_thread_id"`
	Text            string       `json:"text"`
	Chat            telegramChat `json:"chat"`
	From            telegramUser `json:"from"`
}

type telegramChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type telegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func NewTelegramBot(cfg Config, store *Store, hub *EventHub) *TelegramBot {
	return &TelegramBot{
		token:     strings.TrimSpace(cfg.TelegramBotToken),
		chats:     cfg.TelegramChatIDs,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
		store:     store,
		hub:       hub,
		client:    &http.Client{Timeout: 35 * time.Second},
	}
}

func (b *TelegramBot) Enabled() bool {
	return b != nil && b.token != ""
}

func (b *TelegramBot) Run() {
	if !b.Enabled() {
		return
	}
	log.Printf("telegram bot polling enabled")
	if err := b.deleteWebhook(); err != nil {
		log.Printf("telegram deleteWebhook: %v", err)
	}
	var offset int64
	for {
		updates, err := b.getUpdates(offset)
		if err != nil {
			log.Printf("telegram getUpdates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			b.handleMessage(update.Message)
		}
	}
}

func (b *TelegramBot) deleteWebhook() error {
	req, err := http.NewRequest(http.MethodPost, b.apiURL("deleteWebhook"), nil)
	if err != nil {
		return err
	}
	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return fmt.Errorf("telegram returned %d: %s", res.StatusCode, string(data))
	}
	return nil
}

func (b *TelegramBot) NotifyTaskCreated(card Card, actorID, actor string) {
	b.notifyProject(card.ProjectID, b.formatTaskHeader(card)+"\n"+formatActorChange(actorID, actor, "vừa tạo task mới"))
}

func (b *TelegramBot) NotifyTaskUpdated(card Card, actorID, actor, summary string) {
	b.notifyProject(card.ProjectID, b.formatTaskHeader(card)+"\n"+formatActorChange(actorID, actor, summary))
}

func (b *TelegramBot) NotifyTaskCommented(card Card, comment Comment) {
	b.notifyProject(card.ProjectID, b.formatTaskHeader(card)+"\n"+formatActorChange(comment.AuthorID, comment.Author, "bình luận: "+comment.Body))
}

func (b *TelegramBot) notifyProject(projectID, text string) {
	if !b.Enabled() {
		return
	}
	for _, target := range b.store.ListTelegramTargetsForProject(projectID, b.chats) {
		if err := b.sendMessage(target.ChatID, target.MessageThreadID, text); err != nil {
			log.Printf("telegram sendMessage chat=%d thread=%d: %v", target.ChatID, target.MessageThreadID, err)
		}
	}
}

func (b *TelegramBot) handleMessage(message telegramMessage) {
	if message.Chat.ID == 0 || strings.TrimSpace(message.Text) == "" {
		return
	}
	chat := TelegramChat{
		ID:              message.Chat.ID,
		MessageThreadID: message.MessageThreadID,
		Title:           telegramChatTitle(message.Chat, message.MessageThreadID),
		Username:        message.Chat.Username,
	}
	if err := b.store.UpsertTelegramChat(chat); err != nil {
		log.Printf("telegram save chat: %v", err)
	}
	text := strings.TrimSpace(message.Text)
	switch {
	case text == "/start" || strings.HasPrefix(text, "/start "):
		_ = b.reply(message, "Đã nhận chat Telegram. Vào web lấy mã kết nối project rồi gửi /connect <mã>. Nếu dùng forum group, gửi lệnh trong đúng topic cần kết nối. Tạo task: /create \"Tên task\". Gán người phụ trách: /assign #id_task #id_nhan_vien hoặc /assign #id_task email.")
	case strings.HasPrefix(text, "/connect"):
		b.connectProjectFromMessage(message)
	case strings.HasPrefix(text, "/create"):
		b.createTaskFromMessage(message)
	case strings.HasPrefix(text, "/report"):
		b.reportFromMessage(message)
	case strings.HasPrefix(text, "/assign"):
		b.assignTaskFromMessage(message)
	case strings.HasPrefix(text, "/delete"):
		b.deleteTaskFromMessage(message)
	}
}

func (b *TelegramBot) connectProjectFromMessage(message telegramMessage) {
	code := parseTelegramCommandArg(message.Text)
	if code == "" {
		_ = b.reply(message, "Cú pháp: /connect <mã_kết_nối_project>")
		return
	}
	chat := TelegramChat{
		ID:              message.Chat.ID,
		MessageThreadID: message.MessageThreadID,
		Title:           telegramChatTitle(message.Chat, message.MessageThreadID),
		Username:        message.Chat.Username,
	}
	project, err := b.store.BindTelegramChat(code, chat)
	if err != nil {
		_ = b.reply(message, "Không kết nối được project: "+err.Error())
		return
	}
	b.hub.Broadcast("projects")
	_ = b.reply(message, "Đã kết nối Telegram với project: "+project.Name)
}

func (b *TelegramBot) createTaskFromMessage(message telegramMessage) {
	title, description := parseTelegramCreatePayload(message.Text)
	if title == "" {
		_ = b.reply(message, "Cú pháp: /create \"Tên task\" hoặc /create\\nTên task\\nMô tả task")
		return
	}
	projectID := b.store.ProjectIDForTelegramTarget(message.Chat.ID, message.MessageThreadID)
	if projectID == "" {
		_ = b.reply(message, "Group/topic này chưa kết nối project. Vào web lấy mã rồi gửi /connect <mã> trong đúng topic.")
		return
	}
	actor := telegramUserName(message.From)
	card, err := b.store.CreateCard(fmt.Sprintf("telegram:%d", message.From.ID), actor, projectID, title, description, "todo", "medium", "", "", "", 0, "Tạo từ Telegram")
	if err != nil {
		_ = b.reply(message, "Không tạo được task: "+err.Error())
		return
	}
	b.hub.Broadcast("cards:" + projectID)
	_ = b.reply(message, b.formatTaskHeader(card)+"\n"+formatActorChange(fmt.Sprintf("telegram:%d", message.From.ID), actor, "vừa tạo task mới"))
}

func (b *TelegramBot) reportFromMessage(message telegramMessage) {
	projectID := b.store.ProjectIDForTelegramTarget(message.Chat.ID, message.MessageThreadID)
	if projectID == "" {
		_ = b.reply(message, "Group/topic này chưa kết nối project. Vào web lấy mã rồi gửi /connect <mã> trong đúng topic.")
		return
	}
	project, cards, err := b.store.ProjectReport(projectID)
	if err != nil {
		_ = b.reply(message, "Không tạo được report: "+err.Error())
		return
	}
	_ = b.reply(message, b.formatProjectReport(project, cards))
}

func (b *TelegramBot) formatProjectReport(project Project, cards []Card) string {
	groups := map[string][]Card{
		"done":   {},
		"review": {},
		"doing":  {},
		"todo":   {},
	}
	for _, card := range cards {
		status := normalizeCardStatus(card.Status)
		if _, ok := groups[status]; ok {
			groups[status] = append(groups[status], card)
		}
	}
	var builder strings.Builder
	builder.WriteString("Report project: ")
	builder.WriteString(project.Name)
	builder.WriteString("\n")
	order := []struct {
		status string
		title  string
	}{
		{"done", "Task đã hoàn thành"},
		{"review", "Task đang review"},
		{"doing", "Task đang làm"},
		{"todo", "Task cần làm"},
	}
	for _, section := range order {
		items := groups[section.status]
		builder.WriteString("\n")
		builder.WriteString(section.title)
		builder.WriteString(fmt.Sprintf(" (%d)", len(items)))
		builder.WriteString("\n")
		if len(items) == 0 {
			builder.WriteString("- Không có\n")
			continue
		}
		for i, card := range items {
			line := b.formatReportTaskLine(card)
			if builder.Len()+len(line)+64 > 3900 {
				builder.WriteString(fmt.Sprintf("- Còn %d task chưa hiển thị do giới hạn Telegram.\n", len(items)-i))
				return builder.String()
			}
			builder.WriteString(line)
		}
	}
	return builder.String()
}

func (b *TelegramBot) formatReportTaskLine(card Card) string {
	taskID := taskDisplayID(card)
	line := fmt.Sprintf("- #%s : %s", taskID, strings.TrimSpace(card.Title))
	if strings.TrimSpace(card.Assignee) != "" {
		line += " - " + strings.TrimSpace(card.Assignee)
	}
	if strings.TrimSpace(card.DueDate) != "" {
		line += " - deadline " + strings.TrimSpace(card.DueDate)
	}
	if b != nil && b.publicURL != "" {
		line += "\n  " + b.publicURL + "/task/" + url.PathEscape(taskID)
	}
	return line + "\n"
}

func (b *TelegramBot) assignTaskFromMessage(message telegramMessage) {
	args := parseTelegramQuotedArgs(message.Text, "/assign")
	if len(args) < 2 {
		_ = b.reply(message, "Cú pháp: /assign #id_task #id_nhan_vien hoặc /assign #id_task email")
		return
	}
	projectID := b.store.ProjectIDForTelegramTarget(message.Chat.ID, message.MessageThreadID)
	if projectID == "" {
		_ = b.reply(message, "Group/topic này chưa kết nối project. Vào web lấy mã rồi gửi /connect <mã> trong đúng topic.")
		return
	}
	card, err := b.store.FindCard(projectID, args[0])
	if err != nil {
		_ = b.reply(message, "Không tìm thấy task: "+err.Error())
		return
	}
	assignee, err := b.store.FindActiveUser(args[1])
	if err != nil {
		_ = b.reply(message, "Không tìm thấy assignee: "+err.Error())
		return
	}
	assigneeID := fmt.Sprintf("%d", assignee.ID)
	assigneeName := assignee.Name
	if strings.TrimSpace(assigneeName) == "" {
		assigneeName = assignee.Email
	}
	actor := telegramUserName(message.From)
	updated, _, err := b.store.UpdateCard(fmt.Sprintf("telegram:%d", message.From.ID), actor, projectID, card.ID, nil, nil, nil, nil, &assigneeID, &assigneeName, nil, nil, nil)
	if err != nil {
		_ = b.reply(message, "Không đổi được assignee: "+err.Error())
		return
	}
	b.hub.Broadcast("cards:" + projectID)
	_ = b.reply(message, b.formatTaskHeader(updated)+"\n"+formatActorChange(fmt.Sprintf("telegram:%d", message.From.ID), actor, "đổi người phụ trách thành #"+assigneeID+" - "+assigneeName))
}

func (b *TelegramBot) deleteTaskFromMessage(message telegramMessage) {
	args := parseTelegramQuotedArgs(message.Text, "/delete")
	if len(args) < 1 {
		_ = b.reply(message, "Cú pháp: /delete #id_task")
		return
	}
	projectID := b.store.ProjectIDForTelegramTarget(message.Chat.ID, message.MessageThreadID)
	if projectID == "" {
		_ = b.reply(message, "Group/topic này chưa kết nối project. Vào web lấy mã rồi gửi /connect <mã> trong đúng topic.")
		return
	}
	card, err := b.store.FindCard(projectID, args[0])
	if err != nil {
		_ = b.reply(message, "Không tìm thấy task: "+err.Error())
		return
	}
	actor := telegramUserName(message.From)
	if err := b.store.DeleteCard(fmt.Sprintf("telegram:%d", message.From.ID), actor, projectID, card.ID); err != nil {
		_ = b.reply(message, "Không xóa được task: "+err.Error())
		return
	}
	b.hub.Broadcast("cards:" + projectID)
	_ = b.reply(message, b.formatTaskHeader(card)+"\n"+formatActorChange(fmt.Sprintf("telegram:%d", message.From.ID), actor, "đã xóa task"))
}

func (b *TelegramBot) getUpdates(offset int64) ([]telegramUpdate, error) {
	query := url.Values{
		"timeout": {"25"},
	}
	if offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", offset))
	}
	req, err := http.NewRequest(http.MethodGet, b.apiURL("getUpdates")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram returned %d: %s", res.StatusCode, string(body))
	}
	var payload telegramUpdatesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if !payload.OK {
		return nil, errors.New("telegram response not ok")
	}
	return payload.Result, nil
}

func (b *TelegramBot) reply(message telegramMessage, text string) error {
	return b.sendMessage(message.Chat.ID, message.MessageThreadID, text)
}

func (b *TelegramBot) sendMessage(chatID, messageThreadID int64, text string) error {
	if !b.Enabled() || strings.TrimSpace(text) == "" {
		return nil
	}
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if messageThreadID > 0 {
		payload["message_thread_id"] = messageThreadID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, b.apiURL("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return fmt.Errorf("telegram returned %d: %s", res.StatusCode, string(data))
	}
	return nil
}

func (b *TelegramBot) apiURL(method string) string {
	return "https://api.telegram.org/bot" + b.token + "/" + method
}

func parseTelegramCreatePayload(text string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", ""
	}
	command := fields[0]
	if command != "/create" && !strings.HasPrefix(command, "/create@") {
		return "", ""
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), command))
	if value == "" {
		return "", ""
	}
	if title, description, ok := parseMultilineCreate(value); ok {
		return title, description
	}
	if strings.HasPrefix(value, "\"") {
		if parsed, err := strconv.Unquote(value); err == nil {
			return strings.TrimSpace(parsed), ""
		}
	}
	return strings.Trim(value, "\" "), ""
}

func parseMultilineCreate(value string) (string, string, bool) {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return "", "", false
	}
	title := strings.TrimSpace(lines[0])
	if title == "" {
		return "", "", false
	}
	description := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	return title, description, true
}

func parseTelegramCommandArg(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSpace(fields[1])
}

func parseTelegramQuotedArgs(text, command string) []string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return nil
	}
	head := fields[0]
	if head != command && !strings.HasPrefix(head, command+"@") {
		return nil
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), head))
	args := []string{}
	for value != "" {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			end := 1
			escaped := false
			for end < len(value) {
				if value[end] == '\\' && !escaped {
					escaped = true
					end++
					continue
				}
				if value[end] == '"' && !escaped {
					break
				}
				escaped = false
				end++
			}
			if end < len(value) {
				raw := value[:end+1]
				if parsed, err := strconv.Unquote(raw); err == nil {
					args = append(args, strings.TrimSpace(parsed))
				}
				value = strings.TrimSpace(value[end+1:])
				continue
			}
		}
		parts := strings.SplitN(value, " ", 2)
		args = append(args, strings.Trim(parts[0], "\" "))
		if len(parts) == 1 {
			break
		}
		value = parts[1]
	}
	return args
}

func telegramUserName(user telegramUser) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name != "" {
		return name
	}
	return fmt.Sprintf("telegram:%d", user.ID)
}

func telegramChatTitle(chat telegramChat, messageThreadID int64) string {
	suffix := ""
	if messageThreadID > 0 {
		suffix = fmt.Sprintf(" · topic #%d", messageThreadID)
	}
	if chat.Title != "" {
		return chat.Title + suffix
	}
	if chat.Username != "" {
		return "@" + chat.Username + suffix
	}
	name := strings.TrimSpace(chat.FirstName + " " + chat.LastName)
	if name != "" {
		return name + suffix
	}
	return suffix
}

func telegramChatKey(chatID, messageThreadID int64) string {
	if messageThreadID <= 0 {
		return fmt.Sprintf("%d", chatID)
	}
	return fmt.Sprintf("%d:%d", chatID, messageThreadID)
}

func taskAssigneeLine(card Card) string {
	if strings.TrimSpace(card.Assignee) == "" {
		return ""
	}
	return "\nPhụ trách: " + card.Assignee
}

func (b *TelegramBot) formatTaskHeader(card Card) string {
	taskID := taskDisplayID(card)
	header := "#" + taskID + " : " + strings.TrimSpace(card.Title)
	if b != nil && b.publicURL != "" {
		header += "\n" + b.publicURL + "/task/" + url.PathEscape(taskID)
	}
	return header
}

func taskDisplayID(card Card) string {
	if card.Number > 0 {
		return fmt.Sprintf("%d", card.Number)
	}
	return strings.TrimSpace(card.ID)
}

func canUseAutoEstimate(claims Claims) bool {
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	name := strings.ToLower(strings.TrimSpace(claims.Name))
	subject := strings.ToLower(strings.TrimSpace(claims.Subject))
	return email == "quanbka.cntt@gmail.com" ||
		email == "nguyentrunghieu1432000@gmail.com" ||
		strings.Contains(name, "dev01") ||
		strings.Contains(name, "lê văn quân") ||
		strings.Contains(name, "le van quan") ||
		subject == "dev01"
}

func formatActorChange(actorID, actor, change string) string {
	actorID = strings.TrimPrefix(strings.TrimSpace(actorID), "#")
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = actorID
	}
	return "#" + actorID + " - " + actor + " : " + strings.TrimSpace(change)
}

func main() {
	cfg := loadConfig()
	store, err := openStore(cfg.StorePath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	hub := NewEventHub()
	telegram := NewTelegramBot(cfg, store, hub)
	if telegram.Enabled() {
		go telegram.Run()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /auth/login", loginHandler(cfg))
	mux.HandleFunc("GET /auth/callback", callbackHandler(cfg))
	mux.HandleFunc("POST /auth/logout", logoutHandler(cfg))
	mux.HandleFunc("GET /api/me", requireAuth(cfg, meHandler()))
	mux.HandleFunc("GET /api/events", eventsHandler(cfg, hub))
	mux.HandleFunc("GET /api/assignees", requireAuth(cfg, listAssigneesHandler(store)))
	mux.HandleFunc("GET /api/users", requireAuthSession(cfg, listSyncedUsersHandler(store)))
	mux.HandleFunc("POST /api/users/sync", requireAuthSession(cfg, syncIdentityUsersHandler(cfg, store, hub)))
	mux.HandleFunc("GET /api/projects", requireAuth(cfg, listProjectsHandler(store)))
	mux.HandleFunc("GET /api/cards", requireAuth(cfg, listAllCardsHandler(store)))
	mux.HandleFunc("GET /api/stats/completed-hours", requireAuth(cfg, completedHoursStatsHandler(store)))
	mux.HandleFunc("POST /api/projects", requireAuth(cfg, createProjectHandler(store, hub)))
	mux.HandleFunc("PATCH /api/projects/{id}", requireAuth(cfg, updateProjectHandler(store, hub)))
	mux.HandleFunc("DELETE /api/projects/{id}", requireAuth(cfg, deleteProjectHandler(store, hub)))
	mux.HandleFunc("GET /api/tasks/{cardID}", requireAuth(cfg, getTaskDetailByIDHandler(store)))
	mux.HandleFunc("GET /api/projects/{id}/cards", requireAuth(cfg, listCardsHandler(store)))
	mux.HandleFunc("POST /api/projects/{id}/cards", requireAuth(cfg, createCardHandler(store, hub, telegram)))
	mux.HandleFunc("GET /api/projects/{projectID}/cards/{cardID}", requireAuth(cfg, getCardDetailHandler(store)))
	mux.HandleFunc("PATCH /api/projects/{projectID}/cards/{cardID}", requireAuth(cfg, updateCardHandler(store, hub, telegram)))
	mux.HandleFunc("POST /api/projects/{projectID}/cards/{cardID}/estimate", requireAuth(cfg, estimateCardHandler(cfg, store, hub, telegram)))
	mux.HandleFunc("DELETE /api/projects/{projectID}/cards/{cardID}", requireAuth(cfg, deleteCardHandler(store, hub)))
	mux.HandleFunc("POST /api/projects/{projectID}/cards/{cardID}/close", requireAuth(cfg, closeCardHandler(store, hub, telegram)))
	mux.HandleFunc("POST /api/projects/{projectID}/cards/{cardID}/reopen", requireAuth(cfg, reopenCardHandler(store, hub, telegram)))
	mux.HandleFunc("POST /api/projects/{projectID}/cards/{cardID}/comments", requireAuth(cfg, createCommentHandler(store, hub, telegram)))
	mux.HandleFunc("POST /api/projects/{projectID}/cards/{cardID}/attachments", requireAuth(cfg, createAttachmentHandler(store, hub, cfg.UploadPath)))
	mux.HandleFunc("GET /api/projects/{projectID}/cards/{cardID}/attachments/{attachmentID}", requireAuth(cfg, downloadAttachmentHandler(store, cfg.UploadPath)))

	log.Printf("task backend listening on %s issuer=%s client_id=%s", cfg.Addr, cfg.Issuer, cfg.ClientID)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() Config {
	storePath := env("TASK_STORE_PATH", "data/tasks.json")
	return Config{
		Addr:             env("TASK_ADDR", ":8080"),
		Issuer:           strings.TrimRight(env("TASK_ID_ISSUER", "https://id.zentechglobal.io"), "/"),
		ClientID:         env("TASK_ID_CLIENT_ID", "task"),
		RedirectURI:      env("TASK_ID_REDIRECT_URI", "https://task.zentechglobal.io/auth/callback"),
		CookieSecure:     envBool("TASK_COOKIE_SECURE", true),
		StorePath:        storePath,
		UploadPath:       env("TASK_UPLOAD_PATH", filepath.Join(filepath.Dir(storePath), "uploads")),
		TelegramBotToken: env("TASK_TELEGRAM_BOT_TOKEN", ""),
		TelegramChatIDs:  envInt64List("TASK_TELEGRAM_CHAT_IDS"),
		PublicURL:        strings.TrimRight(env("TASK_PUBLIC_URL", "https://task.zentechglobal.io"), "/"),
		OpenAIAPIKey:     env("TASK_OPENAI_API_KEY", ""),
		OpenAIModel:      env("TASK_OPENAI_MODEL", "gpt-4o-mini"),
	}
}

func loginHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := randomToken(24)
		setCookie(w, "task_oauth_state", state, 10*time.Minute, cfg.CookieSecure, false)
		query := url.Values{
			"response_type": {"code"},
			"client_id":     {cfg.ClientID},
			"redirect_uri":  {cfg.RedirectURI},
			"state":         {state},
		}
		http.Redirect(w, r, cfg.Issuer+"/oauth/authorize?"+query.Encode(), http.StatusFound)
	}
}

func callbackHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("code") == "" {
			http.Redirect(w, r, "/?error=missing_code", http.StatusFound)
			return
		}
		stateCookie, err := r.Cookie("task_oauth_state")
		if err != nil || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
			http.Redirect(w, r, "/?error=invalid_state", http.StatusFound)
			return
		}
		clearCookie(w, "task_oauth_state", cfg.CookieSecure)

		pair, err := exchangeCode(cfg, r.URL.Query().Get("code"))
		if err != nil {
			log.Printf("oauth token exchange failed: %v", err)
			http.Redirect(w, r, "/?error=login_failed", http.StatusFound)
			return
		}
		setAuthCookies(w, cfg, pair)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func logoutHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		clearCookie(w, "task_access_token", cfg.CookieSecure)
		clearCookie(w, "task_refresh_token", cfg.CookieSecure)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func meHandler() func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, _ *http.Request, claims Claims) {
		writeJSON(w, http.StatusOK, claims)
	}
}

func eventsHandler(cfg Config, hub *EventHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authenticateRequest(w, r, cfg); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-ch:
				_, _ = fmt.Fprintf(w, "event: update\ndata: %s\n\n", event)
				flusher.Flush()
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	}
}

func listProjectsHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, _ *http.Request, _ Claims) {
		writeJSON(w, http.StatusOK, store.ListProjects())
	}
}

func completedHoursStatsHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		month := strings.TrimSpace(r.URL.Query().Get("month"))
		if month == "" {
			month = "2026-07"
		}
		start, err := parseReportMonth(month)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		stats := store.CompletedHoursStats(month, start)
		writeJSON(w, http.StatusOK, stats)
	}
}

func createProjectHandler(store *Store, hub *EventHub) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		var input struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			EstimateContext string `json:"estimate_context"`
			Status          string `json:"status"`
		}
		if err := readJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		project, err := store.CreateProject(claims.Subject, input.Name, input.Description, input.EstimateContext, input.Status)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("projects")
		writeJSON(w, http.StatusCreated, project)
	}
}

func updateProjectHandler(store *Store, hub *EventHub) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		var input struct {
			Name            *string `json:"name"`
			Description     *string `json:"description"`
			EstimateContext *string `json:"estimate_context"`
			Status          *string `json:"status"`
		}
		if err := readJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		project, err := store.UpdateProject(r.PathValue("id"), input.Name, input.Description, input.EstimateContext, input.Status)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		hub.Broadcast("projects")
		writeJSON(w, http.StatusOK, project)
	}
}

func deleteProjectHandler(store *Store, hub *EventHub) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		if err := store.DeleteProject(r.PathValue("id")); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		hub.Broadcast("projects")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func listCardsHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		cards, err := store.ListCards(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cards)
	}
}

func listAllCardsHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, _ *http.Request, _ Claims) {
		writeJSON(w, http.StatusOK, store.ListAllCards())
	}
}

func createCardHandler(store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		var input struct {
			Title         string  `json:"title"`
			Description   string  `json:"description"`
			Status        string  `json:"status"`
			Priority      string  `json:"priority"`
			AssigneeID    string  `json:"assignee_id"`
			Assignee      string  `json:"assignee"`
			DueDate       string  `json:"due_date"`
			EstimateHours float64 `json:"estimate_hours"`
			EstimateNote  string  `json:"estimate_note"`
		}
		if err := readJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		card, err := store.CreateCard(claims.Subject, claims.Name, r.PathValue("id"), input.Title, input.Description, input.Status, input.Priority, input.AssigneeID, input.Assignee, input.DueDate, input.EstimateHours, input.EstimateNote)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("id"))
		telegram.NotifyTaskCreated(card, claims.Subject, claims.Name)
		writeJSON(w, http.StatusCreated, card)
	}
}

func getCardDetailHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		detail, err := store.GetCardDetail(r.PathValue("projectID"), r.PathValue("cardID"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

func getTaskDetailByIDHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		detail, err := store.GetCardDetailByID(r.PathValue("cardID"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

func updateCardHandler(store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		var input struct {
			Title         *string  `json:"title"`
			Description   *string  `json:"description"`
			Status        *string  `json:"status"`
			Priority      *string  `json:"priority"`
			AssigneeID    *string  `json:"assignee_id"`
			Assignee      *string  `json:"assignee"`
			DueDate       *string  `json:"due_date"`
			EstimateHours *float64 `json:"estimate_hours"`
			EstimateNote  *string  `json:"estimate_note"`
		}
		if err := readJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		card, summary, err := store.UpdateCard(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), input.Title, input.Description, input.Status, input.Priority, input.AssigneeID, input.Assignee, input.DueDate, input.EstimateHours, input.EstimateNote)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		if summary != "" {
			telegram.NotifyTaskUpdated(card, claims.Subject, claims.Name, summary)
		}
		writeJSON(w, http.StatusOK, card)
	}
}

func deleteCardHandler(store *Store, hub *EventHub) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		if err := store.DeleteCard(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID")); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func closeCardHandler(store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		card, err := store.SetCardClosed(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), true)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		telegram.NotifyTaskUpdated(card, claims.Subject, claims.Name, "đã close task")
		writeJSON(w, http.StatusOK, card)
	}
}

func reopenCardHandler(store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		card, err := store.SetCardClosed(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), false)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		telegram.NotifyTaskUpdated(card, claims.Subject, claims.Name, "đã reopen task")
		writeJSON(w, http.StatusOK, card)
	}
}

func estimateCardHandler(cfg Config, store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		if !canUseAutoEstimate(claims) {
			writeError(w, http.StatusForbidden, "auto estimate is only enabled for DEV01")
			return
		}
		project, card, err := store.ProjectAndCard(r.PathValue("projectID"), r.PathValue("cardID"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if card.Closed {
			writeError(w, http.StatusBadRequest, "task is closed")
			return
		}
		result, err := estimateWithOpenAI(r.Context(), cfg, project, card)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		updated, summary, err := store.UpdateCardEstimate(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), result.Hours, result.Note)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		if summary != "" {
			telegram.NotifyTaskUpdated(updated, claims.Subject, claims.Name, summary)
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

func listAssigneesHandler(store *Store) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, _ *http.Request, _ Claims) {
		writeJSON(w, http.StatusOK, map[string]any{"users": store.ListActiveUsers()})
	}
}

func createCommentHandler(store *Store, hub *EventHub, telegram *TelegramBot) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		var input struct {
			Body string `json:"body"`
		}
		if err := readJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		comment, err := store.CreateComment(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), input.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		if detail, err := store.GetCardDetail(r.PathValue("projectID"), r.PathValue("cardID")); err == nil {
			telegram.NotifyTaskCommented(detail.Card, comment)
		}
		writeJSON(w, http.StatusCreated, comment)
	}
}

func createAttachmentHandler(store *Store, hub *EventHub, uploadPath string) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, claims Claims) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid upload")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "file is required")
			return
		}
		defer file.Close()
		attachment, err := store.CreateAttachment(claims.Subject, claims.Name, r.PathValue("projectID"), r.PathValue("cardID"), uploadPath, file, header.Filename, header.Size, header.Header.Get("Content-Type"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hub.Broadcast("cards:" + r.PathValue("projectID"))
		writeJSON(w, http.StatusCreated, attachment)
	}
}

func downloadAttachmentHandler(store *Store, uploadPath string) func(http.ResponseWriter, *http.Request, Claims) {
	return func(w http.ResponseWriter, r *http.Request, _ Claims) {
		attachment, err := store.GetAttachment(r.PathValue("projectID"), r.PathValue("cardID"), r.PathValue("attachmentID"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if attachment.ContentType != "" {
			w.Header().Set("Content-Type", attachment.ContentType)
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
		http.ServeFile(w, r, filepath.Join(uploadPath, attachment.StoredName))
	}
}

func estimateWithOpenAI(ctx context.Context, cfg Config, project Project, card Card) (EstimateResult, error) {
	if strings.TrimSpace(cfg.OpenAIAPIKey) == "" {
		return EstimateResult{}, errors.New("TASK_OPENAI_API_KEY is not configured")
	}
	isQuickCheck := isQuickCheckEstimateTask(card)
	prompt := fmt.Sprintf(`Estimate implementation time in hours for a junior developer who does not use AI.

Return only JSON with:
{"hours": number, "note": "short Vietnamese explanation"}

Rules:
- Estimate only the likely work needed for this exact task, including coding, review, basic manual testing and integration time.
- Use a realistic junior developer pace, but do not add a large risk buffer by default.
- If requirements are unclear, state the assumption in note and keep the estimate for the smallest useful first pass.
- Tasks that are mainly checking reports, debugging why a report/data screen is not showing, reading API docs, validating data, or tracing logs are usually 1-6 hours unless the task explicitly asks to build a new end-to-end feature.
- A 20+ hour estimate is only appropriate for broad implementation work spanning multiple screens/services, schema changes, auth, background jobs, or substantial third-party integration.
- Output hours must be an integer number of hours.
- Minimum estimate is 1 hour.
- Quick report/debug/check classification from backend: %t

Project:
Name: %s
Description: %s
Project estimate context: %s

Task:
Title: %s
Description: %s`, isQuickCheck, project.Name, project.Description, project.EstimateContext, card.Title, card.Description)

	body, err := json.Marshal(map[string]any{
		"model": cfg.OpenAIModel,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a pragmatic senior software estimator. You must return valid compact JSON only."},
			{"role": "user", "content": prompt},
		},
		"temperature":     0.1,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return EstimateResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return EstimateResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return EstimateResult{}, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode >= 300 {
		return EstimateResult{}, fmt.Errorf("openai returned %d: %s", res.StatusCode, string(data))
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return EstimateResult{}, err
	}
	if len(payload.Choices) == 0 {
		return EstimateResult{}, errors.New("openai returned no choices")
	}
	var result EstimateResult
	if err := json.Unmarshal([]byte(payload.Choices[0].Message.Content), &result); err != nil {
		return EstimateResult{}, err
	}
	result.Hours = math.Round(result.Hours)
	if result.Hours < 1 {
		result.Hours = 1
	}
	if isQuickCheck && result.Hours > 6 {
		result.Hours = 6
		if strings.TrimSpace(result.Note) == "" {
			result.Note = "Task dạng kiểm tra/rà soát/debug báo cáo nên giới hạn estimate cho lượt xử lý đầu tiên."
		} else {
			result.Note = strings.TrimSpace(result.Note) + " Giới hạn 6h vì task thuộc nhóm kiểm tra/rà soát/debug báo cáo."
		}
	}
	if result.Note == "" {
		result.Note = "Auto estimate bằng OpenAI theo năng lực junior dev không sử dụng AI."
	}
	return result, nil
}

func isQuickCheckEstimateTask(card Card) bool {
	text := strings.ToLower(strings.TrimSpace(card.Title + " " + card.Description))
	if text == "" {
		return false
	}
	quickSignals := []string{
		"check", "kiểm tra", "kiem tra", "rà soát", "ra soat", "đối soát", "doi soat",
		"báo cáo", "bao cao", "report", "dashboard", "sao không lên", "sao khong len",
		"không lên", "khong len", "không hiển thị", "khong hien thi", "không ra", "khong ra",
		"debug", "trace", "log", "số liệu", "so lieu", "adsense", "gamob",
	}
	hasQuickSignal := false
	for _, signal := range quickSignals {
		if strings.Contains(text, signal) {
			hasQuickSignal = true
			break
		}
	}
	if !hasQuickSignal {
		return false
	}
	largeSignals := []string{
		"xây dựng hệ thống", "xay dung he thong", "tích hợp sâu", "tich hop sau",
		"end-to-end", "migration", "schema", "background job", "realtime",
		"oauth", "sso", "thanh toán", "payment",
	}
	for _, signal := range largeSignals {
		if strings.Contains(text, signal) {
			return false
		}
	}
	return true
}

func listSyncedUsersHandler(store *Store) func(http.ResponseWriter, *http.Request, AuthSession) {
	return func(w http.ResponseWriter, r *http.Request, session AuthSession) {
		if !session.Claims.Admin {
			writeError(w, http.StatusForbidden, "admin required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"cursor": store.UserCursor(),
			"users":  store.ListUsers(),
		})
	}
}

func syncIdentityUsersHandler(cfg Config, store *Store, hub *EventHub) func(http.ResponseWriter, *http.Request, AuthSession) {
	return func(w http.ResponseWriter, r *http.Request, session AuthSession) {
		if !session.Claims.Admin {
			writeError(w, http.StatusForbidden, "admin required")
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, cfg.Issuer+"/sync/users", nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			writeError(w, res.StatusCode, string(body))
			return
		}
		var payload struct {
			Cursor string       `json:"cursor"`
			Users  []SyncedUser `json:"users"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&payload); err != nil {
			writeError(w, http.StatusBadGateway, "invalid sync response")
			return
		}
		if err := store.UpsertUsers(payload.Users, payload.Cursor); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hub.Broadcast("users")
		writeJSON(w, http.StatusOK, map[string]any{
			"cursor": payload.Cursor,
			"synced": len(payload.Users),
			"users":  store.ListUsers(),
		})
	}
}

func requireAuth(cfg Config, next func(http.ResponseWriter, *http.Request, Claims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticateRequest(w, r, cfg)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, session.Claims)
	}
}

func requireAuthSession(cfg Config, next func(http.ResponseWriter, *http.Request, AuthSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticateRequest(w, r, cfg)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, session)
	}
}

func authenticateRequest(w http.ResponseWriter, r *http.Request, cfg Config) (AuthSession, bool) {
	if token, err := r.Cookie("task_access_token"); err == nil && token.Value != "" {
		claims, err := fetchMe(cfg, token.Value)
		if err == nil {
			return AuthSession{Claims: claims, AccessToken: token.Value}, true
		}
	}
	refreshCookie, err := r.Cookie("task_refresh_token")
	if err != nil || refreshCookie.Value == "" {
		return AuthSession{}, false
	}
	pair, err := refreshTokens(cfg, refreshCookie.Value)
	if err != nil {
		clearCookie(w, "task_access_token", cfg.CookieSecure)
		clearCookie(w, "task_refresh_token", cfg.CookieSecure)
		return AuthSession{}, false
	}
	claims, err := fetchMe(cfg, pair.AccessToken)
	if err != nil {
		clearCookie(w, "task_access_token", cfg.CookieSecure)
		clearCookie(w, "task_refresh_token", cfg.CookieSecure)
		return AuthSession{}, false
	}
	setAuthCookies(w, cfg, pair)
	return AuthSession{Claims: claims, AccessToken: pair.AccessToken}, true
}

func exchangeCode(cfg Config, code string) (TokenPair, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"client_id":    {cfg.ClientID},
		"redirect_uri": {cfg.RedirectURI},
	}
	req, err := http.NewRequest(http.MethodPost, cfg.Issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenPair{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return TokenPair{}, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return TokenPair{}, fmt.Errorf("token endpoint returned %d: %s", res.StatusCode, string(body))
	}
	var pair TokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return TokenPair{}, err
	}
	if pair.AccessToken == "" {
		return TokenPair{}, errors.New("token endpoint returned empty access token")
	}
	return pair, nil
}

func refreshTokens(cfg Config, refreshToken string) (TokenPair, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequest(http.MethodPost, cfg.Issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenPair{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return TokenPair{}, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return TokenPair{}, fmt.Errorf("refresh endpoint returned %d: %s", res.StatusCode, string(body))
	}
	var pair TokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return TokenPair{}, err
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		return TokenPair{}, errors.New("refresh endpoint returned incomplete token pair")
	}
	return pair, nil
}

func fetchMe(cfg Config, accessToken string) (Claims, error) {
	req, err := http.NewRequest(http.MethodGet, cfg.Issuer+"/auth/me", nil)
	if err != nil {
		return Claims{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Claims{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return Claims{}, fmt.Errorf("userinfo returned %d", res.StatusCode)
	}
	var claims Claims
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&claims); err != nil {
		return Claims{}, err
	}
	if claims.Subject == "" {
		return Claims{}, errors.New("missing subject")
	}
	return claims, nil
}

func openStore(path string) (*Store, error) {
	store := &Store{
		path:           path,
		Tasks:          map[string][]Task{},
		Projects:       map[string]Project{},
		Cards:          map[string][]Card{},
		Comments:       map[string][]Comment{},
		Attachments:    map[string][]Attachment{},
		History:        map[string][]HistoryEvent{},
		TelegramChats:  map[string]TelegramChat{},
		Users:          map[string]SyncedUser{},
		NextCardNumber: 1,
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	if store.Tasks == nil {
		store.Tasks = map[string][]Task{}
	}
	if store.Projects == nil {
		store.Projects = map[string]Project{}
	}
	if store.Cards == nil {
		store.Cards = map[string][]Card{}
	}
	if store.Comments == nil {
		store.Comments = map[string][]Comment{}
	}
	if store.Attachments == nil {
		store.Attachments = map[string][]Attachment{}
	}
	if store.History == nil {
		store.History = map[string][]HistoryEvent{}
	}
	if store.TelegramChats == nil {
		store.TelegramChats = map[string]TelegramChat{}
	}
	if store.Users == nil {
		store.Users = map[string]SyncedUser{}
	}
	changed := store.ensureProjectTelegramCodesLocked()
	if store.ensureProjectSlugsLocked() {
		changed = true
	}
	if len(store.Projects) == 0 && len(store.Tasks) > 0 {
		store.migrateTasksLocked()
		changed = true
	}
	if store.ensureCardNumbersLocked() {
		changed = true
	}
	if changed {
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) ensureProjectSlugsLocked() bool {
	changed := false
	used := map[string]string{}
	for id, project := range s.Projects {
		slug := normalizeProjectSlug(project.Slug)
		if slug == "" {
			slug = normalizeProjectSlug(project.Name)
		}
		if slug == "" {
			slug = id
		}
		base := slug
		for n := 2; ; n++ {
			owner, exists := used[slug]
			if !exists || owner == id {
				break
			}
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		used[slug] = id
		if project.Slug != slug {
			project.Slug = slug
			s.Projects[id] = project
			changed = true
		}
	}
	return changed
}

func (s *Store) ensureProjectSlugUniqueLocked(project *Project) error {
	project.Slug = normalizeProjectSlug(project.Slug)
	if project.Slug == "" {
		project.Slug = normalizeProjectSlug(project.Name)
	}
	if project.Slug == "" {
		return errors.New("project slug is required")
	}
	for id, existing := range s.Projects {
		if id != project.ID && strings.EqualFold(existing.Slug, project.Slug) {
			return errors.New("project slug must be unique")
		}
	}
	return nil
}

func (s *Store) ensureCardNumbersLocked() bool {
	changed := false
	maxNumber := int64(0)
	for projectID, cards := range s.Cards {
		for i := range cards {
			if cards[i].Number > maxNumber {
				maxNumber = cards[i].Number
			}
			if cards[i].Number <= 0 {
				if s.NextCardNumber <= maxNumber {
					s.NextCardNumber = maxNumber + 1
				}
				if s.NextCardNumber <= 0 {
					s.NextCardNumber = 1
				}
				cards[i].Number = s.NextCardNumber
				s.NextCardNumber++
				if cards[i].Number > maxNumber {
					maxNumber = cards[i].Number
				}
				changed = true
			}
		}
		s.Cards[projectID] = cards
	}
	if s.NextCardNumber <= maxNumber {
		s.NextCardNumber = maxNumber + 1
		changed = true
	}
	if s.NextCardNumber <= 0 {
		s.NextCardNumber = 1
		changed = true
	}
	return changed
}

func (s *Store) ensureProjectTelegramCodesLocked() bool {
	changed := false
	for id, project := range s.Projects {
		if project.TelegramCode == "" {
			project.TelegramCode = newTelegramCode()
			s.Projects[id] = project
			changed = true
		}
	}
	return changed
}

func (s *Store) migrateTasksLocked() {
	now := time.Now().UTC()
	project := Project{ID: randomToken(10), Slug: "du-an-mac-dinh", Name: "Dự án mặc định", Description: "Dữ liệu được chuyển từ task cũ.", Status: "active", TelegramCode: newTelegramCode(), CreatedBy: "system", CreatedAt: now, UpdatedAt: now}
	s.Projects[project.ID] = project
	for userID, tasks := range s.Tasks {
		for _, task := range tasks {
			status := "todo"
			if task.Done {
				status = "done"
			}
			card := Card{
				ID:        task.ID,
				ProjectID: project.ID,
				Title:     task.Title,
				Status:    status,
				Priority:  "medium",
				CreatedBy: userID,
				CreatedAt: task.CreatedAt,
				UpdatedAt: task.UpdatedAt,
			}
			if card.CreatedAt.IsZero() {
				card.CreatedAt = now
			}
			if card.UpdatedAt.IsZero() {
				card.UpdatedAt = now
			}
			s.Cards[project.ID] = append(s.Cards[project.ID], card)
		}
	}
}

func (s *Store) ListProjects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects := make([]Project, 0, len(s.Projects))
	for _, project := range s.Projects {
		projects = append(projects, s.withTelegramChatLocked(project))
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Status == projects[j].Status {
			return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
		}
		return projects[i].Status < projects[j].Status
	})
	return projects
}

func (s *Store) CreateProject(userID, name, description, estimateContext, status string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	status = normalizeProjectStatus(status)
	now := time.Now().UTC()
	project := Project{ID: randomToken(10), Slug: normalizeProjectSlug(name), Name: name, Description: strings.TrimSpace(description), EstimateContext: strings.TrimSpace(estimateContext), Status: status, TelegramCode: newTelegramCode(), CreatedBy: userID, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureProjectSlugUniqueLocked(&project); err != nil {
		return Project{}, err
	}
	s.Projects[project.ID] = project
	if s.Cards == nil {
		s.Cards = map[string][]Card{}
	}
	s.Cards[project.ID] = []Card{}
	return project, s.saveLocked()
}

func (s *Store) UpdateProject(id string, name, description, estimateContext, status *string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.Projects[id]
	if !ok {
		return Project{}, errors.New("project not found")
	}
	if name != nil {
		project.Name = strings.TrimSpace(*name)
		if project.Name == "" {
			return Project{}, errors.New("project name is required")
		}
		project.Slug = normalizeProjectSlug(project.Name)
	}
	if description != nil {
		project.Description = strings.TrimSpace(*description)
	}
	if estimateContext != nil {
		project.EstimateContext = strings.TrimSpace(*estimateContext)
	}
	if status != nil {
		project.Status = normalizeProjectStatus(*status)
	}
	if err := s.ensureProjectSlugUniqueLocked(&project); err != nil {
		return Project{}, err
	}
	project.UpdatedAt = time.Now().UTC()
	s.Projects[id] = project
	return project, s.saveLocked()
}

func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Projects[id]; !ok {
		return errors.New("project not found")
	}
	for _, card := range s.Cards[id] {
		delete(s.Comments, card.ID)
		delete(s.Attachments, card.ID)
		delete(s.History, card.ID)
	}
	for key, chat := range s.TelegramChats {
		if chat.ProjectID == id {
			chat.ProjectID = ""
			s.TelegramChats[key] = chat
		}
	}
	delete(s.Projects, id)
	delete(s.Cards, id)
	return s.saveLocked()
}

func (s *Store) ListCards(projectID string) ([]Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Projects[projectID]; !ok {
		return nil, errors.New("project not found")
	}
	cards := []Card{}
	for _, card := range s.Cards[projectID] {
		if !card.Closed {
			cards = append(cards, card)
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Status == cards[j].Status {
			return cards[i].UpdatedAt.After(cards[j].UpdatedAt)
		}
		return kanbanRank(cards[i].Status) < kanbanRank(cards[j].Status)
	})
	if cards == nil {
		return []Card{}, nil
	}
	return cards, nil
}

func (s *Store) ListAllCards() []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards := []Card{}
	for _, projectCards := range s.Cards {
		for _, card := range projectCards {
			if !card.Closed {
				cards = append(cards, card)
			}
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Status == cards[j].Status {
			return cards[i].UpdatedAt.After(cards[j].UpdatedAt)
		}
		return kanbanRank(cards[i].Status) < kanbanRank(cards[j].Status)
	})
	return cards
}

func (s *Store) ProjectReport(projectID string) (Project, []Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.Projects[projectID]
	if !ok {
		return Project{}, nil, errors.New("project not found")
	}
	cards := []Card{}
	for _, card := range s.Cards[projectID] {
		if !card.Closed {
			cards = append(cards, card)
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Status == cards[j].Status {
			return cards[i].UpdatedAt.After(cards[j].UpdatedAt)
		}
		return kanbanRank(cards[i].Status) < kanbanRank(cards[j].Status)
	})
	return s.withTelegramChatLocked(project), cards, nil
}

func (s *Store) CompletedHoursStats(month string, start time.Time) CompletedHoursStats {
	if start.Before(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		start = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		month = "2026-07"
	}
	end := start.AddDate(0, 1, 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	byEmployee := map[string]*CompletedHoursByEmployee{}
	for _, cards := range s.Cards {
		for _, card := range cards {
			if normalizeCardStatus(card.Status) != "done" || card.CompletedAt == nil {
				continue
			}
			if card.CompletedAt.Before(start) || !card.CompletedAt.Before(end) {
				continue
			}
			key := strings.TrimSpace(card.AssigneeID)
			if key == "" {
				key = "unassigned"
			}
			assignee := strings.TrimSpace(card.Assignee)
			if assignee == "" && key != "unassigned" {
				if user, ok := s.Users[key]; ok {
					assignee = strings.TrimSpace(user.Name)
					if assignee == "" {
						assignee = strings.TrimSpace(user.Email)
					}
				}
			}
			if assignee == "" {
				assignee = "Chưa gán"
			}
			row := byEmployee[key]
			if row == nil {
				row = &CompletedHoursByEmployee{AssigneeID: key, Assignee: assignee}
				byEmployee[key] = row
			}
			row.Hours += card.EstimateHours
			row.TaskCount++
			row.Tasks = append(row.Tasks, card)
		}
	}
	stats := CompletedHoursStats{Month: month, StartedAt: start, Employees: []CompletedHoursByEmployee{}}
	for _, row := range byEmployee {
		sort.SliceStable(row.Tasks, func(i, j int) bool {
			left, right := row.Tasks[i].CompletedAt, row.Tasks[j].CompletedAt
			if left == nil || right == nil {
				return taskDisplayID(row.Tasks[i]) < taskDisplayID(row.Tasks[j])
			}
			return left.After(*right)
		})
		stats.TotalHours += row.Hours
		stats.TotalTasks += row.TaskCount
		stats.Employees = append(stats.Employees, *row)
	}
	sort.SliceStable(stats.Employees, func(i, j int) bool {
		if stats.Employees[i].Hours == stats.Employees[j].Hours {
			return stats.Employees[i].Assignee < stats.Employees[j].Assignee
		}
		return stats.Employees[i].Hours > stats.Employees[j].Hours
	})
	return stats
}

func (s *Store) GetCardDetail(projectID, cardID string) (CardDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	card, ok := s.findCardByLookupLocked(projectID, cardID)
	if !ok {
		return CardDetail{}, errors.New("card not found")
	}
	cardID = card.ID
	return CardDetail{
		Card:        card,
		Comments:    append([]Comment(nil), s.Comments[cardID]...),
		Attachments: attachmentsWithURL(projectID, append([]Attachment(nil), s.Attachments[cardID]...)),
		History:     append([]HistoryEvent(nil), s.History[cardID]...),
	}, nil
}

func (s *Store) GetCardDetailByID(cardID string) (CardDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for projectID := range s.Projects {
		card, ok := s.findCardByLookupLocked(projectID, cardID)
		if !ok {
			continue
		}
		return CardDetail{
			Card:        card,
			Comments:    append([]Comment(nil), s.Comments[card.ID]...),
			Attachments: attachmentsWithURL(projectID, append([]Attachment(nil), s.Attachments[card.ID]...)),
			History:     append([]HistoryEvent(nil), s.History[card.ID]...),
		}, nil
	}
	return CardDetail{}, errors.New("card not found")
}

func (s *Store) ProjectAndCard(projectID, cardID string) (Project, Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.Projects[projectID]
	if !ok {
		return Project{}, Card{}, errors.New("project not found")
	}
	card, ok := s.findCardLocked(projectID, cardID)
	if !ok {
		return Project{}, Card{}, errors.New("card not found")
	}
	return project, card, nil
}

func (s *Store) CreateCard(userID, actor, projectID, title, description, status, priority, assigneeID, assignee, dueDate string, estimateHours float64, estimateNote string) (Card, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Card{}, errors.New("card title is required")
	}
	if estimateHours < 0 {
		estimateHours = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Projects[projectID]; !ok {
		return Card{}, errors.New("project not found")
	}
	now := time.Now().UTC()
	if s.NextCardNumber <= 0 {
		s.NextCardNumber = 1
	}
	normalizedStatus := normalizeCardStatus(status)
	card := Card{
		ID:            randomToken(10),
		Number:        s.NextCardNumber,
		ProjectID:     projectID,
		Title:         title,
		Description:   strings.TrimSpace(description),
		Status:        normalizedStatus,
		Priority:      normalizePriority(priority),
		AssigneeID:    strings.TrimSpace(assigneeID),
		Assignee:      strings.TrimSpace(assignee),
		DueDate:       strings.TrimSpace(dueDate),
		EstimateHours: estimateHours,
		EstimateNote:  strings.TrimSpace(estimateNote),
		CreatedBy:     userID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if normalizedStatus == "done" {
		card.CompletedAt = &now
	}
	s.NextCardNumber++
	s.Cards[projectID] = append(s.Cards[projectID], card)
	s.addHistoryLocked(card.ID, userID, actor, "create", "Tạo task")
	return card, s.saveLocked()
}

func (s *Store) UpdateCard(userID, actor, projectID, cardID string, title, description, status, priority, assigneeID, assignee, dueDate *string, estimateHours *float64, estimateNote *string) (Card, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards := s.Cards[projectID]
	for i := range cards {
		if cards[i].ID != cardID {
			continue
		}
		if cards[i].Closed {
			return Card{}, "", errors.New("task is closed")
		}
		changes := []string{}
		if title != nil {
			if cards[i].Title != strings.TrimSpace(*title) {
				changes = append(changes, "đổi tiêu đề")
			}
			cards[i].Title = strings.TrimSpace(*title)
			if cards[i].Title == "" {
				return Card{}, "", errors.New("card title is required")
			}
		}
		if description != nil {
			if cards[i].Description != strings.TrimSpace(*description) {
				changes = append(changes, "đổi mô tả")
			}
			cards[i].Description = strings.TrimSpace(*description)
		}
		if status != nil {
			next := normalizeCardStatus(*status)
			if cards[i].Status != next {
				changes = append(changes, "chuyển trạng thái "+statusLabel(cards[i].Status)+" -> "+statusLabel(next))
				if next == "done" {
					now := time.Now().UTC()
					cards[i].CompletedAt = &now
				} else if cards[i].Status == "done" {
					cards[i].CompletedAt = nil
				}
			}
			cards[i].Status = next
		}
		if priority != nil {
			next := normalizePriority(*priority)
			if cards[i].Priority != next {
				changes = append(changes, "đổi ưu tiên")
			}
			cards[i].Priority = next
		}
		if assigneeID != nil {
			nextID := strings.TrimSpace(*assigneeID)
			nextName := ""
			if assignee != nil {
				nextName = strings.TrimSpace(*assignee)
			}
			if nextID != "" && nextName == "" {
				if user, ok := s.Users[nextID]; ok {
					nextName = strings.TrimSpace(user.Name)
					if nextName == "" {
						nextName = strings.TrimSpace(user.Email)
					}
				}
			}
			if nextID == "" {
				nextName = ""
			}
			if cards[i].AssigneeID != nextID {
				if nextName == "" {
					nextName = nextID
				}
				if nextID == "" {
					changes = append(changes, "bỏ người phụ trách")
				} else {
					changes = append(changes, "đổi người phụ trách thành #"+strings.TrimPrefix(nextID, "#")+" - "+nextName)
				}
			}
			cards[i].AssigneeID = nextID
			cards[i].Assignee = nextName
		}
		if assignee != nil {
			cards[i].Assignee = strings.TrimSpace(*assignee)
		}
		if dueDate != nil {
			if cards[i].DueDate != strings.TrimSpace(*dueDate) {
				changes = append(changes, "đổi deadline")
			}
			cards[i].DueDate = strings.TrimSpace(*dueDate)
		}
		if estimateHours != nil {
			next := *estimateHours
			if next < 0 {
				next = 0
			}
			if cards[i].EstimateHours != next {
				changes = append(changes, "đổi estimate")
			}
			cards[i].EstimateHours = next
		}
		if estimateNote != nil {
			if cards[i].EstimateNote != strings.TrimSpace(*estimateNote) {
				changes = append(changes, "đổi ghi chú estimate")
			}
			cards[i].EstimateNote = strings.TrimSpace(*estimateNote)
		}
		cards[i].UpdatedAt = time.Now().UTC()
		s.Cards[projectID] = cards
		if len(changes) > 0 {
			s.addHistoryLocked(cardID, userID, actor, "update", strings.Join(changes, ", "))
		}
		summary := strings.Join(changes, ", ")
		return cards[i], summary, s.saveLocked()
	}
	return Card{}, "", errors.New("card not found")
}

func (s *Store) UpdateCardEstimate(userID, actor, projectID, cardID string, hours float64, note string) (Card, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards := s.Cards[projectID]
	for i := range cards {
		if cards[i].ID != cardID {
			continue
		}
		if cards[i].Closed {
			return Card{}, "", errors.New("task is closed")
		}
		if hours < 0 {
			hours = 0
		}
		note = strings.TrimSpace(note)
		changes := []string{}
		if cards[i].EstimateHours != hours {
			changes = append(changes, fmt.Sprintf("auto estimate %.1fh", hours))
		}
		if cards[i].EstimateNote != note {
			changes = append(changes, "cập nhật ghi chú estimate")
		}
		cards[i].EstimateHours = hours
		cards[i].EstimateNote = note
		cards[i].UpdatedAt = time.Now().UTC()
		s.Cards[projectID] = cards
		summary := strings.Join(changes, ", ")
		if summary != "" {
			s.addHistoryLocked(cardID, userID, actor, "estimate", summary)
		}
		return cards[i], summary, s.saveLocked()
	}
	return Card{}, "", errors.New("card not found")
}

func (s *Store) DeleteCard(userID, actor, projectID, cardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards := s.Cards[projectID]
	for i := range cards {
		if cards[i].ID == cardID {
			if cards[i].Closed {
				return errors.New("task is closed")
			}
			s.addHistoryLocked(cardID, userID, actor, "delete", "Xóa task")
			s.Cards[projectID] = append(cards[:i], cards[i+1:]...)
			delete(s.Comments, cardID)
			delete(s.Attachments, cardID)
			return s.saveLocked()
		}
	}
	return errors.New("card not found")
}

func (s *Store) SetCardClosed(userID, actor, projectID, cardID string, closed bool) (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards := s.Cards[projectID]
	for i := range cards {
		if cards[i].ID != cardID {
			continue
		}
		now := time.Now().UTC()
		if cards[i].Closed == closed {
			return cards[i], nil
		}
		cards[i].Closed = closed
		if closed {
			cards[i].ClosedAt = &now
			s.addHistoryLocked(cardID, userID, actor, "close", "Close task")
		} else {
			cards[i].ClosedAt = nil
			s.addHistoryLocked(cardID, userID, actor, "reopen", "Reopen task")
		}
		cards[i].UpdatedAt = now
		s.Cards[projectID] = cards
		return cards[i], s.saveLocked()
	}
	return Card{}, errors.New("card not found")
}

func (s *Store) CreateComment(userID, actor, projectID, cardID, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, errors.New("comment is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	card, ok := s.findCardByLookupLocked(projectID, cardID)
	if !ok {
		return Comment{}, errors.New("card not found")
	}
	cardID = card.ID
	if card.Closed {
		return Comment{}, errors.New("task is closed")
	}
	now := time.Now().UTC()
	comment := Comment{ID: randomToken(10), CardID: cardID, AuthorID: userID, Author: displayActor(actor, userID), Body: body, CreatedAt: now}
	s.Comments[cardID] = append(s.Comments[cardID], comment)
	s.touchCardLocked(projectID, cardID, now)
	s.addHistoryLocked(cardID, userID, actor, "comment", "Thêm bình luận")
	return comment, s.saveLocked()
}

func (s *Store) CreateAttachment(userID, actor, projectID, cardID, uploadPath string, src io.Reader, filename string, size int64, contentType string) (Attachment, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		return Attachment{}, errors.New("filename is required")
	}
	if size > 32<<20 {
		return Attachment{}, errors.New("file is too large")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	card, ok := s.findCardByLookupLocked(projectID, cardID)
	if !ok {
		return Attachment{}, errors.New("card not found")
	}
	cardID = card.ID
	if card.Closed {
		return Attachment{}, errors.New("task is closed")
	}
	if err := os.MkdirAll(uploadPath, 0o755); err != nil {
		return Attachment{}, err
	}
	id := randomToken(10)
	storedName := id + "-" + safeFilename(filename)
	dst, err := os.OpenFile(filepath.Join(uploadPath, storedName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Attachment{}, err
	}
	written, copyErr := io.Copy(dst, io.LimitReader(src, 32<<20+1))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(filepath.Join(uploadPath, storedName))
		return Attachment{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(filepath.Join(uploadPath, storedName))
		return Attachment{}, closeErr
	}
	if written > 32<<20 {
		_ = os.Remove(filepath.Join(uploadPath, storedName))
		return Attachment{}, errors.New("file is too large")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	now := time.Now().UTC()
	attachment := Attachment{
		ID:          id,
		CardID:      cardID,
		UploaderID:  userID,
		Uploader:    displayActor(actor, userID),
		Filename:    filename,
		StoredName:  storedName,
		Size:        written,
		ContentType: contentType,
		CreatedAt:   now,
		URL:         attachmentURL(projectID, cardID, id),
	}
	s.Attachments[cardID] = append(s.Attachments[cardID], attachment)
	s.touchCardLocked(projectID, cardID, now)
	s.addHistoryLocked(cardID, userID, actor, "attachment", "Đính kèm file "+filename)
	return attachment, s.saveLocked()
}

func (s *Store) GetAttachment(projectID, cardID, attachmentID string) (Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	card, ok := s.findCardByLookupLocked(projectID, cardID)
	if !ok {
		return Attachment{}, errors.New("card not found")
	}
	cardID = card.ID
	for _, attachment := range s.Attachments[cardID] {
		if attachment.ID == attachmentID {
			attachment.URL = attachmentURL(projectID, cardID, attachment.ID)
			return attachment, nil
		}
	}
	return Attachment{}, errors.New("attachment not found")
}

func (s *Store) DefaultProjectID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var projects []Project
	for _, project := range s.Projects {
		if project.Status == "active" {
			projects = append(projects, project)
		}
	}
	if len(projects) == 0 {
		for _, project := range s.Projects {
			projects = append(projects, project)
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})
	if len(projects) == 0 {
		return ""
	}
	return projects[0].ID
}

func (s *Store) UpsertTelegramChat(chat TelegramChat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if chat.CreatedAt.IsZero() {
		chat.CreatedAt = now
	}
	chat.UpdatedAt = now
	if s.TelegramChats == nil {
		s.TelegramChats = map[string]TelegramChat{}
	}
	key := telegramChatKey(chat.ID, chat.MessageThreadID)
	if existing, ok := s.TelegramChats[key]; ok {
		chat.CreatedAt = existing.CreatedAt
		if chat.ProjectID == "" {
			chat.ProjectID = existing.ProjectID
		}
	}
	s.TelegramChats[key] = chat
	return s.saveLocked()
}

func (s *Store) BindTelegramChat(code string, chat TelegramChat) (Project, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return Project{}, errors.New("telegram code is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var project Project
	found := false
	for _, item := range s.Projects {
		if strings.EqualFold(item.TelegramCode, code) {
			project = item
			found = true
			break
		}
	}
	if !found {
		return Project{}, errors.New("project code not found")
	}
	now := time.Now().UTC()
	if s.TelegramChats == nil {
		s.TelegramChats = map[string]TelegramChat{}
	}
	key := telegramChatKey(chat.ID, chat.MessageThreadID)
	if existing, ok := s.TelegramChats[key]; ok {
		chat.CreatedAt = existing.CreatedAt
	} else {
		chat.CreatedAt = now
	}
	chat.UpdatedAt = now
	chat.ProjectID = project.ID
	s.TelegramChats[key] = chat
	return s.withTelegramChatLocked(project), s.saveLocked()
}

func (s *Store) ProjectIDForTelegramTarget(chatID, messageThreadID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{telegramChatKey(chatID, messageThreadID)}
	if messageThreadID > 0 {
		keys = append(keys, telegramChatKey(chatID, 0))
	}
	for _, key := range keys {
		chat, ok := s.TelegramChats[key]
		if !ok || chat.ProjectID == "" {
			continue
		}
		if _, ok := s.Projects[chat.ProjectID]; !ok {
			continue
		}
		return chat.ProjectID
	}
	return ""
}

func (s *Store) ListTelegramTargetsForProject(projectID string, extra []int64) []TelegramTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]struct{}{}
	targets := []TelegramTarget{}
	for _, id := range extra {
		if id == 0 {
			continue
		}
		key := telegramChatKey(id, 0)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			targets = append(targets, TelegramTarget{ChatID: id})
		}
	}
	for _, chat := range s.TelegramChats {
		if chat.ID == 0 || chat.ProjectID != projectID {
			continue
		}
		key := telegramChatKey(chat.ID, chat.MessageThreadID)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			targets = append(targets, TelegramTarget{ChatID: chat.ID, MessageThreadID: chat.MessageThreadID})
		}
	}
	return targets
}

func (s *Store) withTelegramChatLocked(project Project) Project {
	for _, chat := range s.TelegramChats {
		if chat.ProjectID == project.ID {
			project.TelegramChatID = chat.ID
			project.TelegramTopicID = chat.MessageThreadID
			project.TelegramChat = chat.Title
			if project.TelegramChat == "" {
				project.TelegramChat = chat.Username
			}
			break
		}
	}
	return project
}

func (s *Store) findCardLocked(projectID, cardID string) (Card, bool) {
	if _, ok := s.Projects[projectID]; !ok {
		return Card{}, false
	}
	for _, card := range s.Cards[projectID] {
		if card.ID == cardID {
			return card, true
		}
	}
	return Card{}, false
}

func (s *Store) findCardByLookupLocked(projectID, value string) (Card, bool) {
	value = normalizeLookupToken(value)
	if _, ok := s.Projects[projectID]; !ok {
		return Card{}, false
	}
	for _, card := range s.Cards[projectID] {
		if normalizeLookupToken(card.ID) == value || fmt.Sprintf("%d", card.Number) == value {
			return card, true
		}
	}
	return Card{}, false
}

func (s *Store) touchCardLocked(projectID, cardID string, now time.Time) {
	cards := s.Cards[projectID]
	for i := range cards {
		if cards[i].ID == cardID {
			cards[i].UpdatedAt = now
			s.Cards[projectID] = cards
			return
		}
	}
}

func (s *Store) addHistoryLocked(cardID, actorID, actor, action, summary string) {
	event := HistoryEvent{
		ID:        randomToken(10),
		CardID:    cardID,
		ActorID:   actorID,
		Actor:     displayActor(actor, actorID),
		Action:    action,
		Summary:   summary,
		CreatedAt: time.Now().UTC(),
	}
	s.History[cardID] = append([]HistoryEvent{event}, s.History[cardID]...)
}

func attachmentsWithURL(projectID string, attachments []Attachment) []Attachment {
	for i := range attachments {
		attachments[i].URL = attachmentURL(projectID, attachments[i].CardID, attachments[i].ID)
	}
	return attachments
}

func attachmentURL(projectID, cardID, attachmentID string) string {
	return "/api/projects/" + url.PathEscape(projectID) + "/cards/" + url.PathEscape(cardID) + "/attachments/" + url.PathEscape(attachmentID)
}

func displayActor(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fallback
}

func safeFilename(name string) string {
	name = filepath.Base(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "_")
	name = replacer.Replace(name)
	if strings.Trim(name, ". ") == "" {
		return "file"
	}
	return name
}

func normalizeLookupToken(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "#"))
}

func (s *Store) ListUsers() []SyncedUser {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]SyncedUser, 0, len(s.Users))
	for _, user := range s.Users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})
	return users
}

func (s *Store) ListActiveUsers() []SyncedUser {
	users := s.ListUsers()
	active := make([]SyncedUser, 0, len(users))
	for _, user := range users {
		if user.IsActive {
			active = append(active, user)
		}
	}
	return active
}

func (s *Store) FindActiveUser(query string) (SyncedUser, error) {
	query = normalizeLookupToken(query)
	if query == "" {
		return SyncedUser{}, errors.New("assignee is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var contains []SyncedUser
	for _, user := range s.Users {
		if !user.IsActive {
			continue
		}
		id := fmt.Sprintf("%d", user.ID)
		email := strings.ToLower(strings.TrimSpace(user.Email))
		name := strings.ToLower(strings.TrimSpace(user.Name))
		if query == id || query == email || query == name {
			return user, nil
		}
		if strings.Contains(email, query) || strings.Contains(name, query) {
			contains = append(contains, user)
		}
	}
	if len(contains) == 1 {
		return contains[0], nil
	}
	if len(contains) > 1 {
		return SyncedUser{}, errors.New("có nhiều user khớp, hãy dùng email")
	}
	return SyncedUser{}, errors.New("user not found")
}

func (s *Store) FindCard(projectID, query string) (Card, error) {
	query = normalizeLookupToken(query)
	if query == "" {
		return Card{}, errors.New("task is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Projects[projectID]; !ok {
		return Card{}, errors.New("project not found")
	}
	var contains []Card
	for _, card := range s.Cards[projectID] {
		title := strings.ToLower(strings.TrimSpace(card.Title))
		if query == normalizeLookupToken(card.ID) || query == fmt.Sprintf("%d", card.Number) || query == title {
			return card, nil
		}
		if strings.Contains(title, query) {
			contains = append(contains, card)
		}
	}
	if len(contains) == 1 {
		return contains[0], nil
	}
	if len(contains) > 1 {
		return Card{}, errors.New("có nhiều task khớp, hãy nhập rõ hơn")
	}
	return Card{}, errors.New("task not found")
}

func (s *Store) UserCursor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UserSyncCursor
}

func (s *Store) UpsertUsers(users []SyncedUser, cursor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if s.Users == nil {
		s.Users = map[string]SyncedUser{}
	}
	for _, user := range users {
		user.SyncedAt = now
		s.Users[fmt.Sprintf("%d", user.ID)] = user
	}
	if cursor != "" {
		s.UserSyncCursor = cursor
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func normalizeProjectStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "paused", "archived":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "active"
	}
}

func normalizeProjectSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '.':
			if builder.Len() > 0 {
				builder.WriteRune(r)
				lastDash = false
			}
		case r == '-' || r == '_' || r == ' ':
			if builder.Len() > 0 && !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), ".-")
}

func normalizeCardStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "doing", "review", "done":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "todo"
	}
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "high", "urgent":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func statusLabel(status string) string {
	switch normalizeCardStatus(status) {
	case "todo":
		return "Cần làm"
	case "doing":
		return "Đang làm"
	case "review":
		return "Review"
	case "done":
		return "Hoàn thành"
	default:
		return status
	}
}

func parseReportMonth(month string) (time.Time, error) {
	parsed, err := time.Parse("2006-01", strings.TrimSpace(month))
	if err != nil {
		return time.Time{}, errors.New("month must use YYYY-MM format")
	}
	if parsed.Before(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		return time.Time{}, errors.New("báo cáo bắt đầu từ tháng 2026-07")
	}
	return parsed, nil
}

func kanbanRank(status string) int {
	switch normalizeCardStatus(status) {
	case "todo":
		return 0
	case "doing":
		return 1
	case "review":
		return 2
	case "done":
		return 3
	default:
		return 4
	}
}

func setCookie(w http.ResponseWriter, name, value string, ttl time.Duration, secure bool, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func setAuthCookies(w http.ResponseWriter, cfg Config, pair TokenPair) {
	accessTTL := time.Duration(pair.ExpiresIn) * time.Second
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	setCookie(w, "task_access_token", pair.AccessToken, accessTTL, cfg.CookieSecure, true)
	setCookie(w, "task_refresh_token", pair.RefreshToken, 30*24*time.Hour, cfg.CookieSecure, true)
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func randomToken(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func newTelegramCode() string {
	code := randomToken(5)
	code = strings.NewReplacer("-", "", "_", "").Replace(code)
	if len(code) > 8 {
		code = code[:8]
	}
	return strings.ToUpper(code)
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func envInt64List(key string) []int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && id != 0 {
			out = append(out, id)
		}
	}
	return out
}
