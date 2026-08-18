import { LATEST_STATE_KEY } from "./core.mjs";

const content = document.getElementById("content");

void loadState();
chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName === "session" && changes[LATEST_STATE_KEY]) {
    renderState(changes[LATEST_STATE_KEY].newValue);
  }
});

async function loadState() {
  const stored = await chrome.storage.session.get(LATEST_STATE_KEY);
  renderState(stored[LATEST_STATE_KEY]);
}

function renderState(state) {
  content.replaceChildren();

  if (!state || state.status === "idle") {
    appendTitle("Select suspicious text");
    appendParagraph(
      "Select text on a page, right-click it, and choose “Check fraud risk with AntiFraud-KnowledgeHub”.",
      "muted"
    );
    return;
  }

  if (state.status === "loading") {
    appendTitle("Analyzing locally…");
    appendSelectedText(state.selected_text);
    return;
  }

  if (state.status === "error") {
    appendTitle("Risk check unavailable");
    if (state.selected_text) {
      appendSelectedText(state.selected_text);
    }
    const box = element("div", "error");
    box.append(element("strong", "", state.error_code || "error"));
    box.append(element("p", "", state.error_message || "The risk check could not be completed."));
    content.append(box);
    return;
  }

  if (state.status === "success" && state.result) {
    renderSuccess(state);
    return;
  }

  appendTitle("Unexpected state");
  appendParagraph("Reload the extension and run the risk check again.", "muted");
}

function renderSuccess(state) {
  appendTitle("Latest explicit risk check");
  appendSelectedText(state.selected_text);

  const result = state.result;
  const scoreRow = element("div", "score-row");
  scoreRow.append(element("span", "score", String(result.risk_score)));
  scoreRow.append(element("span", "level", result.risk_level));
  content.append(scoreRow);

  appendParagraph(result.summary, "summary");

  const rulesTitle = element("h2", "state-title", "Matched rules");
  content.append(rulesTitle);
  if (result.matched_rules.length === 0) {
    appendParagraph("No enabled rule matched the selected text.", "muted");
  } else {
    for (const rule of result.matched_rules) {
      content.append(renderRule(rule));
    }
  }

  const recTitle = element("h2", "state-title", "Recommendations");
  content.append(recTitle);
  if (result.recommendations.length === 0) {
    appendParagraph("No additional recommendation was returned.", "muted");
  } else {
    const list = element("ul", "recommendations");
    for (const recommendation of result.recommendations) {
      list.append(element("li", "", recommendation));
    }
    content.append(list);
  }
}

function renderRule(rule) {
  const card = element("article", "rule-card");
  card.append(element("h3", "", `${rule.rule_name} (${rule.severity})`));
  card.append(element("p", "", `Rule: ${rule.rule_code} · Category: ${rule.category_code} · Weight: ${rule.weight}`));
  card.append(labelledParagraph("Evidence", rule.evidence));
  card.append(labelledParagraph("Why it matters", rule.explanation));
  card.append(labelledParagraph("Recommendation", rule.recommendation));
  return card;
}

function labelledParagraph(label, value) {
  const paragraph = element("p");
  paragraph.append(element("strong", "", `${label}: `));
  paragraph.append(document.createTextNode(value));
  return paragraph;
}

function appendSelectedText(text) {
  if (!text) {
    return;
  }
  content.append(element("pre", "selected-text", text));
}

function appendTitle(text) {
  content.append(element("h2", "state-title", text));
}

function appendParagraph(text, className = "") {
  content.append(element("p", className, text));
}

function element(tagName, className = "", text = "") {
  const node = document.createElement(tagName);
  if (className) {
    node.className = className;
  }
  if (text !== "") {
    node.textContent = text;
  }
  return node;
}
