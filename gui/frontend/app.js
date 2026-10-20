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

const sessionsSidebar = document.getElementById("sessions-sidebar");
const sessionsList = document.getElementById("sessions-list");
const btnCreateSession = document.getElementById("btn-create-session");
const sessionNewTitle = document.getElementById("session-new-title");
const activeSessionLabel = document.getElementById("active-session-label");
const activeSessionAvatar = document.getElementById("active-session-avatar");

const btnToggleSidebar = document.getElementById("btn-toggle-sidebar");
const btnCollapseSidebar = document.getElementById("btn-collapse-sidebar");

const btnCopyContext = document.getElementById("btn-copy-context");
const btnDumpContext = document.getElementById("btn-dump-context");
const btnClearContext = document.getElementById("btn-clear-context");

// --- Actions Management & Real-time Live Interception ---
const btnMoreMenu = document.getElementById("btn-more-menu");
const moreDropdownMenu = document.getElementById("more-dropdown-menu");
const btnNavActions = document.getElementById("btn-nav-actions");
const btnNavChat = document.getElementById("btn-nav-chat");
const btnBackToChat = document.getElementById("btn-back-to-chat");

const viewChat = document.getElementById("view-chat");
const viewActions = document.getElementById("view-actions");
const actionsCardsContainer = document.getElementById("actions-cards-container");
const btnOpenCreateAction = document.getElementById("btn-open-create-action");

const actionEditorModal = document.getElementById("action-editor-modal");
const actionModalTitle = document.getElementById("action-modal-title");
const inputActionTag = document.getElementById("input-action-tag");
const inputActionDesc = document.getElementById("input-action-desc");
const inputActionCmd = document.getElementById("input-action-cmd");
const inputActionParam = document.getElementById("input-action-param");
const btnSaveAction = document.getElementById("btn-save-action");
const btnCancelActionEdit = document.getElementById("btn-cancel-action-edit");

const actionsMasterToggle = document.getElementById("actions-master-toggle");
const actionsToggleLabel = document.getElementById("actions-toggle-label");

// Vector Icon for Cache Hits
const svgBolt = `
  <svg class="meta-icon cache" viewBox="0 0 24 24">
    <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
  </svg>
`;

// --- Generated Navi SVG Avatars ---
// Every session gets its own Wayfinder Spark rendered straight from the brand
// icon markup. Instead of a plain hash - which can hand two consecutive chats
// near-identical hues - palettes are assigned from an alternating high-
// contrast ring: when the hash collides with the previously rendered chat's
// palette, the pick is offset three steps along the ring so neighbors on the
// list always differ visibly.
const CONTRAST_PALETTES = [
  { c1: "#00e5ff", c2: "#9d4edd", glow: "#00e5ff" }, // 0. Cyan / Neon Violet
  { c1: "#ff9100", c2: "#ff007f", glow: "#ff9100" }, // 1. Amber Orange / Hot Pink
  { c1: "#10b981", c2: "#00e5ff", glow: "#10b981" }, // 2. Emerald Mint / Electric Cyan
  { c1: "#f43f5e", c2: "#a855f7", glow: "#f43f5e" }, // 3. Rose Red / Purple
  { c1: "#a3e635", c2: "#06b6d4", glow: "#a3e635" }, // 4. Lime Green / Cyan
  { c1: "#e11d48", c2: "#fbbf24", glow: "#fbbf24" }, // 5. Crimson / Warm Gold
  { c1: "#8b5cf6", c2: "#ec4899", glow: "#8b5cf6" }, // 6. Electric Indigo / Pink
  { c1: "#14b8a6", c2: "#f97316", glow: "#14b8a6" }, // 7. Teal / Tangerine
  { c1: "#38bdf8", c2: "#a855f7", glow: "#38bdf8" }, // 8. Sky Blue / Lavender
  { c1: "#eab308", c2: "#ef4444", glow: "#eab308" }  // 9. Neon Yellow / Red
];

// Map of session ID -> assigned palette. It guarantees a chat keeps its color
// across re-renders instead of redrawing differently every time the list does.
const sessionPaletteMap = new Map();

function assignPaletteForSession(sessionId, prevPaletteIndex = -1) {
  if (sessionPaletteMap.has(sessionId)) {
    const existing = sessionPaletteMap.get(sessionId);
    // If it collides with the previous row's palette, reassign it three steps
    // down the ring, which is always a visually contrasting hue family.
    if (existing.index === prevPaletteIndex) {
      const newIdx = (prevPaletteIndex + 3) % CONTRAST_PALETTES.length;
      const pal = { ...CONTRAST_PALETTES[newIdx], index: newIdx };
      sessionPaletteMap.set(sessionId, pal);
      return pal;
    }
    return existing;
  }

  // Derive the initial pick from a stable hash of the session ID.
  let hash = 0;
  for (let i = 0; i < sessionId.length; i++) {
    hash = (hash << 5) - hash + sessionId.charCodeAt(i);
    hash |= 0;
  }
  let pick = Math.abs(hash) % CONTRAST_PALETTES.length;
  if (pick === prevPaletteIndex) {
    pick = (pick + 3) % CONTRAST_PALETTES.length;
  }
  const pal = { ...CONTRAST_PALETTES[pick], index: pick };
  sessionPaletteMap.set(sessionId, pal);
  return pal;
}

