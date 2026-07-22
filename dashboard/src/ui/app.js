// app.js — the Console (spec §15.4, ADR-024 D6): renders from
// `mc homie history` rows via the API mirror, sends through `homie send`,
// and polls adaptively — tight for 30 s after an operator send, lazy when
// idle. After each history render the dashboard's outbox rows are drained
// trivially (history is the render source, so they are bookkeeping only).

const TIGHT_MS = 1000;
const LAZY_MS = 10000;
const TIGHT_WINDOW_MS = 30000;

const state = {
  sessions: [],
  selected: null,
  tightUntil: 0,
  timer: null,
};

const $ = (id) => document.getElementById(id);

// Beyond-loopback deployments require a token (ADR-024 D4): supplied once
// via the URL fragment (#token=…), kept for the tab's lifetime, attached to
// every API call. The fragment never leaves the browser.
const hashToken = new URLSearchParams(location.hash.slice(1)).get("token");
if (hashToken) {
  sessionStorage.setItem("mc-dashboard-token", hashToken);
  history.replaceState(null, "", location.pathname);
}
const token = sessionStorage.getItem("mc-dashboard-token");

async function api(path, opts = {}) {
  const headers = { ...(opts.headers ?? {}) };
  if (token) {
    headers["authorization"] = `Bearer ${token}`;
  }
  const res = await fetch(path, { ...opts, headers });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(body?.error?.message ?? `request failed (${res.status})`);
  }
  return body;
}

function showError(err) {
  const box = $("error");
  box.textContent = String(err instanceof Error ? err.message : err);
  box.hidden = false;
}

function clearError() {
  $("error").hidden = true;
}

function renderSessions() {
  const list = $("sessions");
  list.replaceChildren(
    ...state.sessions.map((s) => {
      const li = document.createElement("li");
      li.dataset.testid = "session-row";
      li.dataset.session = s.id;
      if (s.id === state.selected) li.classList.add("selected");
      const sid = document.createElement("span");
      sid.className = "sid";
      sid.textContent = s.id;
      const meta = document.createElement("span");
      meta.className = "meta";
      meta.textContent = `${s.status} · ${s.last_activity_at ?? ""}`;
      li.append(sid, meta);
      li.addEventListener("click", () => selectSession(s.id));
      return li;
    }),
  );
}

function renderHistory(history) {
  const messages = $("messages");
  const atBottom =
    messages.scrollHeight - messages.scrollTop - messages.clientHeight < 40;
  messages.replaceChildren(
    ...(history.messages ?? []).map((m) => {
      const li = document.createElement("li");
      li.className = m.direction === "reply" ? "reply" : "inbound";
      li.dataset.testid = "message";
      li.dataset.direction = m.direction;
      const who = document.createElement("span");
      who.className = "who";
      who.textContent =
        m.direction === "reply" ? "homie" : `${m.surface}:${m.channel_ref ?? ""}`;
      const body = document.createElement("span");
      body.textContent = m.body;
      li.append(who, body);
      return li;
    }),
  );
  if (atBottom) messages.scrollTop = messages.scrollHeight;
}

async function refreshSessions() {
  const res = await api("/api/sessions");
  state.sessions = res.sessions ?? [];
  renderSessions();
  renderControls();
}

async function refreshHistory() {
  if (!state.selected) return;
  const requested = state.selected;
  const history = await api(`/api/sessions/${requested}/history`);
  // A slow response for a previously selected session must not render under
  // the newly selected one's header.
  if (state.selected !== requested || history.session_id !== requested) return;
  renderHistory(history);
  await api("/api/outbox/drain", { method: "POST" });
}

/** Send is live only for an active session; an ended/reaped one offers
 * resume instead (spec §15.4: the Console resumes any ended session). */
function renderControls() {
  const row = state.sessions.find((s) => s.id === state.selected);
  const active = row?.status === "active";
  $("send-body").disabled = !active;
  $("send").disabled = !active;
  $("resume").hidden = !row || active;
}

function schedule() {
  clearTimeout(state.timer);
  const ms = Date.now() < state.tightUntil ? TIGHT_MS : LAZY_MS;
  state.timer = setTimeout(tick, ms);
}

async function tick() {
  try {
    await refreshSessions();
    await refreshHistory();
    clearError();
  } catch (err) {
    showError(err);
  }
  schedule();
}

function selectSession(id) {
  state.selected = id;
  $("conversation-head").textContent = id;
  renderSessions();
  renderControls();
  state.tightUntil = Date.now() + TIGHT_WINDOW_MS;
  tick();
}

$("resume").addEventListener("click", async () => {
  if (!state.selected) return;
  try {
    await api(`/api/sessions/${state.selected}/resume`, { method: "POST" });
    state.tightUntil = Date.now() + TIGHT_WINDOW_MS;
    clearError();
    await refreshSessions();
    tick();
  } catch (err) {
    showError(err);
  }
});

$("new-session").addEventListener("click", async () => {
  try {
    const res = await api("/api/sessions", { method: "POST" });
    await refreshSessions();
    selectSession(res.session_id);
  } catch (err) {
    showError(err);
  }
});

$("send-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const input = $("send-body");
  const body = input.value.trim();
  if (!body || !state.selected) return;
  try {
    await api(`/api/sessions/${state.selected}/send`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ body }),
    });
    input.value = "";
    state.tightUntil = Date.now() + TIGHT_WINDOW_MS;
    clearError();
    tick();
  } catch (err) {
    showError(err);
  }
});

tick();
