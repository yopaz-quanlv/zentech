<script setup>
import {
  ArrowLeft,
  BarChart3,
  CalendarDays,
  CheckCircle2,
  Columns3,
  FolderKanban,
  History,
  LogIn,
  LogOut,
  MessageSquare,
  Paperclip,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  Users,
  X,
} from '@lucide/vue';
import { computed, onMounted, onUnmounted, ref } from 'vue';

const columns = [
  { key: 'todo', label: 'Cần làm' },
  { key: 'doing', label: 'Đang làm' },
  { key: 'review', label: 'Review' },
  { key: 'done', label: 'Hoàn thành' },
];

const appVersion = import.meta.env.VITE_APP_VERSION || 'dev';

const loading = ref(true);
const saving = ref(false);
const user = ref(null);
const projects = ref([]);
const cards = ref([]);
const syncedUsers = ref([]);
const userSyncCursor = ref('');
const syncingUsers = ref(false);
const statsMonth = ref('2026-07');
const completedStats = ref(null);
const loadingStats = ref(false);
const estimating = ref(false);
const activeProjectId = ref('');
const activeView = ref('board');
const search = ref('');
const error = ref('');
const now = ref(Date.now());
const draggingCardId = ref('');
const projectModalOpen = ref(false);
const projectMode = ref('create');
const projectForm = ref({ id: '', name: '', description: '', estimate_context: '', status: 'active' });
const selectedProjectDetail = ref(null);
const taskForm = ref(emptyTaskForm());
const selectedCardId = ref('');
const taskDetail = ref(null);
const taskEditing = ref(false);
const commentText = ref('');
const attachmentFile = ref(null);
const uploading = ref(false);
let clock;
let eventSource;

const displayName = computed(() => user.value?.name || user.value?.email || 'User');
const activeProject = computed(() => projects.value.find((item) => item.id === activeProjectId.value) || null);
const workspaceTitle = computed(() => {
  if (activeView.value === 'projects' && !activeProject.value) return 'Danh sách dự án';
  if (activeView.value === 'board' && !activeProject.value) return 'Toàn bộ task';
  return activeProject.value?.name || 'Chưa có project';
});
const workspaceSubtitle = computed(() => {
  if (activeView.value === 'projects' && !activeProject.value) return 'Chọn một dự án để mở board công việc.';
  if (activeView.value === 'board' && !activeProject.value) return 'Kanban tổng hợp task từ toàn bộ project.';
  return activeProject.value ? 'Quản lý task theo kanban board.' : 'Tạo project đầu tiên để bắt đầu quản lý công việc.';
});
const filteredCards = computed(() => {
  const needle = search.value.trim().toLowerCase();
  if (!needle) return cards.value;
  return cards.value.filter((card) =>
    [card.title, card.description, card.assignee, card.priority, card.cost_incurred ? 'phát sinh chi phí' : ''].some((value) =>
      String(value || '').toLowerCase().includes(needle),
    ),
  );
});
const cardsByStatus = computed(() =>
  Object.fromEntries(columns.map((column) => [column.key, filteredCards.value.filter((card) => card.status === column.key)])),
);
const activeUsers = computed(() => syncedUsers.value.filter((item) => item.is_active));
const canAutoEstimate = computed(() => {
  const email = String(user.value?.email || '').toLowerCase();
  const name = String(user.value?.name || '').toLowerCase();
  const subject = String(user.value?.sub || '').toLowerCase();
  return (
    email === 'quanbka.cntt@gmail.com' ||
    email === 'nguyentrunghieu1432000@gmail.com' ||
    name.includes('dev01') ||
    name.includes('lê văn quân') ||
    name.includes('le van quan') ||
    subject === 'dev01'
  );
});
const accessTokenExpiresAt = computed(() => {
  if (!user.value?.exp) return 'Không rõ';
  return new Intl.DateTimeFormat('vi-VN', {
    dateStyle: 'medium',
    timeStyle: 'medium',
    timeZone: 'Asia/Ho_Chi_Minh',
  }).format(new Date(user.value.exp * 1000));
});
const accessTokenRemaining = computed(() => {
  if (!user.value?.exp) return '';
  const seconds = Math.max(0, Math.floor((user.value.exp * 1000 - now.value) / 1000));
  const minutes = Math.floor(seconds / 60);
  const restSeconds = seconds % 60;
  if (minutes <= 0) return `${restSeconds}s còn lại`;
  return `${minutes}m ${restSeconds}s còn lại`;
});

function emptyTaskForm(status = 'todo') {
  return {
    id: '',
    title: '',
    description: '',
    status,
    priority: 'medium',
    cost_incurred: false,
    assignee_id: '',
    assignee: '',
    due_date: '',
    estimate_hours: 0,
    estimate_note: '',
  };
}

async function request(path, options = {}) {
  const isForm = options.body instanceof FormData;
  const res = await fetch(path, {
    headers: isForm ? options.headers || {} : { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options,
  });
  if (res.status === 401) {
    user.value = null;
    projects.value = [];
    cards.value = [];
    throw new Error('unauthorized');
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || 'Request failed');
  }
  return res.json();
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    user.value = await request('/api/me');
    connectRealtime();
    await loadProjects();
    await loadAssignees();
    if (user.value?.admin) await loadUsers();
    const routeTaskId = taskIdFromPath();
    if (routeTaskId) await openTaskById(routeTaskId, false);
    else if (projectSlugFromPath()) await openProjectBySlug(projectSlugFromPath(), false);
    else openProjectList(false);
  } catch (err) {
    if (err.message !== 'unauthorized') error.value = err.message;
  } finally {
    loading.value = false;
  }
}

async function loadProjects() {
  projects.value = await request('/api/projects');
  const routeProjectSlug = projectSlugFromPath();
  const routeProject = routeProjectSlug ? findProjectBySlug(routeProjectSlug) : null;
  if (routeProject) {
    activeProjectId.value = routeProject.id;
  } else if (window.location.pathname === '/' && activeView.value !== 'projects') {
    activeProjectId.value = '';
  } else if (!activeProjectId.value || !projects.value.some((project) => project.id === activeProjectId.value)) {
    activeProjectId.value = projects.value[0]?.id || '';
  }
  if (activeProjectId.value) await loadCards();
  else cards.value = [];
}