function renderNaviAvatarSVG(sessionId, title, size = 26, isActive = false) {
  const pal = sessionPaletteMap.get(sessionId) || CONTRAST_PALETTES[0];
  const gradId = `spark-grad-${sessionId}`;
  const haloId = `spark-halo-${sessionId}`;
  const activeClass = isActive ? "active-avatar-spark" : "";

  return `
    <svg class="session-avatar-icon ${activeClass}" width="${size}" height="${size}" viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="${gradId}" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stop-color="${pal.c1}"/>
          <stop offset="50%" stop-color="#ffffff"/>
          <stop offset="100%" stop-color="${pal.c2}"/>
        </linearGradient>
        <radialGradient id="${haloId}" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stop-color="${pal.c1}" stop-opacity="${isActive ? "0.9" : "0.5"}"/>
          <stop offset="100%" stop-color="${pal.c1}" stop-opacity="0"/>
        </radialGradient>
      </defs>
      <circle cx="50" cy="50" r="${isActive ? "48" : "44"}" fill="url(#${haloId})"/>
      <path d="M50 10 Q50 50 10 50 Q50 50 50 90 Q50 50 90 50 Q50 50 50 10 Z" fill="url(#${gradId})"/>
      <circle cx="50" cy="50" r="7.5" fill="#0b0d13" stroke="${pal.c1}" stroke-width="2.5"/>
      <circle cx="28" cy="28" r="3" fill="${pal.c1}" opacity="${isActive ? "1" : "0.8"}"/>
      <circle cx="72" cy="72" r="3" fill="${pal.c2}" opacity="${isActive ? "1" : "0.8"}"/>
    </svg>
  `;
}

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

// activeModel mirrors the model the engine is actually serving with. It is
// seeded from the engine at startup (which restores the persisted choice) and
// updated whenever the model is switched, so the send path can tell "the user
// picked a different model" from "the field still shows the current one".
let activeModel = "";

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
        activeModel = modelName;
      }
      showToast(`Model set to: ${modelName}`);
    });

    modelList.appendChild(item);
  });
}

async function commitModelInput() {
  const val = modelInput.value.trim();
  if (!val) return;
  if (window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.SetModel(val);
    activeModel = val;
    // Record it so it shows up next time; harmless if the call fails.
    await window.go.main.App.SaveModelHistory(val).catch(() => {});
  }
}

modelTrigger.addEventListener("click", (e) => {
  e.stopPropagation();
  const opening = !modelMenu.classList.contains("active");
  modelMenu.classList.toggle("active", opening);
  modelTrigger.classList.toggle("open", opening);
  if (opening) refreshModelDropdown();
});

// Focusing the input also opens the history dropdown, so the previous models
// are one click away without touching the trigger.
modelInput.addEventListener("focus", () => {
  modelMenu.classList.add("active");
  modelTrigger.classList.add("open");
  refreshModelDropdown();
});

// Committing on change covers blur, paste and autofill, not just Enter.
modelInput.addEventListener("change", async () => {
  await commitModelInput();
});

// Enter in the input commits a typed model name.
modelInput.addEventListener("keydown", async (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    const val = modelInput.value.trim();
    if (!val) return;
    modelMenu.classList.remove("active");
    modelTrigger.classList.remove("open");
    await commitModelInput();
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
});

