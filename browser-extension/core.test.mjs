import assert from "node:assert/strict";
import test from "node:test";

import {
  MAX_SELECTION_CODE_POINTS,
  normalizeSelection,
  parseAnalysisEnvelope
} from "./core.mjs";

test("normalizeSelection trims text and rejects empty selections", () => {
  assert.deepEqual(normalizeSelection("  suspicious text  "), { ok: true, text: "suspicious text" });
  assert.equal(normalizeSelection(" \n\t ").error.code, "empty_selection");
  assert.equal(normalizeSelection(undefined).error.code, "empty_selection");
});

test("normalizeSelection counts Unicode code points and accepts the exact limit", () => {
  const text = "😀".repeat(MAX_SELECTION_CODE_POINTS);
  const result = normalizeSelection(text);
  assert.equal(result.ok, true);
  assert.equal(Array.from(result.text).length, MAX_SELECTION_CODE_POINTS);
});

test("normalizeSelection rejects over-limit text without truncating it", () => {
  const text = "😀".repeat(MAX_SELECTION_CODE_POINTS + 1);
  const result = normalizeSelection(text);
  assert.equal(result.ok, false);
  assert.equal(result.error.code, "selection_too_long");
  assert.equal("text" in result, false);
});

test("parseAnalysisEnvelope accepts an explainable success payload", () => {
  const result = parseAnalysisEnvelope({
    success: true,
    data: {
      risk_score: 45,
      risk_level: "medium",
      matched_rules: [{
        rule_code: "rule_1",
        rule_name: "Example rule",
        category_code: "fake_customer_service",
        weight: 45,
        severity: "high",
        evidence: "安全账户",
        explanation: "Synthetic explanation",
        recommendation: "Verify independently"
      }],
      summary: "One rule matched.",
      recommendations: ["Verify independently"]
    }
  });
  assert.equal(result.ok, true);
  assert.equal(result.result.risk_score, 45);
  assert.equal(result.result.matched_rules[0].evidence, "安全账户");
});

test("parseAnalysisEnvelope normalizes declared API failures", () => {
  const result = parseAnalysisEnvelope({
    success: false,
    error: { code: "invalid_analysis_request", message: "text is required" }
  });
  assert.equal(result.ok, false);
  assert.equal(result.error.code, "api_error");
  assert.equal(result.error.api_code, "invalid_analysis_request");
  assert.match(result.error.message, /text is required/);
});

test("parseAnalysisEnvelope rejects malformed or incomplete success payloads", () => {
  for (const payload of [
    null,
    [],
    {},
    { success: true },
    { success: true, data: { risk_score: 1, risk_level: "low", matched_rules: [], summary: "", recommendations: "bad" } },
    { success: true, data: { risk_score: 1, risk_level: "low", matched_rules: [{}], summary: "", recommendations: [] } },
    { success: false, error: { code: 12, message: "bad" } }
  ]) {
    const result = parseAnalysisEnvelope(payload);
    assert.equal(result.ok, false);
    assert.equal(result.error.code, "invalid_response");
  }
});
