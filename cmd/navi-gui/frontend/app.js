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
    okBtn.className = "btn btn-primary";
    okBtn.innerText = "OK";
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
  setTimeout(() => toast.remove(), 3500);
}

// --- Dropdown Management ---
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
    showToast(`Switched model to ${val}`);
  });
});

// --- Puppet Mode (Pretend to be AI) ---
puppetToggle.addEventListener("change", (e) => {
  if (e.target.checked) {
    roleLabel.innerText = "Mode: Pretend AI (Puppet)";
    roleLabel.style.color = "var(--accent-purple)";
    promptInput.placeholder = "Type as AI agent (will be added without API call)...";
  } else {
    roleLabel.innerText = "Mode: User";
    roleLabel.style.color = "var(--text-muted)";
    promptInput.placeholder = "Type a message or press Shift+Enter for newline...";
  }
});

// --- Message Sending ---
async function handleSend() {
  const text = promptInput.value.trim();
  if (!text) return;
  promptInput.value = "";

  const isPuppet = puppetToggle.checked;

  if (isPuppet) {
    // Human is pretending to be the AI
    appendBubble("assistant", text, "[Injected Manually - $0.00]", true);
    if (window.go && window.go.main && window.go.main.App) {
      await window.go.main.App.PretendAgent(text);
    }
    showToast("Injected response into context buffer");
    return;
  }

  // Normal user question
  appendBubble("user", text);

  // Call Wails Backend
  if (window.go && window.go.main && window.go.main.App) {
    try {
      const res = await window.go.main.App.Ask(text);
      const meta = res.meta.cached
        ? "⚡ CACHE HIT | $0.000000"
        : `In: ${res.meta.prompt_tokens} Out: ${res.meta.output_tokens} | Turn: $${res.meta.turn_cost.toFixed(6)}`;

      appendBubble("assistant", res.text, meta);
      statsCost.innerText = `$${res.meta.session_cost.toFixed(6)}`;
    } catch (err) {
      appendBubble("system", `Error: ${err}`);
    }
  }
}

function appendBubble(role, content, metaText = "", isPuppet = false) {
  const msg = document.createElement("div");
  msg.className = `message ${role} ${isPuppet ? "puppet" : ""}`;

  const bubble = document.createElement("div");
  bubble.className = "bubble";
  bubble.innerText = content;
  msg.appendChild(bubble);

  if (metaText) {
    const meta = document.createElement("div");
    meta.className = "meta-info";
    if (isPuppet) {
      meta.innerHTML = `<span class="puppet-badge">PUPPET</span>${metaText}`;
    } else {
      meta.innerText = metaText;
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
    showToast("Context window copied to clipboard!");
  }
});

btnDumpContext.addEventListener("click", async () => {
  if (window.go && window.go.main && window.go.main.App) {
    try {
      const path = await window.go.main.App.DumpContext();
      await showCustomDialog("Context Dumped", `Context successfully written to current working directory:\n\n${path}`);
    } catch (err) {
      await showCustomDialog("Error", `Failed to dump context: ${err}`);
    }
  }
});

btnClearContext.addEventListener("click", async () => {
  const confirmed = await showCustomDialog("Clear Memory?", "This will wipe the current context window and reset input tokens.", true);
  if (confirmed && window.go && window.go.main && window.go.main.App) {
    await window.go.main.App.ResetHistory();
    chatBox.innerHTML = '<div class="message system"><div class="bubble">Context window cleared.</div></div>';
    showToast("Memory reset to zero.");
  }
});