// --- Working Directory ---
// The header badge tells the user which tree the agent is working in. It is
// the process working directory, which is also where every intercepted action
// command is executed.
async function refreshWorkingDir() {
  if (!(window.go && window.go.main && window.go.main.App) || !window.go.main.App.GetWorkingDir) return;
  try {
    const wd = await window.go.main.App.GetWorkingDir();
    if (!wd) return;
    const el = document.getElementById("session-path-text");
    if (el) el.innerText = wd;
    const badge = document.getElementById("session-path-badge");
    if (badge) badge.title = `Operating Working Directory: ${wd}`;
  } catch (err) {
    // Non-fatal: the badge simply keeps its placeholder.
  }
}

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
    if (activeSessionAvatar) {
      activeSessionAvatar.innerHTML = renderNaviAvatarSVG(active.id, active.title, 24, true);
    }
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
    empty.innerText = "No saved chats yet";
    sessionsList.appendChild(empty);
    return;
  }

  // Palette assignment walks the list in order, so each chat can be checked
  // against the palette the row above it received.
  let prevPaletteIndex = -1;

  sessions.forEach((s) => {
    const isActive = Boolean(active && active.id === s.id);
    const pal = assignPaletteForSession(s.id, prevPaletteIndex);
    prevPaletteIndex = pal.index;

    const row = document.createElement("div");
    row.className = `session-row${isActive ? " active" : ""}`;
    row.setAttribute("data-session-id", s.id);
    row.title = "Click to switch to this chat";

    // Navi avatar with a contrasting gradient, glowing while this chat is up.
    const avatarWrapper = document.createElement("div");
    avatarWrapper.className = "session-avatar-wrapper";
    avatarWrapper.innerHTML = renderNaviAvatarSVG(s.id, s.title, 26, isActive);
    row.appendChild(avatarWrapper);

    const info = document.createElement("div");
    info.className = "session-row-info";

    const title = document.createElement("div");
    title.className = "session-row-title";
    title.innerText = s.title || "(untitled)";
    info.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "session-row-meta";
    meta.innerText = s.model || "default";
    info.appendChild(meta);

    row.appendChild(info);

    // Icon actions: configure (gear) and delete (trash).
    const actions = document.createElement("div");
    actions.className = "session-item-actions";

    const btnSettings = document.createElement("button");
    btnSettings.className = "btn-item-action";
    btnSettings.title = "Configure chat settings (name, model)";
    btnSettings.innerHTML = `
      <svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="3"/>
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
      </svg>
    `;
    btnSettings.addEventListener("click", (e) => {
      e.stopPropagation();
      openChatSettings(s);
    });
    actions.appendChild(btnSettings);

    const btnDelete = document.createElement("button");
    btnDelete.className = "btn-item-action danger";
    btnDelete.title = "Delete chat";
    btnDelete.innerHTML = `
      <svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="3 6 5 6 21 6"/>
        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
      </svg>
    `;
    btnDelete.addEventListener("click", async (e) => {
      e.stopPropagation();
      const ok = await showCustomDialog(
        "Delete Chat?",
        `Permanently delete "${s.title || "(untitled)"}" and its history?`,
        true
      );
      if (!ok) return;
      await window.go.main.App.DeleteSession(s.id);
      await renderSessionsList();
      await refreshSessionUI();
      showToast("Chat deleted");
    });
    actions.appendChild(btnDelete);

    row.appendChild(actions);

    // Clicking anywhere on the row switches, and the highlight is applied
    // immediately so the never-renamed first render has no stale state.
    row.addEventListener("click", async () => {
      document.querySelectorAll(".session-row").forEach((r) => r.classList.remove("active"));
      row.classList.add("active");

      await window.go.main.App.SwitchSession(s.id);
      await loadSessionIntoChat(s.id);
      // Re-render so the glow moves to the new active row and stays in sync.
      await renderSessionsList();
      await refreshSessionUI();
      showToast(`Switched to: ${s.title || "Chat"}`);
    });

    sessionsList.appendChild(row);
  });
}

// --- Dedicated Chat Configuration Modal ---
// Editing a chat means its name and its model; both persist via
// App.UpdateSession, and if the chat being edited is the active one the
// engine starts using that model for the very next question.
const chatSettingsModal = document.getElementById("chat-settings-modal");
const inputChatTitle = document.getElementById("input-chat-title");
const inputChatModel = document.getElementById("input-chat-model");
const btnSaveChatSettings = document.getElementById("btn-save-chat-settings");
const btnCancelChatSettings = document.getElementById("btn-cancel-chat-settings");
let editingSessionId = null;

function openChatSettings(session) {
  editingSessionId = session.id;
  inputChatTitle.value = session.title || "";
  inputChatModel.value = session.model || "";
  chatSettingsModal.style.display = "flex";
  inputChatTitle.focus();
}

function closeChatSettings() {
  chatSettingsModal.style.display = "none";
  editingSessionId = null;
}

btnCancelChatSettings.addEventListener("click", closeChatSettings);
chatSettingsModal.addEventListener("click", (e) => {
  if (e.target === chatSettingsModal) closeChatSettings();
});

// Quick-model tags for common local and cloud names.
document.querySelectorAll(".preset-tags .btn-tag").forEach((btn) => {
  btn.addEventListener("click", () => {
    inputChatModel.value = btn.getAttribute("data-model");
  });
});

btnSaveChatSettings.addEventListener("click", async () => {
  if (!editingSessionId || !(window.go && window.go.main && window.go.main.App)) return;
  const newTitle = inputChatTitle.value.trim() || "New Chat";
  const newModel = inputChatModel.value.trim();

  await window.go.main.App.UpdateSession(editingSessionId, newTitle, newModel);
  closeChatSettings();

  // If the edited chat is the live one, mirror the model into the engine
  // right away and keep the combobox history moving.
  const active = await window.go.main.App.GetActiveSession();
  if (active && active.id === editingSessionId && newModel) {
    await window.go.main.App.SetModel(newModel);
    await window.go.main.App.SaveModelHistory(newModel).catch(() => {});
  }

  await renderSessionsList();
  await refreshSessionUI();
  showToast(`Updated chat: ${newTitle}`);
});

