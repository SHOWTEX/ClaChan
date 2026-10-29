(() => {
  "use strict";

  /* ------------------------------------------------------------------ state */

  const PREFS = {
    theme: "clachan.theme",
    model: "clachan.model",
    lastConversation: "clachan.lastConversation",
    lastProfile: "clachan.lastProfile",
    toolsEnabled: "clachan.toolsEnabled",
    maxToolCalls: "clachan.maxToolCalls",
    reasoningEffort: "clachan.reasoningEffort",
  };

  const state = {
    conversations: [],
    current: null,
    providers: [],
    provider: "openrouter",
    model: "",
    providerSort: "",
    providerOnly: "",
    allowFallbacks: true,
    toolsEnabled: true,
    maxToolCalls: 6,
    reasoningEffort: "",
    modelFilter: "all",
    modelSort: "recommended",
    endpointSort: "recommended",
    routingDraft: null,
    endpoints: [],
    endpointsModel: "",
    endpointsLoading: false,
    modelPickerTarget: "conversation",
    providerReady: false,
    streaming: false,
    streamingMsg: null,
    abort: null,
    pinned: true,
    autoRetried: false,
    retryTimer: null,
    theme: "dark",
    prompts: [],
    promptFilter: "",
    editingPromptID: null,
    promptBusy: false,
    presetSaveTarget: "",
    pendingAttachments: [],
    attachmentBusy: false,
    viewerFile: null,
    health: null,
    profile: null,
    authStatus: null,
    authMode: "login",
    authBusy: false,
  };

  const dom = {
    app: document.getElementById("app"),
    sidebar: document.getElementById("sidebar"),
    scrim: document.getElementById("scrim"),
    menu: document.getElementById("menu"),
    newChat: document.getElementById("new-chat"),
    convList: document.getElementById("conv-list"),
    openSettings: document.getElementById("open-settings"),
    version: document.getElementById("version"),
    chatTitle: document.getElementById("chat-title"),
    chatSub: document.getElementById("chat-sub"),
    modelPicker: document.getElementById("model-picker"),
    modelTriggerName: document.getElementById("model-trigger-name"),
    modelTriggerMeta: document.getElementById("model-trigger-meta"),
    modelTriggerBadges: document.getElementById("model-trigger-badges"),
    routingPicker: document.getElementById("routing-picker"),
    routingTriggerLabel: document.getElementById("routing-trigger-label"),
    modelDialog: document.getElementById("model-dialog"),
    modelDialogSub: document.getElementById("model-dialog-sub"),
    modelSearch: document.getElementById("model-search"),
    modelFilters: document.getElementById("model-filters"),
    modelSort: document.getElementById("model-sort"),
    modelListEl: document.getElementById("model-list"),
    modelCount: document.getElementById("model-count"),
    modelRoutingButton: document.getElementById("model-routing-button"),
    routingDialog: document.getElementById("routing-dialog"),
    routingDialogSub: document.getElementById("routing-dialog-sub"),
    routingOptions: document.getElementById("routing-options"),
    endpointSection: document.getElementById("endpoint-section"),
    endpointSort: document.getElementById("endpoint-sort"),
    endpointList: document.getElementById("endpoint-list"),
    allowFallbacks: document.getElementById("allow-fallbacks"),
    routingNote: document.getElementById("routing-note"),
    routingSave: document.getElementById("routing-save"),
    defaultModelLabel: document.getElementById("default-model-label"),
    defaultModelButton: document.getElementById("default-model-button"),
    defaultModelClear: document.getElementById("default-model-clear"),
    notice: document.getElementById("notice"),
    messages: document.getElementById("messages"),
    composer: document.getElementById("composer"),
    input: document.getElementById("input"),
    toolsToggle: document.getElementById("tools-toggle"),
    toolsToggleLabel: document.getElementById("tools-toggle-label"),
    toolCap: document.getElementById("tool-cap"),
    reasoningMenu: document.getElementById("reasoning-menu"),
    reasoningTrigger: document.getElementById("reasoning-trigger"),
    reasoningTriggerLabel: document.getElementById("reasoning-trigger-label"),
    reasoningDropup: document.getElementById("reasoning-dropup"),
    composerAttachments: document.getElementById("composer-attachments"),
    attachFile: document.getElementById("attach-file"),
    fileInput: document.getElementById("file-input"),
    send: document.getElementById("send"),
    stop: document.getElementById("stop"),
    settings: document.getElementById("settings"),
    settingsForm: document.getElementById("settings-form"),
    settingsNav: document.querySelector(".settings-nav"),
    settingsPanels: document.querySelector(".settings-panels"),
    systemPrompt: document.getElementById("system-prompt"),
    systemPreset: document.getElementById("system-preset"),
    systemPresetSave: document.getElementById("system-preset-save"),
    personaName: document.getElementById("persona-name"),
    personaPrompt: document.getElementById("persona-prompt"),
    personaPreset: document.getElementById("persona-preset"),
    personaPresetSave: document.getElementById("persona-preset-save"),
    userName: document.getElementById("user-name"),
    userPersona: document.getElementById("user-persona"),
    ignoredProviders: document.getElementById("ignored-providers"),
    theme: document.getElementById("theme"),
    saveSettings: document.getElementById("save-settings"),
    settingsNote: document.getElementById("settings-note"),
    exportChat: document.getElementById("export-chat"),
    importChat: document.getElementById("import-chat"),
    importFile: document.getElementById("import-chat-file"),
    exportDialog: document.getElementById("export-dialog"),
    exportSub: document.getElementById("export-sub"),
    exportNote: document.getElementById("export-note"),
    openPrompts: document.getElementById("open-prompts"),
    promptsDialog: document.getElementById("prompts-dialog"),
    promptSub: document.getElementById("prompt-sub"),
    promptSearch: document.getElementById("prompt-search"),
    promptCount: document.getElementById("prompt-count"),
    promptNew: document.getElementById("prompt-new"),
    promptList: document.getElementById("prompt-list"),
    promptForm: document.getElementById("prompt-form"),
    promptTitle: document.getElementById("prompt-title"),
    promptBody: document.getElementById("prompt-body"),
    promptSystem: document.getElementById("prompt-system"),
    promptCancel: document.getElementById("prompt-cancel"),
    promptSave: document.getElementById("prompt-save"),
    fileViewer: document.getElementById("file-viewer"),
    fileViewerTitle: document.getElementById("file-viewer-title"),
    fileViewerSub: document.getElementById("file-viewer-sub"),
    fileViewerCode: document.getElementById("file-viewer-code"),
    fileViewerMeta: document.getElementById("file-viewer-meta"),
    fileViewerDownload: document.getElementById("file-viewer-download"),
    fileViewerPrint: document.getElementById("file-viewer-print"),
    printArea: document.getElementById("print-area"),
    toast: document.getElementById("toast"),
    auth: document.getElementById("auth"),
    authTitle: document.getElementById("auth-title"),
    authSub: document.getElementById("auth-sub"),
    authForm: document.getElementById("auth-form"),
    authProfileField: document.getElementById("auth-profile-field"),
    authProfile: document.getElementById("auth-profile"),
    authNameField: document.getElementById("auth-name-field"),
    authName: document.getElementById("auth-name"),
    authPassword: document.getElementById("auth-password"),
    authConfirmField: document.getElementById("auth-confirm-field"),
    authConfirm: document.getElementById("auth-confirm"),
    authError: document.getElementById("auth-error"),
    authSubmit: document.getElementById("auth-submit"),
    authHelp: document.getElementById("auth-help"),
    openProfiles: document.getElementById("open-profiles"),
    profileAvatar: document.getElementById("profile-avatar"),
    profileChipName: document.getElementById("profile-chip-name"),
    profileChipSub: document.getElementById("profile-chip-sub"),
    profiles: document.getElementById("profiles"),
    profilesAvatar: document.getElementById("profiles-avatar"),
    profilesCurrent: document.getElementById("profiles-current"),
    profilesOwnerBadge: document.getElementById("profiles-owner-badge"),
    profilesList: document.getElementById("profiles-list"),
    addProfileSection: document.getElementById("add-profile-section"),
    addProfileForm: document.getElementById("add-profile-form"),
    newProfileName: document.getElementById("new-profile-name"),
    newProfilePassword: document.getElementById("new-profile-password"),
    newProfileConfirm: document.getElementById("new-profile-confirm"),
    changePasswordForm: document.getElementById("change-password-form"),
    currentPassword: document.getElementById("current-password"),
    newPassword: document.getElementById("new-password"),
    confirmPassword: document.getElementById("confirm-password"),
    logoutBtn: document.getElementById("logout-btn"),
  };

  /* ------------------------------------------------------------- small util */

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined && text !== null) node.textContent = text;
    return node;
  }

  function icon(id, className = "icon") {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", className);
    svg.setAttribute("aria-hidden", "true");
    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    use.setAttribute("href", "#" + id);
    svg.append(use);
    return svg;
  }

  function iconButton(symbol, label, className = "icon-btn small") {
    const button = el("button", className);
    button.type = "button";
    button.setAttribute("aria-label", label);
    button.title = label;
    button.append(icon(symbol));
    return button;
  }

  function tempId() {
    return "tmp-" + Math.random().toString(36).slice(2, 10);
  }

  function pref(key) {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  }

  function setPref(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch {
      /* private mode */
    }
  }

  function removePref(key) {
    try {
      localStorage.removeItem(key);
    } catch {
      /* ignore */
    }
  }

  // Chat preferences are per profile so switching profiles does not leak the
  // last conversation or model between them.
  function pkey(key) {
    return state.profile ? key + "." + state.profile.id : key;
  }

  let toastTimer = null;
  function toast(message) {
    if (!message) return;
    dom.toast.textContent = message;
    dom.toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      dom.toast.hidden = true;
    }, 2800);
  }

  const THEMES = ["dark", "light", "midnight", "midnight-purple", "forest", "sepia"];

  // Shared capability notes appended to every built-in preset. Keep these in
  // sync with the renderer and the tools exposed by the server: when a tool is
  // added, describe it in TOOL_GUIDE so every preset knows it is available.
  const MARKDOWN_GUIDE = [
    "## Formatting",
    "Replies are rendered as Markdown. Available syntax:",
    "- headings, **bold**, *italic*, ~~strikethrough~~, and `inline code`",
    "- fenced code blocks with a language tag",
    "- ordered and unordered lists, tables, blockquotes, and horizontal rules",
    "- links written as [label](https://example.com); bare URLs are linked automatically",
    "Raw HTML is not rendered. Put HTML inside a code block when it is part of the answer.",
  ].join("\n");

  const TOOL_GUIDE = [
    "## Tools and attachments",
    "Users can attach JPEG, PNG, WebP, or GIF images, PDF documents, and UTF-8 text or code files such as .txt, .md, .json, .go, .py, and .js. They arrive as parts of the user turn. Web search and web fetch may be available depending on the chat's tool switch; date/time and OpenRouter model search are always available. Use tools when they genuinely help, cite web sources with links, and never claim to run code, read local files, or access private data. If a request needs information you cannot reach, say so plainly.",
  ].join("\n");

  function withCapabilities(prompt) {
    return [prompt, MARKDOWN_GUIDE, TOOL_GUIDE].join("\n\n");
  }

  const SYSTEM_PRESETS = [
    {
      id: "assistant",
      label: "Assistant",
      prompt: withCapabilities("You are a helpful, accurate assistant. Be clear and direct, ask a brief clarifying question when the request is ambiguous, and say when you are unsure."),
    },
    {
      id: "coder",
      label: "Coder",
      prompt: withCapabilities("You are a senior software engineer. Write correct, maintainable code, prefer the simplest solution that works, and call out edge cases, security risks, and trade-offs briefly."),
    },
    {
      id: "minimal",
      label: "Minimal",
      prompt: withCapabilities("Answer with only the information needed. Do not add preamble, summaries, repetition, or filler."),
    },
    {
      id: "researcher",
      label: "Researcher",
      prompt: withCapabilities("You are a rigorous research assistant. Separate facts from inference, cite sources when available, state uncertainty explicitly, and avoid inventing details."),
    },
    {
      id: "editor",
      label: "Editor",
      prompt: withCapabilities("You are a precise editor. Preserve the author's voice while improving clarity, structure, and concision. Explain the most important changes briefly."),
    },
    {
      id: "teacher",
      label: "Teacher",
      prompt: withCapabilities("You are a patient teacher. Explain concepts from first principles, use concrete examples, and check understanding with one short question at the end."),
    },
  ];

  function applyTheme(theme) {
    state.theme = THEMES.includes(theme) ? theme : "dark";
    document.documentElement.dataset.theme = state.theme;
    document.documentElement.style.colorScheme =
      state.theme === "light" || state.theme === "sepia" ? "light" : "dark";
  }

  /* -------------------------------------------------------------------- api */

  async function api(path, options = {}) {
    const method = options.method || "GET";
    const init = { method, headers: { Accept: "application/json" } };
    if (options.body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(options.body);
    }
    const res = await fetch(path, init);
    if (!res.ok) {
      const err = await httpError(res);
      if (res.status === 401 && !options.ignoreAuth) handleUnauthorized();
      throw err;
    }
    if (res.status === 204) return null;
    return res.json();
  }

  async function httpError(res) {
    let data = null;
    try {
      data = await res.json();
    } catch {
      /* non-JSON error body */
    }
    const err = new Error((data && data.message) || `Request failed (${res.status})`);
    err.status = res.status;
    err.code = (data && data.error) || "";
    err.retryAfter = (data && data.retryAfter) || 0;
    return err;
  }

  function streamError(data) {
    const err = new Error((data && data.message) || "The stream failed.");
    err.code = (data && data.code) || "";
    err.retryAfter = (data && data.retryAfter) || 0;
    return err;
  }

  /* --------------------------------------------------------------- boot/init */

  async function init() {
    bindEvents();
    bindAuthEvents();
    bindProfileEvents();
    applyTheme(pref(PREFS.theme) || "dark");

    try {
      await refreshAuth();
    } catch (err) {
      showAuth({ setupRequired: false, profiles: [] });
      showAuthError("Could not reach the server. " + (err.message || ""));
    }
  }

  async function refreshAuth() {
    const status = await api("/api/auth/status", { ignoreAuth: true });
    state.authStatus = status;
    if (status.current) {
      state.profile = status.current;
      await startApp();
    } else {
      showAuth(status);
    }
  }

  async function startApp() {
    dom.auth.hidden = true;
    dom.app.hidden = false;

    clearRetry();
    state.streaming = false;
    state.streamingMsg = null;
    state.abort = null;
    state.autoRetried = false;
    state.pinned = true;
    state.conversations = [];
    state.current = null;
    state.provider = "openrouter";
    state.model = pref(pkey(PREFS.model)) || "";
    state.providerSort = "";
    state.providerOnly = "";
    state.allowFallbacks = true;
    loadToolPrefs();
    state.endpoints = [];
    state.endpointsModel = "";
    state.prompts = [];
    state.editingPromptID = null;
    dom.send.disabled = true;

    setPref(PREFS.lastProfile, state.profile.id);
    updateProfileChip();
    updateHeading();
    renderConversations();
    renderMessages({ stick: true });
    renderToolBar();

    await Promise.all([loadModels(), loadConversations(true), loadHealth()]);

    const lastID = pref(pkey(PREFS.lastConversation));
    const target = state.conversations.find((c) => c.id === lastID) || state.conversations[0];
    if (target) {
      await selectConversation(target.id, true);
    } else {
      newChat({ focus: false });
    }

    if (window.matchMedia("(pointer: fine)").matches) dom.input.focus();
  }

  function showAuth(status) {
    if (state.abort) {
      try {
        state.abort.abort();
      } catch {
        /* ignore */
      }
    }
    clearRetry();

    state.authStatus = status;
    state.profile = null;
    state.streaming = false;
    state.streamingMsg = null;
    state.abort = null;
    state.conversations = [];
    state.current = null;
    state.providers = [];
    state.provider = "openrouter";
    state.model = "";
    state.providerReady = false;
    state.pinned = true;

    dom.app.hidden = true;
    dom.auth.hidden = false;
    if (dom.profiles.open) dom.profiles.close();
    document.title = "Sign in · Clachan";

    const setup = Boolean(status.setupRequired);
    state.authMode = setup ? "setup" : "login";

    dom.authTitle.textContent = setup ? "Create the owner profile" : "Welcome back";
    dom.authSub.textContent = setup
      ? "This is the first profile. It owns the server and can add more later."
      : "Choose a profile and enter its password.";
    dom.authProfileField.hidden = setup;
    dom.authNameField.hidden = !setup;
    dom.authConfirmField.hidden = !setup;
    dom.authPassword.autocomplete = setup ? "new-password" : "current-password";
    dom.authForm.reset();
    dom.authError.hidden = true;
    dom.authProfile.replaceChildren();
    dom.authHelp.textContent = setup
      ? "Chats are stored on this server and stay with the profile."
      : "Chats and settings follow the profile, not the device.";

    if (!setup) {
      for (const profile of status.profiles || []) {
        const option = el("option", null, profile.name + (profile.isOwner ? " (owner)" : ""));
        option.value = profile.id;
        dom.authProfile.append(option);
      }
      const last = pref(PREFS.lastProfile);
      if (last && (status.profiles || []).some((profile) => profile.id === last)) {
        dom.authProfile.value = last;
      }
    }

    setAuthBusy(false);
    if (setup) {
      dom.authName.focus();
    } else if (dom.authProfile.value) {
      dom.authPassword.focus();
    } else {
      dom.authProfile.focus();
    }
  }

  function showAuthError(message) {
    dom.authError.textContent = message || "";
    dom.authError.hidden = !message;
  }

  function setAuthBusy(busy) {
    state.authBusy = busy;
    dom.authSubmit.disabled = busy;
    if (busy) {
      dom.authSubmit.textContent = state.authMode === "setup" ? "Creating…" : "Signing in…";
    } else {
      dom.authSubmit.textContent = state.authMode === "setup" ? "Create profile" : "Sign in";
    }
  }

  async function submitAuth(event) {
    event.preventDefault();
    if (state.authBusy) return;

    setAuthBusy(true);
    showAuthError("");

    const password = dom.authPassword.value;
    try {
      if (state.authMode === "setup") {
        const name = dom.authName.value.trim();
        if (!name) throw new Error("Enter a profile name.");
        if (password.length < 8) throw new Error("Use at least 8 characters for the password.");
        if (password !== dom.authConfirm.value) throw new Error("The passwords do not match.");
        await api("/api/auth/setup", {
          method: "POST",
          body: { name, password },
          ignoreAuth: true,
        });
      } else {
        const profileId = dom.authProfile.value;
        if (!profileId) throw new Error("Choose a profile.");
        await api("/api/auth/login", {
          method: "POST",
          body: { profileId, password },
          ignoreAuth: true,
        });
        setPref(PREFS.lastProfile, profileId);
      }

      dom.authPassword.value = "";
      dom.authConfirm.value = "";
      await refreshAuth();
    } catch (err) {
      showAuthError(err.message || "Sign-in failed.");
      dom.authPassword.focus();
      dom.authPassword.select();
    } finally {
      setAuthBusy(false);
    }
  }

  function updateProfileChip() {
    if (!state.profile) return;
    const initial = (state.profile.name.trim()[0] || "?").toUpperCase();
    dom.profileAvatar.textContent = initial;
    dom.profileChipName.textContent = state.profile.name;
    dom.profileChipSub.textContent = state.profile.isOwner ? "Owner" : "Profile";
    dom.profilesAvatar.textContent = initial;
    dom.profilesCurrent.textContent = state.profile.name;
    dom.profilesOwnerBadge.textContent = state.profile.isOwner ? "Owner" : "Profile";
    dom.addProfileSection.hidden = !state.profile.isOwner;
  }

  async function openProfiles() {
    if (!state.profile) return;
    closeSidebar();
    updateProfileChip();
    dom.addProfileForm.reset();
    dom.changePasswordForm.reset();
    dom.profilesList.replaceChildren();
    if (!dom.profiles.open) dom.profiles.showModal();

    if (state.profile.isOwner) {
      try {
        const data = await api("/api/profiles");
        renderProfileList(data.profiles || []);
      } catch (err) {
        toast(err.message);
      }
    } else {
      renderProfileList([state.profile]);
    }
  }

  function renderProfileList(profiles) {
    dom.profilesList.replaceChildren();
    for (const profile of profiles) {
      const item = el("li", "profile-list-item");
      item.append(el("span", "avatar avatar-sm", (profile.name.trim()[0] || "?").toUpperCase()));

      const text = el("span", "profile-list-text");
      text.append(el("span", "profile-list-name", profile.name));
      text.append(el("span", "profile-list-meta", profile.isOwner ? "Owner" : "Profile"));
      item.append(text);

      if (state.profile && profile.id === state.profile.id) {
        item.append(el("span", "badge", "You"));
      }
      dom.profilesList.append(item);
    }
  }

  async function submitAddProfile(event) {
    event.preventDefault();
    if (!state.profile || !state.profile.isOwner) return;

    const name = dom.newProfileName.value.trim();
    const password = dom.newProfilePassword.value;
    if (!name) {
      toast("Enter a profile name.");
      return;
    }
    if (password.length < 8) {
      toast("Use at least 8 characters for the password.");
      return;
    }
    if (password !== dom.newProfileConfirm.value) {
      toast("The passwords do not match.");
      return;
    }

    const button = dom.addProfileForm.querySelector("button[type=submit]");
    if (button) button.disabled = true;
    try {
      await api("/api/profiles", { method: "POST", body: { name, password } });
      dom.addProfileForm.reset();
      toast("Profile created. Sign in with it on another device.");
      const data = await api("/api/profiles");
      renderProfileList(data.profiles || []);
    } catch (err) {
      toast(err.message);
    } finally {
      if (button) button.disabled = false;
    }
  }

  async function submitChangePassword(event) {
    event.preventDefault();
    if (!state.profile) return;

    const current = dom.currentPassword.value;
    const next = dom.newPassword.value;
    if (next.length < 8) {
      toast("Use at least 8 characters for the new password.");
      return;
    }
    if (next !== dom.confirmPassword.value) {
      toast("The new passwords do not match.");
      return;
    }

    const button = dom.changePasswordForm.querySelector("button[type=submit]");
    if (button) button.disabled = true;
    try {
      await api("/api/auth/password", {
        method: "POST",
        body: { currentPassword: current, newPassword: next },
      });
      dom.changePasswordForm.reset();
      toast("Password changed. Other devices must sign in again.");
    } catch (err) {
      toast(err.message);
    } finally {
      if (button) button.disabled = false;
    }
  }

  async function logout() {
    if (state.streaming && !window.confirm("A reply is still generating. Log out and stop it?")) {
      return;
    }
    if (state.abort) {
      try {
        state.abort.abort();
      } catch {
        /* ignore */
      }
    }
    try {
      await api("/api/auth/logout", { method: "POST", ignoreAuth: true });
    } catch {
      /* signing out locally is still the right outcome */
    }
    if (dom.profiles.open) dom.profiles.close();
    state.profile = null;
    try {
      await refreshAuth();
    } catch (err) {
      showAuth({ setupRequired: false, profiles: [] });
      showAuthError(err.message || "Signed out.");
    }
  }

  let authRedirecting = false;
  function handleUnauthorized() {
    if (authRedirecting) return;
    authRedirecting = true;
    if (state.abort) {
      try {
        state.abort.abort();
      } catch {
        /* ignore */
      }
    }
    state.abort = null;
    state.streaming = false;
    state.streamingMsg = null;
    if (dom.profiles.open) dom.profiles.close();

    refreshAuth()
      .catch((err) => {
        showAuth(state.authStatus || { setupRequired: false, profiles: [] });
        showAuthError(err.message || "Your session ended. Sign in again.");
      })
      .finally(() => {
        authRedirecting = false;
      });
  }

  function bindAuthEvents() {
    dom.authForm.addEventListener("submit", submitAuth);
    dom.authProfile.addEventListener("change", () => {
      setPref(PREFS.lastProfile, dom.authProfile.value);
      dom.authPassword.focus();
    });
  }

  function bindProfileEvents() {
    dom.openProfiles.addEventListener("click", openProfiles);
    dom.logoutBtn.addEventListener("click", logout);
    dom.addProfileForm.addEventListener("submit", submitAddProfile);
    dom.changePasswordForm.addEventListener("submit", submitChangePassword);

    for (const button of document.querySelectorAll("[data-close]")) {
      button.addEventListener("click", () => {
        const dialog = document.getElementById(button.dataset.close);
        if (dialog && dialog.open) dialog.close();
      });
    }
  }

  async function loadHealth() {
    try {
      const health = await api("/healthz");
      state.health = health;
      dom.version.textContent = "v" + health.version;
    } catch {
      /* the UI still works without health */
    }
  }

  async function loadModels() {
    try {
      const data = await api("/api/models");
      state.providers = Array.isArray(data.providers) ? data.providers : [];
    } catch (err) {
      state.providers = [];
      if (err.status !== 401) toast("Could not load models: " + err.message);
    }

    const provider = providerInfo() || state.providers[0] || null;
    if (provider) {
      state.provider = provider.id;
      if (!state.model && provider.defaultModel) state.model = provider.defaultModel;
      if (!state.model && provider.models && provider.models.length) {
        state.model = provider.models[0].id;
      }
    }

    updateModelTrigger();
    updateRoutingTrigger();
    updateNotice();
    updateHeading();
  }

  function providerInfo() {
    return state.providers.find((p) => p.id === state.provider) || null;
  }

  function updateNotice() {
    const provider = providerInfo();
    state.providerReady = Boolean(provider && provider.enabled);

    let message = "";
    if (!provider) {
      message = "No provider is configured on the server.";
    } else if (!provider.enabled) {
      message = (provider.error || "OpenRouter is not configured on the server.") +
        " Add OPENROUTER_API_KEY to .env and restart Clachan.";
    } else if (!provider.models || provider.models.length === 0) {
      message = "The model list could not be loaded" +
        (provider.error ? " (" + provider.error + ")" : "") +
        ". Check the server log and reload.";
    }

    dom.notice.hidden = !message;
    dom.notice.textContent = message;
    dom.send.disabled = state.streaming || !state.providerReady;
    dom.modelPicker.disabled = !state.providerReady;
    dom.routingPicker.disabled = !state.providerReady || !state.model;
  }

  function assistantName() {
    return (state.profile && state.profile.personaName) || "Clachan";
  }

  function updateHeading() {
    dom.chatTitle.textContent = (state.current && state.current.title) || "New chat";
    const bits = [];
    if (state.model) bits.push(state.model);
    const routing = routingLabel();
    if (routing && routing !== "Balanced") bits.push(routing);
    dom.chatSub.textContent = bits.join(" · ") || "No model selected";

    const title = state.current && state.current.title;
    document.title = title && title !== "New chat" ? title + " · Clachan" : "Clachan";
    dom.input.placeholder = "Message " + assistantName() + "…";
    const messages = (state.current && state.current.messages) || [];
    dom.exportChat.disabled = messages.length === 0;
  }

  function selectedModel() {
    const provider = providerInfo();
    return ((provider && provider.models) || []).find((model) => model.id === state.model) || null;
  }

  function ignoredProviderSet() {
    const raw = (state.profile && state.profile.ignoredProviders) || "";
    return raw.split(",").map((value) => value.trim().toLowerCase()).filter(Boolean);
  }

  function isIgnoredTag(tag) {
    const value = String(tag || "").toLowerCase();
    if (!value) return false;
    return ignoredProviderSet().some((want) => value === want || value.startsWith(want + "/"));
  }

  function routingLabel() {
    if (state.providerOnly) return state.providerOnly;
    switch (state.providerSort) {
      case "price": return "Cheapest";
      case "throughput": return "Fastest";
      case "latency": return "Low latency";
      default: return "Balanced";
    }
  }

  function updateModelTrigger() {
    const model = selectedModel();
    dom.modelTriggerName.textContent = model ? model.name : (state.model || "Choose a model");
    dom.modelTriggerMeta.textContent = model
      ? [formatContext(model.contextLength), formatPricePair(model.promptPricePerM, model.completionPricePerM)]
          .filter(Boolean)
          .join(" · ")
      : "";
    dom.modelTriggerBadges.replaceChildren();
    if (model) {
      if (model.supportsVision) dom.modelTriggerBadges.append(makeTag("Vision", "vision"));
      if (model.supportsTools) dom.modelTriggerBadges.append(makeTag("Tools", "tools"));
    }
    dom.modelPicker.title = model ? model.id : state.model || "Choose a model";
    updateReasoningControl();
  }

  function updateRoutingTrigger() {
    const label = routingLabel();
    dom.routingTriggerLabel.textContent = label;
    dom.routingPicker.classList.toggle("active", Boolean(state.providerOnly || state.providerSort));
    dom.routingPicker.title = state.providerOnly
      ? "Pinned endpoint: " + state.providerOnly
      : "Provider routing: " + label;
    dom.routingPicker.disabled = !state.providerReady || !state.model;
  }

  function formatContext(length) {
    if (!length) return "";
    if (length >= 1_000_000) {
      const millions = length / 1_000_000;
      const text = millions >= 10 ? millions.toFixed(0) : millions.toFixed(1).replace(/\.0$/, "");
      return text + "M ctx";
    }
    if (length >= 1000) return Math.round(length / 1000) + "K ctx";
    return length + " ctx";
  }

  function money(value) {
    if (!value) return "0";
    if (value >= 100) return value.toFixed(0);
    if (value >= 10) return value.toFixed(1).replace(/\.0$/, "");
    if (value >= 1) return value.toFixed(2).replace(/\.?0+$/, "");
    return value.toFixed(3).replace(/0+$/, "").replace(/\.$/, "");
  }

  function formatPricePair(prompt, completion) {
    if (!prompt && !completion) return "Free";
    return "$" + money(prompt) + " / $" + money(completion) + " /M";
  }

  function makeTag(text, kind) {
    return el("span", "tag" + (kind ? " tag-" + kind : ""), text);
  }

  function modelTags(model) {
    const tags = [];
    if (model.supportsVision) tags.push(makeTag("Vision", "vision"));
    if (model.supportsFiles) tags.push(makeTag("Files", "file"));
    if (model.supportsTools) tags.push(makeTag("Tools", "tools"));
    if (model.supportsReasoning) tags.push(makeTag("Reasoning", "reason"));
    if (model.free) tags.push(makeTag("Free", "free"));
    return tags;
  }

  function setActiveFilter() {
    for (const button of dom.modelFilters.querySelectorAll("[data-filter]")) {
      button.classList.toggle("active", button.dataset.filter === state.modelFilter);
    }
  }

  function openModelPicker(target = "conversation") {
    const provider = providerInfo();
    if (!provider || !provider.enabled) {
      toast("OpenRouter is not configured on the server.");
      return;
    }
    if (!provider.models || provider.models.length === 0) {
      toast("The model list is unavailable right now.");
      return;
    }

    state.modelPickerTarget = target === "default" ? "default" : "conversation";
    dom.modelDialogSub.textContent = state.modelPickerTarget === "default"
      ? "Default for new chats"
      : provider.label + " · " + provider.models.length + " models";
    dom.modelRoutingButton.hidden = state.modelPickerTarget === "default";
    dom.modelSearch.value = "";
    dom.modelSort.value = state.modelSort;
    setActiveFilter();
    renderModelList();
    if (!dom.modelDialog.open) dom.modelDialog.showModal();
    dom.modelSearch.focus();
  }

  function pickerModelID() {
    if (state.modelPickerTarget === "default") {
      return (state.profile && state.profile.defaultModel) || "";
    }
    return state.model;
  }

  function filteredModels() {
    const provider = providerInfo();
    const query = dom.modelSearch.value.trim().toLowerCase();
    let items = ((provider && provider.models) || []).map((model, index) => ({ model, index }));

    if (query) {
      items = items.filter(({ model }) =>
        (model.name + " " + model.id + " " + (model.canonicalSlug || "")).toLowerCase().includes(query));
    }
    if (state.modelFilter === "vision") items = items.filter(({ model }) => model.supportsVision);
    if (state.modelFilter === "tools") items = items.filter(({ model }) => model.supportsTools);
    if (state.modelFilter === "reasoning") items = items.filter(({ model }) => model.supportsReasoning);

    const price = (model) => (model.promptPricePerM || 0) + (model.completionPricePerM || 0);
    switch (state.modelSort) {
      case "name-asc":
        items.sort((a, b) => a.model.name.localeCompare(b.model.name));
        break;
      case "name-desc":
        items.sort((a, b) => b.model.name.localeCompare(a.model.name));
        break;
      case "price-asc":
        items.sort((a, b) => price(a.model) - price(b.model));
        break;
      case "price-desc":
        items.sort((a, b) => price(b.model) - price(a.model));
        break;
      case "input-asc":
        items.sort((a, b) => (a.model.promptPricePerM || 0) - (b.model.promptPricePerM || 0));
        break;
      case "output-asc":
        items.sort((a, b) => (a.model.completionPricePerM || 0) - (b.model.completionPricePerM || 0));
        break;
      case "context-desc":
        items.sort((a, b) => (b.model.contextLength || 0) - (a.model.contextLength || 0));
        break;
      case "context-asc":
        items.sort((a, b) => (a.model.contextLength || 0) - (b.model.contextLength || 0));
        break;
      case "newest":
        items.sort((a, b) => (b.model.created || 0) - (a.model.created || 0));
        break;
      case "oldest":
        items.sort((a, b) => (a.model.created || 0) - (b.model.created || 0));
        break;
      default:
        items.sort((a, b) => a.index - b.index);
    }
    return items.map((item) => item.model);
  }

  function renderModelList() {
    const models = filteredModels();
    const selectedID = pickerModelID();
    dom.modelListEl.replaceChildren();

    if (!models.length) {
      dom.modelListEl.append(el("p", "picker-empty", "No models match that search."));
      dom.modelCount.textContent = "0 models";
      return;
    }

    for (const model of models) {
      const row = el("button", "model-row");
      row.type = "button";
      row.dataset.id = model.id;
      row.setAttribute("role", "option");
      row.setAttribute("aria-selected", model.id === selectedID ? "true" : "false");
      if (model.id === selectedID) row.classList.add("selected");

      const main = el("span", "model-main");
      main.append(el("span", "model-name", model.name));
      main.append(el("span", "model-id", model.id));
      const tags = el("span", "model-tags");
      const tagNodes = modelTags(model);
      for (const tag of tagNodes) tags.append(tag);
      if (tagNodes.length) main.append(tags);

      const meta = el("span", "model-meta");
      meta.append(el("span", "model-price", formatPricePair(model.promptPricePerM, model.completionPricePerM)));
      if (model.contextLength) meta.append(el("span", "model-context", formatContext(model.contextLength)));
      row.append(main, meta);

      if (model.id === selectedID) row.append(el("span", "model-check", "✓"));
      row.addEventListener("click", () => chooseModel(model));
      row.addEventListener("keydown", moveModelFocus);
      dom.modelListEl.append(row);
    }

    dom.modelCount.textContent = models.length + " model" + (models.length === 1 ? "" : "s");
    const selected = dom.modelListEl.querySelector(".model-row.selected");
    if (selected) requestAnimationFrame(() => selected.scrollIntoView({ block: "nearest" }));
  }

  function moveModelFocus(event) {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    const rows = Array.from(dom.modelListEl.querySelectorAll(".model-row"));
    const index = rows.indexOf(event.currentTarget);
    if (event.key === "ArrowDown") {
      if (rows[index + 1]) rows[index + 1].focus();
    } else if (index > 0) {
      rows[index - 1].focus();
    } else {
      dom.modelSearch.focus();
    }
  }

  async function chooseModel(model) {
    if (state.modelPickerTarget === "default") {
      const saved = await setDefaultModel(model.id);
      if (saved && dom.modelDialog.open) dom.modelDialog.close();
      return;
    }

    const changed = state.model !== model.id;
    state.model = model.id;
    setPref(pkey(PREFS.model), state.model);

    // A pinned endpoint belongs to one model; keep sorts, drop a stale pin.
    if (changed && state.providerOnly) {
      state.providerOnly = "";
      state.providerSort = "";
      state.allowFallbacks = true;
    }

    if (dom.modelDialog.open) dom.modelDialog.close();
    updateModelTrigger();
    updateRoutingTrigger();
    updateHeading();

    persistConversationSettings({
      model: state.model,
      providerSort: state.providerSort,
      providerOnly: state.providerOnly,
      allowFallbacks: state.allowFallbacks,
    });
  }

  function openRoutingDialog() {
    if (!state.model) {
      toast("Choose a model first.");
      return;
    }
    const model = selectedModel();
    state.routingDraft = {
      mode: state.providerOnly ? "specific" : (state.providerSort || "balanced"),
      only: state.providerOnly,
      allowFallbacks: state.allowFallbacks,
    };
    state.endpoints = [];
    state.endpointsModel = "";
    state.endpointSort = "recommended";
    dom.endpointSort.value = "recommended";
    dom.allowFallbacks.checked = state.allowFallbacks;
    dom.routingDialogSub.textContent = model ? model.name : state.model;
    dom.endpointSection.hidden = state.routingDraft.mode !== "specific";
    dom.endpointList.replaceChildren();
    setRoutingSelection();
    if (!dom.routingDialog.open) dom.routingDialog.showModal();
    if (state.routingDraft.mode === "specific") loadEndpoints();
  }

  function setRoutingSelection() {
    if (!state.routingDraft) return;
    for (const button of dom.routingOptions.querySelectorAll(".route-option")) {
      const active = button.dataset.mode === state.routingDraft.mode;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    }
  }

  function selectRoutingMode(mode) {
    if (!state.routingDraft) return;
    state.routingDraft.mode = mode;
    dom.endpointSection.hidden = mode !== "specific";
    setRoutingSelection();
    if (mode === "specific") loadEndpoints();
  }

  async function loadEndpoints() {
    if (!state.model) return;
    if (state.endpointsModel === state.model && state.endpoints.length) {
      renderEndpointList();
      return;
    }
    if (state.endpointsLoading) return;

    state.endpointsLoading = true;
    dom.endpointList.replaceChildren(el("p", "picker-empty", "Loading providers…"));
    try {
      const data = await api("/api/model-endpoints?model=" + encodeURIComponent(state.model));
      state.endpoints = Array.isArray(data.endpoints) ? data.endpoints : [];
      state.endpointsModel = state.model;
      renderEndpointList();
    } catch (err) {
      state.endpoints = [];
      dom.endpointList.replaceChildren(el("p", "picker-empty", "Could not load providers: " + err.message));
    } finally {
      state.endpointsLoading = false;
    }
  }

  function sortedEndpoints() {
    const available = state.endpoints.filter((endpoint) => endpoint.available);
    const items = available.map((endpoint, index) => ({ endpoint, index }));
    const price = (endpoint) => (endpoint.promptPricePerM || 0) + (endpoint.completionPricePerM || 0);

    switch (state.endpointSort) {
      case "price-asc":
        items.sort((a, b) => price(a.endpoint) - price(b.endpoint));
        break;
      case "price-desc":
        items.sort((a, b) => price(b.endpoint) - price(a.endpoint));
        break;
      case "throughput-desc":
        items.sort((a, b) => (b.endpoint.throughput || 0) - (a.endpoint.throughput || 0));
        break;
      case "throughput-asc":
        items.sort((a, b) => (a.endpoint.throughput || 0) - (b.endpoint.throughput || 0));
        break;
      case "latency-asc":
        items.sort((a, b) => (a.endpoint.latencyMs || Infinity) - (b.endpoint.latencyMs || Infinity));
        break;
      case "latency-desc":
        items.sort((a, b) => (b.endpoint.latencyMs || 0) - (a.endpoint.latencyMs || 0));
        break;
      case "context-desc":
        items.sort((a, b) => (b.endpoint.contextLength || 0) - (a.endpoint.contextLength || 0));
        break;
      case "context-asc":
        items.sort((a, b) => (a.endpoint.contextLength || 0) - (b.endpoint.contextLength || 0));
        break;
      case "name-asc":
        items.sort((a, b) => (a.endpoint.providerName || a.endpoint.name).localeCompare(b.endpoint.providerName || b.endpoint.name));
        break;
      case "name-desc":
        items.sort((a, b) => (b.endpoint.providerName || b.endpoint.name).localeCompare(a.endpoint.providerName || a.endpoint.name));
        break;
      default:
        items.sort((a, b) => a.index - b.index);
    }
    return items.map((item) => item.endpoint);
  }

  function renderEndpointList() {
    const endpoints = sortedEndpoints();
    dom.endpointList.replaceChildren();

    if (!endpoints.length) {
      dom.endpointList.append(el("p", "picker-empty", "No available endpoints for this model."));
      return;
    }

    const hasSuperiorQuant = endpoints.some((endpoint) => quantRank(endpoint.quantization) === 2);

    for (const endpoint of endpoints) {
      const row = el("button", "endpoint-row");
      row.type = "button";
      row.setAttribute("role", "option");
      const selected = state.routingDraft && state.routingDraft.only === endpoint.tag;
      row.setAttribute("aria-selected", selected ? "true" : "false");
      if (selected) row.classList.add("selected");

      const main = el("span", "endpoint-main");
      main.append(el("span", "endpoint-name", endpoint.providerName || endpoint.name));
      main.append(el("span", "endpoint-tag", endpoint.tag));

      const tags = el("span", "endpoint-tags");
      if (endpoint.contextLength) {
        tags.append(makeTag(formatContext(endpoint.contextLength).replace(" ctx", ""), ""));
      }
      if (endpoint.throughput) {
        addStat(tags, Math.round(endpoint.throughput) + " tok/s",
          statClass(endpoint.throughput, 100, 30, true),
          "p50 output tokens/sec over the last 30 minutes");
      }
      if (endpoint.latencyMs) {
        addStat(tags, formatLatency(endpoint.latencyMs),
          statClass(endpoint.latencyMs, 400, 1200, false),
          "p50 time to first token over the last 30 minutes");
      }
      if (endpoint.uptime) {
        let uptimeTitle = "Uptime over the last 24 hours";
        if (endpoint.uptime30m) {
          uptimeTitle += " (30 minutes: " + formatUptime(endpoint.uptime30m) + ")";
        }
        addStat(tags, formatUptime(endpoint.uptime),
          statClass(endpoint.uptime, 99.0, 95.0, true),
          uptimeTitle);
      }
      if (endpoint.quantization) {
        addStat(tags, endpoint.quantization,
          quantClass(endpoint.quantization, hasSuperiorQuant),
          "Quantization");
      }
      if (endpoint.serviceTier) tags.append(makeTag(endpoint.serviceTier, ""));
      if (endpoint.nativeWebSearch) tags.append(makeTag("Web search", "web"));
      if (endpoint.supportsTools) tags.append(makeTag("Tools", "tools"));
      if (endpoint.supportsCaching) tags.append(makeTag("Cache", "cache"));
      if (tags.childNodes.length) main.append(tags);

      const meta = el("span", "model-meta");
      meta.append(el("span", "model-price", formatPricePair(endpoint.promptPricePerM, endpoint.completionPricePerM)));
      row.append(main, meta);

      if (selected) row.append(el("span", "model-check", "✓"));
      row.addEventListener("click", () => {
        if (!state.routingDraft) return;
        state.routingDraft.only = endpoint.tag;
        renderEndpointList();
      });
      dom.endpointList.append(row);
    }
  }

  function formatLatency(ms) {
    if (ms >= 1000) return (ms / 1000).toFixed(2) + "s";
    return Math.round(ms) + "ms";
  }

  function formatUptime(value) {
    if (value >= 100) return "100.00% up";
    return value.toFixed(2) + "% up";
  }

  function statClass(value, good, warn, higherIsBetter) {
    if (higherIsBetter) {
      return value >= good ? "tag-good" : value >= warn ? "tag-warn" : "tag-bad";
    }
    return value <= good ? "tag-good" : value <= warn ? "tag-warn" : "tag-bad";
  }

  function quantRank(value) {
    const quant = String(value || "").toLowerCase();
    if (quant === "fp32" || quant === "fp16" || quant === "bf16") return 2;
    if (quant === "fp8" || quant === "mxfp8") return 1;
    return 0;
  }

  function quantClass(value, hasSuperior) {
    const rank = quantRank(value);
    if (hasSuperior) {
      return rank === 2 ? "tag-good" : rank === 1 ? "tag-warn" : "tag-bad";
    }
    return rank === 1 ? "tag-good" : "tag-warn";
  }

  function addStat(container, text, className, title) {
    const tag = makeTag(text, "");
    if (className) tag.classList.add(className);
    if (title) tag.title = title;
    container.append(tag);
  }

  async function saveRouting() {
    const draft = state.routingDraft;
    if (!draft) return;
    if (state.streaming) {
      toast("Stop the current reply first.");
      return;
    }

    let sort = "";
    let only = "";
    if (draft.mode === "specific") {
      if (!draft.only) {
        toast("Choose an endpoint first.");
        return;
      }
      only = draft.only;
    } else if (draft.mode !== "balanced") {
      sort = draft.mode;
    }

    const patch = {
      providerSort: sort,
      providerOnly: only,
      allowFallbacks: draft.allowFallbacks,
    };
    if (state.current && state.current.id) {
      const saved = await persistConversationSettings(patch);
      if (!saved) return;
    }

    state.providerSort = sort;
    state.providerOnly = only;
    state.allowFallbacks = draft.allowFallbacks;

    if (dom.routingDialog.open) dom.routingDialog.close();
    updateRoutingTrigger();
    updateHeading();
  }

  /* ---------------------------------------------------------- conversations */

  async function loadConversations(quiet = false) {
    try {
      const data = await api("/api/conversations");
      state.conversations = Array.isArray(data.conversations) ? data.conversations : [];
      renderConversations();
    } catch (err) {
      if (!quiet) toast(err.message);
    }
  }

  function renderConversations() {
    dom.convList.replaceChildren();

    if (!state.conversations.length) {
      dom.convList.append(el("p", "conv-empty", "No conversations yet."));
      return;
    }

    for (const conversation of state.conversations) {
      const item = el("div", "conv-item");
      item.dataset.id = conversation.id;
      if (state.current && state.current.id === conversation.id) {
        item.classList.add("active");
      }

      const open = el("button", "conv-open");
      open.type = "button";
      open.append(
        el("span", "conv-title", conversation.title || "Untitled"),
        el("span", "conv-preview", conversation.preview || "No messages yet")
      );
      open.addEventListener("click", () => selectConversation(conversation.id));

      const actions = el("div", "conv-actions");
      const rename = iconButton("i-edit", "Rename conversation");
      rename.addEventListener("click", (event) => {
        event.stopPropagation();
        beginRename(item, conversation);
      });
      const remove = iconButton("i-trash", "Delete conversation");
      remove.addEventListener("click", (event) => {
        event.stopPropagation();
        deleteConversation(conversation);
      });
      actions.append(rename, remove);

      item.append(open, actions);
      dom.convList.append(item);
    }
  }

  async function selectConversation(id, fromBoot = false) {
    if (state.streaming) {
      if (!fromBoot) toast("Stop the current reply first.");
      return;
    }

    try {
      const conversation = await api("/api/conversations/" + encodeURIComponent(id));
      state.current = {
        ...conversation,
        messages: Array.isArray(conversation.messages) ? conversation.messages : [],
      };
      state.provider = conversation.provider || state.provider;
      state.model = conversation.model || state.model;
      state.providerSort = conversation.providerSort || "";
      state.providerOnly = conversation.providerOnly || "";
      state.allowFallbacks = conversation.allowFallbacks !== false;
      state.toolsEnabled = conversation.toolsEnabled !== false;
      state.maxToolCalls = clampToolCalls(conversation.maxToolCalls || 6);
      state.reasoningEffort = typeof conversation.reasoningEffort === "string" ? conversation.reasoningEffort : "";
      setPref(pkey(PREFS.toolsEnabled), state.toolsEnabled ? "1" : "0");
      setPref(pkey(PREFS.maxToolCalls), String(state.maxToolCalls));
      setPref(pkey(PREFS.reasoningEffort), state.reasoningEffort);
      if (state.providerOnly && isIgnoredTag(state.providerOnly)) {
        state.providerSort = "";
        state.providerOnly = "";
        state.allowFallbacks = true;
        persistConversationSettings({ providerSort: "", providerOnly: "", allowFallbacks: true });
      }
      state.pinned = true;
      state.autoRetried = false;
      clearRetry();
      clearAttachments();

      setPref(pkey(PREFS.lastConversation), id);
      if (conversation.model) setPref(pkey(PREFS.model), conversation.model);

      updateModelTrigger();
      updateRoutingTrigger();
      updateNotice();
      updateHeading();
      renderToolBar();
      renderMessages({ stick: true });
      renderConversations();
      closeSidebar();

      if (!fromBoot) dom.input.focus();
    } catch (err) {
      toast(err.message);
    }
  }

  function newChat(options = {}) {
    if (state.streaming) {
      toast("Stop the current reply first.");
      return;
    }
    clearRetry();
    state.autoRetried = false;
    clearAttachments();
    let draftModel = state.model;
    if (state.profile && state.profile.defaultModel) {
      draftModel = state.profile.defaultModel;
    }
    if (draftModel !== state.model && state.providerOnly) {
      state.providerOnly = "";
      state.allowFallbacks = true;
    }
    state.model = draftModel;
    state.current = {
      id: null,
      title: "New chat",
      provider: state.provider,
      model: draftModel,
      providerSort: state.providerSort,
      providerOnly: state.providerOnly,
      allowFallbacks: state.allowFallbacks,
      toolsEnabled: state.toolsEnabled,
      maxToolCalls: state.maxToolCalls,
      reasoningEffort: state.reasoningEffort,
      systemPrompt: (state.profile && state.profile.personaPrompt) || "",
      messages: [],
    };
    state.pinned = true;
    removePref(pkey(PREFS.lastConversation));

    updateHeading();
    updateNotice();
    updateModelTrigger();
    updateRoutingTrigger();
    renderToolBar();
    renderMessages({ stick: true });
    renderConversations();
    closeSidebar();
    if (options.focus !== false) dom.input.focus();
  }

  async function ensureConversation() {
    if (state.current && state.current.id) return state.current;
    if (!state.current) newChat({ focus: false });

    // Omit an empty system prompt so the server can fall back to the profile
    // persona and DEFAULT_SYSTEM_PROMPT. Sending "" would override both.
    const request = {
      provider: state.provider,
      model: state.model,
      providerSort: state.providerSort,
      providerOnly: state.providerOnly,
      allowFallbacks: state.allowFallbacks,
      toolsEnabled: state.toolsEnabled,
      maxToolCalls: state.maxToolCalls,
      reasoningEffort: activeReasoningEffort(),
    };
    const systemPrompt = String(state.current.systemPrompt || "").trim();
    if (systemPrompt) request.systemPrompt = systemPrompt;

    const created = await api("/api/conversations", { method: "POST", body: request });

    state.current = {
      ...created,
      messages: (state.current && state.current.messages) || [],
    };
    setPref(pkey(PREFS.lastConversation), created.id);
    updateHeading();
    await loadConversations(true);
    renderConversations();
    return state.current;
  }

  async function deleteConversation(conversation) {
    if (state.streaming) {
      toast("Stop the current reply first.");
      return;
    }
    if (!window.confirm(`Delete "${conversation.title}"? This cannot be undone.`)) return;

    try {
      await api("/api/conversations/" + encodeURIComponent(conversation.id), { method: "DELETE" });
    } catch (err) {
      toast(err.message);
      return;
    }

    if (state.current && state.current.id === conversation.id) {
      newChat({ focus: false });
    }
    await loadConversations(true);
  }

  function beginRename(item, conversation) {
    const titleNode = item.querySelector(".conv-title");
    if (!titleNode) return;

    const input = el("input", "conv-rename");
    input.value = conversation.title || "";
    input.maxLength = 200;
    titleNode.replaceWith(input);
    input.focus();
    input.select();

    let finished = false;
    const finish = async (save) => {
      if (finished) return;
      finished = true;

      const title = input.value.trim();
      if (!save || !title || title === conversation.title) {
        renderConversations();
        return;
      }

      try {
        const updated = await api("/api/conversations/" + encodeURIComponent(conversation.id), {
          method: "PATCH",
          body: { title },
        });
        conversation.title = updated.title;
        if (state.current && state.current.id === conversation.id) {
          state.current.title = updated.title;
          updateHeading();
        }
      } catch (err) {
        toast(err.message);
      }
      renderConversations();
    };

    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        finish(true);
      } else if (event.key === "Escape") {
        event.preventDefault();
        finish(false);
      }
    });
    input.addEventListener("blur", () => finish(true));
  }

  async function persistConversationSettings(patch) {
    if (!state.current || !state.current.id) return true;
    if (state.streaming) return false;
    try {
      const updated = await api("/api/conversations/" + encodeURIComponent(state.current.id), {
        method: "PATCH",
        body: patch,
      });
      state.current.provider = updated.provider;
      state.current.model = updated.model;
      state.current.providerSort = updated.providerSort || "";
      state.current.providerOnly = updated.providerOnly || "";
      state.current.allowFallbacks = updated.allowFallbacks !== false;
      state.current.toolsEnabled = updated.toolsEnabled !== false;
      state.current.maxToolCalls = clampToolCalls(updated.maxToolCalls || 6);
      state.current.reasoningEffort = typeof updated.reasoningEffort === "string"
        ? updated.reasoningEffort
        : "";
      return true;
    } catch (err) {
      toast(err.message);
      return false;
    }
  }

  /* --------------------------------------------------------------- messages */

  function renderMessages(options = {}) {
    clearTimeout(paintTimer);
    paintTimer = null;

    const keepPinned = state.pinned;
    dom.messages.replaceChildren();

    const messages = (state.current && state.current.messages) || [];
    if (!messages.length) {
      dom.messages.append(emptyState());
      return;
    }

    for (const message of messages) {
      dom.messages.append(messageNode(message));
    }

    if (options.stick || keepPinned) scrollToBottom(true);
  }

  function emptyState() {
    const wrap = el("div", "empty");
    wrap.append(el("div", "empty-mark", "C"));
    wrap.append(el("h2", null, "No messages yet"));
    wrap.append(el("p", null, "Ask " + assistantName() + " anything. Conversations stay on this machine."));
    return wrap;
  }

  function attachmentBlob(attachment) {
    const comma = attachment.data.indexOf(",");
    const header = comma >= 0 ? attachment.data.slice(0, comma) : "";
    const match = header.match(/^data:([^;,]+)(?:;[^,]*)?;base64$/i);
    const mime = (match && match[1]) || attachment.mime || "application/octet-stream";
    const binary = atob(comma >= 0 ? attachment.data.slice(comma + 1) : "");
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return new Blob([bytes], { type: mime });
  }

  function openAttachment(attachment) {
    try {
      const url = URL.createObjectURL(attachmentBlob(attachment));
      window.open(url, "_blank", "noopener,noreferrer");
      setTimeout(() => URL.revokeObjectURL(url), 120000);
    } catch {
      toast("Could not open that attachment.");
    }
  }

  function attachmentGallery(attachments) {
    const wrap = el("div", "msg-attachments");
    for (const attachment of attachments || []) {
      if (attachment.kind === "image") {
        const image = el("img", "msg-image");
        image.src = attachment.data;
        image.alt = attachment.name || "Attached image";
        image.loading = "lazy";
        image.title = "Open image";
        image.addEventListener("click", () => openAttachment(attachment));
        image.addEventListener("load", () => scrollToBottom(false));
        wrap.append(image);
      } else {
        const card = el("button", "msg-file");
        card.type = "button";
        card.title = "Open " + (attachment.name || "file");
        card.append(el("span", "msg-file-kind", attachmentKindLabel(attachment)));
        card.append(el("span", "msg-file-name", attachment.name || "file"));
        card.append(el("span", "msg-file-open", "Open"));
        card.addEventListener("click", () => openAttachment(attachment));
        wrap.append(card);
      }
    }
    return wrap;
  }

  function attachmentKindLabel(attachment) {
    if (attachment.kind === "pdf") return "PDF";
    if (attachment.kind === "text") {
      const ext = String(attachment.name || "").split(".").pop().toUpperCase();
      return ext && ext.length <= 4 ? ext : "TXT";
    }
    return "IMG";
  }

  function citationDomain(raw) {
    try {
      return new URL(raw).hostname.replace(/^www\./, "");
    } catch {
      return "";
    }
  }

  function citationBlock(citations) {
    const box = el("div", "citations");
    box.append(el("div", "citations-title", "Sources"));
    const list = el("div", "citation-list");
    for (const citation of citations) {
      const href = safeHref(citation.url);
      if (!href) continue;
      const link = el("a", "citation");
      link.href = href;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      const domain = citationDomain(href);
      const title = citation.title || domain || href;
      link.append(el("span", "citation-title", title));
      if (domain && domain !== title) link.append(el("span", "citation-domain", domain));
      list.append(link);
    }
    box.append(list);
    return box;
  }

  const TOOL_LABELS = {
    web_search: "Web search",
    web_fetch: "Web fetch",
    datetime: "Date and time",
    search_models: "Model search",
    image_generation: "Image generation",
  };

  function toolLabel(name) {
    const key = String(name || "").replace(/^openrouter:/, "").replace(/^experimental__/, "");
    return TOOL_LABELS[key] || key || "Tool";
  }

  function toolLog(run) {
    const details = el("details", "tool-log");
    const summary = el("summary");
    summary.append(el("span", "tool-log-title", "Tools"));
    const bits = [];
    if (run.calls > 0) bits.push(run.calls + " call" + (run.calls === 1 ? "" : "s"));
    if (run.searches > 0) bits.push(run.searches + " search" + (run.searches === 1 ? "" : "es"));
    if (!bits.length) bits.push(run.enabled ? "web on" : "web off");
    summary.append(el("span", "tool-log-count", bits.join(" · ")));
    details.append(summary);

    const list = el("div", "tool-list");
    for (const activity of run.activities || []) {
      const row = el("div", "tool-row");
      row.append(el("span", "tool-row-name", toolLabel(activity.name)));
      if (activity.detail) row.append(el("span", "tool-row-detail", activity.detail));
      if (activity.status === "failed") {
        row.append(el("span", "tool-row-status", "failed"));
      }
      list.append(row);
    }
    if (!list.childNodes.length) {
      const row = el("div", "tool-row");
      row.append(el("span", "tool-row-name", "Server tools"));
      row.append(el("span", "tool-row-detail", (run.calls || 0) + " step" + ((run.calls || 0) === 1 ? "" : "s") + " executed"));
      list.append(row);
    }
    details.append(list);
    return details;
  }

  function reasoningBlock(text, hint) {
    const value = String(text || "").trim();
    if (!value) return null;
    const details = el("details", "reasoning-block");
    const summary = el("summary");
    summary.append(el("span", "reasoning-title", "Reasoning"));
    summary.append(el("span", "reasoning-hint", hint || "Show thoughts"));
    details.append(summary);
    const body = el("div", "reasoning-text");
    body.textContent = value;
    details.append(body);
    return details;
  }

  function formatDuration(ms) {
    if (!Number.isFinite(ms) || ms <= 0) return "";
    if (ms < 1000) return ms + " ms";
    return (ms / 1000).toFixed(1).replace(/\.0$/, "") + " s";
  }

  function stepCard(step, position) {
    const card = el("section", "step-card");
    if (step.status && step.status !== "completed") card.classList.add("step-unfinished");

    const head = el("div", "step-head");
    head.append(el("span", "step-num", String(position)));
    head.append(el("span", "step-name", toolLabel(step.tool)));
    if (step.query) head.append(el("span", "step-query", step.query));
    if (step.status && step.status !== "completed") {
      head.append(el("span", "step-status", step.status));
    }
    const duration = formatDuration(step.durationMs);
    if (duration) head.append(el("span", "step-duration", duration));
    card.append(head);

    const reasoning = reasoningBlock(step.reasoning);
    if (reasoning) card.append(reasoning);

    if (step.arguments) {
      const args = el("details", "step-block");
      const summary = el("summary", null, "Arguments");
      args.append(summary);
      const pre = el("pre", "step-code");
      pre.textContent = step.arguments;
      args.append(pre);
      card.append(args);
    }

    if (step.error) {
      const error = el("div", "step-error");
      error.textContent = step.error;
      card.append(error);
    } else if (step.result) {
      const result = el("details", "step-block step-result-block");
      result.append(el("summary", null, "Result"));
      const pre = el("pre", "step-code step-result");
      pre.textContent = step.result;
      result.append(pre);
      card.append(result);
    }

    if (Array.isArray(step.sources) && step.sources.length) {
      card.append(citationBlock(step.sources));
    }
    return card;
  }

  function answerCard(message, position) {
    const card = el("section", "step-card step-answer");
    const head = el("div", "step-head");
    head.append(el("span", "step-num", String(position)));
    head.append(el("span", "step-name", "Answer"));
    if (message.streaming && message === state.streamingMsg) {
      head.append(el("span", "step-status", "writing"));
    }
    card.append(head);

    const reasoning = reasoningBlock(message.reasoning);
    if (reasoning) card.append(reasoning);

    const body = el("div", "md");
    body.replaceChildren(renderMarkdown(message.content));
    if (message.streaming && message === state.streamingMsg) body.append(el("span", "caret"));
    card.append(body);

    if (Array.isArray(message.citations) && message.citations.length) {
      card.append(citationBlock(message.citations));
    }
    return card;
  }

  function assistantExtras(message) {
    const hasCitations = Array.isArray(message.citations) && message.citations.length > 0;
    const run = message.toolRun;
    const hasRun = run && ((run.calls || 0) > 0 || (run.searches || 0) > 0 ||
      (Array.isArray(run.activities) && run.activities.length > 0));
    if (!hasCitations && !hasRun) return null;

    const wrap = el("div", "assistant-extras");
    if (hasCitations) wrap.append(citationBlock(message.citations));
    if (hasRun) wrap.append(toolLog(run));
    return wrap;
  }

  function messageNode(message) {
    const article = el("article", "msg msg-" + message.role);
    if (message.id) article.dataset.id = message.id;

    if (message.role === "user") {
      const bubble = el("div", "bubble");
      if (message.attachments && message.attachments.length) {
        bubble.append(attachmentGallery(message.attachments));
      }
      if (message.content) bubble.append(el("div", "bubble-text", message.content));
      article.append(bubble);
    } else {
      const persona = (state.profile && state.profile.personaName) || "";
      if (persona) {
        const author = el("div", "msg-author");
        const initial = (persona.trim()[0] || "C").toUpperCase();
        author.append(el("span", "avatar avatar-xs", initial));
        author.append(el("span", "msg-author-name", persona));
        article.append(author);
      }

      const steps = Array.isArray(message.steps) ? message.steps : [];
      if (steps.length) {
        const list = el("div", "step-list");
        steps.forEach((step, index) => list.append(stepCard(step, index + 1)));
        if (message.content || message.reasoning || (message.streaming && message === state.streamingMsg)) {
          list.append(answerCard(message, steps.length + 1));
        }
        article.append(list);
        if (!message.content && !message.reasoning) {
          const extras = assistantExtras(message);
          if (extras) article.append(extras);
        }
      } else {
        const reasoning = reasoningBlock(message.reasoning);
        if (reasoning) article.append(reasoning);
        const body = el("div", "md");
        body.replaceChildren(renderMarkdown(message.content));
        if (message.streaming && message === state.streamingMsg) {
          body.append(el("span", "caret"));
        }
        article.append(body);
        const extras = assistantExtras(message);
        if (extras) article.append(extras);
      }
    }

    if (message.failed) article.append(failedNote(message));
    if (message.error) article.append(errorNote(message));
    if (message.stopped) article.append(el("p", "msg-note", "Stopped."));
    const limit = finishNote(message);
    if (limit) article.append(limit);
    if (!message.streaming && !message.failed) article.append(actionsBar(message));

    return article;
  }

  function actionsBar(message) {
    const bar = el("div", "msg-actions");

    const copy = iconButton("i-copy", "Copy message", "msg-action");
    copy.addEventListener("click", () => copyText(message.content, { toastOnSuccess: true }));
    bar.append(copy);

    const realID = message.id && !String(message.id).startsWith("tmp-");

    if (message.role === "user" && realID) {
      const edit = iconButton("i-edit", "Edit message", "msg-action");
      edit.addEventListener("click", () => beginEditMessage(message));
      const remove = iconButton("i-trash", "Delete message", "msg-action");
      remove.addEventListener("click", () => deleteMessage(message));
      bar.append(edit, remove);
    }

    if (message.role === "assistant" && realID && !state.streaming) {
      const messages = (state.current && state.current.messages) || [];
      if (messages[messages.length - 1] === message) {
        const regenButton = iconButton("i-refresh", "Regenerate reply", "msg-action");
        regenButton.addEventListener("click", () => regenerate(message));
        bar.append(regenButton);
      }
      const remove = iconButton("i-trash", "Delete message", "msg-action");
      remove.addEventListener("click", () => deleteMessage(message));
      bar.append(remove);
    }

    return bar;
  }

  function finishNote(message) {
    const reason = String(message.finishReason || "").toLowerCase();
    if (reason === "max_output_tokens" || reason === "length" || reason === "incomplete") {
      return el("p", "msg-note", "The reply hit the model's output limit and may be incomplete.");
    }
    if (reason === "content_filter") {
      return el("p", "msg-note", "The model stopped early because of a content filter.");
    }
    return null;
  }

  function errorNote(message) {
    const note = el("div", "msg-error");
    note.append(el("span", null, message.error.message || "The reply was interrupted."));
    const retry = el("button", "btn-small", "Try again");
    retry.type = "button";
    retry.addEventListener("click", () => regenerate(message));
    note.append(retry);
    return note;
  }

  function failedNote(message) {
    const note = el("div", "msg-failed");
    note.append(el("span", null, message.failed.message || "Not sent."));

    if (message.failed.retrying) {
      note.append(el("span", "retry-info", "Retrying in " + message.failed.countdown + "s"));
      const cancel = el("button", "btn-small", "Cancel");
      cancel.type = "button";
      cancel.addEventListener("click", () => {
        clearRetry();
        message.failed.retrying = false;
        renderMessages();
      });
      note.append(cancel);
    } else {
      const retry = el("button", "btn-small", "Retry");
      retry.type = "button";
      retry.addEventListener("click", () => retrySend(message));
      note.append(retry);
    }

    return note;
  }

  function findArticle(message) {
    if (!message || !message.id) return null;
    const nodes = dom.messages.querySelectorAll(".msg");
    for (const node of nodes) {
      if (node.dataset.id === message.id) return node;
    }
    return null;
  }

  function updateFailedNote(message) {
    const article = findArticle(message);
    if (!article) return;
    const info = article.querySelector(".retry-info");
    if (info) info.textContent = "Retrying in " + message.failed.countdown + "s";
  }

  function removeMessage(message) {
    if (!state.current) return;
    state.current.messages = state.current.messages.filter((m) => m !== message);
  }

  function beginEditMessage(message) {
    if (state.streaming) return;
    const article = findArticle(message);
    if (!article) return;
    const target = article.querySelector(".bubble");
    if (!target) return;

    const form = el("form", "edit-box");
    const input = el("textarea", "edit-input");
    input.value = message.content;
    input.rows = Math.min(10, Math.max(3, message.content.split("\n").length));

    const actions = el("div", "edit-actions");
    const cancel = el("button", "btn btn-quiet", "Cancel");
    cancel.type = "button";
    cancel.addEventListener("click", () => renderMessages());
    const save = el("button", "btn btn-primary", "Save & resend");
    save.type = "submit";

    form.addEventListener("submit", (event) => {
      event.preventDefault();
      saveEditMessage(message, input.value);
    });

    actions.append(cancel, save);
    form.append(input, actions);
    target.replaceWith(form);

    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
  }

  async function deleteMessage(message) {
    if (state.streaming) {
      toast("Stop the current reply first.");
      return;
    }

    if (!message.id || String(message.id).startsWith("tmp-")) {
      removeMessage(message);
      renderMessages();
      return;
    }

    try {
      await api("/api/messages/" + encodeURIComponent(message.id), { method: "DELETE" });
    } catch (err) {
      toast(err.message);
      return;
    }

    removeMessage(message);
    renderMessages();
    scheduleConversationRefresh();
  }

  /* --------------------------------------------------------------- streaming */

  function assistantDraft(id) {
    return {
      id: id || tempId(),
      role: "assistant",
      content: "",
      reasoning: "",
      steps: [],
      citations: [],
      toolRun: {
        enabled: state.toolsEnabled,
        maxCalls: state.maxToolCalls,
        calls: 0,
        searches: 0,
        activities: [],
      },
      streaming: true,
    };
  }

  async function sendMessage(raw, options = {}) {
    const text = String(raw || "").trim();
    const attachments = Array.isArray(options.attachments)
      ? options.attachments
      : state.pendingAttachments.slice();
    if ((!text && !attachments.length) || state.streaming) return;
    if (state.attachmentBusy) {
      toast("Wait for the attachment to finish processing.");
      return;
    }
    if (!state.providerReady) {
      toast("No OpenRouter API key is configured on the server.");
      return;
    }

    if (!options.autoRetry) {
      state.autoRetried = false;
      clearRetry();
    }

    try {
      await ensureConversation();
    } catch (err) {
      toast(err.message);
      return;
    }

    // Failed sends were never persisted, so drop them before a new turn. The
    // visible transcript should match the history the server sends upstream.
    state.current.messages = Array.isArray(state.current.messages) ? state.current.messages : [];
    state.current.messages = state.current.messages.filter((message) => !message.failed);

    const user = {
      id: tempId(),
      role: "user",
      content: text,
      attachments,
      createdAt: Date.now(),
      pending: true,
    };
    const assistant = assistantDraft();

    state.current.messages.push(user, assistant);
    clearInput();
    clearAttachments();
    state.pinned = true;
    renderMessages({ stick: true });

    await streamInto(assistant, { content: text, attachments }, {
      onPreMetaError(err) {
        removeMessage(assistant);
        markFailed(user, err);
      },
    });
  }

  async function regenerate(target) {
    if (state.streaming || !state.current || !state.current.id) return;

    const messages = state.current.messages;
    const last = messages[messages.length - 1];
    if (!last || last.role !== "assistant" || last.streaming) return;
    // A retry button on an older reply must not regenerate a newer one.
    if (target && target !== last) {
      toast("Only the latest reply can be regenerated.");
      return;
    }

    clearRetry();
    const snapshot = messages.slice();
    messages.pop();

    const assistant = assistantDraft(last.id);
    messages.push(assistant);
    state.pinned = true;
    renderMessages({ stick: true });

    await streamInto(assistant, { action: "regenerate", messageId: last.id }, {
      onPreMetaError(err) {
        if (state.current) state.current.messages = snapshot;
        toast(err.message);
      },
    });
  }

  async function saveEditMessage(original, raw) {
    const text = String(raw || "").trim();
    if (!text || state.streaming || !state.current || !state.current.id) return;

    clearRetry();
    const snapshot = state.current.messages.slice();
    const index = snapshot.findIndex((m) => m.id === original.id);
    if (index < 0) return;

    const edited = { ...original, content: text, failed: null };
    const assistant = assistantDraft();

    state.current.messages = [...snapshot.slice(0, index), edited, assistant];
    state.pinned = true;
    renderMessages({ stick: true });

    await streamInto(assistant, { action: "edit", messageId: original.id, content: text }, {
      onPreMetaError(err) {
        if (state.current) state.current.messages = snapshot;
        toast(err.message);
      },
    });
  }

  function mergeCitations(message, citations) {
    const existing = Array.isArray(message.citations) ? message.citations : [];
    const seen = new Set(existing.map((citation) => citation.url));
    let added = false;
    for (const citation of citations || []) {
      if (!citation || !citation.url || seen.has(citation.url)) continue;
      seen.add(citation.url);
      existing.push(citation);
      added = true;
    }
    message.citations = existing;
    return added;
  }

  function mergeToolActivity(message, activity) {
    if (!activity || !activity.name) return false;
    if (!message.toolRun) {
      message.toolRun = { enabled: state.toolsEnabled, maxCalls: state.maxToolCalls, calls: 0, searches: 0, activities: [] };
    }
    const activities = Array.isArray(message.toolRun.activities) ? message.toolRun.activities : [];
    if (activities.some((item) => item.name === activity.name && item.detail === activity.detail)) return false;
    activities.push(activity);
    message.toolRun.activities = activities;
    message.toolRun.calls = Math.max(message.toolRun.calls || 0, activities.length);
    return true;
  }

  async function streamInto(assistant, payload, handlers = {}) {
    if (!state.current || !state.current.id) return;

    state.streaming = true;
    state.streamingMsg = assistant;
    setBusy(true);

    const controller = new AbortController();
    state.abort = controller;
    let gotMeta = false;
    let gotDone = false;

    const requestBody = {
      conversationId: state.current.id,
      provider: state.provider,
      model: state.model,
      toolsEnabled: state.toolsEnabled,
      maxToolCalls: state.maxToolCalls,
      reasoningEffort: activeReasoningEffort(),
      timezone: localTimezone(),
      ...payload,
    };

    try {
      const res = await fetch("/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify(requestBody),
        signal: controller.signal,
      });

      if (!res.ok) {
        const err = await httpError(res);
        if (res.status === 401) handleUnauthorized();
        throw err;
      }
      if (!res.body) throw new Error("This browser cannot read streaming responses.");

      for await (const event of readSSE(res)) {
        if (event.event === "meta") {
          gotMeta = true;
          applyMeta(event.data, assistant);
        } else if (event.event === "delta") {
          assistant.content += (event.data && event.data.text) || "";
          schedulePaint();
        } else if (event.event === "reasoning") {
          assistant.reasoning += (event.data && event.data.text) || "";
          scheduleStructurePaint();
        } else if (event.event === "step") {
          const step = event.data && event.data.step;
          if (step && step.tool) {
            const steps = Array.isArray(assistant.steps) ? assistant.steps : [];
            steps.push(step);
            assistant.steps = steps;
            assistant.reasoning = "";
            scheduleStructurePaint();
          }
        } else if (event.event === "citation") {
          if (mergeCitations(assistant, event.data && event.data.citations)) {
            scheduleStructurePaint();
          }
        } else if (event.event === "tool") {
          if (mergeToolActivity(assistant, event.data && event.data.activity)) {
            scheduleStructurePaint();
          }
        } else if (event.event === "done") {
          gotDone = true;
          assistant.saved = event.data.saved !== false;
          assistant.finishReason = (event.data && event.data.finishReason) || "";
          if (event.data && typeof event.data.content === "string" && event.data.content) {
            assistant.content = event.data.content;
          }
          if (event.data && Array.isArray(event.data.steps) && event.data.steps.length) {
            assistant.steps = event.data.steps;
          }
          if (event.data && typeof event.data.reasoning === "string") {
            assistant.reasoning = event.data.reasoning;
          } else if ((assistant.steps || []).length) {
            assistant.reasoning = "";
          }
          if (event.data && Array.isArray(event.data.citations)) {
            mergeCitations(assistant, event.data.citations);
          }
          if (event.data && event.data.toolRun) {
            assistant.toolRun = event.data.toolRun;
          }
        } else if (event.event === "error") {
          throw streamError(event.data);
        }
      }

      assistant.streaming = false;

      if (!gotDone) {
        // The connection ended without a completion event: the reply is partial.
        assistant.error = {
          message: "The connection closed before the reply finished. The answer may be incomplete.",
          code: "incomplete_stream",
        };
        if (handlers.onMidStreamError) handlers.onMidStreamError(new Error(assistant.error.message), assistant);
      } else if (!assistant.content && !(assistant.steps || []).length) {
        removeMessage(assistant);
        toast("OpenRouter returned an empty reply.");
      } else if (assistant.saved === false) {
        toast("The reply streamed but could not be saved.");
      }
    } catch (err) {
      if (err && err.name === "AbortError") {
        assistant.streaming = false;
        assistant.stopped = true;
        if (!assistant.content) removeMessage(assistant);
      } else if (!gotMeta) {
        assistant.streaming = false;
        removeMessage(assistant);
        if (err.status === 401) return; // the auth screen is taking over
        if (handlers.onPreMetaError) handlers.onPreMetaError(err);
        else toast(err.message);
      } else {
        assistant.streaming = false;
        assistant.error = {
          message: err.message || "The reply was interrupted.",
          code: err.code || "",
        };
        if (handlers.onMidStreamError) handlers.onMidStreamError(err, assistant);
      }
    } finally {
      state.streaming = false;
      state.abort = null;
      state.streamingMsg = null;
      setBusy(false);
      renderMessages();
      scheduleConversationRefresh();
    }
  }

  function applyMeta(meta, assistant) {
    if (!meta) return;

    if (meta.assistantMessageId) assistant.id = meta.assistantMessageId;

    if (meta.userMessageId) {
      const pending = state.current.messages
        .slice()
        .reverse()
        .find((m) => m.role === "user" && m.pending);
      if (pending) {
        pending.id = meta.userMessageId;
        pending.pending = false;
      }
    }

    if (meta.title) {
      state.current.title = meta.title;
      const row = state.conversations.find((c) => c.id === state.current.id);
      if (row) row.title = meta.title;
      updateHeading();
      renderConversations();
    }
    if (meta.model) {
      state.model = meta.model;
      setPref(pkey(PREFS.model), state.model);
      updateModelTrigger();
      updateRoutingTrigger();
      updateHeading();
    }
  }

  async function* readSSE(res) {
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        buffer = buffer.replace(/\r\n/g, "\n");

        let index;
        while ((index = buffer.indexOf("\n\n")) !== -1) {
          const frame = buffer.slice(0, index);
          buffer = buffer.slice(index + 2);
          const event = parseFrame(frame);
          if (event) yield event;
        }
      }

      buffer += decoder.decode();
      if (buffer.trim()) {
        const event = parseFrame(buffer);
        if (event) yield event;
      }
    } finally {
      reader.releaseLock();
    }
  }

  function parseFrame(frame) {
    let event = "message";
    const data = [];

    for (const line of frame.split("\n")) {
      if (!line || line.startsWith(":")) continue;
      const colon = line.indexOf(":");
      const field = colon === -1 ? line : line.slice(0, colon);
      let value = colon === -1 ? "" : line.slice(colon + 1);
      if (value.startsWith(" ")) value = value.slice(1);

      if (field === "event") event = value;
      else if (field === "data") data.push(value);
    }

    if (!data.length) return null;

    try {
      return { event, data: JSON.parse(data.join("\n")) };
    } catch {
      return { event: "error", data: { message: "Received a malformed stream frame." } };
    }
  }

  function setBusy(busy) {
    dom.send.hidden = busy;
    dom.stop.hidden = !busy;
    dom.send.disabled = busy || !state.providerReady;
    updateAttachmentControls();
    renderToolBar();
  }

  function stopStreaming() {
    if (state.abort) state.abort.abort();
  }

  function markFailed(user, err) {
    user.pending = false;
    user.failed = {
      message: err.message || "Not sent.",
      code: err.code || "",
      status: err.status || 0,
      retryAfter: err.retryAfter || 0,
      retrying: false,
      countdown: 0,
    };

    const rateLimited = err.status === 429 || err.code === "rate_limited";
    if (rateLimited && !state.autoRetried) {
      state.autoRetried = true;
      const delay = Math.max(1, Math.round((err.retryAfter || 5) * (0.8 + Math.random() * 0.6)));
      user.failed.retrying = true;
      user.failed.countdown = delay;
      startRetryCountdown(user);
    }
  }

  function startRetryCountdown(user) {
    clearRetry();
    state.retryTimer = setInterval(() => {
      if (!user.failed || !user.failed.retrying) {
        clearRetry();
        return;
      }
      user.failed.countdown -= 1;
      if (user.failed.countdown <= 0) {
        clearRetry();
        retrySend(user);
        return;
      }
      updateFailedNote(user);
    }, 1000);
  }

  function clearRetry() {
    if (state.retryTimer) {
      clearInterval(state.retryTimer);
      state.retryTimer = null;
    }
  }

  function retrySend(user) {
    if (state.streaming) return;
    clearRetry();
    const attachments = Array.isArray(user.attachments) ? user.attachments : [];
    removeMessage(user);
    renderMessages();
    sendMessage(user.content, { autoRetry: true, attachments });
  }

  let refreshTimer = null;
  function scheduleConversationRefresh() {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => {
      loadConversations(true);
    }, 400);
  }

  /* --------------------------------------------------------------- scrolling */

  function nearBottom() {
    const box = dom.messages;
    return box.scrollHeight - box.scrollTop - box.clientHeight < 80;
  }

  function scrollToBottom(force) {
    if (!force && !state.pinned) return;
    dom.messages.scrollTop = dom.messages.scrollHeight;
  }

  let paintTimer = null;
  function schedulePaint() {
    if (paintTimer) return;
    paintTimer = setTimeout(() => {
      paintTimer = null;
      paintStream();
    }, 70);
  }

  let structureTimer = null;
  function scheduleStructurePaint() {
    if (structureTimer) return;
    structureTimer = setTimeout(() => {
      structureTimer = null;
      renderMessages();
    }, 90);
  }

  function paintStream() {
    const message = state.streamingMsg;
    if (!message) return;

    const nodes = dom.messages.querySelectorAll(".msg-assistant .md");
    const body = nodes.length ? nodes[nodes.length - 1] : null;
    if (!body) return;

    body.replaceChildren(renderMarkdown(message.content));
    body.append(el("span", "caret"));
    scrollToBottom(false);
  }

  /* ---------------------------------------------------------------- composer */

  function clampToolCalls(value) {
    const number = Number.parseInt(value, 10);
    if (!Number.isFinite(number)) return 6;
    return Math.min(30, Math.max(1, number));
  }

  const REASONING_LABELS = {
    max: "Max",
    xhigh: "Extra high",
    high: "High",
    medium: "Medium",
    low: "Low",
    minimal: "Minimal",
    none: "None",
  };

  function reasoningLabel(value) {
    if (!value) return "";
    return REASONING_LABELS[value] || value;
  }

  function reasoningOptions() {
    const model = selectedModel();
    if (!model || !model.supportsReasoning) return [];
    const info = model.reasoning;
    if (!info) return [];
    const efforts = Array.isArray(info.supportedEfforts) ? info.supportedEfforts : [];
    const seen = new Set();
    const list = [];
    for (const raw of efforts) {
      const effort = String(raw || "").trim().toLowerCase();
      if (!effort || seen.has(effort)) continue;
      seen.add(effort);
      list.push(effort);
    }
    return list;
  }

  function effectiveReasoningEffort(options = reasoningOptions()) {
    const current = String(state.reasoningEffort || "").trim().toLowerCase();
    if (current && options.includes(current)) return current;
    const info = (selectedModel() || {}).reasoning;
    const fallback = info && info.defaultEffort ? String(info.defaultEffort).trim().toLowerCase() : "";
    return fallback && options.includes(fallback) ? fallback : "";
  }

  function reasoningTriggerText() {
    const effort = effectiveReasoningEffort();
    return effort ? "Reasoning · " + reasoningLabel(effort) : "Reasoning";
  }

  function activeReasoningEffort() {
    return reasoningOptions().length ? state.reasoningEffort : "";
  }

  function updateReasoningControl() {
    const options = reasoningOptions();
    const model = selectedModel();
    if (state.reasoningEffort && model && (options.length === 0 || !options.includes(state.reasoningEffort))) {
      state.reasoningEffort = "";
      setPref(pkey(PREFS.reasoningEffort), "");
      if (state.current) state.current.reasoningEffort = "";
    }

    dom.reasoningMenu.hidden = options.length === 0;
    dom.reasoningTriggerLabel.textContent = reasoningTriggerText();
    dom.reasoningTrigger.disabled = state.streaming || options.length === 0;
    if (state.streaming) closeReasoningMenu();
    const open = options.length > 0 && !dom.reasoningDropup.hidden;
    dom.reasoningTrigger.setAttribute("aria-expanded", open ? "true" : "false");
    if (!options.length && !dom.reasoningDropup.hidden) closeReasoningMenu();
    renderReasoningMenu(options);
  }

  function renderReasoningMenu(options = reasoningOptions()) {
    dom.reasoningDropup.replaceChildren();

    const head = el("div", "reasoning-head");
    head.append(el("span", "reasoning-head-title", "Reasoning effort"));
    const info = (selectedModel() || {}).reasoning;
    if (info && info.defaultEffort) {
      head.append(el("span", "reasoning-head-note", "Default " + reasoningLabel(info.defaultEffort)));
    }
    if (info && info.mandatory) {
      head.append(el("span", "reasoning-head-note", "Always on for this model"));
    }
    dom.reasoningDropup.append(head);

    for (const effort of options) {
      dom.reasoningDropup.append(reasoningOption(effort, reasoningLabel(effort)));
    }
  }

  function reasoningOption(value, label) {
    const active = effectiveReasoningEffort() === value;
    const button = el("button", "reasoning-option" + (active ? " active" : ""));
    button.type = "button";
    button.dataset.effort = value;
    button.setAttribute("role", "menuitemradio");
    button.setAttribute("aria-checked", active ? "true" : "false");
    button.append(el("span", "reasoning-option-label", label));
    if (active) button.append(el("span", "reasoning-option-check", "✓"));
    button.addEventListener("click", () => chooseReasoningEffort(value));
    return button;
  }

  function chooseReasoningEffort(value) {
    if (state.streaming) return;
    const next = String(value || "");
    state.reasoningEffort = next;
    setPref(pkey(PREFS.reasoningEffort), next);
    if (state.current) state.current.reasoningEffort = next;
    closeReasoningMenu();
    updateReasoningControl();
    if (state.current && state.current.id) {
      persistConversationSettings({ reasoningEffort: next });
    }
  }

  function toggleReasoningMenu() {
    if (dom.reasoningTrigger.disabled) return;
    const open = dom.reasoningDropup.hidden;
    if (open) {
      renderReasoningMenu();
      dom.reasoningDropup.hidden = false;
      dom.reasoningTrigger.setAttribute("aria-expanded", "true");
    } else {
      closeReasoningMenu();
    }
  }

  function closeReasoningMenu() {
    if (dom.reasoningDropup.hidden) return;
    dom.reasoningDropup.hidden = true;
    dom.reasoningTrigger.setAttribute("aria-expanded", "false");
  }

  function localTimezone() {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
    } catch {
      return "UTC";
    }
  }

  function loadToolPrefs() {
    state.toolsEnabled = pref(pkey(PREFS.toolsEnabled)) !== "0";
    state.maxToolCalls = clampToolCalls(pref(pkey(PREFS.maxToolCalls)) || 6);
    state.reasoningEffort = pref(pkey(PREFS.reasoningEffort)) || "";
  }

  function renderToolBar() {
    const on = Boolean(state.toolsEnabled);
    dom.toolsToggle.setAttribute("aria-pressed", on ? "true" : "false");
    dom.toolsToggleLabel.textContent = on ? "Web on" : "Web off";
    dom.toolCap.value = state.maxToolCalls;
    dom.toolsToggle.disabled = state.streaming;
    dom.toolCap.disabled = state.streaming;
    updateReasoningControl();
  }

  function setToolSettings(enabled, calls, persist = true) {
    state.toolsEnabled = Boolean(enabled);
    state.maxToolCalls = clampToolCalls(calls);
    setPref(pkey(PREFS.toolsEnabled), state.toolsEnabled ? "1" : "0");
    setPref(pkey(PREFS.maxToolCalls), String(state.maxToolCalls));
    if (state.current) {
      state.current.toolsEnabled = state.toolsEnabled;
      state.current.maxToolCalls = state.maxToolCalls;
    }
    renderToolBar();
    if (persist && state.current && state.current.id && !state.streaming) {
      persistConversationSettings({
        toolsEnabled: state.toolsEnabled,
        maxToolCalls: state.maxToolCalls,
      });
    }
  }

  function clearInput() {
    dom.input.value = "";
    dom.input.style.height = "auto";
    if (window.matchMedia("(pointer: fine)").matches) dom.input.focus();
  }

  function autoGrow() {
    dom.input.style.height = "auto";
    dom.input.style.height = Math.min(dom.input.scrollHeight, 180) + "px";
    scrollToBottom(false);
  }

  function attachmentBytes(attachment) {
    const comma = String(attachment.data || "").indexOf(",");
    const payload = comma >= 0 ? attachment.data.length - comma - 1 : attachment.data.length;
    return Math.max(0, Math.floor(payload * 3 / 4));
  }

  function clearAttachments() {
    state.pendingAttachments = [];
    renderComposerAttachments();
  }

  function renderComposerAttachments() {
    dom.composerAttachments.replaceChildren();
    dom.composerAttachments.hidden = state.pendingAttachments.length === 0;

    state.pendingAttachments.forEach((attachment, index) => {
      const chip = el("div", "attach-chip");
      chip.title = attachment.name || "";

      if (attachment.kind === "image") {
        const thumb = el("img", "attach-thumb");
        thumb.src = attachment.data;
        thumb.alt = "";
        chip.append(thumb);
      } else {
        chip.append(el("span", "attach-kind", attachmentKindLabel(attachment)));
      }

      const text = el("span", "attach-text");
      text.append(el("span", "attach-name", attachment.name || "Attachment"));
      text.append(el("span", "attach-size", formatBytes(attachmentBytes(attachment))));
      chip.append(text);

      const remove = el("button", "attach-remove", "×");
      remove.type = "button";
      remove.setAttribute("aria-label", "Remove " + (attachment.name || "attachment"));
      remove.addEventListener("click", () => {
        state.pendingAttachments.splice(index, 1);
        renderComposerAttachments();
      });
      chip.append(remove);
      dom.composerAttachments.append(chip);
    });
  }

  function updateAttachmentControls() {
    const disabled = state.streaming || state.attachmentBusy;
    dom.attachFile.disabled = disabled;
    dom.fileInput.disabled = disabled;
  }

  function attachmentMime(file) {
    const declared = String(file.type || "").toLowerCase();
    if (declared && declared !== "application/octet-stream") return declared;
    const ext = String(file.name || "").toLowerCase().split(".").pop();
    switch (ext) {
      case "jpg":
      case "jpeg": return "image/jpeg";
      case "png": return "image/png";
      case "webp": return "image/webp";
      case "gif": return "image/gif";
      case "pdf": return "application/pdf";
      case "txt":
      case "text":
      case "md":
      case "markdown":
      case "json":
      case "jsonl":
      case "go":
      case "py":
      case "js":
      case "mjs":
      case "cjs":
      case "jsx":
      case "ts":
      case "tsx":
      case "css":
      case "scss":
      case "html":
      case "htm":
      case "csv":
      case "tsv":
      case "yaml":
      case "yml":
      case "xml":
      case "sql":
      case "sh":
      case "bash":
      case "zsh":
      case "ps1":
      case "bat":
      case "toml":
      case "ini":
      case "cfg":
      case "conf":
      case "log":
      case "c":
      case "h":
      case "cpp":
      case "hpp":
      case "cc":
      case "rs":
      case "java":
      case "rb":
      case "php":
      case "lua":
      case "swift":
      case "kt":
      case "kts":
      case "r":
      case "cs":
      case "scala":
      case "vue":
      case "svelte":
      case "gradle":
      case "env":
        return "text/plain";
      default: return "";
    }
  }

  function isTextMime(mime) {
    if (mime.startsWith("text/")) return true;
    return [
      "application/json",
      "application/xml",
      "application/javascript",
      "application/typescript",
      "application/x-yaml",
      "application/x-sh",
      "application/toml",
    ].includes(mime);
  }

  function readFileDataURL(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result || ""));
      reader.onerror = () => reject(new Error("Could not read that file."));
      reader.readAsDataURL(file);
    });
  }

  async function loadImageFile(file) {
    if ("createImageBitmap" in window) {
      try {
        return await createImageBitmap(file);
      } catch {
        /* fall back to an <img> element */
      }
    }
    return new Promise((resolve, reject) => {
      const url = URL.createObjectURL(file);
      const image = new Image();
      image.onload = () => {
        URL.revokeObjectURL(url);
        resolve(image);
      };
      image.onerror = () => {
        URL.revokeObjectURL(url);
        reject(new Error("Could not read that image."));
      };
      image.src = url;
    });
  }

  async function prepareImageAttachment(file, mime) {
    if (mime !== "image/jpeg" && mime !== "image/png" && mime !== "image/webp" && mime !== "image/gif") {
      throw new Error("Images must be JPEG, PNG, WebP, or GIF.");
    }

    const image = await loadImageFile(file);
    const width = image.width || image.naturalWidth || 0;
    const height = image.height || image.naturalHeight || 0;
    if (!width || !height) throw new Error("That image has no readable dimensions.");

    const maxDimension = 1600;
    const scale = Math.min(1, maxDimension / Math.max(width, height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(width * scale));
    canvas.height = Math.max(1, Math.round(height * scale));

    const context = canvas.getContext("2d");
    if (!context) throw new Error("This browser cannot resize images.");
    // JPEG has no alpha channel. A white matte keeps transparent PNGs legible.
    context.fillStyle = "#ffffff";
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    if (typeof image.close === "function") image.close();

    let data = "";
    for (const quality of [0.88, 0.78, 0.68, 0.58]) {
      data = canvas.toDataURL("image/jpeg", quality);
      if (attachmentBytes({ data }) <= 2 * 1024 * 1024) break;
    }
    if (attachmentBytes({ data }) > 8 * 1024 * 1024) {
      throw new Error("That image is still too large after resizing.");
    }

    const base = String(file.name || "image").replace(/\.[^.]+$/, "") || "image";
    return {
      kind: "image",
      name: base + ".jpg",
      mime: "image/jpeg",
      data,
    };
  }

  async function prepareTextAttachment(file, mime) {
    if (!isTextMime(mime)) throw new Error("That is not a text file.");
    if (file.size > 4 * 1024 * 1024) {
      throw new Error("Text files must be 4 MB or smaller.");
    }

    const bytes = new Uint8Array(await file.arrayBuffer());
    let text = "";
    try {
      text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    } catch {
      throw new Error("Text files must be valid UTF-8.");
    }
    if (text.includes("\u0000")) {
      throw new Error("That looks like a binary file, not text.");
    }

    return {
      kind: "text",
      name: file.name || "file.txt",
      mime: "text/plain",
      data: await readFileDataURL(new Blob([text], { type: "text/plain" })),
    };
  }

  async function prepareAttachment(file) {
    const mime = attachmentMime(file);
    if (mime === "application/pdf") {
      if (file.size > 12 * 1024 * 1024) {
        throw new Error("PDFs must be 12 MB or smaller.");
      }
      return {
        kind: "pdf",
        name: file.name || "document.pdf",
        mime: "application/pdf",
        data: await readFileDataURL(file),
      };
    }
    if (mime.startsWith("image/")) {
      return prepareImageAttachment(file, mime);
    }
    if (isTextMime(mime)) {
      return prepareTextAttachment(file, mime);
    }
    throw new Error("Attach an image, PDF, or text file.");
  }

  function warnAttachmentCapability(attachment) {
    const model = selectedModel();
    if (!model) return;
    if (attachment.kind === "image" && !model.supportsVision) {
      toast("This model is not marked as vision-capable; the image may be rejected.");
    }
  }

  async function addFiles(fileList) {
    const files = Array.from(fileList || []);
    if (!files.length || state.attachmentBusy || state.streaming) return;

    state.attachmentBusy = true;
    updateAttachmentControls();
    try {
      for (const file of files) {
        if (state.pendingAttachments.length >= 6) {
          toast("Attach at most 6 files.");
          break;
        }
        try {
          const attachment = await prepareAttachment(file);
          const total = state.pendingAttachments.reduce((sum, item) => sum + attachmentBytes(item), 0) +
            attachmentBytes(attachment);
          if (total > 24 * 1024 * 1024) {
            toast("Attachments are too large together (24 MB max).");
            continue;
          }
          state.pendingAttachments.push(attachment);
          renderComposerAttachments();
          warnAttachmentCapability(attachment);
        } catch (err) {
          toast(err.message || "Could not attach that file.");
        }
      }
    } finally {
      state.attachmentBusy = false;
      updateAttachmentControls();
      dom.fileInput.value = "";
    }
  }

  /* ----------------------------------------------------------------- sidebar */

  function openSidebar() {
    dom.app.classList.add("sidebar-open");
    dom.scrim.hidden = false;
    dom.menu.setAttribute("aria-expanded", "true");
  }

  function closeSidebar() {
    dom.app.classList.remove("sidebar-open");
    dom.scrim.hidden = true;
    dom.menu.setAttribute("aria-expanded", "false");
  }

  /* ------------------------------------------------------------ file export */

  function downloadText(filename, text, mime = "text/plain") {
    const blob = new Blob([text], { type: mime + ";charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function safeFilename(value) {
    const cleaned = String(value || "clachan-chat")
      .replace(/[^\w.-]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 80);
    return cleaned || "clachan-chat";
  }

  function formatBytes(bytes) {
    if (bytes < 1024) return bytes + " B";
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
    return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  }

  function messageLabel(message) {
    if (message.role === "user") return (state.profile && state.profile.name) || "You";
    return assistantName();
  }

  function currentMessages() {
    return (state.current && state.current.messages) || [];
  }

  function openExportDialog() {
    if (!state.current || !currentMessages().length) {
      toast("There is nothing to export yet.");
      return;
    }
    dom.exportSub.textContent = state.current.title || "New chat";
    const count = currentMessages().length;
    dom.exportNote.textContent = count + " message" + (count === 1 ? "" : "s");
    if (!dom.exportDialog.open) dom.exportDialog.showModal();
  }

  function conversationMarkdown() {
    const conv = state.current;
    const lines = ["# " + (conv.title || "New chat"), ""];
    if (state.model) lines.push("- Model: `" + state.model + "`");
    if (conv.createdAt) lines.push("- Started: " + new Date(conv.createdAt).toLocaleString());
    lines.push("");
    for (const message of currentMessages()) {
      lines.push("## " + messageLabel(message), "");
      if (message.reasoning) {
        lines.push("<details><summary>Reasoning</summary>", "", message.reasoning, "", "</details>", "");
      }
      for (const step of message.steps || []) {
        lines.push("### " + toolLabel(step.tool));
        if (step.arguments) lines.push("", "```json", step.arguments, "```");
        if (step.result) lines.push("", "```", step.result, "```");
        lines.push("");
      }
      if (message.content) lines.push(message.content);
      for (const attachment of message.attachments || []) {
        const kind = attachment.kind === "image" ? "Image" : attachment.kind === "pdf" ? "PDF" : "File";
        lines.push("- " + kind + ": " + (attachment.name || "attachment"));
      }
      if (Array.isArray(message.citations) && message.citations.length) {
        lines.push("", "Sources:");
        for (const citation of message.citations) {
          lines.push("- [" + (citation.title || citation.url) + "](" + citation.url + ")");
        }
      }
      lines.push("");
    }
    return lines.join("\n");
  }

  function conversationJSON() {
    return JSON.stringify({
      exportedAt: new Date().toISOString(),
      assistant: assistantName(),
      profile: state.profile ? state.profile.name : "",
      conversation: state.current,
    }, null, 2);
  }

  function exportConversation(kind) {
    if (!state.current) return;
    const base = safeFilename(state.current.title || "clachan-chat");
    if (kind === "markdown") {
      downloadText(base + ".md", conversationMarkdown(), "text/markdown");
      toast("Markdown exported.");
    } else if (kind === "json") {
      downloadText(base + ".json", conversationJSON(), "application/json");
      toast("JSON exported.");
    } else if (kind === "print") {
      printConversation();
    }
    if (dom.exportDialog.open) dom.exportDialog.close();
  }

  async function importConversationFile(file) {
    if (!file) return;
    if (file.size > 64 * 1024 * 1024) {
      toast("That JSON file is too large to import (64 MB max).");
      dom.importFile.value = "";
      return;
    }

    dom.importChat.disabled = true;
    try {
      const parsed = JSON.parse(await file.text());
      const source = parsed && parsed.conversation ? parsed.conversation : parsed;
      if (!source || typeof source !== "object" || !Array.isArray(source.messages)) {
        throw new Error("That file is not a Clachan chat export.");
      }

      const messages = source.messages.map((message) => {
        if (!message || (message.role !== "user" && message.role !== "assistant")) {
          throw new Error("The import contains a message with an unsupported role.");
        }
        return {
          role: message.role,
          content: typeof message.content === "string" ? message.content : "",
          attachments: Array.isArray(message.attachments) ? message.attachments : [],
          reasoning: typeof message.reasoning === "string" ? message.reasoning : "",
          steps: Array.isArray(message.steps) ? message.steps : [],
          citations: Array.isArray(message.citations) ? message.citations : [],
          toolRun: message.toolRun && typeof message.toolRun === "object" ? message.toolRun : null,
          createdAt: Number.isFinite(message.createdAt) ? message.createdAt : 0,
        };
      });
      if (!messages.length) throw new Error("That chat has no messages.");

      const created = await api("/api/conversations/import", {
        method: "POST",
        body: {
          title: typeof source.title === "string" ? source.title : "",
          provider: typeof source.provider === "string" ? source.provider : state.provider,
          model: typeof source.model === "string" ? source.model : state.model,
          systemPrompt: typeof source.systemPrompt === "string" ? source.systemPrompt : "",
          providerSort: typeof source.providerSort === "string" ? source.providerSort : "",
          providerOnly: typeof source.providerOnly === "string" ? source.providerOnly : "",
          allowFallbacks: source.allowFallbacks !== false,
          toolsEnabled: source.toolsEnabled !== false,
          maxToolCalls: clampToolCalls(source.maxToolCalls || 6),
          reasoningEffort: typeof source.reasoningEffort === "string" ? source.reasoningEffort : "",
          messages,
        },
      });

      await loadConversations(true);
      await selectConversation(created.id);
      toast("Chat imported.");
    } catch (err) {
      toast(err instanceof SyntaxError ? "That file is not valid JSON." : (err.message || "Could not import that chat."));
    } finally {
      dom.importChat.disabled = false;
      dom.importFile.value = "";
    }
  }

  function printConversation() {
    const conv = state.current;
    if (!conv) return;
    const doc = el("article", "print-doc");
    doc.append(el("h1", null, conv.title || "New chat"));
    const meta = [state.model, conv.createdAt ? new Date(conv.createdAt).toLocaleString() : ""]
      .filter(Boolean)
      .join(" · ");
    if (meta) doc.append(el("p", "print-meta", meta));

    for (const message of currentMessages()) {
      const section = el("section", "print-msg");
      section.append(el("h2", null, messageLabel(message)));
      if (message.role === "assistant") {
        const body = el("div", "print-md");
        body.append(renderMarkdown(message.content || ""));
        for (const actions of body.querySelectorAll(".code-actions")) actions.remove();
        section.append(body);
        if (Array.isArray(message.citations) && message.citations.length) {
          const list = el("ul", "print-citations");
          for (const citation of message.citations) {
            const item = el("li");
            const link = el("a", null, citation.title || citation.url);
            link.href = citation.url;
            item.append(link);
            list.append(item);
          }
          section.append(list);
        }
      } else {
        section.append(el("pre", "print-text", message.content || ""));
      }
      doc.append(section);
    }
    printNode(doc, conv.title || "Conversation");
  }

  function printNode(node, title) {
    dom.printArea.replaceChildren(node);
    const previous = document.title;
    document.title = title;
    window.print();
    document.title = previous;
  }

  /* --------------------------------------------------------------- viewer */

  function openFileViewer(file) {
    state.viewerFile = file;
    const lines = file.code ? file.code.split("\n").length : 0;
    dom.fileViewerTitle.textContent = file.name;
    dom.fileViewerSub.textContent = [file.language || "text", lines + " lines", formatBytes(new Blob([file.code]).size)]
      .filter(Boolean)
      .join(" · ");
    dom.fileViewerMeta.textContent = file.name;
    dom.fileViewerCode.textContent = file.code;
    if (!dom.fileViewer.open) dom.fileViewer.showModal();
  }

  function downloadViewerFile() {
    if (!state.viewerFile) return;
    downloadText(state.viewerFile.name, state.viewerFile.code, state.viewerFile.mime);
    toast("File downloaded.");
  }

  function printViewerFile() {
    if (!state.viewerFile) return;
    const doc = el("article", "print-doc");
    doc.append(el("h1", null, state.viewerFile.name));
    doc.append(el("p", "print-meta", state.viewerFile.language || "text"));
    doc.append(el("pre", "print-text", state.viewerFile.code));
    printNode(doc, state.viewerFile.name);
  }

  /* --------------------------------------------------------------- prompts */

  let promptLoading = false;

  async function openPrompts(options = {}) {
    closeSidebar();
    cancelPromptForm();
    state.presetSaveTarget = options.presetTarget || "";
    dom.promptSearch.value = "";
    state.promptFilter = "";
    dom.promptSub.textContent = "Reusable prompts saved to " + ((state.profile && state.profile.name) || "this profile");
    if (!dom.promptsDialog.open) dom.promptsDialog.showModal();
    await loadPrompts();

    if (options.form) {
      showPromptForm(null);
      dom.promptTitle.value = options.title || "";
      dom.promptBody.value = options.body || "";
      dom.promptSystem.checked = options.system !== false;
      dom.promptTitle.focus();
    } else {
      dom.promptSearch.focus();
    }
  }

  async function loadPrompts() {
    if (promptLoading) return;
    promptLoading = true;
    dom.promptList.replaceChildren(el("p", "picker-empty", "Loading prompts…"));
    try {
      const data = await api("/api/prompts");
      state.prompts = Array.isArray(data.prompts) ? data.prompts : [];
      renderPromptList();
      populatePresetSelects();
    } catch (err) {
      dom.promptList.replaceChildren(el("p", "picker-empty", "Could not load prompts: " + err.message));
    } finally {
      promptLoading = false;
    }
  }

  function promptPreview(text) {
    const flat = String(text || "").replace(/\s+/g, " ").trim();
    return flat.length > 150 ? flat.slice(0, 147) + "…" : flat;
  }

  function filteredPrompts() {
    const query = dom.promptSearch.value.trim().toLowerCase();
    if (!query) return state.prompts;
    return state.prompts.filter((prompt) =>
      (prompt.title + " " + prompt.body).toLowerCase().includes(query));
  }

  function renderPromptList() {
    const prompts = filteredPrompts();
    dom.promptList.replaceChildren();
    dom.promptCount.textContent = prompts.length + " prompt" + (prompts.length === 1 ? "" : "s");

    if (!prompts.length) {
      const message = state.prompts.length
        ? "No prompts match that search."
        : "No saved prompts yet. Save one for reuse across chats.";
      dom.promptList.append(el("p", "picker-empty", message));
      return;
    }

    for (const prompt of prompts) {
      const row = el("div", "prompt-row");
      row.dataset.id = prompt.id;

      const main = el("button", "prompt-main");
      main.type = "button";
      main.title = "Insert into the composer";
      const titleRow = el("span", "prompt-title-row");
      titleRow.append(el("span", "prompt-title", prompt.title));
      if (prompt.system) titleRow.append(el("span", "tag tag-tools", "System preset"));
      main.append(titleRow);
      main.append(el("span", "prompt-preview", promptPreview(prompt.body)));
      main.addEventListener("click", () => usePrompt(prompt, false));
      row.append(main);

      const actions = el("div", "prompt-actions");
      const insert = el("button", "btn-small", "Insert");
      insert.type = "button";
      insert.addEventListener("click", () => usePrompt(prompt, false));
      const system = el("button", "btn-small", "System");
      system.type = "button";
      system.title = "Use as this chat's system prompt";
      system.addEventListener("click", () => usePrompt(prompt, true));
      const edit = iconButton("i-edit", "Edit prompt", "prompt-action");
      edit.addEventListener("click", () => beginEditPrompt(prompt));
      const remove = iconButton("i-trash", "Delete prompt", "prompt-action");
      remove.addEventListener("click", () => deletePrompt(prompt));
      actions.append(insert, system, edit, remove);
      row.append(actions);
      dom.promptList.append(row);
    }
  }

  function showPromptForm(prompt) {
    state.editingPromptID = prompt ? prompt.id : null;
    dom.promptForm.hidden = false;
    dom.promptList.hidden = true;
    dom.promptNew.disabled = true;
    dom.promptSave.textContent = prompt ? "Save changes" : "Save prompt";
    dom.promptTitle.value = prompt ? prompt.title : "";
    dom.promptBody.value = prompt ? prompt.body : "";
    dom.promptSystem.checked = prompt ? Boolean(prompt.system) : false;
    dom.promptSub.textContent = prompt ? "Edit prompt" : "New prompt";
    dom.promptTitle.focus();
  }

  function beginNewPrompt() {
    showPromptForm(null);
  }

  function beginEditPrompt(prompt) {
    showPromptForm(prompt);
  }

  function cancelPromptForm() {
    state.editingPromptID = null;
    dom.promptForm.hidden = true;
    dom.promptList.hidden = false;
    dom.promptNew.disabled = false;
    dom.promptForm.reset();
    dom.promptSub.textContent = "Reusable prompts saved to " + ((state.profile && state.profile.name) || "this profile");
  }

  async function savePrompt(event) {
    event.preventDefault();
    const title = dom.promptTitle.value.trim();
    const body = dom.promptBody.value.trim();
    if (!title) {
      toast("Give the prompt a title.");
      dom.promptTitle.focus();
      return;
    }
    if (!body) {
      toast("The prompt body cannot be empty.");
      dom.promptBody.focus();
      return;
    }

    const target = state.presetSaveTarget;
    const system = dom.promptSystem.checked;
    let saved = null;

    dom.promptSave.disabled = true;
    try {
      if (state.editingPromptID) {
        const updated = await api("/api/prompts/" + encodeURIComponent(state.editingPromptID), {
          method: "PATCH",
          body: { title, body, system },
        });
        const index = state.prompts.findIndex((prompt) => prompt.id === updated.id);
        if (index >= 0) state.prompts[index] = updated;
        saved = updated;
        toast("Prompt updated.");
      } else {
        const created = await api("/api/prompts", { method: "POST", body: { title, body, system } });
        state.prompts.unshift(created);
        saved = created;
        toast("Prompt saved.");
      }
      cancelPromptForm();
      state.presetSaveTarget = "";
      renderPromptList();
      populatePresetSelects();

      if (target && saved) {
        const select = target === "system" ? dom.systemPreset : dom.personaPreset;
        const value = "prompt:" + saved.id;
        if (Array.from(select.options).some((option) => option.value === value)) {
          select.value = value;
        }
        if (dom.promptsDialog.open) dom.promptsDialog.close();
        toast("System preset saved.");
      }
    } catch (err) {
      toast(err.message);
    } finally {
      dom.promptSave.disabled = false;
    }
  }

  async function deletePrompt(prompt) {
    if (!window.confirm(`Delete "${prompt.title}"?`)) return;
    try {
      await api("/api/prompts/" + encodeURIComponent(prompt.id), { method: "DELETE" });
      state.prompts = state.prompts.filter((item) => item.id !== prompt.id);
      renderPromptList();
      populatePresetSelects();
      toast("Prompt deleted.");
    } catch (err) {
      toast(err.message);
    }
  }

  function usePrompt(prompt, asSystem) {
    if (asSystem) {
      if (state.streaming) {
        toast("Stop the current reply first.");
        return;
      }
      if (!state.current) newChat({ focus: false });
      state.current.systemPrompt = prompt.body;
      if (state.current.id) {
        persistConversationSettings({ systemPrompt: prompt.body });
      }
      toast("System prompt updated for this chat.");
    } else {
      const existing = dom.input.value.trimEnd();
      dom.input.value = existing ? existing + "\n\n" + prompt.body : prompt.body;
      autoGrow();
      dom.input.focus();
      toast("Prompt inserted.");
    }
    if (dom.promptsDialog.open) dom.promptsDialog.close();
  }

  function openPresetSaver(target) {
    const body = target === "system" ? dom.systemPrompt.value.trim() : dom.personaPrompt.value.trim();
    if (!body) {
      toast("Write a system prompt first.");
      return;
    }
    openPrompts({
      form: true,
      title: "",
      body,
      system: true,
      presetTarget: target,
    });
  }

  /* ---------------------------------------------------------------- settings */

  function setSettingsTab(name) {
    for (const button of dom.settingsNav.querySelectorAll("[data-settings-tab]")) {
      const active = button.dataset.settingsTab === name;
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", active ? "true" : "false");
    }
    for (const panel of dom.settingsPanels.querySelectorAll("[data-settings-panel]")) {
      panel.hidden = panel.dataset.settingsPanel !== name;
    }
  }

  function customSystemPresets() {
    return state.prompts.filter((prompt) => prompt.system);
  }

  function presetValueForText(text) {
    const value = String(text || "").trim();
    if (!value) return "";
    const builtin = SYSTEM_PRESETS.find((preset) => preset.prompt.trim() === value);
    if (builtin) return "builtin:" + builtin.id;
    const custom = customSystemPresets().find((prompt) => prompt.body.trim() === value);
    return custom ? "prompt:" + custom.id : "";
  }

  function presetBody(value) {
    if (value.startsWith("builtin:")) {
      const preset = SYSTEM_PRESETS.find((item) => item.id === value.slice("builtin:".length));
      return preset ? preset.prompt : "";
    }
    if (value.startsWith("prompt:")) {
      const prompt = state.prompts.find((item) => item.id === value.slice("prompt:".length));
      return prompt ? prompt.body : "";
    }
    return "";
  }

  function populatePresetSelect(select, text) {
    const selected = presetValueForText(text);
    select.replaceChildren();

    const custom = new Option("Custom / none", "");
    select.append(custom);

    const builtinGroup = document.createElement("optgroup");
    builtinGroup.label = "Built-in";
    for (const preset of SYSTEM_PRESETS) {
      builtinGroup.append(new Option(preset.label, "builtin:" + preset.id));
    }
    select.append(builtinGroup);

    const saved = customSystemPresets();
    if (saved.length) {
      const savedGroup = document.createElement("optgroup");
      savedGroup.label = "Saved presets";
      for (const prompt of saved) {
        savedGroup.append(new Option(prompt.title, "prompt:" + prompt.id));
      }
      select.append(savedGroup);
    }
    select.value = selected;
  }

  function populatePresetSelects() {
    populatePresetSelect(dom.systemPreset, dom.systemPrompt.value);
    populatePresetSelect(dom.personaPreset, dom.personaPrompt.value);
  }

  function syncPresetSelect(select, text) {
    select.value = presetValueForText(text);
  }

  function applyPreset(select, textarea) {
    const value = select.value;
    if (!value) return;
    const body = presetBody(value);
    if (!body) return;
    textarea.value = body;
    textarea.focus();
  }

  async function openSettings() {
    dom.systemPrompt.value = (state.current && state.current.systemPrompt) || "";
    dom.personaName.value = (state.profile && state.profile.personaName) || "";
    dom.personaPrompt.value = (state.profile && state.profile.personaPrompt) || "";
    dom.userName.value = (state.profile && state.profile.userName) || "";
    dom.userPersona.value = (state.profile && state.profile.userPersona) || "";
    dom.ignoredProviders.value = (state.profile && state.profile.ignoredProviders) || "";
    dom.theme.value = state.theme;
    dom.settingsNote.textContent = state.current && state.current.id
      ? ""
      : "Saved when you send the first message.";
    updateDefaultModelSetting();
    setSettingsTab("chat");
    populatePresetSelects();

    if (!dom.settings.open) dom.settings.showModal();
    // Refresh saved presets; the dialog stays responsive while they load.
    loadPrompts();
  }

  function updateDefaultModelSetting() {
    const id = (state.profile && state.profile.defaultModel) || "";
    const provider = providerInfo();
    const model = id && provider && provider.models
      ? provider.models.find((candidate) => candidate.id === id)
      : null;
    dom.defaultModelLabel.textContent = id ? (model ? model.name : id) : "Last used model";
    dom.defaultModelClear.hidden = !id;
  }

  async function setDefaultModel(id) {
    try {
      const updated = await api("/api/profile", {
        method: "PATCH",
        body: { defaultModel: id },
      });
      state.profile.defaultModel = updated.defaultModel || "";
      updateDefaultModelSetting();
      toast(id ? "Default model set for new chats." : "Default model cleared.");
      return true;
    } catch (err) {
      toast(err.message);
      return false;
    }
  }

  async function saveSettings() {
    const prompt = dom.systemPrompt.value.trim();
    const theme = THEMES.includes(dom.theme.value) ? dom.theme.value : "dark";
    const ignored = dom.ignoredProviders.value.trim();
    const personaName = dom.personaName.value.trim();
    const personaPrompt = dom.personaPrompt.value.trim();
    const userName = dom.userName.value.trim();
    const userPersona = dom.userPersona.value.trim();

    const currentIgnored = (state.profile && state.profile.ignoredProviders) || "";
    const oldPersonaName = (state.profile && state.profile.personaName) || "";
    const oldPersonaPrompt = (state.profile && state.profile.personaPrompt) || "";
    const oldUserName = (state.profile && state.profile.userName) || "";
    const oldUserPersona = (state.profile && state.profile.userPersona) || "";

    if (state.current && state.current.id) {
      try {
        const updated = await api("/api/conversations/" + encodeURIComponent(state.current.id), {
          method: "PATCH",
          body: { systemPrompt: prompt },
        });
        state.current.systemPrompt = updated.systemPrompt || "";
      } catch (err) {
        toast(err.message);
        return;
      }
    } else if (state.current) {
      state.current.systemPrompt = prompt;
    }

    const patch = {};
    if (ignored !== currentIgnored) patch.ignoredProviders = ignored;
    if (personaName !== oldPersonaName) patch.personaName = personaName;
    if (personaPrompt !== oldPersonaPrompt) patch.personaPrompt = personaPrompt;
    if (userName !== oldUserName) patch.userName = userName;
    if (userPersona !== oldUserPersona) patch.userPersona = userPersona;

    if (Object.keys(patch).length) {
      try {
        const updated = await api("/api/profile", { method: "PATCH", body: patch });
        state.profile = {
          ...state.profile,
          ...updated,
          defaultModel: updated.defaultModel || "",
          ignoredProviders: updated.ignoredProviders || "",
          personaName: updated.personaName || "",
          personaPrompt: updated.personaPrompt || "",
          userName: updated.userName || "",
          userPersona: updated.userPersona || "",
        };
      } catch (err) {
        toast(err.message);
        return;
      }

      if (patch.ignoredProviders !== undefined) {
        // The endpoint cache may contain the providers we just hid.
        state.endpoints = [];
        state.endpointsModel = "";
        if (state.providerOnly && isIgnoredTag(state.providerOnly)) {
          state.providerSort = "";
          state.providerOnly = "";
          state.allowFallbacks = true;
          updateRoutingTrigger();
          if (state.current && state.current.id) {
            persistConversationSettings({ providerSort: "", providerOnly: "", allowFallbacks: true });
          }
        }
      }

      if (patch.personaPrompt !== undefined && state.current && !state.current.id) {
        if (!prompt || prompt === oldPersonaPrompt) {
          state.current.systemPrompt = state.profile.personaPrompt || "";
        }
      }

      updateHeading();
      renderMessages();
    }

    setPref(PREFS.theme, theme);
    applyTheme(theme);
    dom.settings.close();
    toast("Settings saved");
  }

  /* ------------------------------------------------------------------- copy */

  async function copyText(text, options = {}) {
    let ok = false;

    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        ok = true;
      }
    } catch {
      /* fall through to the textarea trick */
    }

    if (!ok) {
      const shim = el("textarea", "copy-shim");
      shim.value = text;
      shim.setAttribute("readonly", "");
      shim.setAttribute("aria-hidden", "true");
      document.body.append(shim);
      shim.select();
      try {
        ok = document.execCommand("copy");
      } catch {
        ok = false;
      }
      shim.remove();
    }

    if (!ok) {
      toast("Could not copy");
      return;
    }

    if (options.label) {
      flashLabel(options.label, "Copied");
    } else if (options.toastOnSuccess) {
      toast("Copied");
    }
  }

  function flashLabel(node, text) {
    if (!node) return;
    if (node.dataset.flashing === "1") return;
    node.dataset.flashing = "1";
    const previous = node.textContent;
    node.textContent = text;
    setTimeout(() => {
      node.textContent = previous;
      delete node.dataset.flashing;
    }, 1200);
  }

  /* ---------------------------------------------------------------- markdown */

  const INLINE_RE = /(`+)([\s\S]*?)\1|\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)|(\*\*)([\s\S]+?)\5|(\*)([\s\S]+?)\7|(~~)([\s\S]+?)\9|(https?:\/\/[^\s<>()]+)/g;

  function renderMarkdown(source) {
    const fragment = document.createDocumentFragment();
    const lines = String(source == null ? "" : source).replace(/\r\n?/g, "\n").split("\n");
    let i = 0;
    let codeIndex = 0;

    const isTableStart = (index) => {
      if (index + 1 >= lines.length) return false;
      if (!lines[index].includes("|")) return false;
      return /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[index + 1]) && lines[index + 1].includes("-");
    };

    const isBlockStart = (index) => {
      const line = lines[index];
      return /^ {0,3}(#{1,6})\s+/.test(line) ||
        /^ {0,3}(`{3,}|~{3,})/.test(line) ||
        /^ {0,3}>/.test(line) ||
        /^ {0,3}([-*+]|\d+[.)])\s+/.test(line) ||
        /^ {0,3}([-*_])(?:\s*\1){2,}\s*$/.test(line) ||
        isTableStart(index);
    };

    while (i < lines.length) {
      const line = lines[i];
      if (!line.trim()) {
        i += 1;
        continue;
      }

      const fence = line.match(/^ {0,3}(`{3,}|~{3,})\s*(.*?)\s*$/);
      if (fence) {
        const marker = fence[1][0];
        const minimum = fence[1].length;
        const info = (fence[2] || "").trim();
        const body = [];
        i += 1;

        while (i < lines.length) {
          const closing = lines[i].match(/^ {0,3}(`{3,}|~{3,})\s*$/);
          if (closing && closing[1][0] === marker && closing[1].length >= minimum) {
            i += 1;
            break;
          }
          body.push(lines[i]);
          i += 1;
        }

        codeIndex += 1;
        fragment.append(codeBlock(body.join("\n"), info, codeIndex));
        continue;
      }

      const heading = line.match(/^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/);
      if (heading) {
        const node = el("h" + heading[1].length);
        inlineInto(node, heading[2]);
        fragment.append(node);
        i += 1;
        continue;
      }

      if (/^ {0,3}([-*_])(?:\s*\1){2,}\s*$/.test(line)) {
        fragment.append(el("hr"));
        i += 1;
        continue;
      }

      if (/^ {0,3}>/.test(line)) {
        const quoted = [];
        while (i < lines.length && /^ {0,3}>/.test(lines[i])) {
          quoted.push(lines[i].replace(/^ {0,3}>\s?/, ""));
          i += 1;
        }
        const quote = el("blockquote");
        quote.append(renderMarkdown(quoted.join("\n")));
        fragment.append(quote);
        continue;
      }

      if (isTableStart(i)) {
        const headers = splitRow(lines[i]);
        i += 2;

        const table = el("table");
        const head = el("thead");
        const headRow = el("tr");
        for (const cell of headers) {
          const th = el("th");
          inlineInto(th, cell);
          headRow.append(th);
        }
        head.append(headRow);
        table.append(head);

        const body = el("tbody");
        while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
          const cells = splitRow(lines[i]);
          const row = el("tr");
          for (let c = 0; c < headers.length; c += 1) {
            const td = el("td");
            inlineInto(td, cells[c] || "");
            row.append(td);
          }
          body.append(row);
          i += 1;
        }
        if (body.childNodes.length) table.append(body);
        fragment.append(table);
        continue;
      }

      const listMarker = line.match(/^ {0,3}([-*+]|\d+[.)])\s+/);
      if (listMarker) {
        const ordered = /\d/.test(listMarker[1]);
        const list = el(ordered ? "ol" : "ul");

        while (i < lines.length) {
          const item = lines[i].match(/^ {0,3}([-*+]|\d+[.)])\s+(.*)$/);
          if (!item) break;
          if (/\d/.test(item[1]) !== ordered) break;
          const li = el("li");
          inlineInto(li, item[2]);
          list.append(li);
          i += 1;
        }

        fragment.append(list);
        continue;
      }

      const paragraph = [];
      while (i < lines.length && lines[i].trim() !== "") {
        if (paragraph.length && isBlockStart(i)) break;
        paragraph.push(lines[i]);
        i += 1;
      }
      const p = el("p");
      inlineInto(p, paragraph.join("\n"));
      fragment.append(p);
    }

    return fragment;
  }

  function inlineInto(root, text, depth = 0) {
    if (depth > 4) {
      root.append(document.createTextNode(text));
      return;
    }

    const source = String(text == null ? "" : text);
    let last = 0;
    INLINE_RE.lastIndex = 0;

    for (const match of source.matchAll(INLINE_RE)) {
      if (match.index > last) {
        root.append(document.createTextNode(source.slice(last, match.index)));
      }

      if (match[2] !== undefined) {
        root.append(el("code", null, match[2]));
      } else if (match[4] !== undefined) {
        const href = safeHref(match[4]);
        if (href) {
          const link = el("a", null, "");
          link.href = href;
          link.target = "_blank";
          link.rel = "noopener noreferrer";
          inlineInto(link, match[3] || match[4], depth + 1);
          root.append(link);
        } else {
          root.append(document.createTextNode(match[3] || match[4]));
        }
      } else if (match[6] !== undefined) {
        const strong = el("strong");
        inlineInto(strong, match[6], depth + 1);
        root.append(strong);
      } else if (match[8] !== undefined) {
        const em = el("em");
        inlineInto(em, match[8], depth + 1);
        root.append(em);
      } else if (match[10] !== undefined) {
        const del = el("del");
        inlineInto(del, match[10], depth + 1);
        root.append(del);
      } else if (match[11] !== undefined) {
        let url = match[11];
        let trailing = "";
        const tail = url.match(/[.,;:!?)]+$/);
        if (tail) {
          trailing = tail[0];
          url = url.slice(0, -trailing.length);
        }
        const href = safeHref(url);
        if (href) {
          const link = el("a", null, url);
          link.href = href;
          link.target = "_blank";
          link.rel = "noopener noreferrer";
          root.append(link);
        } else {
          root.append(document.createTextNode(url));
        }
        if (trailing) root.append(document.createTextNode(trailing));
      }

      last = match.index + match[0].length;
    }

    if (last < source.length) {
      root.append(document.createTextNode(source.slice(last)));
    }
  }

  function safeHref(raw) {
    try {
      const url = new URL(raw, window.location.href);
      if (url.protocol === "http:" || url.protocol === "https:") return url.href;
    } catch {
      /* invalid URL */
    }
    return null;
  }

  function splitRow(line) {
    let row = line.trim();
    if (row.startsWith("|")) row = row.slice(1);
    if (row.endsWith("|")) row = row.slice(0, -1);
    return row.split("|").map((cell) => cell.trim());
  }

  const CODE_TYPES = {
    go: { ext: "go", label: "Go", mime: "text/x-go" },
    js: { ext: "js", label: "JavaScript", mime: "text/javascript" },
    javascript: { ext: "js", label: "JavaScript", mime: "text/javascript" },
    jsx: { ext: "jsx", label: "JSX", mime: "text/javascript" },
    ts: { ext: "ts", label: "TypeScript", mime: "text/typescript" },
    typescript: { ext: "ts", label: "TypeScript", mime: "text/typescript" },
    tsx: { ext: "tsx", label: "TSX", mime: "text/typescript" },
    py: { ext: "py", label: "Python", mime: "text/x-python" },
    python: { ext: "py", label: "Python", mime: "text/x-python" },
    rb: { ext: "rb", label: "Ruby", mime: "text/x-ruby" },
    ruby: { ext: "rb", label: "Ruby", mime: "text/x-ruby" },
    rs: { ext: "rs", label: "Rust", mime: "text/x-rust" },
    rust: { ext: "rs", label: "Rust", mime: "text/x-rust" },
    java: { ext: "java", label: "Java", mime: "text/x-java" },
    kt: { ext: "kt", label: "Kotlin", mime: "text/x-kotlin" },
    kotlin: { ext: "kt", label: "Kotlin", mime: "text/x-kotlin" },
    swift: { ext: "swift", label: "Swift", mime: "text/x-swift" },
    c: { ext: "c", label: "C", mime: "text/x-c" },
    h: { ext: "h", label: "C header", mime: "text/x-c" },
    cpp: { ext: "cpp", label: "C++", mime: "text/x-c++src" },
    "c++": { ext: "cpp", label: "C++", mime: "text/x-c++src" },
    cs: { ext: "cs", label: "C#", mime: "text/x-csharp" },
    "c#": { ext: "cs", label: "C#", mime: "text/x-csharp" },
    php: { ext: "php", label: "PHP", mime: "text/x-php" },
    sh: { ext: "sh", label: "Shell", mime: "application/x-sh" },
    bash: { ext: "sh", label: "Shell", mime: "application/x-sh" },
    shell: { ext: "sh", label: "Shell", mime: "application/x-sh" },
    zsh: { ext: "sh", label: "Shell", mime: "application/x-sh" },
    powershell: { ext: "ps1", label: "PowerShell", mime: "text/x-powershell" },
    ps1: { ext: "ps1", label: "PowerShell", mime: "text/x-powershell" },
    sql: { ext: "sql", label: "SQL", mime: "application/sql" },
    html: { ext: "html", label: "HTML", mime: "text/html" },
    css: { ext: "css", label: "CSS", mime: "text/css" },
    scss: { ext: "scss", label: "SCSS", mime: "text/x-scss" },
    json: { ext: "json", label: "JSON", mime: "application/json" },
    yaml: { ext: "yaml", label: "YAML", mime: "application/yaml" },
    yml: { ext: "yaml", label: "YAML", mime: "application/yaml" },
    toml: { ext: "toml", label: "TOML", mime: "application/toml" },
    xml: { ext: "xml", label: "XML", mime: "application/xml" },
    md: { ext: "md", label: "Markdown", mime: "text/markdown" },
    markdown: { ext: "md", label: "Markdown", mime: "text/markdown" },
    txt: { ext: "txt", label: "Text", mime: "text/plain" },
    text: { ext: "txt", label: "Text", mime: "text/plain" },
    csv: { ext: "csv", label: "CSV", mime: "text/csv" },
    tsv: { ext: "tsv", label: "TSV", mime: "text/tab-separated-values" },
    diff: { ext: "diff", label: "Diff", mime: "text/x-diff" },
    patch: { ext: "patch", label: "Patch", mime: "text/x-diff" },
    dockerfile: { ext: "dockerfile", label: "Dockerfile", mime: "text/plain" },
    makefile: { ext: "makefile", label: "Makefile", mime: "text/plain" },
    graphql: { ext: "graphql", label: "GraphQL", mime: "application/graphql" },
    gql: { ext: "graphql", label: "GraphQL", mime: "application/graphql" },
    proto: { ext: "proto", label: "Protocol Buffers", mime: "text/plain" },
    lua: { ext: "lua", label: "Lua", mime: "text/x-lua" },
    r: { ext: "r", label: "R", mime: "text/x-r" },
    scala: { ext: "scala", label: "Scala", mime: "text/x-scala" },
    dart: { ext: "dart", label: "Dart", mime: "application/dart" },
    vue: { ext: "vue", label: "Vue", mime: "text/plain" },
    svelte: { ext: "svelte", label: "Svelte", mime: "text/plain" },
    tex: { ext: "tex", label: "LaTeX", mime: "text/x-tex" },
    log: { ext: "log", label: "Log", mime: "text/plain" },
  };

  const CODE_TYPES_BY_EXT = {};
  for (const value of Object.values(CODE_TYPES)) {
    if (!CODE_TYPES_BY_EXT[value.ext]) CODE_TYPES_BY_EXT[value.ext] = value;
  }

  function parseCodeInfo(info, index) {
    const raw = String(info || "").trim();
    let filename = "";

    const named = raw.match(/(?:filename|file|title|name)\s*=\s*["']?([^"'\s]+)["']?/i);
    if (named) filename = named[1];

    const tokens = raw.split(/\s+/).filter(Boolean);
    if (!filename) {
      for (const token of tokens) {
        const candidate = token.replace(/^["'{]+|["'},]+$/g, "");
        if (/^[\w.-]+\.[A-Za-z0-9]{1,8}$/.test(candidate)) {
          filename = candidate;
          break;
        }
      }
    }

    let languageToken = tokens[0] ? tokens[0].toLowerCase() : "";
    if (languageToken.includes("=") || languageToken.includes(".")) languageToken = "";
    languageToken = languageToken.replace(/^language-/, "");
    const type = CODE_TYPES[languageToken] || null;
    if (!filename) filename = "snippet-" + index + "." + (type ? type.ext : "txt");

    const ext = (filename.split(".").pop() || "").toLowerCase();
    const extType = CODE_TYPES_BY_EXT[ext] || null;
    const describe = type || extType;
    const language = describe ? describe.label : (languageToken || "text");
    const mime = describe ? describe.mime : "text/plain";
    return { name: filename, language, mime };
  }

  function codeAction(symbol, label, title) {
    const button = el("button", "code-action");
    button.type = "button";
    button.title = title || label;
    button.setAttribute("aria-label", title || label);
    button.append(icon(symbol), el("span", null, label));
    return button;
  }

  function codeBlock(code, info, index) {
    const file = { ...parseCodeInfo(info, index), code };
    const wrapper = el("div", "code-block");

    const head = el("div", "code-head");
    const identity = el("div", "file-identity");
    identity.append(icon("i-file", "icon file-icon"));
    const text = el("div", "file-text");
    text.append(el("span", "file-name", file.name));
    const lines = code ? code.split("\n").length : 0;
    text.append(el("span", "file-meta",
      [file.language, lines + (lines === 1 ? " line" : " lines"), formatBytes(new Blob([code]).size)].join(" · ")));
    identity.append(text);
    head.append(identity);

    const actions = el("div", "code-actions");
    const view = codeAction("i-eye", "View", "View raw text");
    view.addEventListener("click", () => openFileViewer(file));
    const download = codeAction("i-download", "Download", "Download " + file.name);
    download.addEventListener("click", () => {
      downloadText(file.name, code, file.mime);
      toast("File downloaded.");
    });
    const copy = codeAction("i-copy", "Copy", "Copy code");
    copy.addEventListener("click", () => copyText(code, { toastOnSuccess: true }));
    actions.append(view, download, copy);
    head.append(actions);

    const pre = el("pre");
    pre.append(el("code", null, code));
    wrapper.append(head, pre);
    return wrapper;
  }

  /* ----------------------------------------------------------------- events */

  function bindEvents() {
    dom.newChat.addEventListener("click", () => newChat());

    dom.menu.addEventListener("click", () => {
      if (dom.app.classList.contains("sidebar-open")) closeSidebar();
      else openSidebar();
    });
    dom.scrim.addEventListener("click", closeSidebar);

    dom.composer.addEventListener("submit", (event) => {
      event.preventDefault();
      sendMessage(dom.input.value);
    });

    dom.input.addEventListener("input", autoGrow);
    dom.input.addEventListener("keydown", (event) => {
      const finePointer = window.matchMedia("(pointer: fine)").matches;
      if (event.key === "Enter" && !event.shiftKey && !event.isComposing && finePointer) {
        event.preventDefault();
        sendMessage(dom.input.value);
      }
    });

    dom.stop.addEventListener("click", stopStreaming);

    dom.reasoningTrigger.addEventListener("click", (event) => {
      event.stopPropagation();
      toggleReasoningMenu();
    });
    dom.reasoningDropup.addEventListener("click", (event) => event.stopPropagation());
    document.addEventListener("click", closeReasoningMenu);
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeReasoningMenu();
    });

    dom.toolsToggle.addEventListener("click", () => {
      if (state.streaming) return;
      setToolSettings(!state.toolsEnabled, state.maxToolCalls);
    });
    dom.toolCap.addEventListener("input", () => {
      state.maxToolCalls = clampToolCalls(dom.toolCap.value);
      setPref(pkey(PREFS.maxToolCalls), String(state.maxToolCalls));
    });
    dom.toolCap.addEventListener("change", () => {
      if (state.streaming) {
        renderToolBar();
        return;
      }
      const calls = clampToolCalls(dom.toolCap.value);
      setToolSettings(state.toolsEnabled, calls);
    });

    dom.attachFile.addEventListener("click", () => dom.fileInput.click());
    dom.fileInput.addEventListener("change", () => addFiles(dom.fileInput.files));

    dom.openPrompts.addEventListener("click", openPrompts);
    dom.promptSearch.addEventListener("input", renderPromptList);
    dom.promptNew.addEventListener("click", beginNewPrompt);
    dom.promptCancel.addEventListener("click", cancelPromptForm);
    dom.promptForm.addEventListener("submit", savePrompt);

    dom.exportChat.addEventListener("click", openExportDialog);
    dom.importChat.addEventListener("click", () => dom.importFile.click());
    dom.importFile.addEventListener("change", () => importConversationFile(dom.importFile.files[0]));
    dom.exportDialog.addEventListener("click", (event) => {
      const option = event.target.closest("[data-export]");
      if (option) exportConversation(option.dataset.export);
    });

    dom.fileViewerDownload.addEventListener("click", downloadViewerFile);
    dom.fileViewerPrint.addEventListener("click", printViewerFile);

    dom.modelPicker.addEventListener("click", () => openModelPicker("conversation"));
    dom.routingPicker.addEventListener("click", openRoutingDialog);
    dom.defaultModelButton.addEventListener("click", () => openModelPicker("default"));
    dom.defaultModelClear.addEventListener("click", () => setDefaultModel(""));
    dom.modelRoutingButton.addEventListener("click", () => {
      if (dom.modelDialog.open) dom.modelDialog.close();
      openRoutingDialog();
    });

    dom.modelSearch.addEventListener("input", renderModelList);
    dom.modelSearch.addEventListener("keydown", (event) => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        const first = dom.modelListEl.querySelector(".model-row");
        if (first) first.focus();
      }
    });
    dom.modelSort.addEventListener("change", () => {
      state.modelSort = dom.modelSort.value;
      renderModelList();
    });
    dom.modelFilters.addEventListener("click", (event) => {
      const button = event.target.closest("[data-filter]");
      if (!button) return;
      state.modelFilter = button.dataset.filter;
      setActiveFilter();
      renderModelList();
    });

    dom.routingOptions.addEventListener("click", (event) => {
      const button = event.target.closest("[data-mode]");
      if (button) selectRoutingMode(button.dataset.mode);
    });
    dom.endpointSort.addEventListener("change", () => {
      state.endpointSort = dom.endpointSort.value;
      renderEndpointList();
    });
    dom.allowFallbacks.addEventListener("change", () => {
      if (state.routingDraft) state.routingDraft.allowFallbacks = dom.allowFallbacks.checked;
    });
    dom.routingSave.addEventListener("click", saveRouting);

    dom.openSettings.addEventListener("click", () => {
      closeSidebar();
      openSettings();
    });
    dom.saveSettings.addEventListener("click", saveSettings);
    dom.settingsForm.addEventListener("submit", (event) => {
      event.preventDefault();
      saveSettings();
    });

    dom.theme.addEventListener("change", () => {
      const theme = THEMES.includes(dom.theme.value) ? dom.theme.value : "dark";
      applyTheme(theme);
      setPref(PREFS.theme, theme);
    });

    dom.settingsNav.addEventListener("click", (event) => {
      const tab = event.target.closest("[data-settings-tab]");
      if (tab) setSettingsTab(tab.dataset.settingsTab);
    });

    dom.systemPreset.addEventListener("change", () => applyPreset(dom.systemPreset, dom.systemPrompt));
    dom.personaPreset.addEventListener("change", () => applyPreset(dom.personaPreset, dom.personaPrompt));
    dom.systemPrompt.addEventListener("input", () => syncPresetSelect(dom.systemPreset, dom.systemPrompt.value));
    dom.personaPrompt.addEventListener("input", () => syncPresetSelect(dom.personaPreset, dom.personaPrompt.value));
    dom.systemPresetSave.addEventListener("click", () => openPresetSaver("system"));
    dom.personaPresetSave.addEventListener("click", () => openPresetSaver("persona"));

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && dom.app.classList.contains("sidebar-open")) closeSidebar();
    });

    dom.messages.addEventListener("scroll", () => {
      state.pinned = nearBottom();
    }, { passive: true });

    window.addEventListener("resize", () => scrollToBottom(false));
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", () => scrollToBottom(false));
    }

    window.addEventListener("online", () => toast("Back online"));
  }

  init().catch((err) => {
    console.error(err);
    toast("Failed to start: " + (err && err.message ? err.message : err));
  });
})();
