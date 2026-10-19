// --- DOM Elements ---
const chatBox = document.getElementById("chat-box");
const promptInput = document.getElementById("prompt-input");
const btnSend = document.getElementById("btn-send");
const puppetToggle = document.getElementById("puppet-mode-toggle");
const roleLabel = document.getElementById("role-label");
const statsCost = document.getElementById("stats-cost");

const modelTrigger = document.getElementById("model-trigger");
const modelMenu = document.getElementById("model-menu");
const currentModelLabel = document.getElementById("current-model");

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

// --- Custom Dropdown Handlers ---
modelTrigger.addEventListener("click", () => modelMenu.classList.toggle("active"));

document.addEventListener("click", (e) => {
  if (!modelTrigger.contains(e.target) && !modelMenu.contains(e.target)) {
    modelMenu.classList.remove("active");
  }
});

document.querySelectorAll(".dropdown-item").forEach((item) => {
  item.addEventListener("click", async () => {
    const val = item.getAttribute("data-value");
    currentModelLabel.innerText = val;
    modelMenu.classList.remove("active");
    if (window.go && window.go.main && window.go.main.App) {
      await window.go.main.App.SetModel(val);
    }
    showToast(`Model set to: ${val}`);
  });
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