// --- Provider Switcher ---
// Toggles the engine between local runtimes (Ollama, LM Studio, llama.cpp)
// and cloud services, plus custom OpenAI-compatible endpoints. The dropdown
// markup lives in index.html; this block wires state and handlers.
const providerWrapper = document.getElementById("provider-dropdown-wrapper");
const btnProviderTrigger = document.getElementById("btn-provider-trigger");
const providerMenu = document.getElementById("provider-menu");
const providerStatusDot = document.getElementById("provider-status-dot");
const providerIcon = document.getElementById("provider-icon");
const providerCurrentName = document.getElementById("provider-current-name");

const endpointModal = document.getElementById("endpoint-modal");
const btnCustomEndpoint = document.getElementById("btn-custom-endpoint");
const inputCustomEndpoint = document.getElementById("input-custom-endpoint");
const btnSaveEndpoint = document.getElementById("btn-save-endpoint");
const btnCancelEndpoint = document.getElementById("btn-cancel-endpoint");

// refreshActiveProviderUI mirrors the engine's current provider into the
// trigger pill (name, local/cloud styling) and the highlighted menu item.
async function refreshActiveProviderUI() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  try {
    const prov = await window.go.main.App.GetActiveProvider();
    if (!prov) return;

    providerCurrentName.innerText = prov.name;
    if (prov.is_local) {
      providerStatusDot.className = "provider-status-dot local";
      providerIcon.innerText = "💻";
    } else {
      providerStatusDot.className = "provider-status-dot cloud";
      providerIcon.innerText = "☁️";
    }

    // Custom endpoints carry IDs like "custom", which no menu row claims, so
    // no row lights up - the pill label is the only signal, deliberately.
    document.querySelectorAll(".provider-item").forEach((item) => {
      const match = item.getAttribute("data-provider") === prov.id;
      item.classList.toggle("active", match);
    });
  } catch (err) {
    // Non-fatal: the switcher still opens with its static labels.
  }
}

btnProviderTrigger.addEventListener("click", (e) => {
  e.stopPropagation();
  providerMenu.classList.toggle("active");
});

// Switching to a provider actually repoints the engine. The model input is
// cleared so it stays honest: empty always means "the current provider's
// default", never a stale cloud name left over from before the switch.
document.querySelectorAll(".provider-item").forEach((item) => {
  item.addEventListener("click", async () => {
    const providerId = item.getAttribute("data-provider");
    const isLocal = item.getAttribute("data-type") === "local";

    if (window.go && window.go.main && window.go.main.App) {
      const newProv = await window.go.main.App.SwitchProvider(providerId);
      providerMenu.classList.remove("active");
      modelInput.value = "";
      await refreshActiveProviderUI();

      if (isLocal) {
        showToast(`Switched to ${newProv.name} · Free local tokens ($0.00)`);
      } else {
        // Cloud providers need a key; point the user at the key dialog the
        // first time they land on one without one.
        const key = await window.go.main.App.GetAPIKey();
        if (!key) {
          showToast(`Switched to ${newProv.name}. Tip: Click "API Key" to add your token.`);
        } else {
          showToast(`Switched to ${newProv.name}`);
        }
      }
    }
  });
});

// Custom Endpoint configuration modal: for LAN-hosted servers, vLLM, or any
// OpenAI-compatible proxy not covered by the registry.
btnCustomEndpoint.addEventListener("click", async () => {
  providerMenu.classList.remove("active");
  if (window.go && window.go.main && window.go.main.App) {
    inputCustomEndpoint.value = await window.go.main.App.GetBaseURL();
  }
  endpointModal.style.display = "flex";
  inputCustomEndpoint.focus();
});

btnCancelEndpoint.addEventListener("click", () => {
  endpointModal.style.display = "none";
});

btnSaveEndpoint.addEventListener("click", async () => {
  const url = inputCustomEndpoint.value.trim();
  if (url && window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.SetBaseURL(url);
    endpointModal.style.display = "none";
    modelInput.value = "";
    await refreshActiveProviderUI();
    showToast(`Switched base URL to: ${url}`);
  }
});
endpointModal.addEventListener("click", (e) => {
  if (e.target === endpointModal) endpointModal.style.display = "none";
});

