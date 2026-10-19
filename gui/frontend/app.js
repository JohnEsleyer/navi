// --- DOM Elements ---
const chatBox = document.getElementById("chat-box");
const promptInput = document.getElementById("prompt-input");
const btnSend = document.getElementById("btn-send");
const puppetToggle = document.getElementById("puppet-mode-toggle");
const roleLabel = document.getElementById("role-label");
const statsCost = document.getElementById("stats-cost");

const modelInput = document.getElementById("model-input");
const modelTrigger = document.getElementById("model-trigger");
const modelMenu = document.getElementById("model-menu");
const modelList = document.getElementById("model-list");
const modelWrapper = document.getElementById("model-combobox-wrapper");

const btnSessions = document.getElementById("btn-sessions");
const sessionsModal = document.getElementById("sessions-modal");
const sessionsList = document.getElementById("sessions-list");
const btnCreateSession = document.getElementById("btn-create-session");
const btnCloseSessions = document.getElementById("btn-close-sessions");
const sessionNewTitle = document.getElementById("session-new-title");
const activeSessionLabel = document.getElementById("active-session-label");

const btnCopyContext = document.getElementById("btn-copy-context");
const btnDumpContext = document.getElementById("btn-dump-context");
const btnClearContext = document.getElementById("btn-clear-context");

// Vector Icon for Cache Hits
const svgBolt = `
  <svg class="meta-icon cache" viewBox="0 0 24 24">
    <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
  </svg>
`;

// --- Custom Modal Helper ---
function showCustomDialog(title, message, isConfirm = false) {
  return new Promise((resolve) => {
    const modal = document.getElementById("custom-modal");
    const modalTitle = document.getElementById("modal-title");
    const modalBody = document.getElementById("modal-body");
    const actions = document.getElementById("modal-actions");

    modalTitle.innerText = title;
    modalBody.innerText = message;
    actions.innerHTML = "";

    if (isConfirm) {
      const cancelBtn = document.createElement("button");
      cancelBtn.className = "btn";
      cancelBtn.innerText = "Cancel";
      cancelBtn.onclick = () => { modal.style.display = "none"; resolve(false); };
      actions.appendChild(cancelBtn);
    }

    const okBtn = document.createElement("button");
    okBtn.className = "btn";
    okBtn.style.background = "var(--accent-cyan)";
    okBtn.style.color = "#000";
    okBtn.innerText = "Confirm";
    okBtn.onclick = () => { modal.style.display = "none"; resolve(true); };
    actions.appendChild(okBtn);

    modal.style.display = "flex";
  });
}

function showToast(message) {
  const container = document.getElementById("toast-container");
  const toast = document.createElement("div");
  toast.className = "toast";
  toast.innerText = message;
  container.appendChild(toast);
  setTimeout(() => toast.remove(), 3200);
}

// --- Model Combobox ---
// The input accepts any model name (so Ollama tags and unlisted providers work),
// while the dropdown lists what was actually used before and offers deletion.

async function refreshModelDropdown() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  let history = [];
  try {
    history = (await window.go.main.App.GetModelHistory()) || [];
  } catch (err) {
    return;
  }

  modelList.innerHTML = "";

  if (history.length === 0) {
    const empty = document.createElement("div");
    empty.className = "model-history-empty";
    empty.innerText = "No recent models yet";
    modelList.appendChild(empty);
    return;
  }

  history.forEach((modelName) => {
    const item = document.createElement("div");
    item.className = "model-history-item";
    item.setAttribute("data-value", modelName);

    const label = document.createElement("span");
    label.className = "model-history-label";
    label.innerText = modelName;
    item.appendChild(label);

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn-delete-item";
    btnDelete.type = "button";
    btnDelete.title = `Remove ${modelName} from history`;
    btnDelete.innerText = "×";
    // stopPropagation is essential: without it this would also select the model.
    btnDelete.addEventListener("click", async (e) => {
      e.stopPropagation();
      if (window.go && window.go.main && window.go.main.App) {
        await window.go.main.App.DeleteModelHistory(modelName);
      }
      item.remove();
      if (!modelList.querySelector(".model-history-item")) {
        modelList.innerHTML = '<div class="model-history-empty">No recent models yet</div>';
      }
      showToast(`Removed ${modelName} from history`);
    });
    item.appendChild(btnDelete);

    item.addEventListener("click", async () => {
      modelInput.value = modelName;
      modelMenu.classList.remove("active");
      modelTrigger.classList.remove("open");
      if (window.go && window.go.main && window.go.main.App) {
        await window.go.main.App.SetModel(modelName);
      }
      showToast(`Model set to: ${modelName}`);
    });

    modelList.appendChild(item);
  });
}