async function loadCards() {
  if (!activeProjectId.value) {
    cards.value = await request('/api/cards');
    return;
  }
  cards.value = await request(`/api/projects/${activeProjectId.value}/cards`);
}

async function loadTaskDetail() {
  if (!activeProjectId.value || !selectedCardId.value) return;
  taskDetail.value = await request(`/api/projects/${activeProjectId.value}/cards/${selectedCardId.value}`);
  taskForm.value = { ...emptyTaskForm(), ...taskDetail.value.card };
}

async function openTaskById(cardId, pushUrl = true) {
  selectedCardId.value = cardId;
  taskDetail.value = null;
  activeView.value = 'task-detail';
  const detail = await request(`/api/tasks/${cardId}`);
  taskDetail.value = detail;
  taskForm.value = { ...emptyTaskForm(), ...detail.card };
  taskEditing.value = false;
  activeProjectId.value = detail.card.project_id;
  selectedCardId.value = detail.card.id;
  await loadCards();
  if (pushUrl) setTaskUrl(detail.card);
}

async function loadUsers() {
  const payload = await request('/api/users');
  syncedUsers.value = Array.isArray(payload.users) ? payload.users : [];
  userSyncCursor.value = payload.cursor || '';
}

async function loadAssignees() {
  const payload = await request('/api/assignees');
  syncedUsers.value = Array.isArray(payload.users) ? payload.users : [];
}

async function loadCompletedStats() {
  loadingStats.value = true;
  error.value = '';
  try {
    completedStats.value = await request(`/api/stats/completed-hours?month=${encodeURIComponent(statsMonth.value || '2026-07')}`);
  } catch (err) {
    error.value = err.message;
  } finally {
    loadingStats.value = false;
  }
}

function openStats() {
  activeView.value = 'stats';
  loadCompletedStats();
}

async function syncUsers() {
  syncingUsers.value = true;
  error.value = '';
  try {
    const payload = await request('/api/users/sync', { method: 'POST' });
    syncedUsers.value = Array.isArray(payload.users) ? payload.users : [];
    userSyncCursor.value = payload.cursor || '';
  } catch (err) {
    error.value = err.message;
  } finally {
    syncingUsers.value = false;
  }
}

function login() {
  window.location.href = '/auth/login';
}

async function logout() {
  await fetch('/auth/logout', { method: 'POST' });
  disconnectRealtime();
  user.value = null;
  projects.value = [];
  cards.value = [];
  syncedUsers.value = [];
  userSyncCursor.value = '';
  activeView.value = 'board';
}

async function selectProject(project) {
  await openProjectBySlug(projectSlug(project));
}

async function openProjectBySlug(slug, pushUrl = true) {
  const project = findProjectBySlug(slug);
  if (!project) {
    activeProjectId.value = '';
    activeView.value = 'projects';
    cards.value = [];
    return;
  }
  activeProjectId.value = project.id;
  selectedCardId.value = '';
  taskDetail.value = null;
  activeView.value = 'board';
  await loadCards();
  if (pushUrl) setProjectUrl(project);
  else if (slug !== projectSlug(project)) window.history.replaceState({}, '', `/projects/${encodeURIComponent(projectSlug(project))}`);
}

function openProjectDetail(project) {
  selectedProjectDetail.value = project;
  activeView.value = 'project-detail';
}

function openProjectList(pushUrl = true) {
  activeView.value = 'projects';
  activeProjectId.value = '';
  selectedCardId.value = '';
  taskDetail.value = null;
  cards.value = [];
  if (pushUrl && window.location.pathname !== '/') window.history.pushState({}, '', '/');
}

async function openAllBoard(pushUrl = true) {
  activeView.value = 'board';
  activeProjectId.value = '';
  selectedCardId.value = '';
  taskDetail.value = null;
  taskEditing.value = false;
  await loadCards();
  if (pushUrl && window.location.pathname !== '/') window.history.pushState({}, '', '/');
}

function openProjectModal() {
  projectMode.value = 'create';
  projectForm.value = { id: '', name: '', description: '', estimate_context: '', status: 'active' };
  projectModalOpen.value = true;
}

function openProjectEdit(project) {
  projectMode.value = 'edit';
  projectForm.value = {
    id: project.id,
    name: project.name || '',
    description: project.description || '',
    estimate_context: project.estimate_context || '',
    status: project.status || 'active',
  };
  projectModalOpen.value = true;
}