// Close the provider menu when clicking anywhere outside its wrapper.
document.addEventListener("click", (e) => {
  if (providerWrapper && !providerWrapper.contains(e.target)) {
    providerMenu.classList.remove("active");
  }
});

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
  if (stats) {
    statsCost.innerText = `$${stats.total_cost.toFixed(6)}`;
    // Switching to a conversation adopts the model it was answered with, so
    // the combobox follows along. activeModel is kept in step, otherwise the
    // send path would treat it as a fresh choice and persist it.
    if (stats.model) {
      activeModel = stats.model;
      modelInput.value = stats.model;
    }
  }
  await refreshSessionUI();
}

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
  await loadSessionIntoChat(sess.id);
  showToast(`Created session: ${sess.title}`);
});

// --- Sidebar Toggle ---
// The chat management panel is a real layout column, not an overlay: the
// collapse animation only moves the sidebar edge, never the chat text.
if (btnToggleSidebar && sessionsSidebar) {
  btnToggleSidebar.addEventListener("click", () => {
    sessionsSidebar.classList.toggle("collapsed");
  });
}
if (btnCollapseSidebar && sessionsSidebar) {
  btnCollapseSidebar.addEventListener("click", () => {
    sessionsSidebar.classList.add("collapsed");
  });
}

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

// --- Actions: View Switching & Management ---
// The whole right pane swaps between the chat and the Actions Management
// component; view state lives only in the DOM, so scrolling and bubbles
// survive an out-and-back.

function showView(view) {
  if (view === "actions") {
    viewChat.style.display = "none";
    viewActions.style.display = "flex";
    loadActionsCards();
  } else {
    viewActions.style.display = "none";
    viewChat.style.display = "flex";
  }
}

btnMoreMenu.addEventListener("click", (e) => {
  e.stopPropagation();
  moreDropdownMenu.classList.toggle("active");
});
document.addEventListener("click", () => moreDropdownMenu.classList.remove("active"));
moreDropdownMenu.addEventListener("click", (e) => e.stopPropagation());

btnNavActions.addEventListener("click", () => showView("actions"));
btnNavChat.addEventListener("click", () => showView("chat"));
btnBackToChat.addEventListener("click", () => showView("chat"));

// Master actions toggle: off means "no tag will ever execute", and the
// backend injects a system notice so the engine stops emitting action tags.
actionsMasterToggle.addEventListener("change", async (e) => {
  const enabled = e.target.checked;
  actionsToggleLabel.innerText = enabled ? "Actions: Active" : "Actions: Off";
  actionsToggleLabel.style.color = enabled ? "var(--accent-cyan)" : "var(--text-muted)";
  if (window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.SetActionsEnabled(enabled);
  }
  showToast(enabled ? "Action tags execution enabled" : "Action tags execution disabled");
});

// syncActionsToggle pulls the persisted switch state, so a toggle made in an
// earlier run shows correctly instead of always starting "Active".
async function syncActionsToggle() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  try {
    const enabled = await window.go.main.App.GetActionsEnabled();
    actionsMasterToggle.checked = enabled;
    actionsToggleLabel.innerText = enabled ? "Actions: Active" : "Actions: Off";
    actionsToggleLabel.style.color = enabled ? "var(--accent-cyan)" : "var(--text-muted)";
  } catch (err) {
    // Non-fatal: the checkbox keeps its markup default.
  }
}

// --- Actions Management Cards ---
async function loadActionsCards() {
  if (!(window.go && window.go.main && window.go.main.App)) return;
  const actions = (await window.go.main.App.GetActions()) || [];
  actionsCardsContainer.innerHTML = "";

  if (actions.length === 0) {
    const empty = document.createElement("div");
    empty.className = "model-history-empty";
    empty.innerText = "No actions registered";
    actionsCardsContainer.appendChild(empty);
  }

  actions.forEach((act) => {
    const card = document.createElement("div");
    card.className = `action-card${act.enabled ? "" : " disabled"}`;

    // Header: the XML tag with its on/off toggle. Built-ins can be turned
    // off but never deleted or renamed.
    const header = document.createElement("div");
    header.className = "action-card-header";
    header.innerHTML = `
      <span class="action-tag-badge">&lt;${act.tag}&gt;</span>
      <label class="toggle-container" title="Enable or disable this action">
        <input type="checkbox" ${act.enabled ? "checked" : ""}>
        <span class="slider"></span>
      </label>
    `;

    const toggle = header.querySelector("input");
    toggle.addEventListener("change", async (e) => {
      await window.go.main.App.ToggleAction(act.id, e.target.checked);
      card.classList.toggle("disabled", !e.target.checked);
      showToast(`Action <${act.tag}> ${e.target.checked ? "enabled" : "disabled"}`);
    });

    const desc = document.createElement("div");
    desc.className = "action-card-desc";
    desc.innerText = act.description || "(no description)";

    const cmd = document.createElement("div");
    cmd.className = "action-card-command";
    cmd.innerText = act.is_builtin ? "Executes the tag body in shell" : `Command: ${act.command_template}`;

    const footer = document.createElement("div");
    footer.className = "action-card-footer";
    footer.innerHTML = `
      <span class="action-card-badge">${act.is_builtin ? "Built-in (Navi Core)" : "User-Defined"}</span>
      <div class="session-item-actions">
        ${!act.is_builtin ? `<button class="btn-item-action btn-edit-act" title="Edit this action">
          <svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
          </svg>
        </button>` : ""}
      </div>
    `;

    if (!act.is_builtin) {
      footer.querySelector(".btn-edit-act").addEventListener("click", () => openActionEditor(act));
    }

    card.appendChild(header);
    card.appendChild(desc);
    card.appendChild(cmd);
    card.appendChild(footer);
    actionsCardsContainer.appendChild(card);
  });
}

