export const MAX_SELECTION_CODE_POINTS = 4000;
export const LATEST_STATE_KEY = "latestAnalysis";

const ERROR_MESSAGES = Object.freeze({
  empty_selection: "Select suspicious text before running the risk check.",
  selection_too_long: `Selected text exceeds ${MAX_SELECTION_CODE_POINTS} Unicode code points.`,
  backend_unreachable: "The local AntiFraud-KnowledgeHub API is unavailable at 127.0.0.1:8080.",
  invalid_response: "The local API returned an unexpected response.",
  api_error: "The local API rejected the analysis request."
});

export function normalizeSelection(value) {
  const text = typeof value === "string" ? value.trim() : "";
  if (!text) {
    return failure("empty_selection");
  }
  if (Array.from(text).length > MAX_SELECTION_CODE_POINTS) {
    return failure("selection_too_long");
  }
  return { ok: true, text };
}

export function parseAnalysisEnvelope(payload) {
  if (!isObject(payload) || typeof payload.success !== "boolean") {
    return failure("invalid_response");
  }

  if (payload.success === false) {
    const apiError = payload.error;
    if (!isObject(apiError) || typeof apiError.code !== "string" || typeof apiError.message !== "string") {
      return failure("invalid_response");
    }
    const code = boundedText(apiError.code, 80);
    const message = boundedText(apiError.message, 500);
    if (!code || !message) {
      return failure("invalid_response");
    }
    return {
      ok: false,
      error: {
        code: "api_error",
        message: `${code}: ${message}`,
        api_code: code
      }
    };
  }

  const data = payload.data;
  if (!isObject(data) || !Number.isFinite(data.risk_score) || typeof data.risk_level !== "string" ||
      !Array.isArray(data.matched_rules) || typeof data.summary !== "string" ||
      !Array.isArray(data.recommendations) || !data.recommendations.every((item) => typeof item === "string")) {
    return failure("invalid_response");
  }

  const matchedRules = [];
  for (const item of data.matched_rules) {
    if (!isObject(item) || typeof item.rule_code !== "string" || typeof item.rule_name !== "string" ||
        typeof item.category_code !== "string" || !Number.isFinite(item.weight) ||
        typeof item.severity !== "string" || typeof item.evidence !== "string" ||
        typeof item.explanation !== "string" || typeof item.recommendation !== "string") {
      return failure("invalid_response");
    }
    matchedRules.push({
      rule_code: item.rule_code,
      rule_name: item.rule_name,
      category_code: item.category_code,
      weight: item.weight,
      severity: item.severity,
      evidence: item.evidence,
      explanation: item.explanation,
      recommendation: item.recommendation
    });
  }

  return {
    ok: true,
    result: {
      risk_score: data.risk_score,
      risk_level: data.risk_level,
      matched_rules: matchedRules,
      summary: data.summary,
      recommendations: [...data.recommendations]
    }
  };
}

export function clientErrorState(code, selectedText = "") {
  const message = ERROR_MESSAGES[code] ?? ERROR_MESSAGES.invalid_response;
  return {
    status: "error",
    selected_text: selectedText,
    error_code: code in ERROR_MESSAGES ? code : "invalid_response",
    error_message: message,
    updated_at: new Date().toISOString()
  };
}

export function backendErrorState(code, message, selectedText = "") {
  return {
    status: "error",
    selected_text: selectedText,
    error_code: code,
    error_message: boundedText(message, 600) || ERROR_MESSAGES.invalid_response,
    updated_at: new Date().toISOString()
  };
}

function failure(code) {
  return {
    ok: false,
    error: {
      code,
      message: ERROR_MESSAGES[code] ?? ERROR_MESSAGES.invalid_response
    }
  };
}

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function boundedText(value, maxLength) {
  return typeof value === "string" ? value.trim().slice(0, maxLength) : "";
}