modelTrigger.addEventListener("click", (e) => {
  e.stopPropagation();
  const opening = !modelMenu.classList.contains("active");
  modelMenu.classList.toggle("active", opening);
  modelTrigger.classList.toggle("open", opening);
  if (opening) refreshModelDropdown();
});

// Enter in the input commits a typed model name.
modelInput.addEventListener("keydown", async (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    const val = modelInput.value.trim();
    if (!val) return;
    modelMenu.classList.remove("active");
    modelTrigger.classList.remove("open");
    if (window.go && window.go.main && window.go.main.App) {
      await window.go.main.App.SetModel(val);
      // Record it so it shows up next time; harmless if the call fails.
      await window.go.main.App.SaveModelHistory(val).catch(() => {});
    }
    showToast(`Model set to: ${val}`);
  } else if (e.key === "Escape") {
    modelMenu.classList.remove("active");
    modelTrigger.classList.remove("open");
  }
});

document.addEventListener("click", (e) => {
  if (!modelWrapper.contains(e.target)) {
    modelMenu.classList.remove("active");
    modelTrigger.classList.remove("open");
  }
  if (!sessionsModal.contains(e.target) && e.target === sessionsModal) {
    sessionsModal.style.display = "none";
  }
});

// --- Sessions ---
async function refreshSessionUI() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  let active = null;
  try {
    active = await window.go.main.App.GetActiveSession();
  } catch (err) {
    active = null;
  }
  if (active) {
    activeSessionLabel.innerText = active.title || "Chats";
  }
}

async function renderSessionsList() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  const sessions = (await window.go.main.App.ListSessions()) || [];
  const active = await window.go.main.App.GetActiveSession();

  sessionsList.innerHTML = "";

  if (sessions.length === 0) {
    const empty = document.createElement("div");
    empty.className = "model-history-empty";
    empty.innerText = "No saved sessions yet";
    sessionsList.appendChild(empty);
    return;
  }

  sessions.forEach((s) => {
    const row = document.createElement("div");
    row.className = `session-row${active && active.id === s.id ? " active" : ""}`;
    row.title = "Click to switch to this session";

    const info = document.createElement("div");
    info.className = "session-row-info";

    const title = document.createElement("div");
    title.className = "session-row-title";
    title.innerText = s.title || "(untitled)";
    info.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "session-row-meta";
    const when = s.updated_at ? new Date(s.updated_at).toLocaleString() : "";
    meta.innerText = `${s.model || ""}${when ? " · " + when : ""}`;
    info.appendChild(meta);

    row.appendChild(info);

    const actions = document.createElement("div");
    actions.className = "session-row-actions";

    const btnSwitch = document.createElement("button");
    btnSwitch.className = "btn btn-primary";
    btnSwitch.innerText = "Open";
    btnSwitch.addEventListener("click", async (e) => {
      e.stopPropagation();
      await window.go.main.App.SwitchSession(s.id);
      sessionsModal.style.display = "none";
      await loadSessionIntoChat(s.id);
      showToast(`Switched to: ${s.title || "(untitled)"}`);
    });
    actions.appendChild(btnSwitch);

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn btn-danger";
    btnDelete.innerText = "Delete";
    btnDelete.addEventListener("click", async (e) => {
      e.stopPropagation();
      const ok = await showCustomDialog(
        "Delete Session?",
        `Permanently delete "${s.title || "(untitled)"}" and its messages?`,
        true
      );
      if (!ok) return;
      await window.go.main.App.DeleteSession(s.id);
      await renderSessionsList();
      await refreshSessionUI();
      showToast("Session deleted");
    });
    actions.appendChild(btnDelete);

    row.appendChild(actions);
    sessionsList.appendChild(row);
  });
}