// --- Action Editor Modal (create / edit user-defined actions) ---
let editingActionId = null;

// openActionEditor(null) creates; passing an action edits it in place.
function openActionEditor(act = null) {
  if (act) {
    editingActionId = act.id;
    actionModalTitle.innerText = "Edit Action";
    inputActionTag.value = act.tag || "";
    inputActionDesc.value = act.description || "";
    inputActionCmd.value = act.command_template || "";
    inputActionParam.value = act.param_name || "x";
  } else {
    editingActionId = null;
    actionModalTitle.innerText = "Create Action";
    inputActionTag.value = "";
    inputActionDesc.value = "";
    inputActionCmd.value = "";
    inputActionParam.value = "x";
  }
  actionEditorModal.style.display = "flex";
  inputActionTag.focus();
}

btnOpenCreateAction.addEventListener("click", () => openActionEditor(null));
btnCancelActionEdit.addEventListener("click", () => actionEditorModal.style.display = "none");
actionEditorModal.addEventListener("click", (e) => {
  if (e.target === actionEditorModal) actionEditorModal.style.display = "none";
});

btnSaveAction.addEventListener("click", async () => {
  const tag = inputActionTag.value.trim();
  const desc = inputActionDesc.value.trim();
  const cmd = inputActionCmd.value.trim();
  const param = inputActionParam.value.trim() || "x";

  if (!tag || !cmd) {
    showToast("Tag and command template are required");
    return;
  }

  await window.go.main.App.SaveAction({
    id: editingActionId || "",
    tag: tag,
    description: desc,
    command_template: cmd,
    param_name: param,
    is_builtin: false,
    enabled: true,
  });

  actionEditorModal.style.display = "none";
  await loadActionsCards();
  showToast(editingActionId ? `Updated action <${tag}>` : `Created action <${tag}>`);
});

// --- Tag and Output Formatter Helper ---
// The model speaks in XML-ish tags: `<terminal>pwd</terminal>` is a command
// request, `<terminal_output>...</terminal_output>` is what the harness fed
// back. Rendering those raw would look like markup soup, so every tag is
// lifted out of the text and drawn as a dedicated terminal card.

