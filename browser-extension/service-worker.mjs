import {
  LATEST_STATE_KEY,
  backendErrorState,
  clientErrorState,
  normalizeSelection,
  parseAnalysisEnvelope
} from "./core.mjs";

const MENU_ID = "antifraud-check-selection";
const API_URL = "http://127.0.0.1:8080/api/v1/analysis/preview";

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({
      id: MENU_ID,
      title: "Check fraud risk with AntiFraud-KnowledgeHub",
      contexts: ["selection"]
    });
  });
});

chrome.contextMenus.onClicked.addListener((info) => {
  if (info.menuItemId !== MENU_ID) {
    return;
  }
  void analyzeSelection(info.selectionText);
});

async function analyzeSelection(rawSelection) {
  const normalized = normalizeSelection(rawSelection);
  if (!normalized.ok) {
    await storeState(clientErrorState(normalized.error.code));
    await setBadge("!");
    return;
  }

  const selectedText = normalized.text;
  await storeState({
    status: "loading",
    selected_text: selectedText,
    updated_at: new Date().toISOString()
  });
  await setBadge("…");

  let response;
  try {
    response = await fetch(API_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: selectedText }),
      credentials: "omit",
      cache: "no-store"
    });
  } catch {
    await storeState(clientErrorState("backend_unreachable", selectedText));
    await setBadge("!");
    return;
  }

  let payload;
  try {
    payload = await response.json();
  } catch {
    await storeState(clientErrorState("invalid_response", selectedText));
    await setBadge("!");
    return;
  }

  const parsed = parseAnalysisEnvelope(payload);
  if (!parsed.ok) {
    await storeState(
      parsed.error.code === "api_error"
        ? backendErrorState("api_error", parsed.error.message, selectedText)
        : clientErrorState("invalid_response", selectedText)
    );
    await setBadge("!");
    return;
  }

  if (!response.ok) {
    await storeState(clientErrorState("invalid_response", selectedText));
    await setBadge("!");
    return;
  }

  await storeState({
    status: "success",
    selected_text: selectedText,
    result: parsed.result,
    updated_at: new Date().toISOString()
  });
  await setBadge(formatScoreBadge(parsed.result.risk_score));
}

async function storeState(state) {
  await chrome.storage.session.set({ [LATEST_STATE_KEY]: state });
}

async function setBadge(text) {
  await chrome.action.setBadgeText({ text });
}

function formatScoreBadge(score) {
  const rounded = Math.max(0, Math.min(100, Math.round(score)));
  return String(rounded).length <= 3 ? String(rounded) : "OK";
}