// loadSessionIntoChat re-renders the chat pane from stored messages, so a
// switched-to conversation is actually visible rather than blank.
async function loadSessionIntoChat(sessionId) {
  chatBox.innerHTML = "";
  if (!(window.go && window.go.main && window.go.main.App)) return;

  let msgs = [];
  try {
    msgs = (await window.go.main.App.GetSessionMessages(sessionId)) || [];
  } catch (err) {
    msgs = [];
  }

  if (msgs.length === 0) {
    appendBubble("system", "Empty session. Start the conversation.");
    await refreshSessionUI();
    return;
  }

  msgs.forEach((m) => {
    const role = m.role === "assistant" ? "assistant" : m.role === "system" ? "system" : "user";
    let meta = "";
    if (m.cost && m.cost > 0) {
      meta = `Restored · $${m.cost.toFixed(6)}`;
    }
    appendBubble(role, m.content, meta);
  });

  const stats = await window.go.main.App.GetStats();
  if (stats) statsCost.innerText = `$${stats.total_cost.toFixed(6)}`;
  await refreshSessionUI();
}

btnSessions.addEventListener("click", async () => {
  await renderSessionsList();
  sessionsModal.style.display = "flex";
});

btnCloseSessions.addEventListener("click", () => {
  sessionsModal.style.display = "none";
});

btnCreateSession.addEventListener("click", async () => {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  const title = sessionNewTitle.value.trim() || "New Chat";
  const sess = await window.go.main.App.CreateSession(title);
  sessionNewTitle.value = "";
  if (!sess) {
    showToast("Could not create session");
    return;
  }
  await renderSessionsList();
  await refreshSessionUI();
  chatBox.innerHTML = "";
  appendBubble("system", `Started new session: ${sess.title}`);
  sessionsModal.style.display = "none";
  showToast(`Created session: ${sess.title}`);
});

sessionNewTitle.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    btnCreateSession.click();
  }
});

// --- Mode Toggle (Puppet / User) ---
puppetToggle.addEventListener("change", (e) => {
  if (e.target.checked) {
    roleLabel.innerText = "Mode: Puppet (AI)";
    roleLabel.style.color = "var(--accent-purple)";
    promptInput.placeholder = "Write response as the AI agent (direct context injection)...";
  } else {
    roleLabel.innerText = "Mode: User";
    roleLabel.style.color = "var(--text-muted)";
    promptInput.placeholder = "Type a message or press Shift+Enter for newline...";
  }
});

// --- Message Sending Logic ---
async function handleSend() {
  const text = promptInput.value.trim();
  if (!text) return;
  promptInput.value = "";

  const isPuppet = puppetToggle.checked;

  if (isPuppet) {
    // Injected manually by human
    appendBubble("assistant", text, "Injected Manually · $0.00", true);
    if (window.go && window.go.main && window.go.main.App) {
      await window.go.main.App.PretendAgent(text);
    }
    showToast("Assistant completion injected into context");
    return;
  }

  // Normal human query
  appendBubble("user", text);

  if (window.go && window.go.main && window.go.main.App) {
    try {
      const res = await window.go.main.App.Ask(text);
      const meta = res.meta.cached
        ? `${svgBolt} CACHE HIT · $0.000000`
        : `In: ${res.meta.prompt_tokens} Out: ${res.meta.output_tokens} · Turn: $${res.meta.turn_cost.toFixed(6)}`;

      appendBubble("assistant", res.text, meta);
      statsCost.innerText = `$${res.meta.session_cost.toFixed(6)}`;
    } catch (err) {
      appendBubble("system", `Error: ${err}`);
    }
  }
}

function appendBubble(role, content, metaHtml = "", isPuppet = false) {
  const msg = document.createElement("div");
  msg.className = `message ${role} ${isPuppet ? "puppet" : ""}`;

  const bubble = document.createElement("div");
  bubble.className = "bubble";
  bubble.innerText = content;
  msg.appendChild(bubble);

  if (metaHtml) {
    const meta = document.createElement("div");
    meta.className = "meta-info";
    if (isPuppet) {
      meta.innerHTML = `<span class="puppet-badge">PUPPET</span>${metaHtml}`;
    } else {
      meta.innerHTML = metaHtml;
    }
    msg.appendChild(meta);
  }

  chatBox.appendChild(msg);
  chatBox.scrollTop = chatBox.scrollHeight;
}

btnSend.addEventListener("click", handleSend);
promptInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    handleSend();
  }
});