function formatTagLabel(tag) {
  if (!tag) return "Terminal";
  if (tag.toLowerCase() === "terminal") return "Terminal";
  return tag.replace(/[-_]/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function createActionOutputBanner(tagName, body) {
  const isOutput = tagName.toLowerCase().endsWith("_output");
  const cleanTag = isOutput ? tagName.toLowerCase().replace(/_output$/, "") : tagName;
  const banner = document.createElement("div");
  banner.className = `action-execution-banner ${isOutput ? "bubble-action-output" : "bubble-action-command"}`;

  const header = document.createElement("div");
  header.className = "action-banner-header";
  header.innerHTML = `
    <svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <polyline points="4 17 10 11 4 5"/>
      <line x1="12" y1="19" x2="20" y2="19"/>
    </svg>
    <span>${isOutput ? formatTagLabel(cleanTag) + " Output" : "Action &lt;" + tagName + "&gt;"}</span>
  `;

  const output = document.createElement("div");
  output.className = "action-banner-output";
  output.innerText = body;

  banner.appendChild(header);
  banner.appendChild(output);
  return banner;
}

// renderBubbleContent turns a raw message into its display form: plain
// sentences stay plain sentences, while every tag becomes a terminal card.
// The raw text is kept on the element so the streaming path can keep
// appending to it without re-parsing what it already knows.
function renderBubbleContent(bubbleEl, content) {
  bubbleEl.rawContent = content;
  bubbleEl.innerHTML = "";

  if (!content) {
    return;
  }

  // Fast path for plain text without tags
  if (!content.includes("<")) {
    bubbleEl.innerText = content;
    return;
  }

  const tagRegex = /<([a-zA-Z0-9_-]+)>([\s\S]*?)<\/\1>/g;
  let lastIndex = 0;
  let match;
  let matchedAny = false;

  while ((match = tagRegex.exec(content)) !== null) {
    matchedAny = true;
    const textBefore = content.substring(lastIndex, match.index);
    if (textBefore.trim()) {
      const seg = document.createElement("div");
      seg.className = "bubble-text-segment";
      seg.innerText = textBefore.trim();
      bubbleEl.appendChild(seg);
    }

    const tagName = match[1];
    const tagBody = match[2].trim();
    bubbleEl.appendChild(createActionOutputBanner(tagName, tagBody));

    lastIndex = tagRegex.lastIndex;
  }

  const remainder = content.substring(lastIndex);

  // If a tag is currently streaming in unclosed
  const unclosedMatch = /<([a-zA-Z0-9_-]+)>([\s\S]*)$/.exec(remainder);
  if (unclosedMatch) {
    const textBeforeUnclosed = remainder.substring(0, unclosedMatch.index);
    if (textBeforeUnclosed.trim()) {
      const seg = document.createElement("div");
      seg.className = "bubble-text-segment";
      seg.innerText = textBeforeUnclosed.trim();
      bubbleEl.appendChild(seg);
    }
    const unclosedTag = unclosedMatch[1];
    const unclosedBody = unclosedMatch[2].trim();
    bubbleEl.appendChild(createActionOutputBanner(unclosedTag, unclosedBody));
  } else if (remainder.trim()) {
    const seg = document.createElement("div");
    seg.className = "bubble-text-segment";
    seg.innerText = remainder.trim();
    bubbleEl.appendChild(seg);
  } else if (!matchedAny && !unclosedMatch) {
    bubbleEl.innerText = content;
  }
}

// --- Real-Time Streaming & Live Interception Listeners ---
// Tokens arrive continuously; when an action closes, a banner is parked in
// the stream and filled with the command output, and the next turn's tokens
// continue in a fresh bubble.
let activeStreamBubble = null;

function bubbleEl() {
  if (!activeStreamBubble) {
    activeStreamBubble = appendBubble("assistant", "");
  }
  return activeStreamBubble ? activeStreamBubble.querySelector(".bubble") : null;
}

function setupStreamListeners() {
  window.runtime.EventsOn("stream:token", (token) => {
    const el = bubbleEl();
    if (!el) return;
    el.rawContent = (el.rawContent || "") + token;
    renderBubbleContent(el, el.rawContent);
    chatBox.scrollTop = chatBox.scrollHeight;
  });

  window.runtime.EventsOn("stream:action_start", (data) => {
    // If the active bubble only held the action tag or is empty, remove it so no empty bubble remains
    if (activeStreamBubble) {
      const bubble = activeStreamBubble.querySelector(".bubble");
      if (bubble) {
        let text = (bubble.rawContent || bubble.innerText || "").trim();
        const tagPattern = new RegExp(`<${data.tag}>[\\s\\S]*?<\\/${data.tag}>`, "gi");
        text = text.replace(tagPattern, "").trim();
        if (!text) {
          activeStreamBubble.remove();
        } else {
          renderBubbleContent(bubble, text);
        }
      }
      activeStreamBubble = null;
    }

    const banner = document.createElement("div");
    banner.className = "action-execution-banner live-action";
    banner.innerHTML = `
      <div class="action-banner-header">
        <svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="4 17 10 11 4 5"/>
          <line x1="12" y1="19" x2="20" y2="19"/>
        </svg>
        <span>⚡ Intercepted &lt;${data.tag}&gt;</span>
        <span style="font-weight: normal; opacity: 0.8;">Running: ${data.command}</span>
      </div>
      <div class="action-banner-output">Executing command...</div>
    `;
    chatBox.appendChild(banner);
    chatBox.scrollTop = chatBox.scrollHeight;
  });

  window.runtime.EventsOn("stream:action_result", (data) => {
    const banners = chatBox.querySelectorAll(".action-execution-banner.live-action");
    if (banners.length > 0) {
      const banner = banners[banners.length - 1];
      const out = banner.querySelector(".action-banner-output");
      if (out) {
        let text = data.output || "";
        const m = /<[a-zA-Z0-9_-]+_output>([\s\S]*?)<\/[a-zA-Z0-9_-]+_output>/i.exec(text);
        if (m) {
          text = m[1].trim();
        }
        out.innerText = text;
      }
    }
    chatBox.scrollTop = chatBox.scrollHeight;
  });

  window.runtime.EventsOn("stream:complete", (res) => {
    if (activeStreamBubble) {
      const bubble = activeStreamBubble.querySelector(".bubble");
      if (bubble) {
        const text = (bubble.rawContent || bubble.innerText || "").trim();
        if (!text) {
          activeStreamBubble.remove();
        } else {
          renderBubbleContent(bubble, text);
        }
      }
    }
    activeStreamBubble = null;

    if (res && res.meta) {
      statsCost.innerText = `$${res.meta.session_cost.toFixed(6)}`;
      refreshSessionUI();
      // Resetting the sidebar model meta keeps chat rows in sync when a
      // streamed turn was the first use of a new model.
      renderSessionsList();

      // The streamed turn never went through appendBubble's meta path, so
      // the token/cost line is attached to the finished bubble here.
      const assistantMsgs = chatBox.querySelectorAll(".message.assistant");
      if (assistantMsgs.length > 0) {
        const lastMsg = assistantMsgs[assistantMsgs.length - 1];
        if (!lastMsg.querySelector(".meta-info")) {
          const metaDiv = document.createElement("div");
          metaDiv.className = "meta-info";
          metaDiv.innerHTML = res.meta.cached
            ? `${svgBolt} CACHE HIT · $0.000000`
            : `In: ${res.meta.prompt_tokens} Out: ${res.meta.output_tokens} · Turn: $${res.meta.turn_cost.toFixed(6)}`;
          lastMsg.appendChild(metaDiv);
        }
      }
    }
  });

  window.runtime.EventsOn("stream:error", (err) => {
    if (activeStreamBubble) {
      const bubble = activeStreamBubble.querySelector(".bubble");
      if (bubble && !(bubble.rawContent || bubble.innerText || "").trim()) {
        activeStreamBubble.remove();
      }
    }
    appendBubble("system", `Streaming error: ${err}`);
    activeStreamBubble = null;
  });
}

// Listeners attach only under the real Wails runtime. Without it (plain
// browser dev) there is no event bridge, so callers fall back to Ask.
if (window.runtime && typeof window.runtime.EventsOn === "function") {
  setupStreamListeners();
}

// --- Message Sending Logic ---
async function handleSend() {
  const text = promptInput.value.trim();
  if (!text) return;
  promptInput.value = "";

  if (puppetToggle.checked) {
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
      // Sync the combobox before asking, so a typed-but-not-committed model is
      // used for this turn and recorded in history too. Setting the model
      // again would be a no-op with a side effect - it clears the response
      // cache - so it only happens when the model actually changed.
      const currModel = modelInput.value.trim();
      if (currModel && currModel !== activeModel) {
        await window.go.main.App.SetModel(currModel);
        activeModel = currModel;
        await window.go.main.App.SaveModelHistory(currModel).catch(() => {});
      }

      if (window.runtime && typeof window.runtime.EventsOn === "function") {
        // Live streaming with token-by-token action interception.
        activeStreamBubble = null;
        await window.go.main.App.AskStream(text);
        return;
      }

      // Fallback (no Wails runtime): the original one-shot path.
      const res = await window.go.main.App.Ask(text);
      const meta = res.meta.cached
        ? `${svgBolt} CACHE HIT · $0.000000`
        : `In: ${res.meta.prompt_tokens} Out: ${res.meta.output_tokens} · Turn: $${res.meta.turn_cost.toFixed(6)}`;

      appendBubble("assistant", res.text, meta);
      statsCost.innerText = `$${res.meta.session_cost.toFixed(6)}`;
    } catch (err) {
      appendBubble("system", `Error: ${err}`);
      activeStreamBubble = null;
    }
  }
}

function appendBubble(role, content, metaHtml = "", isPuppet = false) {
  const msg = document.createElement("div");
  msg.className = `message ${role} ${isPuppet ? "puppet" : ""}`;

  const bubble = document.createElement("div");
  bubble.className = "bubble";
  renderBubbleContent(bubble, content);
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
  return msg;
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
  // Writing a file is a side effect the user may not want, so it is
  // confirmed in the app's own dialog rather than the native webview one.
  const confirmed = await showCustomDialog(
    "Dump Context Window?",
    "Export the entire current context window to a local .txt file in the operating directory?",
    true
  );
  if (!confirmed) return;

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
      statsCost.innerText = `$${stats.total_cost.toFixed(6)}`;
      // The engine restores the model that was set in the last run, so show
      // it in the combobox: the field then reflects what is actually in use
      // instead of hiding it behind an empty input.
      if (stats.model) {
        activeModel = stats.model;
        modelInput.value = stats.model;
      }
    }
  } catch (err) {
    // Non-fatal: the chat pane is still usable without stats.
  }

  // The path the session is operating in, shown next to the session name.
  await refreshWorkingDir().catch(() => {});

  try {
    const active = await window.go.main.App.GetActiveSession();
    if (active) {
      await loadSessionIntoChat(active.id);
    }
  } catch (err) {
    appendBubble("system", "Navi harness initialized. Context window active.");
  }

  // Sidebar transcript list, after the bindings are confirmed.
  await renderSessionsList().catch(() => {});

  // Provider pill in the topbar, matching wherever the engine points now.
  await refreshActiveProviderUI().catch(() => {});

  // Actions master toggle and the management view's initial cards.
  await syncActionsToggle().catch(() => {});
  await loadActionsCards().catch(() => {});
});
