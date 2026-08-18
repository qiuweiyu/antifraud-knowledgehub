# Browser Extension Prototype Design

Status: **Frozen for the first implementation slice**

Parent issue: #3  
Design issue: #62

## 1. Goal

Define the smallest Chromium/Chrome Manifest V3 browser-extension prototype that lets a user explicitly select suspicious text, submit only that selected text to a local AntiFraud-KnowledgeHub API, and inspect the existing explainable rule result without broad page access or silent analysis-history persistence.

This document is the implementation contract for the first slice. Any change to permissions, persistence semantics, network destination, or client-visible API shape requires a new bounded review before coding.

## 2. Non-goals

The first slice does **not** provide:

- automatic page scanning;
- content-script injection;
- DOM scraping;
- page URL/title/history collection;
- remote/cloud analysis;
- browser-account identity;
- extension login or token storage;
- browser-store packaging/signing;
- Firefox/Safari packaging;
- background monitoring;
- notifications permission;
- rule submission/review/publication controls;
- LLM-assisted analysis;
- a general configurable remote API endpoint.

## 3. Existing behavior that must remain compatible

`POST /api/v1/analysis/text` currently:

1. validates `{ "text": "..." }`;
2. loads enabled `RiskRule` rows;
3. computes the explainable `riskengine.Result`;
4. persists an `AnalysisRecord`, including the original input text;
5. returns the result through the standard response envelope.

That persisted-history behavior is useful for the existing dashboard/API and **must not change** in this slice.

The browser extension must not call this persisted endpoint by default because arbitrary selected browser text can contain sensitive information and a toolbar prototype should not silently create analysis-history rows.

## 4. New backend contract: no-write preview analysis

Add:

```text
POST /api/v1/analysis/preview
Content-Type: application/json
```

Request:

```json
{
  "text": "selected suspicious text"
}
```

Success response remains the existing envelope and `riskengine.Result` shape:

```json
{
  "success": true,
  "data": {
    "risk_score": 80,
    "risk_level": "high",
    "matched_rules": [
      {
        "rule_code": "...",
        "rule_name": "...",
        "category_code": "...",
        "weight": 40,
        "severity": "high",
        "evidence": "...",
        "explanation": "...",
        "recommendation": "..."
      }
    ],
    "summary": "...",
    "recommendations": ["..."]
  }
}
```

Invalid request behavior remains aligned with the existing analysis endpoint:

```text
HTTP 400
error.code = invalid_analysis_request
```

### Required no-write invariant

A successful or rejected request to `/api/v1/analysis/preview` must create **zero** `AnalysisRecord` rows.

The endpoint may read enabled rules. It must not mutate rule, case, workflow, analysis-history, or audit tables.

### Shared analysis logic

Avoid duplicating risk-engine assembly. Extract a small shared analysis helper used by both handlers:

```text
analyze text -> load enabled rules -> riskengine.Analyze -> result
```

Then:

- `/analysis/preview` returns the result directly;
- `/analysis/text` calls the same helper and then performs the existing `AnalysisRecord` persistence.

No response-shape fork is introduced.

## 5. Extension target

First implementation target:

```text
Chromium / Google Chrome
Manifest V3
```

Cross-browser compatibility is future work.

## 6. Minimal permissions

`manifest.json` must request exactly the capabilities required by this prototype.

Required extension permissions:

```json
"permissions": [
  "contextMenus",
  "storage"
]
```

Required host permission:

```json
"host_permissions": [
  "http://127.0.0.1:8080/*"
]
```

The first slice must **not** request:

```text
<all_urls>
tabs
activeTab
scripting
webRequest
cookies
history
clipboardRead
clipboardWrite
notifications
unlimitedStorage
```

There must be no `content_scripts` entry.

### Why loopback is fixed

The extension connects only to the local development/runtime server at `127.0.0.1:8080`.