async function saveProject() {
  const payload = { ...projectForm.value, name: projectForm.value.name.trim() };
  if (!payload.name) return;
  saving.value = true;
  error.value = '';
  try {
    const project = projectMode.value === 'edit'
      ? await request(`/api/projects/${projectForm.value.id}`, {
          method: 'PATCH',
          body: JSON.stringify(payload),
        })
      : await request('/api/projects', {
          method: 'POST',
          body: JSON.stringify(payload),
        });
    projects.value = projectMode.value === 'edit'
      ? projects.value.map((item) => (item.id === project.id ? project : item))
      : [project, ...projects.value];
    if (selectedProjectDetail.value?.id === project.id) selectedProjectDetail.value = project;
    activeProjectId.value = project.id;
    projectModalOpen.value = false;
    await loadCards();
    setProjectUrl(project);
    activeView.value = 'board';
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

function openTaskCreate(status = 'todo') {
  if (!activeProject.value) return;
  taskForm.value = emptyTaskForm(status);
  selectedCardId.value = '';
  taskDetail.value = null;
  activeView.value = 'task-create';
}

async function openTaskDetail(card) {
  await openTaskById(taskPublicId(card));
}

function backToBoard() {
  selectedCardId.value = '';
  taskDetail.value = null;
  taskEditing.value = false;
  commentText.value = '';
  attachmentFile.value = null;
  if (activeProject.value) {
    activeView.value = 'board';
    setProjectUrl(activeProject.value);
    return;
  }
  openAllBoard();
}

function taskPayload() {
  return {
    title: taskForm.value.title.trim(),
    description: taskForm.value.description || '',
    status: taskForm.value.status,
    priority: taskForm.value.priority,
    cost_incurred: Boolean(taskForm.value.cost_incurred),
    assignee_id: taskForm.value.assignee_id || '',
    assignee: taskForm.value.assignee_id ? selectedAssigneeName(taskForm.value.assignee_id, taskForm.value.assignee) : '',
    due_date: taskForm.value.due_date || '',
    estimate_hours: Number(taskForm.value.estimate_hours || 0),
    estimate_note: taskForm.value.estimate_note || '',
  };
}

async function createTask() {
  if (!activeProjectId.value || !taskForm.value.title.trim()) return;
  saving.value = true;
  error.value = '';
  try {
    const card = await request(`/api/projects/${activeProjectId.value}/cards`, {
      method: 'POST',
      body: JSON.stringify(taskPayload()),
    });
    cards.value = [card, ...cards.value];
    await openTaskDetail(card);
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function updateTask() {
  if (!activeProjectId.value || !taskForm.value.id || !taskForm.value.title.trim()) return;
  saving.value = true;
  error.value = '';
  try {
    const card = await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}`, {
      method: 'PATCH',
      body: JSON.stringify(taskPayload()),
    });
    cards.value = cards.value.map((item) => (item.id === card.id ? card : item));
    selectedCardId.value = card.id;
    await loadTaskDetail();
    taskEditing.value = false;
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function updateTaskAssignee() {
  if (!activeProjectId.value || !taskForm.value.id) return;
  saving.value = true;
  error.value = '';
  try {
    const assigneeId = taskForm.value.assignee_id || '';
    const card = await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}`, {
      method: 'PATCH',
      body: JSON.stringify({
        assignee_id: assigneeId,
        assignee: assigneeId ? selectedAssigneeName(assigneeId, taskForm.value.assignee) : '',
      }),
    });
    cards.value = cards.value.map((item) => (item.id === card.id ? card : item));
    taskDetail.value = { ...taskDetail.value, card };
    taskForm.value = { ...emptyTaskForm(), ...card };
    if (activeView.value === 'stats') await loadCompletedStats();
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function updateTaskCostIncurred(nextValue) {
  if (!activeProjectId.value || !taskForm.value.id) return;
  saving.value = true;
  error.value = '';
  try {
    const card = await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}`, {
      method: 'PATCH',
      body: JSON.stringify({ cost_incurred: Boolean(nextValue) }),
    });
    cards.value = cards.value.map((item) => (item.id === card.id ? card : item));
    taskDetail.value = { ...taskDetail.value, card };
    taskForm.value = { ...emptyTaskForm(), ...card };
    if (activeView.value === 'stats') await loadCompletedStats();
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function autoEstimateTask() {
  if (!activeProjectId.value || !taskForm.value.id || !canAutoEstimate.value) return;
  estimating.value = true;
  error.value = '';
  try {
    const card = await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}/estimate`, { method: 'POST' });
    taskDetail.value = { ...taskDetail.value, card };
    taskForm.value = { ...emptyTaskForm(), ...card };
    cards.value = cards.value.map((item) => (item.id === card.id ? card : item));
  } catch (err) {
    error.value = err.message;
  } finally {
    estimating.value = false;
  }
}

async function moveCard(card, status) {
  if (card.status === status) return;
  const previous = card.status;
  card.status = status;
  try {
    const updated = await request(`/api/projects/${activeProjectId.value}/cards/${card.id}`, {
      method: 'PATCH',
      body: JSON.stringify({ status }),
    });
    cards.value = cards.value.map((item) => (item.id === updated.id ? updated : item));
  } catch (err) {
    card.status = previous;
    error.value = err.message;
  }
}

function dragStart(card, event) {
  draggingCardId.value = card.id;
  event.dataTransfer.effectAllowed = 'move';
  event.dataTransfer.setData('text/plain', card.id);
}

function dragEnd() {
  draggingCardId.value = '';
}

function dropOnColumn(status, event) {
  event.preventDefault();
  const id = event.dataTransfer.getData('text/plain') || draggingCardId.value;
  const card = cards.value.find((item) => item.id === id);
  draggingCardId.value = '';
  if (card) moveCard(card, status);
}

async function deleteCurrentTask() {
  if (!activeProjectId.value || !taskForm.value.id) return;
  if (!window.confirm(`Xóa task #${taskPublicId(taskForm.value)}?`)) return;
  const previous = cards.value;
  cards.value = cards.value.filter((item) => item.id !== taskForm.value.id);
  saving.value = true;
  try {
    await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}`, { method: 'DELETE' });
    backToBoard();
  } catch (err) {
    cards.value = previous;
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function setTaskClosed(closed) {
  if (!activeProjectId.value || !taskForm.value.id) return;
  const action = closed ? 'close' : 'reopen';
  saving.value = true;
  error.value = '';
  try {
    const card = await request(`/api/projects/${activeProjectId.value}/cards/${taskForm.value.id}/${action}`, { method: 'POST' });
    taskDetail.value = { ...taskDetail.value, card };
    taskForm.value = { ...emptyTaskForm(), ...card };
    if (closed) {
      cards.value = cards.value.filter((item) => item.id !== card.id);
      taskEditing.value = false;
    } else {
      await loadCards();
    }
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

async function addComment() {
  if (!commentText.value.trim() || !selectedCardId.value) return;
  saving.value = true;
  error.value = '';
  try {
    await request(`/api/projects/${activeProjectId.value}/cards/${selectedCardId.value}/comments`, {
      method: 'POST',
      body: JSON.stringify({ body: commentText.value.trim() }),
    });
    commentText.value = '';
    await loadTaskDetail();
  } catch (err) {
    error.value = err.message;
  } finally {
    saving.value = false;
  }
}

function onFileSelected(event) {
  attachmentFile.value = event.target.files?.[0] || null;
}

async function uploadAttachment() {
  if (!attachmentFile.value || !selectedCardId.value) return;
  const form = new FormData();
  form.append('file', attachmentFile.value);
  uploading.value = true;
  error.value = '';
  try {
    await request(`/api/projects/${activeProjectId.value}/cards/${selectedCardId.value}/attachments`, {
      method: 'POST',
      body: form,
    });
    attachmentFile.value = null;
    const input = document.getElementById('task-attachment-input');
    if (input) input.value = '';
    await loadTaskDetail();
  } catch (err) {
    error.value = err.message;
  } finally {
    uploading.value = false;
  }
}

function connectRealtime() {
  if (eventSource || !window.EventSource) return;
  eventSource = new EventSource('/api/events');
  eventSource.addEventListener('update', async (event) => {
    const type = event.data || '';
    if (type === 'projects') {
      await loadProjects();
      return;
    }
    if (type === 'users' && user.value?.admin) {
      await loadUsers();
      return;
    }
    if (type === 'users') {
      await loadAssignees();
      return;
    }
    if (type === `cards:${activeProjectId.value}`) {
      await loadCards();
      if (activeView.value === 'task-detail' && selectedCardId.value) await loadTaskDetail();
      if (activeView.value === 'stats') await loadCompletedStats();
    }
  });
  eventSource.onerror = () => {
    eventSource?.close();
    eventSource = null;
    if (user.value) window.setTimeout(connectRealtime, 3000);
  };
}

function disconnectRealtime() {
  eventSource?.close();
  eventSource = null;
}

function taskIdFromPath() {
  const match = window.location.pathname.match(/^\/task\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

function projectSlugFromPath() {
  const match = window.location.pathname.match(/^\/projects\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

function projectSlug(project) {
  return project?.slug || project?.id || '';
}

function findProjectBySlug(slug) {
  return projects.value.find((project) => projectSlug(project) === slug || project.id === slug) || null;
}

function taskPublicId(card) {
  return card?.number ? String(card.number) : card?.id || '';
}

function setProjectUrl(project) {
  const next = `/projects/${encodeURIComponent(projectSlug(project))}`;
  if (window.location.pathname !== next) window.history.pushState({}, '', next);
}

function setTaskUrl(card) {
  const next = `/task/${encodeURIComponent(taskPublicId(card))}`;
  if (window.location.pathname !== next) window.history.pushState({}, '', next);
}

function selectedAssigneeName(id, fallback) {
  if (!id) return fallback || '';
  const found = syncedUsers.value.find((item) => String(item.id) === String(id));
  return found?.name || fallback || '';
}

function priorityLabel(priority) {
  return { low: 'Thấp', medium: 'Vừa', high: 'Cao', urgent: 'Gấp' }[priority] || 'Vừa';
}

function statusLabel(status) {
  return columns.find((column) => column.key === status)?.label || status;
}

function projectName(projectId) {
  return projects.value.find((project) => project.id === projectId)?.name || '';
}

function formatDateTime(value) {
  if (!value) return '';
  return new Date(value).toLocaleString('vi-VN', { timeZone: 'Asia/Ho_Chi_Minh' });
}

function formatMonthLabel(month) {
  if (!month) return '';
  const [year, value] = month.split('-');
  return `Tháng ${Number(value)}/${year}`;
}

function formatHours(value) {
  return `${Number(value || 0).toLocaleString('vi-VN', { maximumFractionDigits: 1 })}h`;
}

function formatBytes(size) {
  if (!size) return '0 B';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function isImageAttachment(attachment) {
  const contentType = String(attachment?.content_type || '').toLowerCase();
  if (contentType.startsWith('image/')) return true;
  return /\.(apng|avif|gif|jpe?g|png|svg|webp)$/i.test(String(attachment?.filename || ''));
}

onMounted(() => {
  clock = window.setInterval(() => {
    now.value = Date.now();
  }, 1000);
  window.addEventListener('popstate', handleRouteChange);
  load();
});

onUnmounted(() => {
  if (clock) window.clearInterval(clock);
  window.removeEventListener('popstate', handleRouteChange);
  disconnectRealtime();
});

async function handleRouteChange() {
  const routeTaskId = taskIdFromPath();
  if (routeTaskId) {
    await openTaskById(routeTaskId, false);
    return;
  }
  const routeProjectSlug = projectSlugFromPath();
  if (routeProjectSlug) {
    await openProjectBySlug(routeProjectSlug, false);
    return;
  }
  openProjectList(false);
}

</script>

<template>
  <main class="app-shell">
    <header class="topnav">
      <div class="brand">
        <FolderKanban :size="24" />
        <div>
          <strong>Zentech Projects</strong>
          <span>Quản lý project và Kanban · v{{ appVersion }}</span>
        </div>
      </div>
      <nav v-if="user" class="nav-tabs" aria-label="Điều hướng chính">
        <button type="button" :class="{ active: activeView === 'board' }" @click="openAllBoard">
          <Columns3 :size="17" />
          Board
        </button>
        <button
          type="button"
          :class="{ active: activeView === 'projects' }"
          @click="openProjectList"
        >
          <FolderKanban :size="17" />
          Projects
        </button>
        <button type="button" :class="{ active: activeView === 'stats' }" @click="openStats">
          <BarChart3 :size="17" />
          Thống kê
        </button>
        <button v-if="user.admin" type="button" :class="{ active: activeView === 'users' }" @click="activeView = 'users'">
          <Users :size="17" />
          Users
        </button>
      </nav>
      <div class="top-actions">
        <button v-if="user" class="icon-button" type="button" title="Làm mới" @click="load">
          <RefreshCw :size="18" />
        </button>
        <button v-if="user" class="secondary-button" type="button" @click="logout">
          <LogOut :size="17" />
          Đăng xuất
        </button>
        <button v-else class="primary-button" type="button" @click="login">
          <LogIn :size="17" />
          Đăng nhập
        </button>
      </div>
    </header>

    <section v-if="loading" class="panel state-panel">
      <div class="spinner"></div>
    </section>

    <section v-else-if="!user" class="panel auth-panel">
      <div>
        <p class="eyebrow">Zentech ID</p>
        <h1>Đăng nhập để quản lý project</h1>
        <p>Authentication được xử lý qua id.zentechglobal.io.</p>
      </div>
      <button class="primary-button" type="button" @click="login">
        <LogIn :size="18" />
        Đăng nhập với Zentech ID
      </button>
    </section>

    <template v-else>
      <section class="workspace-head">
        <div>
          <p class="eyebrow">Xin chào {{ displayName }}</p>
          <h1>{{ workspaceTitle }}</h1>
          <p>{{ workspaceSubtitle }}</p>
        </div>
        <div class="head-actions">
          <div class="token-pill">
            <span>Access token</span>
            <strong>{{ accessTokenRemaining }}</strong>
            <small>{{ accessTokenExpiresAt }}</small>
          </div>
          <button class="primary-button" type="button" @click="openProjectModal">
            <Plus :size="17" />
            Project
          </button>
          <button class="primary-button" type="button" :disabled="!activeProject" @click="openTaskCreate('todo')">
            <Plus :size="17" />
            Task
          </button>
        </div>
      </section>

      <p v-if="error" class="error-banner">{{ error }}</p>

      <section v-if="activeProject" class="project-strip" aria-label="Projects">
        <a
          v-for="project in projects"
          :key="project.id"
          :href="`/projects/${projectSlug(project)}`"
          :class="{ active: project.id === activeProjectId }"
          @click.prevent="selectProject(project)"
        >
          <strong>{{ project.name }}</strong>
          <span>{{ project.status }} · {{ project.telegram_chat ? 'Telegram đã kết nối' : 'Chưa kết nối Telegram' }}</span>
        </a>
      </section>

      <section v-if="activeView === 'board'" class="board-view">
        <div class="board-toolbar">
          <label class="search-box">
            <Search :size="17" />
            <input v-model="search" placeholder="Tìm task, mô tả, người phụ trách..." />
          </label>
        </div>
        <div v-if="!projects.length" class="panel empty-state">Chưa có project.</div>
        <div v-else class="kanban">
          <section
            v-for="column in columns"
            :key="column.key"
            class="kanban-column"
            @dragover.prevent
            @drop="dropOnColumn(column.key, $event)"
          >
            <header>
              <div>
                <h2>{{ column.label }}</h2>
                <span>{{ cardsByStatus[column.key]?.length || 0 }} task</span>
              </div>
              <button v-if="activeProject" class="icon-button small" type="button" @click="openTaskCreate(column.key)">
                <Plus :size="16" />
              </button>
            </header>
            <div class="card-stack">
              <article
                v-for="card in cardsByStatus[column.key]"
                :key="card.id"
                class="work-card"
                :class="{ dragging: draggingCardId === card.id }"
                draggable="true"
                @dragstart="dragStart(card, $event)"
                @dragend="dragEnd"
                @click="openTaskDetail(card)"
              >
                <div class="card-topline">
                  <div class="card-badges">
                    <span class="priority" :class="card.priority">{{ priorityLabel(card.priority) }}</span>
                    <span v-if="card.cost_incurred" class="cost-badge">Phát sinh chi phí</span>
                  </div>
                  <span class="status-dot">#{{ taskPublicId(card) }} · {{ column.label }}</span>
                </div>
                <h3>{{ card.title }}</h3>
                <p v-if="!activeProject && projectName(card.project_id)" class="card-project">{{ projectName(card.project_id) }}</p>
                <p v-if="card.description">{{ card.description }}</p>
                <div class="card-meta">
                  <span v-if="card.assignee">{{ card.assignee }}</span>
                  <span v-if="card.due_date"><CalendarDays :size="14" /> {{ card.due_date }}</span>
                  <span v-if="card.estimate_hours">{{ card.estimate_hours }}h estimate</span>
                </div>
              </article>
              <div v-if="(cardsByStatus[column.key]?.length || 0) === 0" class="column-empty">
                Không có task
              </div>
            </div>
          </section>
        </div>
      </section>

      <section v-else-if="activeView === 'task-create'" class="task-page">
        <div class="page-title">
          <button class="secondary-button" type="button" @click="backToBoard">
            <ArrowLeft :size="17" />
            Quay lại board
          </button>
          <div>
            <p class="eyebrow">Task mới</p>
            <h2>Tạo task trong {{ activeProject?.name }}</h2>
          </div>
        </div>
        <form class="panel task-form" @submit.prevent="createTask">
          <div class="task-form-grid">
            <div class="form-main">
              <label>Tiêu đề</label>
              <input v-model="taskForm.title" required />
              <label>Mô tả</label>
              <textarea v-model="taskForm.description" rows="7"></textarea>
              <label>Ghi chú estimate / prompt</label>
              <textarea
                v-model="taskForm.estimate_note"
                rows="5"
                placeholder="Sau này OpenAI sẽ dùng nội dung này để estimate. Prompt có thể điều chỉnh sau."
              ></textarea>
            </div>
            <div class="form-side">
              <label>Trạng thái</label>
              <select v-model="taskForm.status">
                <option v-for="column in columns" :key="column.key" :value="column.key">{{ column.label }}</option>
              </select>
              <label>Ưu tiên</label>
              <select v-model="taskForm.priority">
                <option value="low">Thấp</option>
                <option value="medium">Vừa</option>
                <option value="high">Cao</option>
                <option value="urgent">Gấp</option>
              </select>
              <label class="checkbox-field">
                <input v-model="taskForm.cost_incurred" type="checkbox" />
                <span>Phát sinh chi phí</span>
              </label>
              <label>Người phụ trách</label>
              <select v-model="taskForm.assignee_id">
                <option value="">Chưa gán</option>
                <option v-for="item in activeUsers" :key="item.id" :value="String(item.id)">{{ item.name || item.email }}</option>
              </select>
              <label>Deadline</label>
              <input v-model="taskForm.due_date" type="date" />
              <label>Estimate giờ</label>
              <input v-model.number="taskForm.estimate_hours" min="0" step="0.25" type="number" />
              <div class="form-actions">
                <button class="primary-button" :disabled="saving || !taskForm.title.trim()">
                  <CheckCircle2 :size="17" />
                  Tạo task
                </button>
              </div>
            </div>
          </div>
        </form>
      </section>

      <section v-else-if="activeView === 'task-detail'" class="task-page">
        <div class="page-title">
          <div class="page-title-main">
            <button class="secondary-button" type="button" @click="backToBoard">
              <ArrowLeft :size="17" />
              Quay lại board
            </button>
            <div>
              <p class="eyebrow">Chi tiết task</p>
              <h2>{{ taskDetail?.card ? `#${taskPublicId(taskDetail.card)} · ${taskDetail.card.title}` : 'Đang tải task' }}</h2>
            </div>
          </div>
          <button
            v-if="taskDetail && !taskDetail.card.closed"
            class="primary-button edit-title-button"
            type="button"
            @click="taskEditing = !taskEditing"
          >
            <Pencil :size="17" />
            {{ taskEditing ? 'Đóng chỉnh sửa' : 'Chỉnh sửa' }}
          </button>
        </div>

        <div v-if="!taskDetail" class="panel state-panel">
          <div class="spinner"></div>
        </div>
        <div v-else class="task-detail-grid">
          <div class="detail-main">
          <form v-if="taskEditing" class="panel task-form" @submit.prevent="updateTask">
            <div class="task-editor-heading">
              <div>
                <p class="eyebrow">Chỉnh sửa task</p>
                <h3>Thông tin chính</h3>
              </div>
              <span>#{{ taskPublicId(taskDetail.card) }}</span>
            </div>
            <div class="task-form-grid">
              <div class="form-main">
                <label>Tiêu đề</label>
                <input v-model="taskForm.title" required />
                <label>Mô tả</label>
                <textarea v-model="taskForm.description" rows="7"></textarea>
                <label>Ghi chú estimate / prompt</label>
                <textarea
                  v-model="taskForm.estimate_note"
                  rows="5"
                  placeholder="Sau này OpenAI sẽ dùng nội dung này để estimate. Prompt có thể điều chỉnh sau."
                ></textarea>
              </div>
              <div class="form-side">
                <label>Trạng thái</label>
                <select v-model="taskForm.status">
                  <option v-for="column in columns" :key="column.key" :value="column.key">{{ column.label }}</option>
                </select>
                <label>Ưu tiên</label>
                <select v-model="taskForm.priority">
                  <option value="low">Thấp</option>
                  <option value="medium">Vừa</option>
                  <option value="high">Cao</option>
                  <option value="urgent">Gấp</option>
                </select>
                <label class="checkbox-field">
                  <input v-model="taskForm.cost_incurred" type="checkbox" />
                  <span>Phát sinh chi phí</span>
                </label>
                <label>Người phụ trách</label>
                <select v-model="taskForm.assignee_id">
                  <option value="">Chưa gán</option>
                  <option v-for="item in activeUsers" :key="item.id" :value="String(item.id)">{{ item.name || item.email }}</option>
                </select>
                <label>Deadline</label>
                <input v-model="taskForm.due_date" type="date" />
                <label>Estimate giờ</label>
                <input v-model.number="taskForm.estimate_hours" min="0" step="0.25" type="number" />
                <div class="form-actions">
                  <button class="primary-button" :disabled="saving || !taskForm.title.trim()">
                    <CheckCircle2 :size="17" />
                    Lưu chỉnh sửa
                  </button>
                </div>
              </div>
            </div>
          </form>
          <section v-else class="panel task-readonly">
            <div class="task-editor-heading">
              <div>
                <p class="eyebrow">Thông tin task</p>
                <h3>{{ taskDetail.card.title }}</h3>
              </div>
              <span>#{{ taskPublicId(taskDetail.card) }}</span>
            </div>
            <div class="readonly-grid">
              <div>
                <span>Ưu tiên</span>
                <strong class="readonly-priority">
                  {{ priorityLabel(taskDetail.card.priority) }}
                  <span v-if="taskDetail.card.cost_incurred" class="cost-badge">Phát sinh chi phí</span>
                </strong>
              </div>
              <div>
                <span>Deadline</span>
                <strong>{{ taskDetail.card.due_date || 'Chưa có' }}</strong>
              </div>
              <div>
                <span>Estimate</span>
                <strong>{{ taskDetail.card.estimate_hours || 0 }}h</strong>
              </div>
              <div>
                <span>Trạng thái</span>
                <strong>{{ taskDetail.card.closed ? 'Closed' : statusLabel(taskDetail.card.status) }}</strong>
              </div>
              <div>
                <span>Completed at</span>
                <strong>{{ taskDetail.card.completed_at ? formatDateTime(taskDetail.card.completed_at) : 'Chưa hoàn thành' }}</strong>
              </div>
            </div>
            <div class="readonly-block">
              <span>Mô tả</span>
              <p>{{ taskDetail.card.description || 'Chưa có mô tả.' }}</p>
            </div>
            <div class="readonly-block">
              <span>Ghi chú estimate / prompt</span>
              <p>{{ taskDetail.card.estimate_note || 'Chưa có ghi chú estimate.' }}</p>
            </div>
          </section>

            <section class="panel side-section">
              <div class="side-heading">
                <h3><MessageSquare :size="18" /> Bình luận</h3>
                <span>{{ taskDetail.comments?.length || 0 }}</span>
              </div>
              <div class="comment-list">
                <article v-for="comment in taskDetail.comments" :key="comment.id" class="comment-item">
                  <strong>{{ comment.author }}</strong>
                  <time>{{ formatDateTime(comment.created_at) }}</time>
                  <p>{{ comment.body }}</p>
                </article>
                <p v-if="!taskDetail.comments?.length" class="muted">Chưa có bình luận.</p>
              </div>
              <form class="comment-form" @submit.prevent="addComment">
                <textarea v-model="commentText" rows="3" placeholder="Nhập bình luận..." :disabled="taskDetail.card.closed"></textarea>
                <button class="primary-button" :disabled="saving || taskDetail.card.closed || !commentText.trim()">
                  <MessageSquare :size="17" />
                  Gửi bình luận
                </button>
              </form>
            </section>

            <section class="panel side-section">
              <div class="side-heading">
                <h3><Paperclip :size="18" /> Đính kèm</h3>
                <span>{{ taskDetail.attachments?.length || 0 }}</span>
              </div>
              <div class="attachment-list">
                <a
                  v-for="attachment in taskDetail.attachments"
                  :key="attachment.id"
                  class="attachment-item"
                  :class="{ 'image-attachment': isImageAttachment(attachment) }"
                  :href="attachment.url"
                  target="_blank"
                  rel="noreferrer"
                >
                  <img
                    v-if="isImageAttachment(attachment)"
                    class="attachment-preview"
                    :src="attachment.url"
                    :alt="attachment.filename"
                    loading="lazy"
                  />
                  <Paperclip v-else :size="16" />
                  <span>
                    <strong>{{ attachment.filename }}</strong>
                    <small>{{ formatBytes(attachment.size) }} · {{ attachment.uploader }} · {{ formatDateTime(attachment.created_at) }}</small>
                  </span>
                </a>
                <p v-if="!taskDetail.attachments?.length" class="muted">Chưa có file đính kèm.</p>
              </div>
              <div class="upload-row">
                <input id="task-attachment-input" type="file" :disabled="taskDetail.card.closed" @change="onFileSelected" />
                <button class="secondary-button" type="button" :disabled="uploading || taskDetail.card.closed || !attachmentFile" @click="uploadAttachment">
                  <Upload :size="17" />
                  {{ uploading ? 'Đang tải' : 'Tải lên' }}
                </button>
              </div>
            </section>
          </div>

          <aside class="detail-side">
            <section class="panel side-section" :class="{ 'closed-zone': taskDetail.card.closed }">
              <div class="side-heading">
                <h3><CheckCircle2 :size="18" /> Trạng thái task</h3>
                <span>{{ taskDetail.card.closed ? 'Closed' : 'Open' }}</span>
              </div>
              <button
                v-if="!taskDetail.card.closed"
                class="secondary-button full-width"
                type="button"
                :disabled="saving"
                @click="setTaskClosed(true)"
              >
                Close task
              </button>
              <button
                v-else
                class="primary-button full-width"
                type="button"
                :disabled="saving"
                @click="setTaskClosed(false)"
              >
                Reopen task
              </button>
            </section>

            <section class="panel side-section">
              <div class="side-heading">
                <h3>Phát sinh chi phí</h3>
                <span v-if="taskDetail.card.cost_incurred" class="cost-badge">Có</span>
                <span v-else>Không</span>
              </div>
              <label class="checkbox-field cost-toggle">
                <input
                  :checked="taskDetail.card.cost_incurred"
                  type="checkbox"
                  :disabled="saving || taskDetail.card.closed"
                  @change="updateTaskCostIncurred($event.target.checked)"
                />
                <span>Đánh dấu phát sinh chi phí</span>
              </label>
            </section>

            <section v-if="canAutoEstimate" class="panel side-section">
              <div class="side-heading">
                <h3><RefreshCw :size="18" /> AI estimate</h3>
                <span>{{ taskDetail.card.estimate_hours || 0 }}h</span>
              </div>
              <button
                class="secondary-button full-width"
                type="button"
                :disabled="estimating || taskDetail.card.closed"
                @click="autoEstimateTask"
              >
                <RefreshCw :size="17" />
                {{ estimating ? 'Đang estimate' : 'Auto estimate' }}
              </button>
            </section>

            <section class="panel side-section danger-zone">
              <div class="side-heading">
                <h3><Trash2 :size="18" /> Xóa task</h3>
                <span>#{{ taskPublicId(taskDetail.card) }}</span>
              </div>
              <button class="danger-button full-width" type="button" :disabled="saving || taskDetail.card.closed" @click="deleteCurrentTask">
                <Trash2 :size="17" />
                Xóa task này
              </button>
            </section>

            <section class="panel side-section">
              <div class="side-heading">
                <h3><Users :size="18" /> Người phụ trách</h3>
                <span v-if="taskForm.assignee">{{ taskForm.assignee }}</span>
                <span v-else>Chưa gán</span>
              </div>
              <div class="assignee-sidebar">
                <select v-model="taskForm.assignee_id" :disabled="taskDetail.card.closed">
                  <option value="">Chưa gán</option>
                  <option v-for="item in activeUsers" :key="item.id" :value="String(item.id)">{{ item.name || item.email }}</option>
                </select>
                <button class="secondary-button" type="button" :disabled="saving || taskDetail.card.closed" @click="updateTaskAssignee">
                  <CheckCircle2 :size="17" />
                  Đổi assignee
                </button>
              </div>
            </section>

            <section class="panel side-section">
              <div class="side-heading">
                <h3><History :size="18" /> Lịch sử thay đổi</h3>
                <span>{{ taskDetail.history?.length || 0 }}</span>
              </div>
              <div class="history-list">
                <article v-for="event in taskDetail.history" :key="event.id" class="history-item">
                  <strong>{{ event.summary }}</strong>
                  <span>{{ event.actor }} · {{ formatDateTime(event.created_at) }}</span>
                </article>
                <p v-if="!taskDetail.history?.length" class="muted">Chưa có lịch sử.</p>
              </div>
            </section>
          </aside>
        </div>
      </section>

      <section v-else-if="activeView === 'projects'" class="panel">
        <div class="panel-heading">
          <div>
            <h2>Projects</h2>
            <span>{{ projects.length }} project</span>
          </div>
          <button class="primary-button" type="button" @click="openProjectModal">
            <Plus :size="17" />
            Tạo project
          </button>
        </div>
        <div class="project-grid">
          <a
            v-for="project in projects"
            :key="project.id"
            class="project-card"
            :href="`/projects/${projectSlug(project)}`"
            @click.prevent="selectProject(project)"
          >
            <div class="project-card-top">
              <span class="badge">{{ project.status }}</span>
              <div class="project-card-actions">
                <button class="secondary-button compact-button" type="button" @click.prevent.stop="selectProject(project)">
                  Board
                </button>
                <button class="secondary-button compact-button" type="button" @click.prevent.stop="openProjectEdit(project)">
                  <Pencil :size="15" />
                  Edit
                </button>
              </div>
            </div>
            <h3>{{ project.name }}</h3>
            <div class="telegram-connect" @click.stop>
              <span>Telegram</span>
              <code>/connect {{ project.telegram_code }}</code>
              <small v-if="project.telegram_chat">Đã kết nối: {{ project.telegram_chat }}</small>
              <small v-else>Gửi lệnh này trong group hoặc đúng topic Telegram của project.</small>
            </div>
          </a>
        </div>
      </section>

      <section v-else-if="activeView === 'project-detail' && selectedProjectDetail" class="project-detail-page">
        <div class="page-title">
          <div class="page-title-main">
            <button class="secondary-button" type="button" @click="activeView = 'projects'">
              <ArrowLeft :size="17" />
              Projects
            </button>
            <div>
              <p class="eyebrow">Chi tiết dự án</p>
              <h2>{{ selectedProjectDetail.name }}</h2>
            </div>
          </div>
          <button class="primary-button edit-title-button" type="button" @click="openProjectEdit(selectedProjectDetail)">
            <Pencil :size="17" />
            Chỉnh sửa
          </button>
        </div>
        <div class="panel project-detail-panel">
          <div class="readonly-grid">
            <div>
              <span>Trạng thái</span>
              <strong>{{ selectedProjectDetail.status }}</strong>
            </div>
            <div>
              <span>Telegram</span>
              <strong>{{ selectedProjectDetail.telegram_chat || 'Chưa kết nối' }}</strong>
            </div>
          </div>
          <div class="readonly-block">
            <span>Mô tả dự án</span>
            <p>{{ selectedProjectDetail.description || 'Chưa có mô tả.' }}</p>
          </div>
          <div class="readonly-block">
            <span>Thông tin estimate</span>
            <p>{{ selectedProjectDetail.estimate_context || 'Chưa có thông tin estimate.' }}</p>
          </div>
          <div class="telegram-connect">
            <span>Telegram connect</span>
            <code>/connect {{ selectedProjectDetail.telegram_code }}</code>
            <small>Gửi trong group hoặc đúng topic cần gắn với project.</small>
          </div>
        </div>
      </section>

      <section v-else-if="activeView === 'stats'" class="panel stats-panel">
        <div class="panel-heading">
          <div>
            <h2>Thống kê giờ hoàn thành</h2>
            <span>Bắt đầu từ tháng 7/2026, tính theo completed at</span>
          </div>
          <div class="stats-controls">
            <input v-model="statsMonth" min="2026-07" type="month" @change="loadCompletedStats" />
            <button class="secondary-button" type="button" :disabled="loadingStats" @click="loadCompletedStats">
              <RefreshCw :size="17" />
              {{ loadingStats ? 'Đang tải' : 'Làm mới' }}
            </button>
          </div>
        </div>
        <div class="stats-summary">
          <div>
            <span>Tháng</span>
            <strong>{{ formatMonthLabel(completedStats?.month || statsMonth) }}</strong>
          </div>
          <div>
            <span>Tổng giờ hoàn thành</span>
            <strong>{{ formatHours(completedStats?.total_hours) }}</strong>
          </div>
          <div>
            <span>Task hoàn thành</span>
            <strong>{{ completedStats?.total_tasks || 0 }}</strong>
          </div>
          <div>
            <span>Nhân viên</span>
            <strong>{{ completedStats?.employees?.length || 0 }}</strong>
          </div>
        </div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Nhân viên</th>
                <th>Số task</th>
                <th>Tổng giờ</th>
                <th>Task đã hoàn thành</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in completedStats?.employees || []" :key="item.assignee_id">
                <td>
                  <strong>{{ item.assignee }}</strong>
                  <small v-if="item.assignee_id !== 'unassigned'">#{{ item.assignee_id }}</small>
                </td>
                <td>{{ item.task_count }}</td>
                <td><span class="badge ok">{{ formatHours(item.hours) }}</span></td>
                <td>
                  <div class="stats-task-list">
                    <a v-for="task in item.tasks" :key="task.id" :href="`/task/${task.number || task.id}`">
                      #{{ task.number || task.id }} · {{ task.title }} · {{ formatHours(task.estimate_hours) }}
                    </a>
                  </div>
                </td>
              </tr>
              <tr v-if="!loadingStats && !(completedStats?.employees || []).length">
                <td colspan="4">Chưa có task hoàn thành trong tháng này.</td>
              </tr>
              <tr v-if="loadingStats">
                <td colspan="4">Đang tải thống kê...</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeView === 'users' && user.admin" class="panel users-panel">
        <div class="panel-heading">
          <div>
            <h2>Users đồng bộ từ ID</h2>
            <span>{{ activeUsers.length }} active / {{ syncedUsers.length }} total</span>
          </div>
          <button class="secondary-button" type="button" :disabled="syncingUsers" @click="syncUsers">
            <Users :size="17" />
            {{ syncingUsers ? 'Đang đồng bộ' : 'Đồng bộ lại user' }}
          </button>
        </div>
        <p v-if="userSyncCursor" class="sync-note">
          Lần đồng bộ gần nhất:
          {{ new Date(userSyncCursor).toLocaleString('vi-VN', { timeZone: 'Asia/Ho_Chi_Minh' }) }}
        </p>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Email</th>
                <th>Name</th>
                <th>Role</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in syncedUsers" :key="item.id">
                <td>{{ item.id }}</td>
                <td>{{ item.email }}</td>
                <td>{{ item.name }}</td>
                <td><span class="badge">{{ item.is_admin ? 'Admin' : 'User' }}</span></td>
                <td>
                  <span class="badge" :class="{ ok: item.is_active, off: !item.is_active }">
                    {{ item.is_active ? 'Active' : 'Off' }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>

    <div v-if="projectModalOpen" class="modal-backdrop" @click.self="projectModalOpen = false">
      <form class="modal project-modal" @submit.prevent="saveProject">
        <header>
          <h2>{{ projectMode === 'edit' ? 'Sửa project' : 'Tạo project' }}</h2>
          <button type="button" class="ghost-button" @click="projectModalOpen = false"><X :size="18" /></button>
        </header>
        <label>Tên project</label>
        <input v-model="projectForm.name" required />
        <label>Mô tả</label>
        <textarea v-model="projectForm.description" rows="10"></textarea>
        <template v-if="canAutoEstimate">
          <label>Thông tin estimate của dự án</label>
          <textarea
            v-model="projectForm.estimate_context"
            rows="8"
            placeholder="Mô tả stack, quy ước estimate, mức độ phức tạp thường gặp, năng lực junior dev..."
          ></textarea>
        </template>
        <label>Trạng thái</label>
        <select v-model="projectForm.status">
          <option value="active">Active</option>
          <option value="paused">Paused</option>
          <option value="archived">Archived</option>
        </select>
        <div class="modal-actions">
          <button type="button" class="secondary-button" @click="projectModalOpen = false">Hủy</button>
          <button class="primary-button" :disabled="saving">{{ projectMode === 'edit' ? 'Lưu project' : 'Tạo project' }}</button>
        </div>
      </form>
    </div>
  </main>
</template>