// --- Context Actions ---
btnCopyContext.addEventListener("click", async () => {
  if (window.go && window.go.main && window.go.main.App) {
    const contextText = await window.go.main.App.GetContextText();
    await navigator.clipboard.writeText(contextText);
    showToast("Context window copied to clipboard");
  }
});

btnDumpContext.addEventListener("click", async () => {
  if (window.go && window.go.main && window.go.main.App) {
    try {
      const path = await window.go.main.App.DumpContext();
      await showCustomDialog("Context Dumped", `Context successfully written to:\n\n${path}`);
    } catch (err) {
      await showCustomDialog("Dump Error", `Could not write file: ${err}`);
    }
  }
});

btnClearContext.addEventListener("click", async () => {
  const confirmed = await showCustomDialog(
    "Reset Context?",
    "This will clear the current conversation memory and reset input tokens.",
    true
  );
  if (confirmed && window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.ResetHistory();
    chatBox.innerHTML = '<div class="message system"><div class="bubble">Context window cleared.</div></div>';
    showToast("Memory reset to zero");
  }
});

// --- API Key Modal Logic ---
const btnApiKey = document.getElementById("btn-api-key");
const keyModal = document.getElementById("key-modal");
const inputApiKey = document.getElementById("input-api-key");
const keyMaskedDisplay = document.getElementById("key-masked-display");
const btnToggleKeyVis = document.getElementById("btn-toggle-key-visibility");
const keyModalSave = document.getElementById("key-modal-save");
const keyModalCancel = document.getElementById("key-modal-cancel");

function closeKeyModal() {
  keyModal.style.display = "none";
  // Don't leave the secret sitting in the DOM once the dialog is gone.
  inputApiKey.value = "";
}

btnApiKey.addEventListener("click", async () => {
  if (window.go && window.go.main && window.go.main.App) {
    const masked = await window.go.main.App.MaskedAPIKey();
    const endpoint = await window.go.main.App.GetBaseURL();
    keyMaskedDisplay.innerText = `Current: ${masked}`;
    keyModal.querySelector("#key-modal-desc").innerText =
      `Endpoint: ${endpoint}`;
    // Prefill so the key can be edited rather than retyped from scratch.
    const raw = await window.go.main.App.GetAPIKey();
    inputApiKey.value = raw || "";
  }
  inputApiKey.type = "password";
  btnToggleKeyVis.innerText = "Show";
  keyModal.style.display = "flex";
  inputApiKey.focus();
});

btnToggleKeyVis.addEventListener("click", () => {
  if (inputApiKey.type === "password") {
    inputApiKey.type = "text";
    btnToggleKeyVis.innerText = "Hide";
  } else {
    inputApiKey.type = "password";
    btnToggleKeyVis.innerText = "Show";
  }
});

keyModalCancel.addEventListener("click", closeKeyModal);

// Clicking the backdrop (outside the card) also dismisses.
keyModal.addEventListener("click", (e) => {
  if (e.target === keyModal) closeKeyModal();
});

async function saveApiKey() {
  const newKey = inputApiKey.value.trim();
  if (window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.SetAPIKey(newKey);
    const masked = await window.go.main.App.MaskedAPIKey();
    keyMaskedDisplay.innerText = `Current: ${masked}`;
  }
  closeKeyModal();
  showToast(newKey ? "API key updated for this session" : "API key cleared");
}

keyModalSave.addEventListener("click", saveApiKey);
inputApiKey.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    saveApiKey();
  } else if (e.key === "Escape") {
    closeKeyModal();
  }
});

// --- Startup ---
// Restore the persisted session and its transcript so a reopened GUI shows the
// conversation the CLI (or a previous run) left behind.
window.addEventListener("DOMContentLoaded", async () => {
  if (!(window.go && window.go.main && window.go.main.App)) return;

  try {
    const stats = await window.go.main.App.GetStats();
    if (stats) {
      modelInput.value = stats.model;
      statsCost.innerText = `$${stats.total_cost.toFixed(6)}`;
    }
  } catch (err) {
    // Non-fatal: the chat pane is still usable without stats.
  }

  try {
    const active = await window.go.main.App.GetActiveSession();
    if (active) {
      await loadSessionIntoChat(active.id);
    }
  } catch (err) {
    appendBubble("system", "Navi harness initialized. Context window active.");
  }
});