The first slice deliberately avoids a configurable arbitrary host because that would broaden host permissions and introduce additional trust/configuration semantics.

Users running the API with the documented `:8080` listener can reach it through loopback even if they normally type `localhost` in a browser.

## 7. Explicit-user-action data flow

The only page-text entry point is the browser context menu.

### Install

The extension service worker registers one menu item:

```text
Check fraud risk with AntiFraud-KnowledgeHub
```

Menu context:

```text
selection
```

### User action

1. User selects text on a page.
2. User right-clicks the selection.
3. User explicitly chooses the AntiFraud-KnowledgeHub menu item.
4. The service worker receives only the selected text from the context-menu event.
5. The extension validates the selection locally.
6. The service worker sends `{text}` to `http://127.0.0.1:8080/api/v1/analysis/preview`.
7. The service worker stores the latest transient state in `chrome.storage.session`.
8. The toolbar popup renders that transient state when the user opens it.

The extension must not automatically scan a page or submit text without the explicit context-menu action.

## 8. Selection boundary

Normalize selected text by trimming leading/trailing whitespace.

Reject:

- empty/whitespace-only text;
- text longer than `4000` Unicode code points.

Do not silently truncate overlong input because truncation can change fraud-risk meaning.

Client error identifiers:

```text
empty_selection
selection_too_long
```

The backend remains authoritative for request validity. The extension limit is an additional UX/resource bound, not a security authority.

## 9. Network behavior

The request is made from the extension service worker, not from a content script.

Use `fetch()` with:

```text
method: POST
Content-Type: application/json
credentials: omit
cache: no-store
```

Destination is fixed:

```text
http://127.0.0.1:8080/api/v1/analysis/preview
```

No cookies, authorization headers, page origin, page URL, tab title, or browser history are sent.

### Local HTTP boundary

Plain HTTP is accepted only because the destination is fixed loopback for this local prototype.

This design must not be generalized to remote plain-HTTP hosts. A future remote deployment requires a separate HTTPS/authentication design.

## 10. Transient extension state

Use `chrome.storage.session` only.

Stored state may contain:

```text
status: idle | loading | success | error
selected_text
result
error_code
error_message
updated_at
```

The state is the latest explicit analysis only.

Do not use `storage.local` or `storage.sync` for selected text or analysis results.

The extension does not claim that the backend has no persistence globally. The guarantee is narrower:

- extension transient state is session-only;
- the extension uses the new no-write preview endpoint;
- the existing persisted `/analysis/text` endpoint continues to store its own history when explicitly used elsewhere.

## 11. Popup contract

The toolbar popup is read-only for the first slice.

States:

### Idle

Explain how to use the extension:

```text
Select suspicious text on a page, right-click it, and choose
“Check fraud risk with AntiFraud-KnowledgeHub”.
```

### Loading

Show that the selected text is being analyzed locally.

### Success

Show:

- selected text (visually bounded/truncated for display only; the submitted text was not silently truncated);
- risk score;
- risk level;
- summary;
- matched rules;
- each matched rule's evidence, explanation, and recommendation;
- top-level recommendations.

The UI must retain explainability. A bare score without matched-rule evidence is insufficient.

### Error

Show a short deterministic message for:

```text
empty_selection
selection_too_long
backend_unreachable
api_error
invalid_response
```

Do not display raw stack traces.

## 12. Untrusted rendering

Selected page text, API error messages, rule names, evidence, explanations, and recommendations are all untrusted strings.

The popup must render dynamic strings using DOM text APIs such as `textContent`.

Do not use dynamic `innerHTML`, `insertAdjacentHTML`, `document.write`, remote templates, or remotely hosted scripts.

No CDN or remote code is allowed in Manifest V3 package assets.

## 13. Response validation

The service worker must not assume every HTTP 2xx body is valid.

Validate at least:

- top-level object;
- `success === true` for successful result;
- `data` is an object;
- `risk_score` is a finite number;
- `risk_level` is a string;
- `matched_rules` is an array;
- `summary` is a string;
- `recommendations` is an array.

For API-declared failures (`success === false`), preserve a bounded error code/message for display.

Malformed/non-JSON/unexpected responses map to:

```text
invalid_response
```

Connection failures map to:

```text
backend_unreachable
```

## 14. Toolbar badge

The service worker may use the action badge without adding a manifest permission.

Recommended states:

```text
loading -> "…"
success -> numeric risk score when short enough, otherwise "OK"
error   -> "!"
```

The badge is only a navigation cue. It must not replace the explainable popup result.

## 15. Proposed package layout

```text
browser-extension/
  manifest.json
  service-worker.mjs
  core.mjs
  popup.html
  popup.mjs
  popup.css
  README.md
  core.test.mjs
  manifest.test.mjs
```

No package manager dependency is required for the first slice.

## 16. Test contract

### Backend tests

Add focused Go tests proving:

1. valid `/analysis/preview` returns the normal explainable result envelope;
2. valid preview creates zero `AnalysisRecord` rows;
3. invalid preview creates zero `AnalysisRecord` rows;
4. existing `/analysis/text` still creates its expected history record;
5. preview and persisted analysis use the same core result logic for the same rule set/input.

### Extension tests

Use Node's built-in test runner only.

`core.test.mjs` covers:

- trim/empty selection;
- Unicode-code-point length boundary;
- over-limit rejection without truncation;
- success envelope validation;
- API failure normalization;
- malformed response normalization.

`manifest.test.mjs` parses `manifest.json` and freezes:

- Manifest V3;
- exact permissions `contextMenus`, `storage`;
- exact loopback host permission;
- module service worker;
- no content scripts;
- no forbidden broad permissions.

### CI

Add a separate lightweight `extension` job using Node 22:

```text
node --test browser-extension/*.test.mjs
```

The existing backend and frontend jobs remain unchanged and must still pass.

## 17. Acceptance matrix

| Scenario | Expected result | Backend history write |
|---|---|---:|
| No selection / whitespace | local error | 0 |
| >4000 code points | local error | 0 |
| Backend unavailable | `backend_unreachable` | 0 |
| Backend returns malformed payload | `invalid_response` | 0 |
| Valid low-risk text | explainable result | 0 |
| Valid high-risk text | score + level + matched evidence + recommendations | 0 |
| Existing `/analysis/text` request | existing behavior | 1 |

## 18. Security and privacy claims

The implementation may claim only the following:

- analysis starts from an explicit user context-menu action;
- the extension does not inject a content script in this slice;
- the extension does not request broad page host access;
- the extension sends only selected text to the fixed loopback API;
- the extension uses session-only browser storage for the latest result;
- the preview endpoint does not create `AnalysisRecord` history rows;
- the result remains deterministic/explainable according to the enabled rule engine.

The implementation must **not** claim:

- end-to-end encryption;
- cryptographic privacy;
- that all backend activity is non-persistent;
- that the local API is authenticated;
- that the extension protects a compromised local machine;
- that rule-based analysis guarantees a message is or is not fraud.

## 19. Future work deliberately deferred

- user-configurable API endpoint;
- HTTPS remote deployment;
- optional host permissions;
- Chrome Web Store packaging;
- Firefox/Safari compatibility;
- popup manual-input mode;
- page overlays;
- automatic scanning;
- notification UX;
- richer extension settings;
- opt-in persisted analysis history;
- LLM-assisted analysis.

## 20. Primary references

Chrome extension behavior used by this design is based on the official Chrome extension documentation for:

- Manifest V3 manifests;
- declaring minimal permissions;
- `chrome.contextMenus`;
- cross-origin requests from extension service workers with `host_permissions`;
- `chrome.storage.session`.

The implementation must continue to prefer official browser documentation when browser-platform behavior changes.
