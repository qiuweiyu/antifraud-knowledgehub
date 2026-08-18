# Browser Extension Prototype

This directory contains the first privacy-preserving Chromium/Google Chrome Manifest V3 prototype for AntiFraud-KnowledgeHub.

## What it does

The extension analyzes text only after an explicit user action:

1. select suspicious text on a web page;
2. right-click the selection;
3. choose **Check fraud risk with AntiFraud-KnowledgeHub**;
4. open the extension popup to inspect the explainable result.

The popup shows the risk score/level, summary, matched rules, evidence, explanations, and recommendations returned by the local rule engine.

## Privacy and permission boundary

The prototype intentionally requests only:

- `contextMenus`;
- `storage`;
- host access to `http://127.0.0.1:8080/*`.

It does not inject content scripts, request `<all_urls>`, read page URLs/history, use cookies, or automatically scan pages.

Selected text and the latest result are stored only in `chrome.storage.session` on the extension side. The extension calls `POST /api/v1/analysis/preview`, which is designed to create **zero** `AnalysisRecord` history rows. The existing `POST /api/v1/analysis/text` endpoint remains separate and continues to persist its normal analysis history when used elsewhere.

This is a local prototype, not an end-to-end encryption or authenticated-localhost claim. A compromised local machine is outside the extension's trust boundary.

## Run locally

Start AntiFraud-KnowledgeHub so its HTTP API listens on the documented port `8080`. The extension uses the fixed endpoint:

```text
http://127.0.0.1:8080/api/v1/analysis/preview
```

Then in Chrome/Chromium:

1. open `chrome://extensions`;
2. enable **Developer mode**;
3. choose **Load unpacked**;
4. select this `browser-extension/` directory;
5. select suspicious text on a page and use the extension's context-menu action.

## Selection boundary

The prototype trims leading/trailing whitespace and rejects:

- empty selections;
- selections longer than 4,000 Unicode code points.

Overlong text is rejected rather than silently truncated because truncation can change the meaning of a fraud-risk analysis.

## Tests

No package installation is required for extension tests. With Node.js 22:

```bash
node --test browser-extension/*.test.mjs
```

The repository CI runs the same command in a dedicated `extension` job.

## Security note

Dynamic selected text and API-provided strings are treated as untrusted. The popup builds DOM nodes and writes dynamic values through text APIs rather than injecting HTML.

Risk results are indicators produced by the enabled explainable rule set. They do not guarantee that a message is or is not fraudulent.

See `docs/browser-extension-prototype-design.md` for the frozen implementation contract and deferred future work.
